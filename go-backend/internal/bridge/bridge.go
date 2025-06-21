package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go-backend/cmd/bridge/handlers"
	"go-backend/internal/logger"
	"go-backend/internal/session"
	"io"
	"net"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/containerd/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

var bridgeBinary = os.ExpandEnv("/usr/lib/linuxio/linuxio-bridge")

type BridgeProcess struct {
	Cmd       *exec.Cmd
	SessionID string
	StartedAt time.Time
}

// Request represents the standard JSON request format sent to both built-in handlers and external helpers.
type BridgeRequest struct {
	Type    string   `json:"type"`
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
}

// Response represents the standard JSON response returned by both built-in handlers and helpers.
type BridgeResponse struct {
	Status string          `json:"status"`
	Output json.RawMessage `json:"output,omitempty"`
	Error  string          `json:"error,omitempty"`
}

type BridgeHealthRequest struct {
	Type    string `json:"type"`    // e.g., "healthcheck" or "validate"
	Session string `json:"session"` // sessionID
}
type BridgeHealthResponse struct {
	Status  string `json:"status"` // "ok" or "invalid"
	Message string `json:"message,omitempty"`
}

var (
	processes   = make(map[string]*BridgeProcess)
	processesMu sync.Mutex
)

var (
	mainSocketListeners   = make(map[string]net.Listener) // sessionID → Listener
	mainSocketListenersMu sync.Mutex
)

// MainSocketPath returns the per-session main (healthcheck) socket path for the user.
func MainSocketPath(sess *session.Session) (string, error) {
	u, err := user.Lookup(sess.User.ID)
	if err != nil {
		logger.Errorf("could not find user %s: %v", sess.User.ID, err)
		return "", err
	}
	return fmt.Sprintf("/run/user/%s/linuxio-main-%s.sock", u.Uid, sess.SessionID), nil
}

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
	defer conn.Close()
	enc := json.NewEncoder(conn)
	dec := json.NewDecoder(conn)
	if err := enc.Encode(req); err != nil {
		return nil, fmt.Errorf("failed to send request to bridge: %w", err)
	}
	var resp BridgeResponse
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
			"GO_ENV="+os.Getenv("GO_ENV"),
			"VERBOSE="+os.Getenv("VERBOSE"),
			bridgeBinary,
		)
	} else {
		cmd = exec.Command(bridgeBinary)
		cmd.Env = append(os.Environ(),
			"LINUXIO_SESSION_ID="+sess.SessionID,
			"LINUXIO_SESSION_USER="+sess.User.ID,
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
			defer stdin.Close()
			_, _ = stdin.Write(pwBytes)

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

	processes[sess.SessionID] = &BridgeProcess{
		Cmd:       cmd,
		SessionID: sess.SessionID,
		StartedAt: time.Now(),
	}

	// Panic guard for process cleanup goroutine
	go func(sessID string, cmd *exec.Cmd, stdoutBuf, stderrBuf *bytes.Buffer) {
		defer func() {
			if r := recover(); r != nil {
				logger.Errorf("Panic in process cleanup goroutine for session %s: %v", sessID, r)
			}
		}()
		logger.Infof("Captured output buffers for session %s: STDOUT=%d bytes, STDERR=%d bytes", sessID, stdoutBuf.Len(), stderrBuf.Len())

		err := cmd.Wait()
		processesMu.Lock()
		defer processesMu.Unlock()
		delete(processes, sessID)

		stdout := strings.TrimSpace(stdoutBuf.String())
		stderr := strings.TrimSpace(stderrBuf.String())

		if stdout != "" {
			logger.Infof("STDOUT for session %s:\n%s", sessID, stdout)
		}
		if stderr != "" {
			logger.Warnf("STDERR for session %s:\n%s", sessID, stderr)
		}

		if err != nil {
			logger.Warnf("Bridge for session %s exited with error: %v", sessID, err)
		} else {
			logger.Infof("Bridge for session %s exited", sessID)
		}
	}(sess.SessionID, cmd, &stdoutBuf, &stderrBuf)

	return nil
}

// StartBridgeSocket starts a Unix socket server for the main process.
func StartBridgeSocket(sess *session.Session) error {
	socketPath, err := MainSocketPath(sess)
	if err != nil {
		logger.Errorf("Failed to get main socket path for session %s: %v", sess.SessionID, err)
		return err
	}

	_ = os.Remove(socketPath)
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		logger.Errorf("Failed to listen on main socket for session %s: %v", sess.SessionID, err)
		return err
	}

	// Set permissions strictly to 0600 (owner read/write only)
	if err := os.Chmod(socketPath, 0600); err != nil {
		_ = ln.Close()
		_ = os.Remove(socketPath)
		logger.Errorf("Failed to chmod main socket %s: %v", socketPath, err)
		return fmt.Errorf("failed to chmod socket: %w", err)
	}

	// Store the listener so we can close and remove it on logout
	mainSocketListenersMu.Lock()
	mainSocketListeners[sess.SessionID] = ln
	mainSocketListenersMu.Unlock()

	logger.Infof("Main socket for session %s is now listening on %s", sess.SessionID, socketPath)

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				logger.Warnf("Accept failed on main socket for session %s: %v", sess.SessionID, err)
				// Exit the goroutine if the listener is closed
				break
			}
			logger.Infof("Main socket for session %s accepted a connection", sess.SessionID)
			go func() {
				defer func() {
					if r := recover(); r != nil {
						logger.Errorf("Panic in main socket handler: %v", r)
					}
				}()
				handleBridgeRequest(conn)
			}()
		}
	}()

	return nil
}

