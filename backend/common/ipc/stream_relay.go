package ipc

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

// Stream opcodes for the relay protocol.
// These use 0x80+ to avoid conflict with framed protocol (0x01-0x03).
// The server only looks at opcode and streamID for routing - payload is opaque.
const (
	OpStreamOpen     byte = 0x80 // Open a new stream: [opcode][streamID][len][streamType + args]
	OpStreamData     byte = 0x81 // Data frame: [opcode][streamID][len][payload]
	OpStreamClose    byte = 0x82 // Close stream: [opcode][streamID][0x00 0x00 0x00 0x00]
	OpStreamResize   byte = 0x83 // Resize terminal: [opcode][streamID][4][cols:2][rows:2]
	OpStreamProgress byte = 0x84 // Progress update: [opcode][streamID][len][json progress]
	OpStreamResult   byte = 0x85 // Final result: [opcode][streamID][len][json result]
)

// IsStreamFrame returns true if the first byte indicates a StreamFrame protocol message.
func IsStreamFrame(firstByte byte) bool {
	return firstByte >= 0x80 && firstByte <= 0x8F
}

// Stream types for OpStreamOpen
const (
	StreamTypeTerminal  = "terminal"
	StreamTypeContainer = "container"
	// Filebrowser stream types
	StreamTypeFBDownload = "fb-download" // Single file download
	StreamTypeFBUpload   = "fb-upload"   // Single file upload
	StreamTypeFBArchive  = "fb-archive"  // Multi-file archive download
)

// StreamFrame represents a framed message for the relay protocol.
// Format: [opcode:1][streamID:4][length:4][payload:N]
type StreamFrame struct {
	Opcode   byte
	StreamID uint32
	Payload  []byte
}

// WriteRelayFrame writes a StreamFrame to the writer.
func WriteRelayFrame(w io.Writer, f *StreamFrame) error {
	header := make([]byte, 9)
	header[0] = f.Opcode
	binary.BigEndian.PutUint32(header[1:5], f.StreamID)
	binary.BigEndian.PutUint32(header[5:9], uint32(len(f.Payload)))

	if _, err := w.Write(header); err != nil {
		return fmt.Errorf("write header: %w", err)
	}
	if len(f.Payload) > 0 {
		if _, err := w.Write(f.Payload); err != nil {
			return fmt.Errorf("write payload: %w", err)
		}
	}
	return nil
}

// ReadRelayFrame reads a StreamFrame from the reader.
func ReadRelayFrame(r io.Reader) (*StreamFrame, error) {
	header := make([]byte, 9)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}

	f := &StreamFrame{
		Opcode:   header[0],
		StreamID: binary.BigEndian.Uint32(header[1:5]),
	}
	length := binary.BigEndian.Uint32(header[5:9])

	if length > 0 {
		// Cap at 16MB to match yamux MaxStreamWindowSize
		if length > 16*1024*1024 {
			return nil, fmt.Errorf("payload too large: %d bytes", length)
		}
		f.Payload = make([]byte, length)
		if _, err := io.ReadFull(r, f.Payload); err != nil {
			return nil, fmt.Errorf("read payload: %w", err)
		}
	}
	return f, nil
}

// StreamOpenPayload parses the payload of an OpStreamOpen frame.
// Format: streamType\0arg1\0arg2\0...
func ParseStreamOpenPayload(payload []byte) (streamType string, args []string) {
	if len(payload) == 0 {
		return "", nil
	}

	// Split by null bytes
	var parts []string
	start := 0
	for i, b := range payload {
		if b == 0 {
			parts = append(parts, string(payload[start:i]))
			start = i + 1
		}
	}
	// Last part (no trailing null)
	if start < len(payload) {
		parts = append(parts, string(payload[start:]))
	}

	if len(parts) == 0 {
		return "", nil
	}
	return parts[0], parts[1:]
}

// BuildStreamOpenPayload creates a payload for OpStreamOpen.
func BuildStreamOpenPayload(streamType string, args ...string) []byte {
	result := []byte(streamType)
	for _, arg := range args {
		result = append(result, 0)
		result = append(result, []byte(arg)...)
	}
	return result
}

// ProgressFrame represents a progress update for long-running operations.
// Used with OpStreamProgress (0x84).
type ProgressFrame struct {
	Bytes int64  `json:"bytes"`           // Bytes transferred so far
	Total int64  `json:"total"`           // Total bytes (0 if unknown)
	Pct   int    `json:"pct"`             // Percentage (0-100)
	Phase string `json:"phase,omitempty"` // Optional phase description
}

// ResultFrame represents the final result of an operation.
// Used with OpStreamResult (0x85).
type ResultFrame struct {
	Status string          `json:"status"`          // "ok" or "error"
	Error  string          `json:"error,omitempty"` // Error message if status is "error"
	Code   int             `json:"code,omitempty"`  // Optional error code
	Data   json.RawMessage `json:"data,omitempty"`  // Optional result data
}

// WriteProgressFrame writes a progress update to the stream.
func WriteProgressFrame(w io.Writer, streamID uint32, p *ProgressFrame) error {
	payload, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("marshal progress: %w", err)
	}
	return WriteRelayFrame(w, &StreamFrame{
		Opcode:   OpStreamProgress,
		StreamID: streamID,
		Payload:  payload,
	})
}

// WriteResultFrame writes a result frame to the stream.
func WriteResultFrame(w io.Writer, streamID uint32, r *ResultFrame) error {
	payload, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("marshal result: %w", err)
	}
	return WriteRelayFrame(w, &StreamFrame{
		Opcode:   OpStreamResult,
		StreamID: streamID,
		Payload:  payload,
	})
}

// WriteResultOK is a convenience function for writing a successful result.
func WriteResultOK(w io.Writer, streamID uint32, data any) error {
	var rawData json.RawMessage
	if data != nil {
		b, err := json.Marshal(data)
		if err != nil {
			return fmt.Errorf("marshal data: %w", err)
		}
		rawData = b
	}
	return WriteResultFrame(w, streamID, &ResultFrame{
		Status: "ok",
		Data:   rawData,
	})
}

// WriteResultError is a convenience function for writing an error result.
func WriteResultError(w io.Writer, streamID uint32, errMsg string, code int) error {
	return WriteResultFrame(w, streamID, &ResultFrame{
		Status: "error",
		Error:  errMsg,
		Code:   code,
	})
}

// WriteStreamClose sends a close frame for the stream.
func WriteStreamClose(w io.Writer, streamID uint32) error {
	return WriteRelayFrame(w, &StreamFrame{
		Opcode:   OpStreamClose,
		StreamID: streamID,
	})
}
