package logs

import (
	"net"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
)

func TestJournalDataBatchWritesOneFramePerFlush(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()
	defer server.Close()

	done := make(chan error, 1)
	go func() {
		batch := journalDataBatch{stream: server}
		if err := batch.append("first"); err != nil {
			done <- err
			return
		}
		if err := batch.append("second"); err != nil {
			done <- err
			return
		}
		done <- batch.flush()
	}()

	frame, err := relay.ReadRelayFrame(client)
	if err != nil {
		t.Fatalf("read Channel frame: %v", err)
	}
	if frame.Opcode != relay.OpStreamData {
		t.Fatalf("opcode = %d, want data", frame.Opcode)
	}
	if got, want := string(frame.Payload), "first\nsecond\n"; got != want {
		t.Fatalf("payload = %q, want %q", got, want)
	}
	if err := <-done; err != nil {
		t.Fatalf("flush: %v", err)
	}
}
