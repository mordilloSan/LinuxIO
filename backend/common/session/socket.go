package session

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
)

const (
	socketPrefix = "linuxio-bridge-"
	socketSalt   = "linuxio-sock:"
)

// RuntimeDir returns /run/user/<uid> or /tmp/linuxio-run/<uid> (no side effects).
func (s *Session) RuntimeDir() string {
	base := filepath.Join("/run/linuxio", s.User.UID)
	if st, err := os.Stat(base); err == nil && st.IsDir() {
		return base
	}
	return filepath.Join(os.TempDir(), "linuxio-run", s.User.UID)
}

func (s *Session) SocketName() string {
	sum := sha256.Sum256([]byte(socketSalt + s.BridgeSecret))
	return socketPrefix + hex.EncodeToString(sum[:12]) + ".sock"
}

// SocketPath is pure: RuntimeDir + SocketName (no mkdir/chown).
func (s *Session) SocketPath() string {
	return filepath.Join(s.RuntimeDir(), s.SocketName())
}
