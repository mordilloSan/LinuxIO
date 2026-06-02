package relay

import (
	"bytes"
	"context"
	"encoding/json"
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

func TestStreamOpenPayloadEnvelopeRoundTrip(t *testing.T) {
	payload, err := MarshalStreamOpenPayload("docker.start_container", map[string]string{
		"containerId": "abc",
	})
	if err != nil {
		t.Fatalf("MarshalStreamOpenPayload() error = %v", err)
	}

	envelope, err := ParseStreamOpenPayload(payload)
	if err != nil {
		t.Fatalf("ParseStreamOpenPayload() error = %v", err)
	}
	if envelope.Route != "docker.start_container" {
		t.Fatalf("route = %q, want docker.start_container", envelope.Route)
	}

	var request struct {
		ContainerID string `json:"containerId"`
	}
	if err := json.Unmarshal(envelope.Request, &request); err != nil {
		t.Fatalf("json.Unmarshal(request): %v", err)
	}
	if request.ContainerID != "abc" {
		t.Fatalf("containerId = %q, want abc", request.ContainerID)
	}
}

func TestParseStreamOpenPayloadDefaultsMissingRequest(t *testing.T) {
	envelope, err := ParseStreamOpenPayload([]byte(`{"route":"system.get_cpu_info"}`))
	if err != nil {
		t.Fatalf("ParseStreamOpenPayload() error = %v", err)
	}
	if string(envelope.Request) != "{}" {
		t.Fatalf("request = %s, want {}", envelope.Request)
	}
}

func TestParseStreamOpenPayloadRejectsInvalidEnvelope(t *testing.T) {
	for _, payload := range [][]byte{
		[]byte(``),
		[]byte(`not-json`),
		[]byte(`{"request":{}}`),
	} {
		if _, err := ParseStreamOpenPayload(payload); err == nil {
			t.Fatalf("ParseStreamOpenPayload(%q) error = nil, want error", payload)
		}
	}
}
