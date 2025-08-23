package types

import (
	"encoding/json"
	"os/exec"
	"time"
)

// HandlerFunc is the signature for bridge command handler functions.
type HandlerFunc func([]string) (any, error)

// BridgeProcess tracks a running bridge subprocess.
type BridgeProcess struct {
	Cmd       *exec.Cmd
	SessionID string
	StartedAt time.Time
}

// BridgeRequest is the standard JSON request sent to the bridge.
type BridgeRequest struct {
	Type    string   `json:"type"`
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
	Secret  string   `json:"secret"`
}

// BridgeResponse is the *universal* response format for bridge and helpers.
// Output is always JSON-encoded and ready for a second unmarshal.
type BridgeResponse struct {
	Status string          `json:"status"`           // "ok" or "error"
	Output json.RawMessage `json:"output,omitempty"` // actual command output, always marshaled JSON
	Error  string          `json:"error,omitempty"`  // error string if any
}
