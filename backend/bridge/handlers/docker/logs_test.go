package docker

import (
	"bytes"
	"context"
	"encoding/binary"
	"net"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
)

func TestStreamDockerLogsWritesDataFrame(t *testing.T) {
	payload := []byte("container output\n")
	dockerFrame := make([]byte, 8+len(payload))
	dockerFrame[0] = 1
	binary.BigEndian.PutUint32(dockerFrame[4:8], uint32(len(payload)))
	copy(dockerFrame[8:], payload)

	server, client := net.Pipe()
	defer client.Close()
	done := make(chan error, 1)
	go func() {
		defer server.Close()
		done <- streamDockerLogs(context.Background(), server, bytes.NewReader(dockerFrame))
	}()

	frame, err := relay.ReadRelayFrame(client)
	if err != nil {
		t.Fatalf("read Channel frame: %v", err)
	}
	if frame.Opcode != relay.OpStreamData {
		t.Fatalf("opcode = %d, want data", frame.Opcode)
	}
	if !bytes.Equal(frame.Payload, payload) {
		t.Fatalf("payload = %q, want %q", frame.Payload, payload)
	}
	if err := <-done; err != nil {
		t.Fatalf("stream Docker logs: %v", err)
	}
}
