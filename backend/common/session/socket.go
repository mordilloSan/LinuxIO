package session

import (
	"fmt"
	"path/filepath"
)

const RuntimeBase = "/run/linuxio"

func (s *Session) SocketPath() string {
	name := fmt.Sprintf("linuxio-bridge-%s.sock", s.SessionID[:24])
	return filepath.Join(RuntimeBase, s.User.UID, name)
}
