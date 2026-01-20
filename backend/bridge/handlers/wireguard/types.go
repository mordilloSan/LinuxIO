package wireguard

import (
	"net"
)

// --- Constants ---
const (
	wgConfigDir      = "/etc/wireguard"
	configExt        = ".conf"
	defaultKeepalive = 25
	minHostOffset    = 2 // Peers start at host offset 2 (offset 1 is reserved for server)
)

// --- Types ---

type PeerConfig struct {
	PublicKey           string   `json:"public_key"`
	PresharedKey        string   `json:"preshared_key"`
	AllowedIPs          []string `json:"allowed_ips"`
	Endpoint            string   `json:"endpoint"`
	PersistentKeepalive int      `json:"persistent_keepalive"`
	PrivateKey          string   `json:"private_key"`
	Name                string   `json:"name,omitempty"`
}

type InterfaceConfig struct {
	PrivateKey string       `json:"private_key"`
	Address    []string     `json:"address"`
	ListenPort int          `json:"listen_port"`
	DNS        []string     `json:"dns"`
	MTU        int          `json:"mtu"`
	Peers      []PeerConfig `json:"peers"`
}

type WireGuardInterfaceUI struct {
	Name        string `json:"name"`
	IsConnected string `json:"isConnected"`
	Address     string `json:"address"`
	Port        int    `json:"port"`
	PeerCount   int    `json:"peerCount"`
	IsEnabled   bool   `json:"isEnabled"`
}

type PeerInfo struct {
	PeerConfig

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
	maskBits   int // subnet mask size (e.g., 24 for /24)
	maxHost    int // maximum host number based on mask
}

// NATConfig stores NAT configuration for cleanup purposes
type NATConfig struct {
	EgressNic string `json:"egress_nic"`
	Subnet    string `json:"subnet"`
}
