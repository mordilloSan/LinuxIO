package relay

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
)

// Common errors for stream operations.
var (
	ErrAborted           = errors.New("operation aborted")
	ErrNotFound          = errors.New("not found")
	ErrPermissionDenied  = errors.New("permission denied")
	ErrInvalidRequest    = errors.New("invalid request")
	ErrUnsupportedFormat = errors.New("unsupported format")
	ErrAlreadyExists     = errors.New("already exists")
	ErrIsDirectory       = errors.New("is a directory")
	ErrNotDirectory      = errors.New("not a directory")
	ErrConnectionClosed  = errors.New("connection closed")
	ErrTimeout           = errors.New("operation timed out")
)

// Stream opcodes for the binary relay protocol.
// Frame format: [opcode:1][streamID:4][length:4][payload:N]
const (
	OpStreamOpen     byte = 0x80 // Open stream: payload = JSON StreamOpenEnvelope
	OpStreamData     byte = 0x81 // Binary data: payload = raw bytes
	OpStreamClose    byte = 0x82 // Close stream: payload = empty
	OpStreamResize   byte = 0x83 // Terminal resize: payload = [cols:2][rows:2]
	OpStreamProgress byte = 0x84 // Progress update: payload = handler-defined JSON
	OpStreamResult   byte = 0x85 // Final result: payload = JSON ResultFrame
	OpStreamAbort    byte = 0x86 // Abort operation: client requests cancellation
)

// maxRelayPayloadSize is the maximum allowed payload for a single relay frame.
// Matches the cap enforced by ReadRelayFrame (16 MiB).
const maxRelayPayloadSize = 16 * 1024 * 1024
const relayFrameHeaderSize = 9
const firstFrameReadChunkSize = 32 * 1024

// Shared journald field names. Keep in sync with backend/auth/linuxio_protocol.h.
const JournalFieldSessionID = "LINUXIO_SESSION_ID"

// StreamFrame represents a framed message for the relay protocol.
// Format: [opcode:1][streamID:4][length:4][payload:N]
type StreamFrame struct {
	Opcode   byte
	StreamID uint32
	Payload  []byte
}

// StreamFrameHeader is the fixed-size portion of a relay frame. PayloadLength
// is validated against the protocol limit by ReadRelayFrameHeader.
type StreamFrameHeader struct {
	Opcode        byte
	StreamID      uint32
	PayloadLength uint32
}

// checkPayloadSize validates that a payload length is within supported bounds.
// It enforces the protocol-level maximum payload size for a single relay frame.
func checkPayloadSize(payload []byte) (int, error) {
	payloadLen := len(payload)
	if payloadLen > maxRelayPayloadSize {
		return 0, fmt.Errorf("write frame: payload too large (%d bytes)", payloadLen)
	}
	return payloadLen, nil
}

// WriteRelayFrame writes a StreamFrame to the writer in a single write call.
// This avoids interleaving frame headers and payloads when multiple goroutines
// share the same writer.
func WriteRelayFrame(w io.Writer, f *StreamFrame) error {
	payloadLen, err := checkPayloadSize(f.Payload)
	if err != nil {
		return err
	}
	frameLen := uint64(relayFrameHeaderSize) + uint64(payloadLen)
	if frameLen > uint64(math.MaxInt) {
		return fmt.Errorf("write frame: payload size causes integer overflow")
	}
	frame := make([]byte, int(frameLen))
	frame[0] = f.Opcode
	binary.BigEndian.PutUint32(frame[1:5], f.StreamID)
	binary.BigEndian.PutUint32(frame[5:9], uint32(payloadLen))
	copy(frame[9:], f.Payload)

	n, err := w.Write(frame)
	if err != nil {
		return fmt.Errorf("write frame: %w", err)
	}
	if n != len(frame) {
		return fmt.Errorf("write frame: %w", io.ErrShortWrite)
	}
	return nil
}

// ReadRelayFrameHeader reads and validates a frame header without consuming its
// payload. Callers can then stream large payloads instead of allocating them.
func ReadRelayFrameHeader(r io.Reader) (StreamFrameHeader, error) {
	var raw [relayFrameHeaderSize]byte
	if _, err := io.ReadFull(r, raw[:]); err != nil {
		return StreamFrameHeader{}, fmt.Errorf("read header: %w", err)
	}

	header := StreamFrameHeader{
		Opcode:        raw[0],
		StreamID:      binary.BigEndian.Uint32(raw[1:5]),
		PayloadLength: binary.BigEndian.Uint32(raw[5:9]),
	}
	if header.PayloadLength > maxRelayPayloadSize {
		return StreamFrameHeader{}, fmt.Errorf("payload too large: %d bytes", header.PayloadLength)
	}
	return header, nil
}

// ReadRelayFramePayload reads the payload described by a previously validated
// header and returns the complete frame.
func ReadRelayFramePayload(r io.Reader, header StreamFrameHeader) (*StreamFrame, error) {
	if header.PayloadLength > maxRelayPayloadSize {
		return nil, fmt.Errorf("payload too large: %d bytes", header.PayloadLength)
	}
	f := &StreamFrame{Opcode: header.Opcode, StreamID: header.StreamID}
	if header.PayloadLength == 0 {
		return f, nil
	}
	f.Payload = make([]byte, header.PayloadLength)
	if _, err := io.ReadFull(r, f.Payload); err != nil {
		return nil, fmt.Errorf("read payload: %w", err)
	}
	return f, nil
}

