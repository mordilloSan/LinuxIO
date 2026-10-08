package terminal

import (
	"context"
	"io"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/assistant"
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

func TestHandleTerminalSessionRunsAgentLoginCommand(t *testing.T) {
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
	script := filepath.Join(t.TempDir(), "agent.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf 'LOGIN %s %s\\n' \"$1\" \"$NO_BROWSER\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(assistant.OverrideAgent("fake", assistant.Agent{Command: script}))

	rt := runtime.Runtime{Session: &session.Session{
		SessionID: "terminal-login-test",
		User:      session.User{Username: u.Username, UID: uint32(uid), GID: uint32(gid)},
	}}
	client, server := net.Pipe()
	defer client.Close()
	done := make(chan error, 1)
	go func() {
		done <- HandleTerminalSession(context.Background(), rt, server, apischema.TerminalOpenRequest{
			Cols: 80, Rows: 24, Agent: "fake", Args: []string{"--cli"},
		})
	}()

	var output []byte
	deadline := time.Now().Add(15 * time.Second)
	for !strings.Contains(string(output), "LOGIN --cli 1") {
		_ = client.SetReadDeadline(deadline)
		frame, err := ipc.ReadRelayFrame(client)
		if err != nil {
			t.Fatalf("read frame: %v (output so far %q)", err, output)
		}
		if frame.Opcode == ipc.OpStreamClose {
			t.Fatalf("stream closed before login output; got %q", output)
		}
		output = append(output, frame.Payload...)
	}
	// net.Pipe is unbuffered: drain the trailing close frame so the handler can finish.
	go func() { _, _ = io.Copy(io.Discard, client) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("handler returned error: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("handler did not return after the login command exited")
	}
}

func TestHandleTerminalSessionRejectsUnknownAgent(t *testing.T) {
	rt := runtime.Runtime{Session: &session.Session{User: session.User{Username: "nobody"}}}
	client, server := net.Pipe()
	defer client.Close()
	done := make(chan error, 1)
	go func() {
		done <- HandleTerminalSession(context.Background(), rt, server, apischema.TerminalOpenRequest{Agent: "nope"})
	}()
	frame, err := ipc.ReadRelayFrame(client)
	if err != nil {
		t.Fatal(err)
	}
	if frame.Opcode != ipc.OpStreamClose {
		t.Fatalf("expected close frame, got opcode %#x", frame.Opcode)
	}
	if err := <-done; err == nil {
		t.Fatal("expected an error for an unknown agent")
	}
}
