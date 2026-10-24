package wireguard

const (
	configExt        = ".conf"
	defaultKeepalive = 25
	minHostOffset    = 2 // Peers start at host offset 2 (offset 1 is reserved for server)
)

var wgConfigDir = "/etc/wireguard"

type PeerConfig struct {
	PublicKey           string   `json:"public_key"`
	PresharedKey        string   `json:"preshared_key"`
	AllowedIPs          []string `json:"allowed_ips"`
	Endpoint            string   `json:"endpoint"`
	PersistentKeepalive int      `json:"persistent_keepalive"`
	PrivateKey          string   `json:"private_key"`
	Name                string   `json:"name,omitempty"`
}

// WireGuardConfig is one server config. Host and DNS are LinuxIO metadata
// kept as "# linuxio-host:" and "# linuxio-dns:" comment lines above
// [Interface]: they describe what exported client configs get and are never
// wg-quick keys, since a DNS key there would change the server's resolver.
type WireGuardConfig struct {
	PrivateKey string       `json:"private_key"`
	Address    []string     `json:"address"`
	ListenPort int          `json:"listen_port"`
	Host       string       `json:"host"`
	DNS        []string     `json:"dns"`
	MTU        int          `json:"mtu"`
	PreUp      []string     `json:"pre_up"`
	PostUp     []string     `json:"post_up"`
	PreDown    []string     `json:"pre_down"`
	PostDown   []string     `json:"post_down"`
	Peers      []PeerConfig `json:"peers"`
	// Extra holds [Interface] keys LinuxIO does not model (Table, FwMark,
	// SaveConfig, DNS, ...), in file order, so rewrites keep them.
	Extra [][2]string `json:"-"`
}

type PeerInfo struct {
	peerFile

	PublicKey         string  `json:"public_key"`
	LastHandshake     string  `json:"last_handshake"`      // RFC3339 or "never"
	LastHandshakeUnix int64   `json:"last_handshake_unix"` // 0 if never
	RxBytes           int64   `json:"rx_bytes"`
	TxBytes           int64   `json:"tx_bytes"`
	RxBps             float64 `json:"rx_bps"` // bytes per second
	TxBps             float64 `json:"tx_bps"` // bytes per second
	runtimeStatsKnown bool
}
