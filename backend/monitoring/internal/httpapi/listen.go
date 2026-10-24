package httpapi

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/config"
)

// OpenListener opens the TCP or unix listener described by a normalized
// listen value. mode applies to unix sockets only; 0 restricts the socket to
// the owning user and group.
func OpenListener(addr string, mode os.FileMode) (net.Listener, error) {
	network, address := config.SplitListenAddress(addr)
	if network == "unix" {
		if err := prepareUnixSocket(address); err != nil {
			return nil, err
		}
	}
	listener, err := net.Listen(network, address)
	if err != nil {
		return nil, err
	}
	if network == "unix" {
		if mode == 0 {
			mode = config.ListenerSocketMode
		}
		if err := os.Chmod(address, mode); err != nil {
			_ = listener.Close()
			return nil, err
		}
	}
	return listener, nil
}

// prepareUnixSocket clears the way for net.Listen: it creates the parent
// directory and removes a socket file left behind by an unclean shutdown.
// A socket that still accepts connections belongs to a live agent, so it is
// left alone and the caller gets an error instead of stealing the address.
func prepareUnixSocket(path string) error {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return os.MkdirAll(filepath.Dir(path), 0o755)
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("listen path %s exists and is not a socket", path)
	}
	conn, err := net.DialTimeout("unix", path, time.Second)
	if err == nil {
		_ = conn.Close()
		return fmt.Errorf("socket %s is already in use by another process", path)
	}
	return os.Remove(path)
}
