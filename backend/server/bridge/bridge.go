package bridge

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
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
// Returns error if no hash embedded, hash mismatch, or file cannot be read.
func validateBridgeHash(bridgePath string) error {
	expectedHash := config.BridgeSHA256
	if expectedHash == "" {
		return fmt.Errorf("bridge hash not embedded at build time")
	}

	f, err := os.Open(bridgePath)
	if err != nil {
		return fmt.Errorf("failed to open bridge binary: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("failed to read bridge binary: %w", err)
	}
	actualHash := hex.EncodeToString(h.Sum(nil))

	if actualHash != expectedHash {
		logger.ErrorKV("bridge binary hash mismatch - possible tampering",
			"expected", expectedHash,
			"actual", actualHash,
			"path", bridgePath)
		return fmt.Errorf("bridge integrity check failed: hash mismatch")
	}

	logger.DebugKV("bridge hash validated", "hash", actualHash[:16]+"...")
	return nil
}

const bridgeBinaryPath = config.BinDir + "/linuxio-bridge"

// StartBridge launches linuxio-bridge via the auth daemon.
// On success, creates a yamux session for the bridge connection and stores it.
// Returns (privilegedMode, motd, error). privilegedMode reflects the daemon's decision.
func StartBridge(sess *session.Session, password string, verbose bool) (bool, error) {
	// Validate bridge binary hash before proceeding
	if err := validateBridgeHash(bridgeBinaryPath); err != nil {
		return false, fmt.Errorf("bridge security validation failed: %w", err)
	}
	logger.Debugf("Auth daemon available, using socket-based auth")
	req := BuildRequest(sess, password, verbose)
	result, err := Authenticate(req)
	if err != nil {
		return false, fmt.Errorf("auth daemon failed: %w", err)
	}

	// Create yamux client session from the connection
	// (auth daemon forked bridge and passed our FD to it via dup2)
	yamuxSession, err := ipc.NewYamuxClient(result.Conn)
	if err != nil {
		result.Conn.Close()
		return false, fmt.Errorf("failed to create yamux session: %w", err)
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

	return result.Privileged, nil
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
