package cleanup

import (
	"fmt"
	"os"

	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/go_logger/logger"
)

// FullCleanup does all bridge-side cleanup for a session.
func FullCleanup(shutdownReason string, sess *session.Session) error {
	logger.Debugf("Shutdown initiated: %s", shutdownReason)
	if err := cleanupBridgeSocket(sess); err != nil {
		return fmt.Errorf("cleanup bridge socket: %w", err)
	}
	return nil
}

// cleanupBridgeSocket removes the bridge socket for the session (idempotent).
func cleanupBridgeSocket(sess *session.Session) error {
	sock := sess.SocketPath // <-- field, not method
	if sock == "" {
		logger.Warnf("No socket path set on session %s; nothing to remove", sess.SessionID)
		return nil
	}

	if err := os.Remove(sock); err == nil {
		logger.Infof("Removed bridge socket %s for session %s", sock, sess.SessionID)
		return nil
	} else if os.IsNotExist(err) {
		logger.Debugf("Bridge socket %s not found (already removed) for session %s", sock, sess.SessionID)
		return nil
	} else {
		logger.Warnf("Failed to remove bridge socket %s: %v", sock, err)
		return err
	}
}
