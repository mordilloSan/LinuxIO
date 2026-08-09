package logs

import (
	"context"
	"net"
	"os/exec"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
)

func TestStreamServiceLogsWritesDataFrames(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()
	done := make(chan error, 1)
	go func() {
		defer server.Close()
		_, err := streamServiceLogs(
			context.Background(),
			server,
			strings.NewReader("first\nsecond\n"),
			&exec.Cmd{},
		)
		done <- err
	}()

	for _, want := range []string{"first\n", "second\n"} {
		frame, err := relay.ReadRelayFrame(client)
		if err != nil {
			t.Fatalf("read Channel frame: %v", err)
		}
		if frame.Opcode != relay.OpStreamData {
			t.Fatalf("opcode = %d, want data", frame.Opcode)
		}
		if got := string(frame.Payload); got != want {
			t.Fatalf("payload = %q, want %q", got, want)
		}
	}
	if err := <-done; err != nil {
		t.Fatalf("stream service logs: %v", err)
	}
}
