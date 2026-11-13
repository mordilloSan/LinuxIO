package apischema

// WireGuardHookKind names one wg-quick [Interface] hook, and is also its key
// in the config file.
type WireGuardHookKind string

const (
	WireGuardPreUp    WireGuardHookKind = "PreUp"
	WireGuardPostUp   WireGuardHookKind = "PostUp"
	WireGuardPreDown  WireGuardHookKind = "PreDown"
	WireGuardPostDown WireGuardHookKind = "PostDown"
)

// WireGuardHookKinds is the one list of hook kinds, in the order they are
// written to the config file. Everything that handles hooks loops over it,
// and StringEnums publishes it as the WireGuardHookKind union.
var WireGuardHookKinds = []WireGuardHookKind{WireGuardPreUp, WireGuardPostUp, WireGuardPreDown, WireGuardPostDown}

// WireGuardHooks holds wg-quick hook command lines by kind.
type WireGuardHooks map[WireGuardHookKind][]string

// WireGuardInterface carries every hook kind in Hooks, with an empty list
// for a kind the interface does not use.
type WireGuardInterface struct {
	Address     string         `json:"address"`
	DNS         []string       `json:"dns"`
	Hooks       WireGuardHooks `json:"hooks"`
	Host        string         `json:"host"`
	IsConnected string         `json:"isConnected"`
	IsEnabled   bool           `json:"isEnabled"`
	MTU         int            `json:"mtu"`
	Name        string         `json:"name"`
	PeerCount   int            `json:"peerCount"`
	Port        int            `json:"port"`
}

type Peer struct {
	Address             string   `json:"address"`
	ClientAllowedIPs    []string `json:"client_allowed_ips"`
	DNS                 []string `json:"dns"`
	Enabled             bool     `json:"enabled"`
	Endpoint            *string  `json:"endpoint,omitzero"`
	ID                  string   `json:"id"`
	LastHandshake       *string  `json:"last_handshake,omitzero"`
	LastHandshakeUnix   *int64   `json:"last_handshake_unix,omitzero"`
	MTU                 int      `json:"mtu"`
	Name                string   `json:"name"`
	PersistentKeepalive *int     `json:"persistent_keepalive,omitzero"`
	PresharedKey        *string  `json:"preshared_key,omitzero"`
	PublicKey           string   `json:"public_key"`
	RXBPS               *float64 `json:"rx_bps,omitzero"`
	RXBytes             *int64   `json:"rx_bytes,omitzero"`
	ServerAllowedIPs    []string `json:"server_allowed_ips"`
	TXBPS               *float64 `json:"tx_bps,omitzero"`
	TXBytes             *int64   `json:"tx_bytes,omitzero"`
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
	Name                *string  `json:"name,omitzero"`
	Enabled             *bool    `json:"enabled,omitzero"`
	ClientAllowedIPs    []string `json:"clientAllowedIPs,omitempty"`
	ServerAllowedIPs    []string `json:"serverAllowedIPs,omitempty"`
	DNS                 []string `json:"dns,omitempty"`
	MTU                 *int     `json:"mtu,omitzero"`
	PersistentKeepalive *int     `json:"persistentKeepalive,omitzero"`
	PresharedKey        string   `json:"presharedKey,omitempty"`
	RegenerateKeys      bool     `json:"regenerateKeys,omitzero"`
}

// WireGuardUpdateInterfaceRequest is a partial update: absent fields are
// unchanged. An empty host clears the override, an empty dns list falls back
// to the gateway, MTU 0 removes the line. Hooks holds only the kinds to
// change: an absent or null kind is unchanged, an empty list removes that
// kind, blank lines are dropped, and a kind outside WireGuardHookKinds is
// refused.
type WireGuardUpdateInterfaceRequest struct {
	Name       string         `json:"name"`
	Host       *string        `json:"host,omitzero"`
	DNS        []string       `json:"dns,omitempty"`
	MTU        *int           `json:"mtu,omitzero"`
	ListenPort *int           `json:"listenPort,omitzero"`
	Hooks      WireGuardHooks `json:"hooks,omitempty"`
}

type WireGuardAddInterfaceRequest struct {
	Name       string  `json:"name"`
	Addresses  string  `json:"addresses"`
	ListenPort string  `json:"listenPort"`
	EgressNic  string  `json:"egressNic"`
	Host       *string `json:"host,omitzero"`
	DNS        *string `json:"dns,omitzero"`
	MTU        *string `json:"mtu,omitzero"`
	NumPeers   *string `json:"numPeers,omitzero"`
}
