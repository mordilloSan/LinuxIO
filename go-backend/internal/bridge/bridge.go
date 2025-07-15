package bridge

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go-backend/cmd/bridge/handlers"
	"go-backend/cmd/bridge/handlers/types"
	"go-backend/internal/logger"
	"go-backend/internal/session"
	"io"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

var bridgeBinary = getBridgeBinaryPath()

var (
	processes   = make(map[string]*types.BridgeProcess)
	processesMu sync.Mutex
)

// BridgeSocketPath returns the per-session bridge command socket path for the user.
func BridgeSocketPath(sess *session.Session) (string, error) {
	u, err := user.Lookup(sess.User.ID)
	if err != nil {
		logger.Errorf("could not find user %s: %v", sess.User.ID, err)
		return "", err
	}
	return fmt.Sprintf("/run/user/%s/linuxio-bridge-%s.sock", u.Uid, sess.SessionID), nil
}

// Use everywhere for bridge actions: returns *raw* JSON response string (for HTTP handler to decode output as needed)
func CallWithSession(sess *session.Session, reqType, command string, args []string) ([]byte, error) {
	socketPath, err := BridgeSocketPath(sess)
	if err != nil {
		return nil, fmt.Errorf("could not determine bridge socket path: %w", err)
	}
	return CallViaSocket(socketPath, reqType, command, args)
}

func CallViaSocket(socketPath, reqType, command string, args []string) ([]byte, error) {
	req := map[string]any{
		"type":    reqType,
		"command": command,
	}
	if args != nil {
		req["args"] = args
	}
	conn, err := net.DialTimeout("unix", socketPath, 2*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to bridge: %w", err)
	}
	defer func() {
		if cerr := conn.Close(); cerr != nil {
			logger.Warnf("failed to close connection: %v", cerr)
		}
	}()

	enc := json.NewEncoder(conn)
	dec := json.NewDecoder(conn)
	if err := enc.Encode(req); err != nil {
		return nil, fmt.Errorf("failed to send request to bridge: %w", err)
	}
	var resp types.BridgeResponse
	if err := dec.Decode(&resp); err != nil {
		return nil, fmt.Errorf("failed to decode response from bridge: %w", err)
	}
	b, err := json.Marshal(resp)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal bridge response: %w", err)
	}
	return b, nil
}

// StartBridge starts the bridge process for a given session.
func StartBridge(sess *session.Session, sudoPassword string) error {
	processesMu.Lock()
	defer processesMu.Unlock()

	if _, exists := processes[sess.SessionID]; exists {
		return errors.New("bridge already running for this session")
	}

	var cmd *exec.Cmd
	if sess.Privileged {
		cmd = exec.Command("sudo", "-S", "env",
			"LINUXIO_SESSION_ID="+sess.SessionID,
			"LINUXIO_SESSION_USER="+sess.User.ID,
			"LINUXIO_BRIDGE_SECRET="+sess.BridgeSecret,
			"GO_ENV="+os.Getenv("GO_ENV"),
			"VERBOSE="+os.Getenv("VERBOSE"),
			bridgeBinary,
		)
	} else {
		cmd = exec.Command(bridgeBinary)
		cmd.Env = append(os.Environ(),
			"LINUXIO_SESSION_ID="+sess.SessionID,
			"LINUXIO_SESSION_USER="+sess.User.ID,
			"LINUXIO_BRIDGE_SECRET="+sess.BridgeSecret,
			"GO_ENV="+os.Getenv("GO_ENV"),
			"VERBOSE="+os.Getenv("VERBOSE"),
		)
	}

	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = io.MultiWriter(os.Stdout, &stdoutBuf)
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderrBuf)

	if sess.Privileged && sudoPassword != "" {
		stdin, err := cmd.StdinPipe()
		if err != nil {
			logger.Errorf("Failed to get stdin pipe: %v", err)
			return err
		}

		// Convert password to a mutable byte slice
		pwBytes := []byte(sudoPassword + "\n")
		go func() {
			defer func() {
				if cerr := stdin.Close(); cerr != nil {
					logger.Warnf("failed to close stdin: %v", cerr)
				}
			}()
			if _, err := stdin.Write(pwBytes); err != nil {
				logger.Warnf("failed to write sudo password to stdin: %v", err)
			}

			// Wipe the password bytes after use
			for i := range pwBytes {
				pwBytes[i] = 0
			}
		}()
	}

	if err := cmd.Start(); err != nil {
		logger.Errorf("Failed to start bridge for session %s: %v", sess.SessionID, err)
		return err
	}

	logger.Infof("Started %sbridge for session %s (pid=%d)",
		func() string {
			if sess.Privileged {
				return "privileged "
			}
			return ""
		}(), sess.SessionID, cmd.Process.Pid)

	processes[sess.SessionID] = &types.BridgeProcess{
		Cmd:       cmd,
		SessionID: sess.SessionID,
		StartedAt: time.Now(),
	}

	// Process cleanup goroutine
	go func(sessID string, cmd *exec.Cmd) {
		err := cmd.Wait()
		processesMu.Lock()
		delete(processes, sessID)
		processesMu.Unlock()
		if err != nil {
			logger.Warnf("Bridge for session %s exited with error: %v", sessID, err)
		}
	}(sess.SessionID, cmd)

	return nil
}

