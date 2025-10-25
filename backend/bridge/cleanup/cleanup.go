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
		logger.WarnKV("bridge socket missing", "reason", "empty path")
		return nil
	}

	if err := os.Remove(sock); err == nil {
		logger.InfoKV("bridge socket removed", "socket_path", sock)
		return nil
	} else if os.IsNotExist(err) {
		logger.DebugKV("bridge socket already removed", "socket_path", sock)
		return nil
	}

	logger.WarnKV("bridge socket remove failed", "socket_path", sock, "error", err)
	return err
}