// ReadRelayFrame reads a complete StreamFrame from the reader.
func ReadRelayFrame(r io.Reader) (*StreamFrame, error) {
	header, err := ReadRelayFrameHeader(r)
	if err != nil {
		return nil, err
	}
	return ReadRelayFramePayload(r, header)
}

// ReadRelayFrameProgressive reads a frame while growing the payload only as
// bytes arrive. It is intended for the first frame on an untrusted stream,
// where a large declared length must not cause an immediate allocation.
func ReadRelayFrameProgressive(r io.Reader) (*StreamFrame, error) {
	header, err := ReadRelayFrameHeader(r)
	if err != nil {
		return nil, err
	}
	f := &StreamFrame{Opcode: header.Opcode, StreamID: header.StreamID}
	if header.PayloadLength == 0 {
		return f, nil
	}
	remaining := int(header.PayloadLength)
	for remaining > 0 {
		chunkLen := min(remaining, firstFrameReadChunkSize)
		chunk := make([]byte, chunkLen)
		if _, err := io.ReadFull(r, chunk); err != nil {
			return nil, fmt.Errorf("read payload: %w", err)
		}
		f.Payload = append(f.Payload, chunk...)
		remaining -= chunkLen
	}
	return f, nil
}

// StreamOpenEnvelope is the JSON payload for an OpStreamOpen frame.
type StreamOpenEnvelope struct {
	Route   string          `json:"route"`
	Request json.RawMessage `json:"request"`
}

// ParseStreamOpenPayload parses the payload of an OpStreamOpen frame.
func ParseStreamOpenPayload(payload []byte) (StreamOpenEnvelope, error) {
	if len(payload) == 0 {
		return StreamOpenEnvelope{}, fmt.Errorf("%w: empty stream open payload", ErrInvalidRequest)
	}

	var envelope StreamOpenEnvelope
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return StreamOpenEnvelope{}, fmt.Errorf("%w: decode stream open payload: %v", ErrInvalidRequest, err)
	}
	if envelope.Route == "" {
		return StreamOpenEnvelope{}, fmt.Errorf("%w: missing route", ErrInvalidRequest)
	}
	if len(envelope.Request) == 0 || string(envelope.Request) == "null" {
		envelope.Request = json.RawMessage("{}")
	}
	return envelope, nil
}

// ResultFrame represents the final result of an operation.
// Used with OpStreamResult (0x85).
type ResultFrame struct {
	Status string          `json:"status"`          // "ok" or "error"
	Error  string          `json:"error,omitempty"` // Error message if status is "error"
	Code   int             `json:"code,omitempty"`  // Optional error code
	Data   json.RawMessage `json:"data,omitempty"`  // Optional result data
}

// WriteProgress writes a progress update to the stream.
// The data parameter can be any JSON-serializable struct defined by the handler.
func WriteProgress(w io.Writer, streamID uint32, data any) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal progress: %w", err)
	}
	if _, err := checkPayloadSize(payload); err != nil {
		return fmt.Errorf("progress payload invalid: %w", err)
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
	if _, err := checkPayloadSize(payload); err != nil {
		return fmt.Errorf("result payload invalid: %w", err)
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

// WriteResultOKAndClose writes a successful result and then closes the stream.
func WriteResultOKAndClose(w io.Writer, streamID uint32, data any) error {
	if err := WriteResultOK(w, streamID, data); err != nil {
		return err
	}
	return WriteStreamClose(w, streamID)
}

// WriteResultErrorAndClose writes an error result and then closes the stream.
func WriteResultErrorAndClose(w io.Writer, streamID uint32, errMsg string, code int) error {
	if err := WriteResultError(w, streamID, errMsg, code); err != nil {
		return err
	}
	return WriteStreamClose(w, streamID)
}

// WriteStreamClose sends a close frame for the stream.
func WriteStreamClose(w io.Writer, streamID uint32) error {
	return WriteRelayFrame(w, &StreamFrame{
		Opcode:   OpStreamClose,
		StreamID: streamID,
	})
}

// CancelFunc returns true if the operation should be cancelled.
type CancelFunc func() bool

// ProgressFunc is called with the number of bytes processed.
type ProgressFunc func(bytes int64)

// CompleteFunc is called when an item is completed (e.g., file extracted).
type CompleteFunc func(path string)

// OperationCallbacks provides common callbacks for long-running operations.
// All fields are optional - nil callbacks are safely ignored.
type OperationCallbacks struct {
	Progress   ProgressFunc // Called with bytes processed
	Cancel     CancelFunc   // Returns true if operation should abort
	OnComplete CompleteFunc // Called when an item completes
}

// ReportProgress safely calls the progress callback if set.
func (o *OperationCallbacks) ReportProgress(bytes int64) {
	if o != nil && o.Progress != nil {
		o.Progress(bytes)
	}
}

// IsCancelled safely checks the cancel function if set.
func (o *OperationCallbacks) IsCancelled() bool {
	if o != nil && o.Cancel != nil {
		return o.Cancel()
	}
	return false
}

// ReportComplete safely calls the completion callback if set.
func (o *OperationCallbacks) ReportComplete(path string) {
	if o != nil && o.OnComplete != nil {
		o.OnComplete(path)
	}
}
