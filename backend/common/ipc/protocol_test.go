package ipc

import (
	"bytes"
	"encoding/json"
	"testing"
)

// TestWriteResultOK_WireCompat ensures WriteResultOK produces the same wire
// bytes as the canonical WriteResultFrame path for both nil and non-nil data.
func TestWriteResultOK_WireCompat(t *testing.T) {
	tests := []struct {
		name string
		data any
	}{
		{"nil data", nil},
		{"string data", "hello"},
		{"struct data", struct {
			Count int    `json:"count"`
			Name  string `json:"name"`
		}{42, "test"}},
		{"nested data", map[string]any{
			"items": []string{"a", "b"},
			"total": 2,
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var gotBuf, wantBuf bytes.Buffer
			streamID := uint32(7)

			// Fast path under test
			if err := WriteResultOK(&gotBuf, streamID, tc.data); err != nil {
				t.Fatalf("WriteResultOK: %v", err)
			}

			// Canonical path via WriteResultFrame
			r := &ResultFrame{Status: "ok"}
			if tc.data != nil {
				b, err := json.Marshal(tc.data)
				if err != nil {
					t.Fatalf("json.Marshal: %v", err)
				}
				r.Data = b
			}
			if err := WriteResultFrame(&wantBuf, streamID, r); err != nil {
				t.Fatalf("WriteResultFrame: %v", err)
			}

			gotFrame, err := ReadRelayFrame(&gotBuf)
			if err != nil {
				t.Fatalf("ReadRelayFrame(got): %v", err)
			}
			wantFrame, err := ReadRelayFrame(&wantBuf)
			if err != nil {
				t.Fatalf("ReadRelayFrame(want): %v", err)
			}

			if gotFrame.Opcode != wantFrame.Opcode {
				t.Errorf("opcode: got %x, want %x", gotFrame.Opcode, wantFrame.Opcode)
			}
			if gotFrame.StreamID != wantFrame.StreamID {
				t.Errorf("streamID: got %d, want %d", gotFrame.StreamID, wantFrame.StreamID)
			}
			if !bytes.Equal(gotFrame.Payload, wantFrame.Payload) {
				t.Errorf("payload mismatch:\n  got:  %s\n  want: %s", gotFrame.Payload, wantFrame.Payload)
			}
		})
	}
}
