package ipc

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Stream opcodes for the relay protocol.
// These use 0x80+ to avoid conflict with framed protocol (0x01-0x03).
// The server only looks at opcode and streamID for routing - payload is opaque.
const (
	OpStreamOpen   byte = 0x80 // Open a new stream: [opcode][streamID][len][streamType + args]
	OpStreamData   byte = 0x81 // Data frame: [opcode][streamID][len][payload]
	OpStreamClose  byte = 0x82 // Close stream: [opcode][streamID][0x00 0x00 0x00 0x00]
	OpStreamResize byte = 0x83 // Resize terminal: [opcode][streamID][4][cols:2][rows:2]
)

// IsStreamFrame returns true if the first byte indicates a StreamFrame protocol message.
func IsStreamFrame(firstByte byte) bool {
	return firstByte >= 0x80 && firstByte <= 0x8F
}

// Stream types for OpStreamOpen
const (
	StreamTypeTerminal  = "terminal"
	StreamTypeContainer = "container"
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
		// Cap at 1MB to prevent memory exhaustion
		if length > 1024*1024 {
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
