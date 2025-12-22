package ipc

import (
	"encoding/json"
	"errors"
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
	Output json.RawMessage `json:"output,omitempty"` // raw JSON payload
	Error  string          `json:"error,omitempty"`
}

var ErrEmptyBridgeOutput = errors.New("bridge returned empty output")

// HandlerFunc is the bridge handler signature.
type HandlerFunc func(args []string) (any, error)

// WrapSimpleHandler is a no-op wrapper for backwards compatibility.
// TODO: Remove this and update all callers to pass handlers directly.
func WrapSimpleHandler(h HandlerFunc) HandlerFunc {
	return h
}
