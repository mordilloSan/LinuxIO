package utils

type User struct {
	ID   string // Username (unique key)
	Name string // Display name (can be same as ID)
}

type ManifestEntry struct {
	File string   `json:"file"`
	CSS  []string `json:"css"`
}

type ViteManifest map[string]struct {
	File string   `json:"file"`
	Css  []string `json:"css"`
}

type PeerConfig struct {
	PublicKey           string   `json:"public_key"`
	PresharedKey        string   `json:"preshared_key"`
	AllowedIPs          []string `json:"allowed_ips"`
	Endpoint            string   `json:"endpoint"`
	PersistentKeepalive int      `json:"persistent_keepalive"`
	PrivateKey          string   `json:"private_key"`
	Name                string   `json:"name,omitempty"`
}
