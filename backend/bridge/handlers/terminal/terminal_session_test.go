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

	// Drain output until the shell goes quiet: the hang this guards against
	// only reproduces when the stream dies while the PTY relay sits in a read
	// on an idle shell with nothing left to flush.
	buf := make([]byte, 4096)
	sawOutput := false
	for {
		_ = client.SetReadDeadline(time.Now().Add(1 * time.Second))
		_, readErr := client.Read(buf)
		if readErr == nil {
			sawOutput = true
			continue
		}
		if !sawOutput {
			select {
			case handlerErr := <-done:
				t.Skipf("terminal unavailable in this environment: handler=%v read=%v", handlerErr, readErr)
			case <-time.After(2 * time.Second):
				t.Fatalf("no terminal output but handler still running: %v", readErr)
			}
		}
		break
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
