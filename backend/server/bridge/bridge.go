package bridge

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mordilloSan/go_logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/common/config"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// yamuxSessions manages persistent yamux sessions per socket path
var yamuxSessions = struct {
	sync.RWMutex
	sessions map[string]*ipc.YamuxSession
}{
	sessions: make(map[string]*ipc.YamuxSession),
}

// validateBridgeHash computes SHA256 of the bridge binary and compares to expected.
// Returns nil if hash matches or no hash is embedded (development mode).
// Returns error if hash mismatch (security violation).
func validateBridgeHash(bridgePath string) error {
	expectedHash := config.BridgeSHA256

	// Skip validation in development (no hash embedded)
	if expectedHash == "" {
		logger.Debugf("Bridge hash validation skipped (no embedded hash - development mode?)")
		return nil
	}

	// Open the bridge binary
	f, err := os.Open(bridgePath)
	if err != nil {
		return fmt.Errorf("failed to open bridge binary for hash validation: %w", err)
	}
	defer f.Close()

	// Compute SHA256
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("failed to read bridge binary for hash: %w", err)
	}
	actualHash := hex.EncodeToString(h.Sum(nil))

	// Compare hashes
	if actualHash != expectedHash {
		logger.ErrorKV("bridge binary hash mismatch - possible tampering detected",
			"expected", expectedHash,
			"actual", actualHash,
			"path", bridgePath)
		return fmt.Errorf("bridge binary integrity check failed: hash mismatch (expected %s..., got %s...)",
			expectedHash[:16], actualHash[:16])
	}

	logger.DebugKV("bridge binary hash validated",
		"hash", actualHash[:16]+"...",
		"path", bridgePath)
	return nil
}

