package bridge

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/mordilloSan/go_logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/common/config"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// yamuxSessions manages persistent yamux sessions per session ID
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
// On success, creates a yamux session for the bridge connection and stores it.
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
	result, err := Authenticate(req)
	if err != nil {
		return false, "", fmt.Errorf("auth daemon failed: %w", err)
	}

	// Create yamux client session from the connection
	// (auth daemon forked bridge and passed our FD to it via dup2)
	yamuxSession, err := ipc.NewYamuxClient(result.Conn)
	if err != nil {
		result.Conn.Close()
		return false, "", fmt.Errorf("failed to create yamux session: %w", err)
	}

	// Store the session keyed by session ID
	yamuxSessions.Lock()
	// Clean up old session if exists
	if old, exists := yamuxSessions.sessions[sess.SessionID]; exists {
		old.Close()
	}
	yamuxSession.SetOnClose(func() {
		yamuxSessions.Lock()
		delete(yamuxSessions.sessions, sess.SessionID)
		yamuxSessions.Unlock()
		logger.DebugKV("yamux session closed and removed", "session_id", sess.SessionID)
	})
	yamuxSessions.sessions[sess.SessionID] = yamuxSession
	yamuxSessions.Unlock()

	logger.InfoKV("bridge launch via daemon acknowledged",
		"user", sess.User.Username,
		"privileged", result.Privileged,
		"session_id", sess.SessionID)

	return result.Privileged, result.Motd, nil
}

// ============================================================================
// Comunication with the bridge
// ============================================================================

// GetYamuxSession returns an existing yamux session for the given session ID.
// The session must have been created by StartBridge.
func GetYamuxSession(sessionID string) (*ipc.YamuxSession, error) {
	yamuxSessions.RLock()
	session, exists := yamuxSessions.sessions[sessionID]
	yamuxSessions.RUnlock()

	if !exists {
		return nil, fmt.Errorf("no yamux session for session %s", sessionID)
	}

	if session.IsClosed() {
		// Clean up stale entry
		yamuxSessions.Lock()
		delete(yamuxSessions.sessions, sessionID)
		yamuxSessions.Unlock()
		return nil, fmt.Errorf("yamux session for %s is closed", sessionID)
	}

	return session, nil
}

// CloseYamuxSession closes the yamux session for a session ID
func CloseYamuxSession(sessionID string) {
	// Remove from map first, then close OUTSIDE the lock.
	// This prevents deadlock: Close() triggers OnClose callback which tries to Lock().
	yamuxSessions.Lock()
	session, exists := yamuxSessions.sessions[sessionID]
	if exists {
		delete(yamuxSessions.sessions, sessionID)
	}
	yamuxSessions.Unlock()

	if exists {
		session.Close()
		logger.DebugKV("yamux session closed", "session_id", sessionID)
	}
}

// ============================================================================
// Helpers
// ============================================================================

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
