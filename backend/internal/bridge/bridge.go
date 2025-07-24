package bridge

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/mordilloSan/LinuxIO/backend/cmd/bridge/handlers/types"
	"github.com/mordilloSan/LinuxIO/backend/internal/logger"
	"github.com/mordilloSan/LinuxIO/backend/internal/session"
	"io"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
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
	return CallViaSocket(socketPath, reqType, command, args, sess.BridgeSecret)
}

func CallViaSocket(socketPath, reqType, command string, args []string, secret string) ([]byte, error) {
	req := map[string]any{
		"type":    reqType,
		"command": command,
		"secret":  secret,
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

// Helper to get the bridge binnay in dev and prod mode.
func getBridgeBinaryPath() string {
	if os.Getenv("GO_ENV") == "development" {
		dir, err := os.Getwd()
		if err != nil {
			logger.Warnf("Failed to get working directory: %v", err)
			return "linuxio-bridge"
		}
		rootDir := filepath.Dir(dir)
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
