package wireguard

import (
	"net"

	"github.com/mordilloSan/LinuxIO/internal/ipc"
)

// --- Constants ---
const (
	wgConfigDir      = "/etc/wireguard"
	configExt        = ".conf"
	defaultKeepalive = 25
	maxHostIP        = 254
	minHostIP        = 2
)

// --- Types ---
type InterfaceConfig struct {
	PrivateKey string           `json:"private_key"`
	Address    []string         `json:"address"`
	ListenPort int              `json:"listen_port"`
	DNS        []string         `json:"dns"`
	MTU        int              `json:"mtu"`
	Peers      []ipc.PeerConfig `json:"peers"`
}

type WireGuardInterfaceUI struct {
	Name        string `json:"name"`
	IsConnected string `json:"isConnected"`
	Address     string `json:"address"`
	Port        int    `json:"port"`
	PeerCount   int    `json:"peerCount"`
}

type PeerInfo struct {
	ipc.PeerConfig

	LastHandshake     string  `json:"last_handshake"`      // RFC3339 or "never"
	LastHandshakeUnix int64   `json:"last_handshake_unix"` // 0 if never
	RxBytes           int64   `json:"rx_bytes"`
	TxBytes           int64   `json:"tx_bytes"`
	RxBps             float64 `json:"rx_bps"` // bytes per second
	TxBps             float64 `json:"tx_bps"` // bytes per second
}

type ipManager struct {
	netBase    net.IP
	serverHost int
}