func handleBridgeRequest(conn net.Conn) {
	defer conn.Close()
	logger.Infof("Main socket accepted a connection")
	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)

	var req BridgeHealthRequest
	if err := decoder.Decode(&req); err != nil {
		logger.Warnf("Invalid JSON on main socket: %v", err)
		_ = encoder.Encode(BridgeHealthResponse{Status: "error", Message: "invalid json"})
		return
	}

	if req.Type == "validate" {
		logger.Infof("Healthcheck received for session %s", req.Session)
		if session.IsValid(req.Session) {
			_ = encoder.Encode(BridgeHealthResponse{Status: "ok"})
		} else {
			_ = encoder.Encode(BridgeHealthResponse{Status: "invalid", Message: "session expired"})
		}
		return
	}
	logger.Warnf("Unknown healthcheck request type: %s (session %s)", req.Type, req.Session)
	_ = encoder.Encode(BridgeHealthResponse{Status: "error", Message: "unknown request type"})
}

// HandleMainRequest processes incoming bridge requests.
func HandleMainRequest(conn net.Conn, id string) {
	logger.Debugf("HANDLECONNECTION: [%s] called!", id)
	defer conn.Close()

	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)

	var req BridgeRequest
	if err := decoder.Decode(&req); err != nil {
		if err == io.EOF {
			logger.Debugf("🔁 [%s] connection closed without data (likely healthcheck probe)", id)
		} else {
			logger.Warnf("❌ [%s] invalid JSON from client: %v", id, err)
		}
		_ = encoder.Encode(BridgeResponse{Status: "error", Error: "invalid JSON"})
		return
	}

	// (1) DEFENSE-IN-DEPTH: Validate handler name for fallback
	if strings.ContainsAny(req.Type, "./\\") || strings.ContainsAny(req.Command, "./\\") {
		logger.Warnf("❌ [%s] Invalid characters in type/command: type=%q, command=%q", id, req.Type, req.Command)
		_ = encoder.Encode(BridgeResponse{Status: "error", Error: "invalid characters in command/type"})
		return
	}

	logger.Infof("➡️ Received request: type=%s, command=%s, args=%v", req.Type, req.Command, req.Args)

	group, found := handlers.HandlersByType[req.Type]
	if !found || group == nil {
		logger.Warnf("❌ Unknown type: %s", req.Type)
		_ = encoder.Encode(BridgeResponse{Status: "error", Error: fmt.Sprintf("unknown type: %s", req.Type)})
		return
	}
	handler, ok := group[req.Command]
	if !ok {
		logger.Warnf("❌ Unknown command for type %s: %s", req.Type, req.Command)
		_ = encoder.Encode(BridgeResponse{Status: "error", Error: fmt.Sprintf("unknown command: %s", req.Command)})
		return
	}

	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("🔥 Panic in %s command handler: %v", req.Type, r)
			_ = encoder.Encode(BridgeResponse{Status: "error", Error: fmt.Sprintf("panic: %v", r)})
		}
	}()
	out, err := handler(req.Args)
	if err == nil {
		var raw json.RawMessage
		if out != nil {
			rawBytes, marshalErr := json.Marshal(out)
			if marshalErr != nil {
				_ = encoder.Encode(BridgeResponse{Status: "error", Error: "failed to marshal handler output"})
				return
			}
			raw = json.RawMessage(rawBytes)
		}
		_ = encoder.Encode(BridgeResponse{Status: "ok", Output: raw})
		return
	}

	logger.Errorf("❌ %s %s failed: %v", req.Type, req.Command, err)
	_ = encoder.Encode(BridgeResponse{Status: "error", Error: err.Error()})
}

