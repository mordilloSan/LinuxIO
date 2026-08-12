package terminal

import (
	"context"
	"net"
	"os/user"
	"strconv"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	ipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// A dead client stream must not leave the handler waiting on an idle PTY:
// the bridge only finishes shutting down once all stream handlers return.
func TestHandleTerminalSessionReturnsWhenStreamCloses(t *testing.T) {
	u, err := user.Current()
	if err != nil {
		t.Fatalf("current user: %v", err)
	}
	uid, err := strconv.ParseUint(u.Uid, 10, 32)
	if err != nil {
		t.Skipf("non-numeric uid %q: %v", u.Uid, err)
	}
	gid, err := strconv.ParseUint(u.Gid, 10, 32)
	if err != nil {
		t.Skipf("non-numeric gid %q: %v", u.Gid, err)
	}

	rt := runtime.Runtime{Session: &session.Session{
		SessionID: "terminal-test",
		User: session.User{
			Username: u.Username,
			UID:      uint32(uid),
			GID:      uint32(gid),
		},
	}}

	client, server := net.Pipe()
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- HandleTerminalSession(context.Background(), rt, server, apischema.TerminalOpenRequest{Cols: 80, Rows: 24})
	}()

	// A completed write proves that the handler has started its stream-to-PTY
	// relay. Do not wait for unsolicited prompt output: a healthy interactive
	// shell may remain alive without emitting any bytes under load.
	_ = client.SetWriteDeadline(time.Now().Add(10 * time.Second))
	readyErr := ipc.WriteRelayFrame(client, &ipc.StreamFrame{
		Opcode:   ipc.OpStreamResize,
		StreamID: 1,
		Payload:  []byte{0, 80, 0, 24},
	})
	_ = client.SetWriteDeadline(time.Time{})
	if readyErr != nil {
		_ = client.Close()
		select {
		case handlerErr := <-done:
			if handlerErr != nil {
				t.Skipf("terminal unavailable in this environment: handler=%v readiness=%v", handlerErr, readyErr)
			}
			t.Fatalf("terminal stream relay did not start before the handler returned: %v", readyErr)
		case <-time.After(2 * time.Second):
			t.Fatalf("terminal stream relay did not start: %v", readyErr)
		}
	}

	if closeErr := client.Close(); closeErr != nil {
		t.Fatalf("close client side: %v", closeErr)
	}

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("HandleTerminalSession did not return after the stream closed")
	}
}