// StartBridge launches linuxio-bridge via the setuid helper.
// Returns (privilegedMode, error). privilegedMode reflects the helper's decision.
func StartBridge(sess *session.Session, password string, envMode string, verbose bool, bridgeBinary string) (bool, error) {
	// Resolve bridge binary (helper also validates)
	if bridgeBinary == "" {
		bridgeBinary = GetBridgeBinaryPath("")
	}
	if bridgeBinary == "" {
		return false, errors.New("bridge binary not found (looked beside server and in PATH)")
	}

	// Validate bridge binary hash before proceeding
	if err := validateBridgeHash(bridgeBinary); err != nil {
		return false, fmt.Errorf("bridge security validation failed: %w", err)
	}

	helperPath := getAuthHelperPath()
	if helperPath == "" {
		return false, fmt.Errorf("auth helper not found; expected %s or LINUXIO_PAM_HELPER override", config.AuthHelperPath)
	}

	logger.Debugf("Using bridge binary: %s", bridgeBinary)
	logger.Debugf("Using auth helper: %s", helperPath)

	// Create pipe for bridge logs in development mode
	var logPipeR, logPipeW *os.File
	if strings.ToLower(envMode) == config.EnvDevelopment {
		var err error
		logPipeR, logPipeW, err = os.Pipe()
		if err != nil {
			logger.Warnf("Failed to create log pipe: %v (falling back to file logging)", err)
		} else {
			// Start goroutine to read bridge logs and display them
			go func() {
				defer logPipeR.Close()
				scanner := bufio.NewScanner(logPipeR)
				for scanner.Scan() {
					fmt.Printf("[bridge] %s\n", scanner.Text())
				}
				if err := scanner.Err(); err != nil {
					logger.Debugf("Bridge log pipe scanner error: %v", err)
				}
			}()
		}
	}

	// Build env for the helper (helper now decides privilege itself)
	env := append(os.Environ(),
		"LINUXIO_ENV="+strings.ToLower(envMode),
		"LINUXIO_BRIDGE_BIN="+bridgeBinary,
		"LINUXIO_SESSION_ID="+sess.SessionID,
		"LINUXIO_SESSION_USER="+sess.User.Username,
		"LINUXIO_BRIDGE_SECRET="+sess.BridgeSecret,
		"LINUXIO_SOCKET_PATH="+sess.SocketPath,
	)
	if verbose {
		env = append(env, "LINUXIO_VERBOSE=1")
	}
	if v := os.Getenv("LINUXIO_SERVER_BASE_URL"); v != "" {
		env = append(env, "LINUXIO_SERVER_BASE_URL="+v)
	}
	if v := os.Getenv("LINUXIO_SERVER_CERT"); v != "" {
		env = append(env, "LINUXIO_SERVER_CERT="+v)
	}
	// Pass log pipe FD if available
	// ExtraFiles start at FD 3, so logPipeW will be FD 3 in the auth-helper
	// Auth-helper will dup it to a higher FD before using FD 3 for bootstrap
	if logPipeW != nil {
		env = append(env, "LINUXIO_LOG_FD=3")
	}

	cmd := exec.Command(helperPath)
	cmd.Env = env
	// Pass the log pipe as an extra file descriptor (becomes FD 3 in auth-helper)
	if logPipeW != nil {
		cmd.ExtraFiles = []*os.File{logPipeW}
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return false, fmt.Errorf("helper stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return false, fmt.Errorf("helper stderr pipe: %w", err)
	}
	// If your helper expects one line of password (or an empty line), just:
	if password == "" {
		cmd.Stdin = strings.NewReader("\n") // harmless if helper ignores it
	} else {
		cmd.Stdin = strings.NewReader(password + "\n")
	}

	if err := cmd.Start(); err != nil {
		if logPipeW != nil {
			logPipeW.Close()
		}
		return false, fmt.Errorf("start helper: %w", err)
	}

	// Setup cleanup for log pipe on error paths
	var bridgeStarted bool
	defer func() {
		if !bridgeStarted && logPipeW != nil {
			logPipeW.Close()
		}
	}()

	// Read first line = MODE=...
	br := bufio.NewReader(stdout)

	readLine := func(timeout time.Duration) (string, error) {
		type res struct {
			s string
			e error
		}
		ch := make(chan res, 1)
		go func() {
			line, e := br.ReadString('\n')
			ch <- res{line, e}
		}()
		select {
		case r := <-ch:
			return strings.TrimSpace(r.s), r.e
		case <-time.After(timeout):
			return "", fmt.Errorf("timeout waiting for helper line")
		}
	}

	// capture stderr for diagnostics (non-blocking)
	seCh := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(stderr)
		seCh <- strings.TrimSpace(string(b))
	}()

	// Expect MODE line, then OK
	modeLine, e1 := readLine(2 * time.Second)
	if e1 != nil && e1 != io.EOF {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		var serr string
		select {
		case serr = <-seCh:
		default:
		}
		if serr != "" {
			return false, fmt.Errorf("helper mode read error: %v (%s)", e1, serr)
		}
		return false, fmt.Errorf("helper mode read error: %v", e1)
	}

	privileged := false
	if strings.HasPrefix(modeLine, "MODE=") {
		if strings.EqualFold(modeLine, "MODE=privileged") {
			privileged = true
		} else {
			privileged = false
		}
		// read the next line for OK
		okLine, e2 := readLine(2 * time.Second)
		if e2 != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			var serr string
			select {
			case serr = <-seCh:
			default:
			}
			if serr != "" {
				return false, fmt.Errorf("helper did not confirm OK: %v (%s)", e2, serr)
			}
			return false, fmt.Errorf("helper did not confirm OK: %v", e2)
		}
		if okLine != "OK" {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			var serr string
			select {
			case serr = <-seCh:
			default:
			}
			if serr != "" {
				return false, fmt.Errorf("helper did not confirm: %q (%s)", okLine, serr)
			}
			return false, fmt.Errorf("helper did not confirm: %q", okLine)
		}
	} else {
		// Unexpected first line
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		var serr string
		select {
		case serr = <-seCh:
		default:
		}
		if serr != "" {
			return false, fmt.Errorf("unexpected helper output: %q (%s)", modeLine, serr)
		}
		return false, fmt.Errorf("unexpected helper output: %q", modeLine)
	}

	// Reap the parent helper (nanny owns bridge)
	if err := cmd.Wait(); err != nil {
		logger.WarnKV("auth helper exited non-zero after OK", "error", err)
	}

	// Mark bridge as successfully started before closing our copy of the log pipe FD
	bridgeStarted = true

	// Close the write end of the log pipe now that the bridge has inherited it
	// This ensures the pipe reader will get EOF when the bridge exits
	if logPipeW != nil {
		logPipeW.Close()
	}

	logger.InfoKV("bridge launch acknowledged", "user", sess.User.Username, "privileged", privileged)
	return privileged, nil
}

