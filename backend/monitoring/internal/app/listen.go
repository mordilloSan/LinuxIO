package app

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ListenDisabled is the canonical listen value that turns off the HTTP API.
const ListenDisabled = "none"

// GetAddress normalizes a listen value: bare ports bind to 127.0.0.1,
// unix:/path (or an absolute path) selects a unix socket, and none/off/disabled
// turns the HTTP API off entirely.
func GetAddress(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "127.0.0.1:45876"
	}
	if IsListenDisabled(addr) {
		return ListenDisabled
	}
	if network, _ := SplitListenAddress(addr); network == "unix" {
		return addr
	}
	// A bare port stays local-only; use an explicit host (or ":port") to
	// listen on all interfaces.
	if !strings.Contains(addr, ":") {
		addr = "127.0.0.1:" + addr
	}
	return addr
}

// IsListenDisabled reports whether the listen value disables the HTTP API.
func IsListenDisabled(addr string) bool {
	switch strings.ToLower(strings.TrimSpace(addr)) {
	case "none", "off", "disabled":
		return true
	}
	return false
}

// SplitListenAddress returns the network and address to pass to net.Listen or
// a dialer. A unix: prefix or an absolute path selects a unix socket;
// everything else is TCP.
func SplitListenAddress(addr string) (network, address string) {
	if path, ok := strings.CutPrefix(addr, "unix:"); ok {
		return "unix", path
	}
	if strings.HasPrefix(addr, "/") {
		return "unix", addr
	}
	return "tcp", addr
}

// openListener opens the TCP or unix listener described by a normalized
// listen value. mode applies to unix sockets only; 0 restricts the socket to
// the owning user and group.
func openListener(addr string, mode os.FileMode) (net.Listener, error) {
	network, address := SplitListenAddress(addr)
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
			mode = 0o660
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
