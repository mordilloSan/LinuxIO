package cleanup

import (
	"os"
	"time"

	"github.com/mordilloSan/LinuxIO/cmd/server/bridge"
	"github.com/mordilloSan/LinuxIO/internal/ipc"
	"github.com/mordilloSan/LinuxIO/internal/logger"
	"github.com/mordilloSan/LinuxIO/internal/session"
)

// ShutdownAllBridges asks all active session bridges to exit.
// Non-blocking per session with a small timeout; best-effort socket cleanup on failure.
func ShutdownAllBridges(reason string) {
	ids, err := session.GetActiveSessionIDs()
	if err != nil || len(ids) == 0 {
		return
	}
	logger.Infof("Shutting down %d bridge(s)...", len(ids))

	for _, id := range ids {
		sess, err := session.GetSession(id)
		if err != nil || sess == nil || sess.User.ID == "" {
			continue
		}

		done := make(chan error, 1)
		go func() {
			_, e := bridge.CallWithSession(sess, "control", "shutdown", []string{reason})
			done <- e
		}()

		select {
		case e := <-done:
			if e != nil {
				logger.Warnf("Bridge shutdown (session=%s) failed: %v", id, e)
				// Best-effort: remove stale socket
				if sock, pathErr := ipc.SocketPathFor(sess); pathErr == nil {
					_ = os.Remove(sock)
				}
			}
		case <-time.After(2 * time.Second):
			logger.Warnf("Bridge shutdown (session=%s) timed out", id)
			if sock, pathErr := ipc.SocketPathFor(sess); pathErr == nil {
				_ = os.Remove(sock)
			}
		}
	}
}
