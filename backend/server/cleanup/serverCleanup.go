package cleanup

import (
	"io"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/server/bridge"
	"github.com/mordilloSan/go_logger/logger"
)

// ShutdownAllBridges asks all active session bridges to exit.
// Non-blocking per session with a small timeout; best-effort.
func ShutdownAllBridges(sm *session.Manager, reason string) {
	sessions, err := sm.ActiveSessions()
	if err != nil || len(sessions) == 0 {
		return
	}
	logger.Infof("Shutting down %d bridge(s)...", len(sessions))

	for _, s := range sessions {
		// fire each shutdown in its own goroutine with a per-call timeout
		done := make(chan error, 1)

		go func(sess *session.Session) {
			done <- bridge.CallTypedWithSession(sess, "control", "shutdown", []string{reason}, nil)
		}(s)

		select {
		case e := <-done:
			if e == nil {
				logger.Debugf("Bridge shutdown confirmed")
			} else if e == io.EOF ||
				strings.Contains(e.Error(), "EOF") ||
				strings.Contains(e.Error(), "shutting down") {
				// Expected during graceful shutdown
				logger.Debugf("Bridge shutdown (expected): %v", e)
			} else {
				logger.Warnf("Bridge shutdown failed: %v", e)
			}
		case <-time.After(2 * time.Second):
			logger.Warnf("Bridge shutdown timed out")
		}
	}
}
