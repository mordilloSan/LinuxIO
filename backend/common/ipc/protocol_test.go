package ipc

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

type countingWriter struct {
	buf   bytes.Buffer
	calls int
}

func (w *countingWriter) Write(p []byte) (int, error) {
	w.calls++
	return w.buf.Write(p)
}

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	return len(p) - 1, nil
}

func TestWriteRelayFrameUsesSingleWrite(t *testing.T) {
	w := &countingWriter{}
	frame := &StreamFrame{
		Opcode:   OpStreamData,
		StreamID: 7,
		Payload:  []byte("payload"),
	}

	if err := WriteRelayFrame(w, frame); err != nil {
		t.Fatalf("WriteRelayFrame() error = %v", err)
	}
	if w.calls != 1 {
		t.Fatalf("WriteRelayFrame() write calls = %d, want 1", w.calls)
	}

	got, err := ReadRelayFrame(bytes.NewReader(w.buf.Bytes()))
	if err != nil {
		t.Fatalf("ReadRelayFrame() error = %v", err)
	}
	if got.Opcode != frame.Opcode {
		t.Fatalf("Opcode = %x, want %x", got.Opcode, frame.Opcode)
	}
	if got.StreamID != frame.StreamID {
		t.Fatalf("StreamID = %d, want %d", got.StreamID, frame.StreamID)
	}
	if !bytes.Equal(got.Payload, frame.Payload) {
		t.Fatalf("Payload = %q, want %q", got.Payload, frame.Payload)
	}
}

func TestWriteRelayFrameShortWrite(t *testing.T) {
	err := WriteRelayFrame(shortWriter{}, &StreamFrame{
		Opcode:   OpStreamClose,
		StreamID: 1,
	})
	if !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("WriteRelayFrame() error = %v, want %v", err, io.ErrShortWrite)
	}
}

func TestWriteRelayFrameRejectsOversizePayload(t *testing.T) {
	err := WriteRelayFrame(io.Discard, &StreamFrame{
		Opcode:   OpStreamData,
		StreamID: 1,
		Payload:  bytes.Repeat([]byte("x"), maxRelayPayloadSize+1),
	})
	if err == nil {
		t.Fatal("WriteRelayFrame() error = nil, want oversize payload error")
	}
	if !strings.Contains(err.Error(), "payload too large") {
		t.Fatalf("WriteRelayFrame() error = %v, want payload too large", err)
	}
}

func TestWriteProgressRejectsOversizePayload(t *testing.T) {
	err := WriteProgress(io.Discard, 1, map[string]string{
		"data": strings.Repeat("x", maxRelayPayloadSize),
	})
	if err == nil {
		t.Fatal("WriteProgress() error = nil, want oversize payload error")
	}
	if !strings.Contains(err.Error(), "progress payload invalid") {
		t.Fatalf("WriteProgress() error = %v, want progress payload invalid", err)
	}
}

func TestWriteResultFrameRejectsOversizePayload(t *testing.T) {
	err := WriteResultFrame(io.Discard, 1, &ResultFrame{
		Status: "error",
		Error:  strings.Repeat("x", maxRelayPayloadSize),
	})
	if err == nil {
		t.Fatal("WriteResultFrame() error = nil, want oversize payload error")
	}
	if !strings.Contains(err.Error(), "result payload invalid") {
		t.Fatalf("WriteResultFrame() error = %v, want result payload invalid", err)
	}
}

func TestAbortContextCleanupCancelsContext(t *testing.T) {
	reader, writer := io.Pipe()
	ctx, cancelFn, cleanup := AbortContext(context.Background(), reader)

	if cancelFn() {
		t.Fatalf("cancelFn() = true before abort, want false")
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close() error = %v", err)
	}

	cleanup()

	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatalf("cleanup did not cancel context")
	}
}
