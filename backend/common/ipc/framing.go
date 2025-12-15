package ipc

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

// Message types for framed protocol
const (
	// MsgTypeJSON is standard JSON RPC (backward compatible)
	MsgTypeJSON = 0x01
	// MsgTypeBinary is raw binary data chunk
	MsgTypeBinary = 0x02
	// MsgTypeStream is streaming JSON message (one of many)
	MsgTypeStream = 0x03
)

// Frame represents a single message with type and payload
type Frame struct {
	Type    byte
	Payload []byte
}

// WriteFrame writes a framed message: [type:1][length:4][payload:length]
func WriteFrame(w io.Writer, msgType byte, payload []byte) error {
	// Write message type (1 byte)
	if _, err := w.Write([]byte{msgType}); err != nil {
		return fmt.Errorf("write frame type: %w", err)
	}

	// Write payload length (4 bytes, big endian)
	length := uint32(len(payload))
	if err := binary.Write(w, binary.BigEndian, length); err != nil {
		return fmt.Errorf("write frame length: %w", err)
	}

	// Write payload
	if len(payload) > 0 {
		if _, err := w.Write(payload); err != nil {
			return fmt.Errorf("write frame payload: %w", err)
		}
	}

	return nil
}

// ReadFrame reads a framed message
func ReadFrame(r io.Reader) (*Frame, error) {
	// Read message type (1 byte)
	typeBuf := make([]byte, 1)
	if _, err := io.ReadFull(r, typeBuf); err != nil {
		return nil, fmt.Errorf("read frame type: %w", err)
	}

	// Read payload length (4 bytes)
	var length uint32
	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return nil, fmt.Errorf("read frame length: %w", err)
	}

	// Sanity check (prevent huge allocations)
	const maxFrameSize = 1024 * 1024 * 1024 // 1GB max per frame
	if length > maxFrameSize {
		return nil, fmt.Errorf("frame too large: %d bytes (max %d)", length, maxFrameSize)
	}

	// Read payload
	payload := make([]byte, length)
	if length > 0 {
		if _, err := io.ReadFull(r, payload); err != nil {
			return nil, fmt.Errorf("read frame payload: %w", err)
		}
	}

	return &Frame{
		Type:    typeBuf[0],
		Payload: payload,
	}, nil
}

// WriteJSONFrame is a helper to write JSON-encoded data as a frame
func WriteJSONFrame(w io.Writer, msgType byte, v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal JSON: %w", err)
	}
	return WriteFrame(w, msgType, payload)
}

// WriteRequestFrame writes a Request as a JSON frame
func WriteRequestFrame(w io.Writer, req *Request) error {
	return WriteJSONFrame(w, MsgTypeJSON, req)
}

// WriteResponseFrame writes a Response as a JSON frame
func WriteResponseFrame(w io.Writer, resp *Response) error {
	return WriteJSONFrame(w, MsgTypeJSON, resp)
}

// WriteStreamFrame writes a streaming message (partial response)
func WriteStreamFrame(w io.Writer, resp *Response) error {
	return WriteJSONFrame(w, MsgTypeStream, resp)
}

// WriteBinaryFrame writes raw binary data
func WriteBinaryFrame(w io.Writer, data []byte) error {
	return WriteFrame(w, MsgTypeBinary, data)
}

// ReadJSONFrame reads a frame and unmarshals JSON payload
func ReadJSONFrame(r io.Reader, v any) (msgType byte, err error) {
	frame, err := ReadFrame(r)
	if err != nil {
		return 0, err
	}

	if err := json.Unmarshal(frame.Payload, v); err != nil {
		return 0, fmt.Errorf("unmarshal frame JSON: %w", err)
	}

	return frame.Type, nil
}
