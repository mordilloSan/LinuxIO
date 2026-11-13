package assistant

import (
	"context"
	"encoding/json/v2"
	"net"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/acpagents"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/acpagents/acpagentstest"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
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

// fakeAgent installs script as the agent "fake" under a temporary home and
// points the relay's agent lookup at it. extra adjusts the catalogue entry.
func fakeAgent(t *testing.T, script string, extra ...func(*acpagents.Agent)) {
	t.Helper()
	agent := acpagents.Agent{Package: "fake-agent", Version: "1.0.0", Bin: "fake-agent"}
	for _, adjust := range extra {
		adjust(&agent)
	}
	t.Cleanup(acpagents.Override("fake", agent))
	home := t.TempDir()
	acpagentstest.Install(t, home, "fake", agent, agent.Version, script)
	useAgentHome(t, home)
}

func useAgentHome(t *testing.T, home string) {
	t.Helper()
	orig := agentHome
	agentHome = func(string) (string, error) { return home, nil }
	t.Cleanup(func() { agentHome = orig })
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

func TestHandleAssistantSessionReportsExitWithInvalidUTF8Stderr(t *testing.T) {
	rt, _ := testRuntime(t)
	fakeAgent(t, "#!/bin/sh\nprintf 'caf\\351' >&2\nexit 1\n")

	client, server := net.Pipe()
	defer client.Close()
	done := make(chan error, 1)
	go func() {
		done <- HandleAssistantSession(context.Background(), rt, server, apischema.AssistantOpenRequest{Agent: "fake"})
	}()
	readData(t, client) // ready
	var exitLine struct {
		Stderr string `json:"stderr"`
	}
	require.NoError(t, json.Unmarshal(readData(t, client).Payload, &exitLine))
	require.Equal(t, "caf\ufffd", exitLine.Stderr)
	readData(t, client) // close

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
	case err := <-done:
		require.NoError(t, err)
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

func TestHandleAssistantSessionRefusesAnAgentThatIsNotInstalled(t *testing.T) {
	rt, _ := testRuntime(t)
	t.Cleanup(acpagents.Override("fake", acpagents.Agent{Package: "fake-agent", Version: "1.0.0", Bin: "fake-agent"}))
	useAgentHome(t, t.TempDir())

	client, server := net.Pipe()
	defer client.Close()
	done := make(chan error, 1)
	go func() {
		done <- HandleAssistantSession(context.Background(), rt, server, apischema.AssistantOpenRequest{Agent: "fake"})
	}()
	frame := readData(t, client)
	require.Equal(t, ipc.OpStreamClose, frame.Opcode)
	err := <-done
	require.ErrorIs(t, err, bridgeipc.ErrConflict)
	require.ErrorContains(t, err, "fake-agent is not installed")
}

func TestHandleAssistantSessionPointsTheAgentAtTheInstalledCLI(t *testing.T) {
	rt, _ := testRuntime(t)
	// sh is on every login-shell PATH, so it stands in for the agent's CLI.
	fakeAgent(t, "#!/bin/sh\nprintf '%s\\n' \"$FAKE_CLI_EXECUTABLE\"\n", func(agent *acpagents.Agent) {
		agent.CLI = "sh"
		agent.CLIEnv = "FAKE_CLI_EXECUTABLE"
	})

	client, server := net.Pipe()
	defer client.Close()
	done := make(chan error, 1)
	go func() {
		done <- HandleAssistantSession(context.Background(), rt, server, apischema.AssistantOpenRequest{Agent: "fake"})
	}()
	readData(t, client) // ready
	printed := strings.TrimSpace(string(readData(t, client).Payload))
	require.True(t, filepath.IsAbs(printed), "CLI path %q", printed)
	require.Equal(t, "sh", filepath.Base(printed))
	readData(t, client) // exit
	readData(t, client) // close
	require.NoError(t, <-done)
}
