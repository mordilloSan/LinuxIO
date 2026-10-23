package config

import (
	"os"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

// Fixed paths and the config schema version.
const (
	// CurrentVersion is the only config schema version the daemon accepts.
	CurrentVersion = 1
	// DefaultPath is the YAML config file the systemd unit points the daemon at.
	DefaultPath = version.ConfigDir + "/monitoring/config.yaml"
	// DataDir holds metrics.db. The path is fixed: the packaged unit owns it.
	DataDir = version.DataDir + "/monitoring"
)

// Defaults for settings the YAML config can override.
const (
	// DefaultCollectorInterval is how often the collector samples every plugin.
	DefaultCollectorInterval = time.Minute
	// DefaultSmartRefreshInterval is how often SMART data is re-read.
	DefaultSmartRefreshInterval = time.Hour
	// DefaultDiskUsageCache re-reads usage on every collection; a longer value
	// keeps sleeping disks asleep.
	DefaultDiskUsageCache time.Duration = 0
	// DefaultHistoryRetention is how long full-resolution history is kept.
	DefaultHistoryRetention = 30 * 24 * time.Hour
	// DefaultListenAddress is what an empty listen value normalises to: local
	// only, never every interface.
	DefaultListenAddress = "127.0.0.1:45876"
)

// Security policy of the HTTP listeners. These are limits, not preferences:
// loosening one widens what an unauthenticated or world-connectable listener
// accepts.
const (
	// APISocketMode lets every local user read the metrics on api.sock.
	APISocketMode os.FileMode = 0o666
	// ControlSocketMode restricts control.sock, which also serves commands, to
	// root; the listener additionally rejects non-root peers.
	ControlSocketMode os.FileMode = 0o600
	// ListenerSocketMode applies to a configured unix listener that sets no mode.
	ListenerSocketMode os.FileMode = 0o660
	// MaxHeaderValueCount caps how many header values the API parses per
	// request. The API only ever sees small GETs and command POSTs, so this sits
	// far below net/http's default of 500.
	MaxHeaderValueCount = 64
	// ReadHeaderTimeout bounds how long a client may take to send its headers.
	ReadHeaderTimeout = 5 * time.Second
	// ShutdownTimeout bounds how long a listener drains in-flight requests.
	ShutdownTimeout = 5 * time.Second
	// APIRequestTimeout bounds a single API request.
	APIRequestTimeout = 5 * time.Second
	// MaxHistoryLimit is the largest history page a request may ask for.
	MaxHistoryLimit = 1000
)