// HandleMainRequest processes incoming bridge requests.
func HandleMainRequest(conn net.Conn, id string) {
	logger.Debugf("HANDLECONNECTION: [%s] called!", id)
	defer func() {
		if cerr := conn.Close(); cerr != nil {
			logger.Warnf("failed to close connection [%s]: %v", id, cerr)
		}
	}()

	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)

	var req types.BridgeRequest
	if err := decoder.Decode(&req); err != nil {
		if err == io.EOF {
			logger.Debugf("🔁 [%s] connection closed without data", id)
		} else {
			logger.Warnf("❌ [%s] invalid JSON from client: %v", id, err)
		}
		_ = encoder.Encode(types.BridgeResponse{Status: "error", Error: "invalid JSON"})
		return
	}

	// (1) DEFENSE-IN-DEPTH: Validate handler name for fallback
	if strings.ContainsAny(req.Type, "./\\") || strings.ContainsAny(req.Command, "./\\") {
		logger.Warnf("❌ [%s] Invalid characters in type/command: type=%q, command=%q", id, req.Type, req.Command)
		_ = encoder.Encode(types.BridgeResponse{Status: "error", Error: "invalid characters in command/type"})
		return
	}

	logger.Infof("➡️ Received request: type=%s, command=%s, args=%v", req.Type, req.Command, req.Args)

	group, found := handlers.HandlersByType[req.Type]
	if !found || group == nil {
		logger.Warnf("❌ Unknown type: %s", req.Type)
		_ = encoder.Encode(types.BridgeResponse{Status: "error", Error: fmt.Sprintf("unknown type: %s", req.Type)})
		return
	}
	handler, ok := group[req.Command]
	if !ok {
		logger.Warnf("❌ Unknown command for type %s: %s", req.Type, req.Command)
		_ = encoder.Encode(types.BridgeResponse{Status: "error", Error: fmt.Sprintf("unknown command: %s", req.Command)})
		return
	}

	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("🔥 Panic in %s command handler: %v", req.Type, r)
			_ = encoder.Encode(types.BridgeResponse{Status: "error", Error: fmt.Sprintf("panic: %v", r)})
		}
	}()
	out, err := handler(req.Args)
	if err == nil {
		var raw json.RawMessage
		if out != nil {
			rawBytes, marshalErr := json.Marshal(out)
			if marshalErr != nil {
				_ = encoder.Encode(types.BridgeResponse{Status: "error", Error: "failed to marshal handler output"})
				return
			}
			raw = json.RawMessage(rawBytes)
		}
		_ = encoder.Encode(types.BridgeResponse{Status: "ok", Output: raw})
		return
	}

	logger.Errorf("❌ %s %s failed: %v", req.Type, req.Command, err)
	_ = encoder.Encode(types.BridgeResponse{Status: "error", Error: err.Error()})
}

// CleanupBridgeSocket removes the bridge socket for the session.
func CleanupBridgeSocket(sess *session.Session) error {
	bridgeSock, err := BridgeSocketPath(sess)
	if err != nil {
		logger.Warnf("Could not determine bridge socket path: %v", err)
		return err
	}
	if err := os.Remove(bridgeSock); err == nil {
		logger.Infof("Removed bridge socket file %s for session %s", bridgeSock, sess.SessionID)
	} else if !os.IsNotExist(err) {
		logger.Warnf("Failed to remove bridge socket file %s: %v", bridgeSock, err)
		return err
	}
	return nil
}

// CreateAndOwnSocket creates a unix socket at socketPath, ensures only the target user can access it.
func CreateAndOwnSocket(socketPath, username string) (net.Listener, int, int, error) {
	u, err := user.Lookup(username)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("failed to lookup user %s: %w", username, err)
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)

	_ = os.Remove(socketPath) // it's okay to ignore error here (socket might not exist)

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("failed to listen on socket: %w", err)
	}

	if err := os.Chmod(socketPath, 0600); err != nil {
		if cerr := listener.Close(); cerr != nil {
			logger.Warnf("failed to close listener after chmod failure: %v", cerr)
		}
		if rerr := os.Remove(socketPath); rerr != nil {
			logger.Warnf("failed to remove socket after chmod failure: %v", rerr)
		}
		return nil, 0, 0, fmt.Errorf("failed to chmod socket: %w", err)
	}
	if err := os.Chown(socketPath, uid, gid); err != nil {
		if cerr := listener.Close(); cerr != nil {
			logger.Warnf("failed to close listener after chown failure: %v", cerr)
		}
		if rerr := os.Remove(socketPath); rerr != nil {
			logger.Warnf("failed to remove socket after chown failure: %v", rerr)
		}
		return nil, 0, 0, fmt.Errorf("failed to chown socket: %w", err)
	}

	return listener, uid, gid, nil
}

func getBridgeBinaryPath() string {
	if os.Getenv("GO_ENV") == "development" {
		dir, err := os.Getwd()
		if err != nil {
			logger.Warnf("Failed to get working directory: %v", err)
			return "linuxio-bridge"
		}
		rootDir := filepath.Dir(dir) // go-backend → LinuxIO
		return filepath.Join(rootDir, "linuxio-bridge")
	}

	// Production mode: use executable path
	exe, err := os.Executable()
	if err != nil {
		logger.Warnf("Failed to get executable path: %v", err)
		return "linuxio-bridge"
	}
	return filepath.Join(filepath.Dir(exe), "linuxio-bridge")
}
