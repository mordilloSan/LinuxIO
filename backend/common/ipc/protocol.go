package ipc

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
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
	OpStreamOpen     byte = 0x80 // Open stream: payload = streamType\0arg1\0arg2...
	OpStreamData     byte = 0x81 // Binary data: payload = raw bytes
	OpStreamClose    byte = 0x82 // Close stream: payload = empty
	OpStreamResize   byte = 0x83 // Terminal resize: payload = [cols:2][rows:2]
	OpStreamProgress byte = 0x84 // Progress update: payload = handler-defined JSON
	OpStreamResult   byte = 0x85 // Final result: payload = JSON ResultFrame
	OpStreamAbort    byte = 0x86 // Abort operation: client requests cancellation
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

// WriteResultFrameAndClose writes a raw result frame and then closes the stream.
func WriteResultFrameAndClose(w io.Writer, streamID uint32, r *ResultFrame) error {
	if err := WriteResultFrame(w, streamID, r); err != nil {
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

// AbortMonitor monitors a stream for abort signals (OpStreamAbort).
// Returns a cancel function that returns true when abort is received.
// The cleanup function waits for the monitor goroutine to exit (with timeout).
func AbortMonitor(r io.Reader) (cancelFn CancelFunc, cleanup func()) {
	aborted := make(chan struct{})
	done := make(chan struct{})

	go func() {
		defer close(done)
		for {
			frame, err := ReadRelayFrame(r)
			if err != nil {
				return
			}
			if frame.Opcode == OpStreamAbort {
				close(aborted)
				return
			}
		}
	}()

	cancelFn = func() bool {
		select {
		case <-aborted:
			return true
		default:
			return false
		}
	}

	// cleanup waits for the goroutine to finish with a reasonable timeout.
	// The goroutine will exit when the reader returns an error (e.g., stream closed).
	cleanup = func() {
		select {
		case <-done:
			// Goroutine finished cleanly
		case <-time.After(100 * time.Millisecond):
			// Timeout - goroutine will exit when stream closes
		}
	}

	return cancelFn, cleanup
}

// AbortContext creates a context derived from parent that is cancelled when an
// abort signal (OpStreamAbort) is received on the stream. Uses channel-based
// notification — no polling.
//
// Returns:
//   - ctx: a context that is cancelled on abort or when parent is done.
//   - cancelFn: a CancelFunc that returns true once abort has been received.
//     Can be passed to OperationCallbacks.Cancel for synchronous checks.
//   - cleanup: blocks until the monitor goroutine exits (with a short timeout).
//     Callers must always defer cleanup().
func AbortContext(parent context.Context, stream io.Reader) (ctx context.Context, cancelFn CancelFunc, cleanup func()) {
	ctx, cancel := context.WithCancel(parent)

	aborted := make(chan struct{})
	done := make(chan struct{})

	// Monitor goroutine: reads frames until abort or stream error.
	go func() {
		defer close(done)
		for {
			frame, err := ReadRelayFrame(stream)
			if err != nil {
				return
			}
			if frame.Opcode == OpStreamAbort {
				close(aborted)
				cancel()
				return
			}
		}
	}()

	cancelFn = func() bool {
		select {
		case <-aborted:
			return true
		default:
			return false
		}
	}

	cleanup = func() {
		select {
		case <-done:
		case <-time.After(100 * time.Millisecond):
		}
	}

	return ctx, cancelFn, cleanup
}
