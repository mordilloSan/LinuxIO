package assistant

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	ipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

func testRuntime(t *testing.T) (runtime.Runtime, string) {
	t.Helper()
	u, err := user.Current()
	require.NoError(t, err)
	uid, err := strconv.ParseUint(u.Uid, 10, 32)
	if err != nil {
		t.Skipf("non-numeric uid %q", u.Uid)
	}
	gid, err := strconv.ParseUint(u.Gid, 10, 32)
	if err != nil {
		t.Skipf("non-numeric gid %q", u.Gid)
	}
	home, err := filepath.EvalSymlinks(u.HomeDir)
	require.NoError(t, err)
	return runtime.Runtime{Session: &session.Session{
		SessionID: "assistant-test",
		User:      session.User{Username: u.Username, UID: uint32(uid), GID: uint32(gid)},
	}}, home
}

func fakeAgent(t *testing.T, script string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent.sh")
	require.NoError(t, os.WriteFile(path, []byte(script), 0o700))
	t.Cleanup(OverrideAgent("fake", Agent{Command: path}))
}

func readData(t *testing.T, conn net.Conn) *ipc.StreamFrame {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	frame, err := ipc.ReadRelayFrame(conn)
	require.NoError(t, err)
	return frame
}

func writeData(t *testing.T, conn net.Conn, payload string) {
	t.Helper()
	_ = conn.SetWriteDeadline(time.Now().Add(15 * time.Second))
	require.NoError(t, ipc.WriteRelayFrame(conn, &ipc.StreamFrame{Opcode: ipc.OpStreamData, StreamID: 1, Payload: []byte(payload)}))
}

func TestHandleAssistantSessionRelaysStdioAndReportsExit(t *testing.T) {
	rt, home := testRuntime(t)
	fakeAgent(t, "#!/bin/sh\nprintf 'warn\\n' >&2\nread -r line\nprintf '%s\\n' \"$line\"\nexit 3\n")

	client, server := net.Pipe()
	defer client.Close()
	done := make(chan error, 1)
	go func() {
		done <- HandleAssistantSession(context.Background(), rt, server, apischema.AssistantOpenRequest{Agent: "fake"})
	}()

	ready := readData(t, client)
	require.Equal(t, ipc.OpStreamData, ready.Opcode)
	var readyLine struct {
		LinuxIO string `json:"linuxio"`
		Cwd     string `json:"cwd"`
	}
	require.NoError(t, json.Unmarshal(ready.Payload, &readyLine))
	require.Equal(t, "ready", readyLine.LinuxIO)
	require.Equal(t, home, readyLine.Cwd)

	writeData(t, client, "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\"}\n")
	echo := readData(t, client)
	require.JSONEq(t, `{"jsonrpc":"2.0","id":1,"method":"initialize"}`, string(echo.Payload))
	require.Equal(t, byte('\n'), echo.Payload[len(echo.Payload)-1])

	exit := readData(t, client)
	var exitLine struct {
		LinuxIO string `json:"linuxio"`
		Code    int    `json:"code"`
		Stderr  string `json:"stderr"`
	}
	require.NoError(t, json.Unmarshal(exit.Payload, &exitLine))
	require.Equal(t, "exit", exitLine.LinuxIO)
	require.Equal(t, 3, exitLine.Code)
	require.Equal(t, "warn\n", exitLine.Stderr)

	closeFrame := readData(t, client)
	require.Equal(t, ipc.OpStreamClose, closeFrame.Opcode)

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("handler did not return after the agent exited")
	}
}

func TestHandleAssistantSessionDoesNotSignalAfterNormalExit(t *testing.T) {
	rt, _ := testRuntime(t)
	fakeAgent(t, "#!/bin/sh\nexit 0\n")
	var signals atomic.Int32
	orig := signalGroup
	signalGroup = func(int, syscall.Signal) error {
		signals.Add(1)
		return nil
	}
	t.Cleanup(func() { signalGroup = orig })

	client, server := net.Pipe()
	defer client.Close()
	done := make(chan error, 1)
	go func() {
		done <- HandleAssistantSession(context.Background(), rt, server, apischema.AssistantOpenRequest{Agent: "fake"})
	}()
	readData(t, client) // ready
	readData(t, client) // exit
	readData(t, client) // close

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("handler did not return after the agent exited")
	}
	require.Zero(t, signals.Load())
}

func TestHandleAssistantSessionStopsAgentWhenStreamCloses(t *testing.T) {
	rt, _ := testRuntime(t)
	fakeAgent(t, "#!/bin/sh\nexec sleep 30\n")

	client, server := net.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- HandleAssistantSession(context.Background(), rt, server, apischema.AssistantOpenRequest{Agent: "fake"})
	}()
	readData(t, client) // ready line
	require.NoError(t, client.Close())

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("handler did not return after the stream closed")
	}
}

func TestHandleAssistantSessionRejectsUnknownAgent(t *testing.T) {
	rt, _ := testRuntime(t)
	client, server := net.Pipe()
	defer client.Close()
	done := make(chan error, 1)
	go func() {
		done <- HandleAssistantSession(context.Background(), rt, server, apischema.AssistantOpenRequest{Agent: "nope"})
	}()
	frame := readData(t, client)
	require.Equal(t, ipc.OpStreamClose, frame.Opcode)
	require.Error(t, <-done)
}
