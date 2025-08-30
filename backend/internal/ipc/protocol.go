package ipc

import (
	"encoding/json"
	"os/exec"
	"time"
)

// Request/Response are the on-the-wire schema used over the unix socket.
type Request struct {
	Type      string   `json:"type"`
	Command   string   `json:"command"`
	Args      []string `json:"args,omitempty"`
	Secret    string   `json:"secret"`
	SessionID string   `json:"session_id"`
}

type Response struct {
	Status string          `json:"status"`           // "ok" | "error"
	Output json.RawMessage `json:"output,omitempty"` // JSON payload
	Error  string          `json:"error,omitempty"`
}

// Optional helper signature for bridge-side handlers
type HandlerFunc func([]string) (any, error)

// BridgeProcess tracks a running bridge subprocess.
type BridgeProcess struct {
	Cmd       *exec.Cmd
	SessionID string
	StartedAt time.Time
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
