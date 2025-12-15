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
var ErrResponseAlreadySent = errors.New("response already sent")

// Optional helper signature for bridge-side handlers
// HandlerFunc is the bridge handler signature. ctx will be nil for
// legacy (non-framed) clients that do not support streaming updates.
type HandlerFunc func(ctx *RequestContext, args []string) (any, error)

// SimpleHandler is the legacy signature used by most handlers.
type SimpleHandler func([]string) (any, error)

// WrapSimpleHandler adapts a SimpleHandler into a context-aware handler.
func WrapSimpleHandler(h SimpleHandler) HandlerFunc {
	return func(_ *RequestContext, args []string) (any, error) {
		return h(args)
	}
}