// ============================================================================
// Comunication with the bridge
// ============================================================================

// GetOrCreateYamuxSession returns an existing yamux session or creates a new one
func GetOrCreateYamuxSession(socketPath string) (*ipc.YamuxSession, error) {
	// Check for existing session
	yamuxSessions.RLock()
	session, exists := yamuxSessions.sessions[socketPath]
	yamuxSessions.RUnlock()

	if exists && !session.IsClosed() {
		return session, nil
	}

	// Create new session
	yamuxSessions.Lock()
	defer yamuxSessions.Unlock()

	// Double-check after acquiring write lock
	if session, exists = yamuxSessions.sessions[socketPath]; exists && !session.IsClosed() {
		return session, nil
	}

	// Clean up old session if exists
	if exists {
		delete(yamuxSessions.sessions, socketPath)
	}

	// Dial the bridge
	conn, err := dialBridgeRaw(socketPath)
	if err != nil {
		return nil, err
	}

	// Create yamux client session
	session, err = ipc.NewYamuxClient(conn)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to create yamux session: %w", err)
	}

	// Set cleanup callback
	session.SetOnClose(func() {
		yamuxSessions.Lock()
		delete(yamuxSessions.sessions, socketPath)
		yamuxSessions.Unlock()
		logger.DebugKV("yamux session closed and removed", "socket_path", socketPath)
	})

	yamuxSessions.sessions[socketPath] = session
	logger.InfoKV("yamux session established", "socket_path", socketPath)

	return session, nil
}

// CloseYamuxSession closes the yamux session for a socket path
func CloseYamuxSession(socketPath string) {
	yamuxSessions.Lock()
	defer yamuxSessions.Unlock()

	if session, exists := yamuxSessions.sessions[socketPath]; exists {
		session.Close()
		delete(yamuxSessions.sessions, socketPath)
		logger.DebugKV("yamux session closed", "socket_path", socketPath)
	}
}

// dialBridgeRaw creates a raw connection to the bridge socket
func dialBridgeRaw(socketPath string) (net.Conn, error) {
	const (
		totalWait   = 2 * time.Second
		step        = 100 * time.Millisecond
		dialTimeout = 500 * time.Millisecond
	)

	var conn net.Conn
	var err error
	deadline := time.Now().Add(totalWait)

	for {
		conn, err = net.DialTimeout("unix", socketPath, dialTimeout)
		if err == nil {
			return conn, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("failed to connect to bridge (%s): %w", socketPath, err)
		}
		time.Sleep(step)
	}
}

