package relay

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
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

type boundedReader struct {
	data []byte
	max  int
}

func (r *boundedReader) Read(p []byte) (int, error) {
	if len(p) > r.max {
		r.max = len(p)
	}
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

func TestReadRelayFrameProgressiveDoesNotAllocateDeclaredPayload(t *testing.T) {
	header := make([]byte, relayFrameHeaderSize)
	header[0] = OpStreamOpen
	binary.BigEndian.PutUint32(header[5:9], maxRelayPayloadSize)
	r := &boundedReader{data: header}
	if _, err := ReadRelayFrameProgressive(r); err == nil {
		t.Fatal("ReadRelayFrameProgressive() error = nil, want truncated payload error")
	}
	if r.max > firstFrameReadChunkSize {
		t.Fatalf("read request = %d bytes, want at most %d", r.max, firstFrameReadChunkSize)
	}
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

func TestParseStreamOpenPayload(t *testing.T) {
	envelope, err := ParseStreamOpenPayload([]byte(`{
		"route":"docker.start_container",
		"request":{"containerId":"abc"}
	}`))
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
