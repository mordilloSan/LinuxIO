package bridge

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"sync"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

// yamuxSessions manages persistent yamux sessions per session ID
var yamuxSessions = struct {
	sync.RWMutex
	sessions map[string]*relay.YamuxSession
}{
	sessions: make(map[string]*relay.YamuxSession),
}

// validateBridgeHash computes SHA256 of the bridge binary and compares to expected.
// Returns error if no hash embedded, hash mismatch, or file cannot be read.
func validateBridgeHash(bridgePath string) error {
	expectedHash := version.BridgeSHA256
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
		slog.Error("bridge binary hash mismatch - possible tampering",
			"expected", expectedHash,
			"actual", actualHash,
			"path", bridgePath)
		return fmt.Errorf("bridge integrity check failed: hash mismatch")
	}
	slog.Debug("bridge hash validated", "hash", actualHash[:16]+"...")
	return nil
}

const bridgeBinaryPath = version.BinDir + "/linuxio-bridge"

// StartBridge launches linuxio-bridge via the auth daemon, persists the
// authenticated session, and stores the resulting yamux transport.
func StartBridge(sm *session.Manager, sessionID, username, password, remoteHost string, verbose bool) (*session.Session, error) {
	// Validate bridge binary hash before proceeding
	if err := validateBridgeHash(bridgeBinaryPath); err != nil {
		return nil, fmt.Errorf("bridge security validation failed: %w", err)
	}
	slog.Debug("Auth daemon available, using socket-based auth")
	req := BuildRequest(username, sessionID, password, remoteHost, verbose)
	result, err := Authenticate(req)
	if err != nil {
		return nil, fmt.Errorf("auth daemon failed: %w", err)
	}

	sess, err := sm.CreateSession(sessionID, result.User, result.Privileged)
	if err != nil {
		result.Conn.Close()
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	if attachErr := attachBridgeSession(sess, result.Conn); attachErr != nil {
		if delErr := sm.DeleteSession(sess.SessionID, session.ReasonManual); delErr != nil {
			slog.Warn("failed to cleanup session after bridge setup failure",
				"session_id", sess.SessionID,
				"error", delErr)
		}
		return nil, attachErr
	}

	slog.Debug("bridge launch via daemon acknowledged",
		"user", sess.User.Username,
		"privileged", result.Privileged)

	return sess, nil
}

func attachBridgeSession(sess *session.Session, conn net.Conn) error {
	// Create yamux client session from the connection
	// (auth daemon forked bridge and passed our FD to it via dup2)
	yamuxSession, err := relay.NewYamuxClient(conn)
	if err != nil {
		conn.Close()
		return fmt.Errorf("failed to create yamux session: %w", err)
	}
	registerYamuxSession(sess, yamuxSession)
	return nil
}

func registerYamuxSession(sess *session.Session, yamuxSession *relay.YamuxSession) {
	var old *relay.YamuxSession
	yamuxSessions.Lock()
	if existing, exists := yamuxSessions.sessions[sess.SessionID]; exists {
		old = existing
	}
	// Publish the replacement before closing the old session so its callback
	// cannot remove or terminate the new mapping.
	yamuxSessions.sessions[sess.SessionID] = yamuxSession
	yamuxSessions.Unlock()

	// SetOnClose may invoke the callback synchronously when the session has
	// already closed, so register it without holding yamuxSessions.Lock.
	yamuxSession.SetOnClose(func() {
		yamuxSessions.Lock()
		// Only this session may remove the mapping. In particular, an old
		// session closing after replacement must not remove the new session.
		current, present := yamuxSessions.sessions[sess.SessionID]
		if present && current == yamuxSession {
			delete(yamuxSessions.sessions, sess.SessionID)
		}
		yamuxSessions.Unlock()

		if !present || current != yamuxSession {
			return
		}
		slog.Debug("yamux session closed and removed", "session_id", sess.SessionID)

		// Terminate the session when bridge dies. This triggers session
		// deletion which closes the WebSocket.
		if err := sess.Terminate(session.ReasonBridgeFailure); err != nil {
			slog.Warn("failed to terminate session after bridge closure",
				"session_id", sess.SessionID,
				"error", err)
		}
	})

	if old != nil {
		_ = old.Close()
	}
}

// ============================================================================
// Communication with the bridge
// ============================================================================

// GetYamuxSession returns an existing yamux session for the given session ID.
// The session must have been created by StartBridge.
func GetYamuxSession(sessionID string) (*relay.YamuxSession, error) {
	yamuxSessions.RLock()
	yamuxSession, exists := yamuxSessions.sessions[sessionID]
	yamuxSessions.RUnlock()

	if !exists {
		return nil, fmt.Errorf("no yamux session for session %s", sessionID)
	}

	if yamuxSession.IsClosed() {
		// Clean up stale entry
		yamuxSessions.Lock()
		if current := yamuxSessions.sessions[sessionID]; current == yamuxSession {
			delete(yamuxSessions.sessions, sessionID)
		}
		yamuxSessions.Unlock()
		return nil, fmt.Errorf("yamux session for %s is closed", sessionID)
	}

	return yamuxSession, nil
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
		slog.Debug("yamux session closed", "session_id", sessionID)
	}
}
