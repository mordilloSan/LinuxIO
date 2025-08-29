package ipc

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"sync"

	"github.com/mordilloSan/LinuxIO/internal/session"
)

const (
	socketPrefix = "linuxio-bridge-"
	socketSalt   = "linuxio-sock:"
)

var uidCache sync.Map // username -> uid string

// SocketPathFor builds the per-session bridge socket path for the given user.
// Prefers /run/user/<uid>/ … falls back to $TMP/linuxio-run/<uid>/ (mkdir 0700).
func SocketPathFor(sess *session.Session) (string, error) {
	uid, err := lookupUIDCached(sess.User.ID)
	if err != nil {
		return "", fmt.Errorf("lookup user %q: %w", sess.User.ID, err)
	}

	dir := preferredRuntimeDir(uid)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("ensure runtime dir %q: %w", dir, err)
	}
	return filepath.Join(dir, socketNameFromSecret(sess.BridgeSecret)), nil
}

func preferredRuntimeDir(uid string) string {
	base := filepath.Join("/run/user", uid)
	if st, err := os.Stat(base); err == nil && st.IsDir() {
		return base
	}
	return filepath.Join(os.TempDir(), "linuxio-run", uid)
}

func socketNameFromSecret(secret string) string {
	// Short, opaque, stable; stays well under AF_UNIX path limits.
	sum := sha256.Sum256([]byte(socketSalt + secret))
	return socketPrefix + hex.EncodeToString(sum[:12]) + ".sock"
}

func lookupUIDCached(username string) (string, error) {
	if v, ok := uidCache.Load(username); ok {
		return v.(string), nil
	}
	u, err := user.Lookup(username)
	if err != nil {
		return "", err
	}
	uid := u.Uid
	uidCache.Store(username, uid)
	return uid, nil
}
