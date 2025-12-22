package ipc

import (
	"encoding/json"
	"errors"
)

// Request/Response are the on-the-wire schema used over the unix socket.
type Request struct {
	Type      string
	Command   string
	Args      []string
	Secret    string
	SessionID string
}

type Response struct {
	Status string          // "ok" | "error"
	Output json.RawMessage // handler result as raw JSON (avoids double-encoding)
	Error  string
}

var ErrEmptyBridgeOutput = errors.New("bridge returned empty output")

// HandlerFunc is the bridge handler signature.
type HandlerFunc func(args []string) (any, error)
