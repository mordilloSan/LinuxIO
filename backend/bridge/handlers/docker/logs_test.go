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
		done <- streamDockerLogs(context.Background(), server, bytes.NewReader(dockerFrame), false)
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

func TestStreamDockerLogsWritesRawTTYOutput(t *testing.T) {
	// Raw TTY output whose bytes 4-7 would decode as a huge multiplexed frame size.
	raw := []byte("No UOS_UUID present\r\n\x1b[32mstarted\x1b[0m\r\n")
	want := []byte("No UOS_UUID present\r\nstarted\r\n")

	server, client := net.Pipe()
	defer client.Close()
	done := make(chan error, 1)
	go func() {
		defer server.Close()
		done <- streamDockerLogs(context.Background(), server, bytes.NewReader(raw), true)
	}()

	frame, err := relay.ReadRelayFrame(client)
	if err != nil {
		t.Fatalf("read Channel frame: %v", err)
	}
	if frame.Opcode != relay.OpStreamData {
		t.Fatalf("opcode = %d, want data", frame.Opcode)
	}
	if !bytes.Equal(frame.Payload, want) {
		t.Fatalf("payload = %q, want %q", frame.Payload, want)
	}
	if err := <-done; err != nil {
		t.Fatalf("stream Docker logs: %v", err)
	}
}
