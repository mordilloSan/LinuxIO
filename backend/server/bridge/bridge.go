package bridge

import (
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

// StartBridge launches linuxio-bridge via the auth daemon.
// Returns (privilegedMode, motd, error). privilegedMode reflects the daemon's decision.
func StartBridge(sess *session.Session, password string, envMode string, verbose bool, bridgeBinary string) (bool, string, error) {
	// Resolve bridge binary (helper also validates)
	if bridgeBinary == "" {
		bridgeBinary = GetBridgeBinaryPath("")
	}
	if bridgeBinary == "" {
		return false, "", errors.New("bridge binary not found (looked beside server and in PATH)")
	}

	// Validate bridge binary hash before proceeding
	if err := validateBridgeHash(bridgeBinary); err != nil {
		return false, "", fmt.Errorf("bridge security validation failed: %w", err)
	}

	if !DaemonAvailable() {
		return false, "", errors.New("auth daemon not available")
	}

	logger.Debugf("Auth daemon available, using socket-based auth")
	req := BuildRequest(sess, password, bridgeBinary, strings.ToLower(envMode), verbose)
	privileged, motd, err := Authenticate(req)
	if err != nil {
		return false, "", fmt.Errorf("auth daemon failed: %w", err)
	}

	logger.InfoKV("bridge launch via daemon acknowledged", "user", sess.User.Username, "privileged", privileged)
	return privileged, motd, nil
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

// CallTypedWithSession makes a bridge call and decodes the response directly into result.
// Uses JSON encoding for IPC.
func CallTypedWithSession(sess *session.Session, reqType, command string, args []string, result any) error {
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
		return err
	}

	// Get or create yamux session
	yamuxSession, err := GetOrCreateYamuxSession(socketPath)
	if err != nil {
		logger.ErrorKV("bridge call failed: yamux session error",
			"user", sess.User.Username,
			"command", command,
			"error", err)
		terminateSessionOnBridgeFailure(sess)
		return err
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
		return fmt.Errorf("failed to open yamux stream: %w", err)
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

	// Send request using JSON encoding
	if err = ipc.WriteRequest(stream, &req); err != nil {
		logger.ErrorKV("bridge call failed: write error",
			"user", sess.User.Username,
			"type", reqType,
			"command", command,
			"error", err)
		return fmt.Errorf("failed to send request: %w", err)
	}

	// Read JSON-encoded response
	resp, err := ipc.ReadResponse(stream)
	if err != nil {
		logger.ErrorKV("bridge call failed: read error",
			"user", sess.User.Username,
			"type", reqType,
			"command", command,
			"error", err)
		return fmt.Errorf("failed to read response: %w", err)
	}

	logger.DebugKV("bridge call completed (yamux)",
		"user", sess.User.Username,
		"type", reqType,
		"command", command)

	if resp.Status != "ok" {
		return fmt.Errorf("bridge error: %s", resp.Error)
	}

	if result == nil {
		return nil
	}

	if len(resp.Output) == 0 {
		return ipc.ErrEmptyBridgeOutput
	}

	// Directly unmarshal RawMessage into result (no double-encoding)
	if err := json.Unmarshal(resp.Output, result); err != nil {
		return fmt.Errorf("decode output: %w", err)
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
