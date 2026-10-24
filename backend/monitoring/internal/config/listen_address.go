package config

import (
	"strings"
)

// ListenDisabled is the canonical listen value that turns off the HTTP API.
const ListenDisabled = "none"

// GetAddress normalizes a listen value: bare ports bind to 127.0.0.1,
// unix:/path (or an absolute path) selects a unix socket, and none/off/disabled
// turns the HTTP API off entirely.
func GetAddress(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return DefaultListenAddress
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
