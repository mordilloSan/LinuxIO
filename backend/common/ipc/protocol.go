package ipc

// Request/Response are the on-the-wire schema used over the unix socket.
type Request struct {
	Type      string   `json:"type"`
	Command   string   `json:"command"`
	Args      []string `json:"args,omitempty"`
	Secret    string   `json:"secret"`
	SessionID string   `json:"session_id"`
}

type Response struct {
	Status string `json:"status"`           // "ok" | "error"
	Output any    `json:"output,omitempty"` // NOT json.RawMessage
	Error  string `json:"error,omitempty"`
}

// Optional helper signature for bridge-side handlers
type HandlerFunc func([]string) (any, error)
