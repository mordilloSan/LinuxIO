package apischema

type WireGuardInterface struct {
	Address     string   `json:"address"`
	DNS         []string `json:"dns"`
	Host        string   `json:"host"`
	IsConnected string   `json:"isConnected"`
	IsEnabled   bool     `json:"isEnabled"`
	MTU         int      `json:"mtu"`
	Name        string   `json:"name"`
	PeerCount   int      `json:"peerCount"`
	Port        int      `json:"port"`
	PostDown    []string `json:"postDown"`
	PostUp      []string `json:"postUp"`
	PreDown     []string `json:"preDown"`
	PreUp       []string `json:"preUp"`
}

type Peer struct {
	Address             string   `json:"address"`
	ClientAllowedIPs    []string `json:"client_allowed_ips"`
	DNS                 []string `json:"dns"`
	Enabled             bool     `json:"enabled"`
	Endpoint            *string  `json:"endpoint,omitempty"`
	ID                  string   `json:"id"`
	LastHandshake       *string  `json:"last_handshake,omitempty"`
	LastHandshakeUnix   *int64   `json:"last_handshake_unix,omitempty"`
	MTU                 int      `json:"mtu"`
	Name                string   `json:"name"`
	PersistentKeepalive *int     `json:"persistent_keepalive,omitempty"`
	PresharedKey        *string  `json:"preshared_key,omitempty"`
	PublicKey           string   `json:"public_key"`
	RXBPS               *float64 `json:"rx_bps,omitempty"`
	RXBytes             *int64   `json:"rx_bytes,omitempty"`
	ServerAllowedIPs    []string `json:"server_allowed_ips"`
	TXBPS               *float64 `json:"tx_bps,omitempty"`
	TXBytes             *int64   `json:"tx_bytes,omitempty"`
}

type PeerConfigDownload struct {
	Content  string `json:"content"`
	Filename string `json:"filename"`
}

type QRCodeResponse struct {
	QRCode string `json:"qrcode"`
}

type InterfaceNameRequest struct {
	InterfaceName string `json:"interfaceName"`
}

type WireGuardPeerRequest struct {
	InterfaceName string `json:"interfaceName"`
	PeerID        string `json:"peerId"`
}

type WireGuardAddPeerRequest struct {
	InterfaceName string `json:"interfaceName"`
	Name          string `json:"name,omitempty"`
}

// WireGuardUpdatePeerRequest is a partial update: absent fields are unchanged.
// Empty lists reset to the default (full tunnel for client routes, the
// interface DNS for dns, nothing for server-side networks). PresharedKey is
// an action, "generate" or "remove".
type WireGuardUpdatePeerRequest struct {
	InterfaceName       string   `json:"interfaceName"`
	PeerID              string   `json:"peerId"`
	Name                *string  `json:"name,omitempty"`
	Enabled             *bool    `json:"enabled,omitempty"`
	ClientAllowedIPs    []string `json:"clientAllowedIPs,omitempty"`
	ServerAllowedIPs    []string `json:"serverAllowedIPs,omitempty"`
	DNS                 []string `json:"dns,omitempty"`
	MTU                 *int     `json:"mtu,omitempty"`
	PersistentKeepalive *int     `json:"persistentKeepalive,omitempty"`
	PresharedKey        string   `json:"presharedKey,omitempty"`
	RegenerateKeys      bool     `json:"regenerateKeys,omitempty"`
}

// WireGuardUpdateInterfaceRequest is a partial update: absent fields are
// unchanged. An empty host clears the override, an empty dns list falls back
// to the gateway, MTU 0 removes the line, an empty hook list removes that
// hook kind.
type WireGuardUpdateInterfaceRequest struct {
	Name       string   `json:"name"`
	Host       *string  `json:"host,omitempty"`
	DNS        []string `json:"dns,omitempty"`
	MTU        *int     `json:"mtu,omitempty"`
	ListenPort *int     `json:"listenPort,omitempty"`
	PreUp      []string `json:"preUp,omitempty"`
	PostUp     []string `json:"postUp,omitempty"`
	PreDown    []string `json:"preDown,omitempty"`
	PostDown   []string `json:"postDown,omitempty"`
}

type WireGuardAddInterfaceRequest struct {
	Name       string  `json:"name"`
	Addresses  string  `json:"addresses"`
	ListenPort string  `json:"listenPort"`
	EgressNic  string  `json:"egressNic"`
	Host       *string `json:"host,omitempty"`
	DNS        *string `json:"dns,omitempty"`
	MTU        *string `json:"mtu,omitempty"`
	NumPeers   *string `json:"numPeers,omitempty"`
}
