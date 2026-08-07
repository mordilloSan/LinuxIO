package cmd

import (
	"io"
	"log/slog"
	"os"
	"sync"

	authipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/auth"
)

// startupStatusFD is the inherited socket the auth launcher uses for the
// bridge startup handoff. Keep in sync with STARTUP_STATUS_FD in
// backend/auth/linuxio-auth.c.
const startupStatusFD = 4

// startupStatus reports bridge readiness to the auth launcher. The zero value
// is inert: with an old launcher (no ReadyAck bootstrap flag) the fd was
// closed at exec and must never be written.
type startupStatus struct {
	enabled bool
	f       *os.File
	once    sync.Once
}

func newStartupStatus(enabled bool) *startupStatus {
	if !enabled {
		return &startupStatus{}
	}
	return &startupStatus{
		enabled: true,
		f:       os.NewFile(startupStatusFD, "startup-status"),
	}
}

// ready tells the launcher that bridge initialization is complete, then waits
// for permission to start yamux. This keeps yamux from writing transport bytes
// to the client socket before the launcher has written the auth response.
// Disabled status (from an old launcher) proceeds immediately.
func (s *startupStatus) ready() bool {
	if s == nil || !s.enabled {
		return true
	}

	proceed := false
	s.once.Do(func() {
		if s.f == nil {
			return
		}
		if _, err := s.f.Write([]byte{authipc.ProtoStartupReady}); err != nil {
			slog.Warn("startup-status ready write failed", "error", err)
			s.close()
			return
		}

		var response [1]byte
		if _, err := io.ReadFull(s.f, response[:]); err != nil {
			slog.Warn("startup-status go read failed", "error", err)
			s.close()
			return
		}
		if response[0] != authipc.ProtoStartupGo {
			slog.Warn("startup-status received invalid go byte", "value", response[0])
			s.close()
			return
		}

		proceed = true
		s.close()
	})
	return proceed
}

// fail reports a fatal startup error with a short message to surface in the
// login response. No-op after ready or a previous fail.
func (s *startupStatus) fail(msg string) {
	if len(msg) > authipc.MaxStartupErrorLen {
		msg = msg[:authipc.MaxStartupErrorLen]
	}
	if s == nil || !s.enabled {
		return
	}
	s.once.Do(func() {
		if s.f == nil {
			return
		}
		if _, err := s.f.Write(append([]byte{authipc.ProtoStartupError}, msg...)); err != nil {
			slog.Warn("startup-status write failed", "error", err)
		}
		s.close()
	})
}

func (s *startupStatus) close() {
	if err := s.f.Close(); err != nil {
		slog.Debug("startup-status close failed", "error", err)
	}
	s.f = nil
}