func CleanupBridgeSocket(sess *session.Session) error {
	var firstErr error

	mainSocketListenersMu.Lock()
	ln, ok := mainSocketListeners[sess.SessionID]
	if ok {
		if err := ln.Close(); err != nil {
			logger.Warnf("Error closing main socket listener for session %s: %v", sess.SessionID, err)
			firstErr = err
		} else {
			logger.Infof("Closed main socket listener for session %s", sess.SessionID)
		}
		delete(mainSocketListeners, sess.SessionID)
	}
	mainSocketListenersMu.Unlock()

	mainSock, err := MainSocketPath(sess)
	if err != nil {
		logger.Warnf("Could not determine main socket path: %v", err)
		if firstErr == nil {
			firstErr = err
		}
	} else {
		if err := os.Remove(mainSock); err == nil {
			logger.Infof("Removed main socket file %s for session %s", mainSock, sess.SessionID)
		} else if !os.IsNotExist(err) {
			logger.Warnf("Failed to remove main socket file %s: %v", mainSock, err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	bridgeSock, err := BridgeSocketPath(sess)
	if err != nil {
		logger.Warnf("Could not determine bridge socket path: %v", err)
		if firstErr == nil {
			firstErr = err
		}
	} else {
		if err := os.Remove(bridgeSock); err == nil {
			logger.Infof("Removed bridge socket file %s for session %s", bridgeSock, sess.SessionID)
		} else if !os.IsNotExist(err) {
			logger.Warnf("Failed to remove bridge socket file %s: %v", bridgeSock, err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	return firstErr
}

func CleanupFilebrowserContainer() error {
	containerName := "/filebrowser"
	timeout := 0 // seconds

	var errors []error

	logger.Infof("Stopping FileBrowser container: %s", containerName)
	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		logger.Warnf("Failed to create Docker client: %v", err)
		return err
	}
	defer cli.Close()

	if err := cli.ContainerStop(context.Background(), containerName, container.StopOptions{Timeout: &timeout}); err != nil {
		if errdefs.IsNotFound(err) {
			logger.Infof("Container %s was not running.", containerName)
		} else {
			logger.Warnf("Failed to stop container %s: %v", containerName, err)
			errors = append(errors, fmt.Errorf("stop: %w", err))
		}
	} else {
		logger.Infof("Stopped FileBrowser container: %s", containerName)
	}

	logger.Infof("Removing FileBrowser container: %s", containerName)
	if err := cli.ContainerRemove(context.Background(), containerName, container.RemoveOptions{Force: true}); err != nil {
		if errdefs.IsNotFound(err) {
			logger.Infof("Container %s already removed.", containerName)
		} else {
			logger.Warnf("Failed to remove container %s: %v", containerName, err)
			errors = append(errors, fmt.Errorf("remove: %w", err))
		}
	} else {
		logger.Infof("Removed FileBrowser container: %s", containerName)
	}

	if len(errors) > 0 {
		return fmt.Errorf("CleanupFilebrowserContainer encountered errors: %v", errors)
	}
	return nil
}

// createAndOwnSocket creates a unix socket at socketPath, ensures only the target user can access it.
func CreateAndOwnSocket(socketPath, username string) (net.Listener, int, int, error) {
	u, err := user.Lookup(username)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("failed to lookup user %s: %w", username, err)
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)

	_ = os.Remove(socketPath)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("failed to listen on socket: %w", err)
	}

	if err := os.Chmod(socketPath, 0600); err != nil {
		listener.Close()
		os.Remove(socketPath)
		return nil, 0, 0, fmt.Errorf("failed to chmod socket: %w", err)
	}
	if err := os.Chown(socketPath, uid, gid); err != nil {
		listener.Close()
		os.Remove(socketPath)
		return nil, 0, 0, fmt.Errorf("failed to chown socket: %w", err)
	}

	return listener, uid, gid, nil
}
