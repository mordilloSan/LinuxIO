package ipc

import (
	"errors"
	"fmt"
)

// Streaming validation constants
const (
	MaxChunkSize = 512 * 1024 * 1024        // 512 MiB decoded
	MaxFileSize  = 100 * 1024 * 1024 * 1024 // 100 GiB

	// base64 overhead ~ 4/3. This computes the max encoded length that can decode to MaxChunkSize.
	MaxPayloadSize = ((MaxChunkSize + 2) / 3) * 4
)

// Request/Response are the on-the-wire schema used over the unix socket.
type Request struct {
	Type      string   `json:"type"`
	Command   string   `json:"command"`
	Args      []string `json:"args,omitempty"`
	Secret    string   `json:"secret"`
	SessionID string   `json:"session_id"`

	// Streaming fields for file upload/download
	RequestID string `json:"request_id,omitempty"` // Unique ID for tracking multi-chunk operations
	Offset    int64  `json:"offset,omitempty"`     // Byte offset for this chunk
	Total     int64  `json:"total,omitempty"`      // Total file size in bytes (0 = unknown)
	Payload   string `json:"payload,omitempty"`    // Base64-encoded chunk data
	Final     bool   `json:"final,omitempty"`      // True if this is the last chunk
}

type Response struct {
	Status    string `json:"status"`           // "ok" | "error"
	Output    any    `json:"output,omitempty"` // NOT json.RawMessage
	Error     string `json:"error,omitempty"`
	RequestID string `json:"request_id,omitempty"` // Echo back the request ID for correlation

	// Streaming fields for file download
	Payload string `json:"payload,omitempty"` // Base64-encoded chunk data
	Final   bool   `json:"final,omitempty"`   // True if this is the last chunk
}

// Optional helper signature for bridge-side handlers
type HandlerFunc func([]string) (any, error)

// StreamingHandlerFunc is for handlers that need access to the full Request
// (e.g., for file upload/download with chunking support)
type StreamingHandlerFunc func(*Request) (*Response, error)

// DecodedPayloadSizeApprox returns an upper bound for the decoded payload size
// without actually decoding. May overestimate by up to 2-3 bytes due to padding.
func (r *Request) DecodedPayloadSizeApprox() int {
	l := len(r.Payload)
	if l == 0 {
		return 0
	}
	// Base64: every 4 chars decode to 3 bytes (upper bound)
	return (l / 4) * 3
}

// HasStreamingFields reports whether any streaming-related fields are set.
func (r *Request) HasStreamingFields() bool {
	return r.RequestID != "" ||
		r.Offset != 0 ||
		r.Total != 0 ||
		r.Payload != "" ||
		r.Final
}

// ValidateStreaming validates streaming-specific fields for upload/download requests.
func (r *Request) ValidateStreaming() error {
	if r.RequestID == "" {
		return errors.New("streaming request requires request_id")
	}

	// Validate offset is non-negative
	if r.Offset < 0 {
		return fmt.Errorf("invalid offset: %d (must be >= 0)", r.Offset)
	}

	// Validate total size if specified (0 means unknown for some operations)
	if r.Total < 0 {
		return fmt.Errorf("invalid total: %d (must be >= 0)", r.Total)
	}

	// Validate total doesn't exceed max file size
	if r.Total > MaxFileSize {
		return fmt.Errorf("total size %d exceeds maximum allowed size %d", r.Total, MaxFileSize)
	}

	// Validate offset doesn't exceed total (if total is known)
	if r.Total > 0 && r.Offset > r.Total {
		return fmt.Errorf("offset %d exceeds total size %d", r.Offset, r.Total)
	}

	// Even if total is unknown, offset should be reasonable
	if r.Total == 0 && r.Offset > MaxFileSize {
		return fmt.Errorf("offset %d exceeds max file size (total unknown)", r.Offset)
	}

	// Validate encoded payload size (this implicitly bounds decoded size via MaxPayloadSize formula)
	encodedLen := len(r.Payload)
	if encodedLen > MaxPayloadSize {
		return fmt.Errorf("payload size %d exceeds maximum allowed size %d", encodedLen, MaxPayloadSize)
	}

	if encodedLen == 0 {
		return nil
	}

	// Structural base64 sanity: length must be a multiple of 4
	if encodedLen%4 != 0 {
		return fmt.Errorf("invalid base64 payload length %d (must be multiple of 4)", encodedLen)
	}

	// Cheap syntactic validation: check it's valid base64 alphabet.
	// We don't decode to avoid allocating large buffers twice;
	// the handler will decode and handle invalid base64 if present.
	for i := 0; i < encodedLen; i++ {
		c := r.Payload[i]
		// Valid base64 chars: A-Z, a-z, 0-9, +, /, =
		if !((c >= 'A' && c <= 'Z') ||
			(c >= 'a' && c <= 'z') ||
			(c >= '0' && c <= '9') ||
			c == '+' || c == '/' || c == '=') {
			return fmt.Errorf("invalid base64 payload: illegal character at position %d", i)
		}
	}

	// Validate Final flag consistency with data (best-effort)
	if r.Final && r.Total > 0 {
		decodedSize := r.DecodedPayloadSizeApprox()
		expectedEnd := r.Offset + int64(decodedSize)
		if expectedEnd < r.Total {
			return fmt.Errorf("final chunk at offset %d + size %d = %d, but total is %d",
				r.Offset, decodedSize, expectedEnd, r.Total)
		}
	}

	return nil
}

// ValidateBasic performs basic validation on non-streaming request fields.
func (r *Request) ValidateBasic() error {
	if r.Type == "" {
		return errors.New("request type is required")
	}
	if r.Command == "" {
		return errors.New("request command is required")
	}
	if r.Secret == "" {
		return errors.New("request secret is required")
	}
	if r.SessionID == "" {
		return errors.New("request session_id is required")
	}
	return nil
}

// Validate runs basic validation and, if any streaming fields are present,
// also validates streaming-specific constraints.
func (r *Request) Validate() error {
	if err := r.ValidateBasic(); err != nil {
		return err
	}
	if r.HasStreamingFields() {
		return r.ValidateStreaming()
	}
	return nil
}

// Validate validates a Response for correctness.
func (r *Response) Validate() error {
	if r.Status != "ok" && r.Status != "error" {
		return fmt.Errorf("invalid status: %s (must be 'ok' or 'error')", r.Status)
	}
	if r.Status == "error" && r.Error == "" {
		return errors.New("error status requires error message")
	}
	if len(r.Payload) > MaxPayloadSize {
		return fmt.Errorf("response payload size %d exceeds maximum %d", len(r.Payload), MaxPayloadSize)
	}
	return nil
}
