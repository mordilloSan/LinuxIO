package cleanup

import (
	"github.com/mordilloSan/go_logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// FullCleanup does all bridge-side cleanup for a session.
// With single-socket architecture, there's no per-session socket file to clean up.
func FullCleanup(shutdownReason string, sess *session.Session) error {
	logger.Debugf("Shutdown initiated: %s (user=%s, session=%s)",
		shutdownReason, sess.User.Username, sess.SessionID)
	return nil
}