// CallTypedWithSession makes a bridge call and returns raw bytes
func CallWithSession(sess *session.Session, reqType, command string, args []string) ([]byte, error) {
	logger.DebugKV("bridge call initiated (yamux)",
		"user", sess.User.Username,
		"type", reqType,
		"command", command,
		"args", fmt.Sprintf("%v", args))

	socketPath := sess.SocketPath
	if socketPath == "" {
		err := fmt.Errorf("empty session.SocketPath")
		logger.ErrorKV("bridge call failed: invalid socket path",
			"user", sess.User.Username,
			"error", err)
		terminateSessionOnBridgeFailure(sess)
		return nil, err
	}

	// Get or create yamux session
	yamuxSession, err := GetOrCreateYamuxSession(socketPath)
	if err != nil {
		logger.ErrorKV("bridge call failed: yamux session error",
			"user", sess.User.Username,
			"command", command,
			"error", err)
		terminateSessionOnBridgeFailure(sess)
		return nil, err
	}

	// Open a new stream for this request
	stream, err := yamuxSession.Open()
	if err != nil {
		logger.ErrorKV("bridge call failed: stream open error",
			"user", sess.User.Username,
			"command", command,
			"error", err)
		// Session might be dead, close it so next call creates a new one
		CloseYamuxSession(socketPath)
		terminateSessionOnBridgeFailure(sess)
		return nil, fmt.Errorf("failed to open yamux stream: %w", err)
	}
	defer stream.Close()

	// Build request
	req := ipc.Request{
		Type:      reqType,
		Command:   command,
		Secret:    sess.BridgeSecret,
		Args:      args,
		SessionID: sess.SessionID,
	}

	// Send request using framed protocol
	if err = ipc.WriteRequestFrame(stream, &req); err != nil {
		logger.ErrorKV("bridge call failed: write error",
			"user", sess.User.Username,
			"type", reqType,
			"command", command,
			"error", err)
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	// Read response
	var resp ipc.Response
	msgType, err := ipc.ReadJSONFrame(stream, &resp)
	if err != nil {
		logger.ErrorKV("bridge call failed: read error",
			"user", sess.User.Username,
			"type", reqType,
			"command", command,
			"error", err)
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if msgType != ipc.MsgTypeJSON {
		return nil, fmt.Errorf("unexpected response type: 0x%02x", msgType)
	}

	// Marshal response back to raw JSON for compatibility
	raw, err := json.Marshal(resp)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response: %w", err)
	}

	logger.DebugKV("bridge call completed (yamux)",
		"user", sess.User.Username,
		"type", reqType,
		"command", command,
		"response_bytes", len(raw))

	return raw, nil
}

// CallTypedWithSession makes a bridge call and decodes the response directly into result.
func CallTypedWithSession(sess *session.Session, reqType, command string, args []string, result interface{}) error {
	raw, err := CallWithSession(sess, reqType, command, args)
	if err != nil {
		terminateSessionOnBridgeFailure(sess)
		return err
	}

	var resp struct {
		Status string          `json:"status"`
		Output json.RawMessage `json:"output,omitempty"`
		Error  string          `json:"error,omitempty"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return fmt.Errorf("decode bridge response: %w", err)
	}

	if resp.Status != "ok" {
		return fmt.Errorf("bridge error: %s", resp.Error)
	}

	if result == nil {
		return nil
	}

	if len(resp.Output) == 0 {
		return ipc.ErrEmptyBridgeOutput
	}

	if err := json.Unmarshal(resp.Output, result); err != nil {
		return fmt.Errorf("decode bridge output: %w", err)
	}

	return nil
}

// ============================================================================
// Helpers
// ============================================================================

func terminateSessionOnBridgeFailure(sess *session.Session) {
	if sess == nil {
		return
	}
	if err := sess.Terminate(session.ReasonBridgeFailure); err != nil {
		logger.WarnKV("failed to terminate session after bridge failure",
			"user", sess.User.Username,
			"error", err)
	}
}

// GetBridgeBinaryPath returns an absolute or name-only path for the bridge.
func GetBridgeBinaryPath(override string) string {
	const binaryName = "linuxio-bridge"

	if override != "" && isExec(override) {
		return override
	}
	if v := os.Getenv("LINUXIO_BRIDGE_BIN"); v != "" && isExec(v) {
		return v
	}

	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), binaryName)
		if isExec(candidate) {
			return candidate
		}
	}
	if path, err := exec.LookPath(binaryName); err == nil && isExec(path) {
		return path
	}
	logger.Debugf("[bridge.GetBridgeBinaryPath] %s not found beside server, or in user $PATH; consider installing into a well-known path or setting LINUXIO_BRIDGE_BIN.", binaryName)
	return ""
}

func isExec(p string) bool {
	st, err := os.Stat(p)
	if err != nil || st.IsDir() {
		return false
	}
	return st.Mode()&0o111 != 0
}

func getAuthHelperPath() string {
	if v := os.Getenv("LINUXIO_PAM_HELPER"); v != "" && isExec(v) {
		return v
	}
	if isExec(config.AuthHelperPath) {
		return config.AuthHelperPath
	}
	if p, err := exec.LookPath("linuxio-auth-helper"); err == nil && isExec(p) {
		return p
	}
	return ""
}
