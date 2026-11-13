package assistant

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/acpagents"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	ipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
)

// paneAgent installs a fake agent that drives sh as its CLI. It prints the
// variables the bridge gave it as one line, then echoes its stdin, so every
// byte that reached it comes back to the browser.
func paneAgent(t *testing.T, panes bool) {
	t.Helper()
	script := "#!/bin/sh\n" +
		`printf '{"cli":"%s","bin":"%s","socket":"%s"}\n' "$FAKE_CLI_EXECUTABLE" "$LINUXIO_CLAUDE_BIN" "$LINUXIO_PANE_SOCKET"` + "\n" +
		"exec cat\n"
	fakeAgent(t, script, func(agent *acpagents.Agent) {
		agent.CLI = "sh"
		agent.CLIEnv = "FAKE_CLI_EXECUTABLE"
		agent.Panes = panes
	})
}

// requireVerbatim compares lines byte for byte: JSONEq would also accept a
// line the bridge had re-encoded.
func requireVerbatim(t *testing.T, want, got string) {
	t.Helper()
	require.Equal(t, want, got)
}

// useRelay points the bridge at a relay; the fake agent never runs it.
func useRelay(t *testing.T, path string) {
	t.Helper()
	orig := paneRelayPath
	paneRelayPath = path
	t.Cleanup(func() { paneRelayPath = orig })
}

func installedRelay(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "linuxio-claude-relay")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\nexit 1\n"), 0o755))
	return path
}

// useTempDir gives the pane socket directory a short, empty parent that the
// test can watch. It comes after anything that calls t.TempDir, which would
// otherwise put the test's own files there.
func useTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "lpt")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	t.Setenv("TMPDIR", dir)
	return dir
}

type agentEnv struct {
	CLI    string `json:"cli"`
	Bin    string `json:"bin"`
	Socket string `json:"socket"`
}

// browser is the test's end of an assistant.open stream.
type browser struct {
	t      *testing.T
	conn   net.Conn
	frames chan *ipc.StreamFrame
	buf    []byte
	done   chan error
}

func openBrowser(t *testing.T, rt runtime.Runtime, req apischema.AssistantOpenRequest) *browser {
	t.Helper()
	client, server := net.Pipe()
	b := &browser{t: t, conn: client, frames: make(chan *ipc.StreamFrame, 4096), done: make(chan error, 1)}
	go func() { b.done <- HandleAssistantSession(context.Background(), rt, server, req) }()
	go func() {
		defer close(b.frames)
		for {
			frame, err := ipc.ReadRelayFrame(client)
			if err != nil {
				return
			}
			b.frames <- frame
		}
	}()
	t.Cleanup(func() { _ = client.Close() })
	return b
}

// line returns the next whole line the browser receives, or "" once the
// stream is closed.
func (b *browser) line() string {
	b.t.Helper()
	for {
		if i := bytes.IndexByte(b.buf, '\n'); i >= 0 {
			line := string(b.buf[:i+1])
			b.buf = b.buf[i+1:]
			return line
		}
		select {
		case frame, ok := <-b.frames:
			if !ok || frame.Opcode == ipc.OpStreamClose {
				require.Empty(b.t, b.buf, "partial line at close")
				return ""
			}
			b.buf = append(b.buf, frame.Payload...)
		case <-time.After(15 * time.Second):
			b.t.Fatal("no line from the bridge")
		}
	}
}

func (b *browser) send(payload string) {
	b.t.Helper()
	writeData(b.t, b.conn, payload)
}

func (b *browser) start() agentEnv {
	b.t.Helper()
	require.Contains(b.t, b.line(), `"linuxio":"ready"`)
	var env agentEnv
	require.NoError(b.t, json.Unmarshal([]byte(b.line()), &env))
	return env
}

// finish closes the stream from the browser and returns every line up to
// and including the exit line.
func (b *browser) finish() []string {
	b.t.Helper()
	_ = b.conn.SetWriteDeadline(time.Now().Add(15 * time.Second))
	require.NoError(b.t, ipc.WriteStreamClose(b.conn, 1))
	var lines []string
	for {
		line := b.line()
		if line == "" {
			break
		}
		lines = append(lines, line)
	}
	select {
	case err := <-b.done:
		require.NoError(b.t, err)
	case <-time.After(15 * time.Second):
		b.t.Fatal("handler did not return")
	}
	require.NotEmpty(b.t, lines)
	require.Contains(b.t, lines[len(lines)-1], `"linuxio":"exit"`)
	return lines
}

// relayConn is the test standing in for linuxio-claude-relay.
type relayConn struct {
	t      *testing.T
	conn   net.Conn
	reader *bufio.Reader
}

func dialRelay(t *testing.T, socket string) *relayConn {
	t.Helper()
	conn, err := net.Dial("unix", socket)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return &relayConn{t: t, conn: conn, reader: bufio.NewReader(conn)}
}

func (r *relayConn) send(line string) {
	r.t.Helper()
	require.NoError(r.t, r.conn.SetWriteDeadline(time.Now().Add(15*time.Second)))
	_, err := io.WriteString(r.conn, line)
	require.NoError(r.t, err)
}

func (r *relayConn) line() (string, error) {
	r.t.Helper()
	require.NoError(r.t, r.conn.SetReadDeadline(time.Now().Add(15*time.Second)))
	return r.reader.ReadString('\n')
}

func TestHandleAssistantSessionWithoutPanesIsUnchanged(t *testing.T) {
	cases := map[string]struct {
		request    bool
		agentPanes bool
		relay      func(t *testing.T) string
	}{
		"panes not requested": {request: false, agentPanes: true, relay: installedRelay},
		"relay not installed": {request: true, agentPanes: true, relay: func(t *testing.T) string { return filepath.Join(t.TempDir(), "missing") }},
		"agent without panes": {request: true, agentPanes: false, relay: installedRelay},
		"relay not executable": {request: true, agentPanes: true, relay: func(t *testing.T) string {
			path := installedRelay(t)
			require.NoError(t, os.Chmod(path, 0o644))
			return path
		}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			rt, _ := testRuntime(t)
			useRelay(t, tc.relay(t))
			paneAgent(t, tc.agentPanes)
			tmp := useTempDir(t)

			b := openBrowser(t, rt, apischema.AssistantOpenRequest{Agent: "fake", Panes: tc.request})
			env := b.start()
			require.Equal(t, "sh", filepath.Base(env.CLI))
			require.Empty(t, env.Bin)
			require.Empty(t, env.Socket)
			entries, err := os.ReadDir(tmp)
			require.NoError(t, err)
			require.Empty(t, entries, "no pane socket directory")

			// A pane-shaped line is just agent input.
			pane := `{"linuxio":"pane","conn":1,"id":"p1","request":{"subtype":"ui_panes"}}` + "\n"
			b.send(pane)
			require.Equal(t, pane, b.line())
			require.Len(t, b.finish(), 1)
		})
	}
}

func TestHandleAssistantSessionCarriesPaneLines(t *testing.T) {
	rt, _ := testRuntime(t)
	relayPath := installedRelay(t)
	useRelay(t, relayPath)
	paneAgent(t, true)
	tmp := useTempDir(t)

	b := openBrowser(t, rt, apischema.AssistantOpenRequest{Agent: "fake", Panes: true})
	env := b.start()
	require.Equal(t, relayPath, env.CLI, "the adapter runs the relay")
	require.True(t, filepath.IsAbs(env.Bin))
	require.Equal(t, "sh", filepath.Base(env.Bin), "the relay runs the real CLI")
	dir := filepath.Dir(env.Socket)
	require.Equal(t, tmp, filepath.Dir(dir))
	dirInfo, err := os.Stat(dir)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o700), dirInfo.Mode().Perm())
	sockInfo, err := os.Stat(env.Socket)
	require.NoError(t, err)
	require.Equal(t, os.ModeSocket, sockInfo.Mode().Type())
	require.Equal(t, os.FileMode(0o600), sockInfo.Mode().Perm())

	relay := dialRelay(t, env.Socket)
	require.JSONEq(t, `{"conn":1,"event":"open","linuxio":"pane"}`, b.line())

	relay.send(`{"type":"relay_ready","capabilities":["ui_surface_v1"]}` + "\n")
	require.JSONEq(t, `{"conn":1,"linuxio":"pane","message":{"type":"relay_ready","capabilities":["ui_surface_v1"]}}`, b.line())

	// Lines that are not one JSON value, or are over 4 MiB, are dropped.
	relay.send("not json\n{\"a\":1} {\"b\":2}\n\n\"" + strings.Repeat("a", 4<<20) + "\"\n")
	relay.send(`{"type":"pane_response","id":"p0","ok":true,"response":{}}` + "\n")
	require.JSONEq(t, `{"conn":1,"linuxio":"pane","message":{"type":"pane_response","id":"p0","ok":true,"response":{}}}`, b.line())

	// A pane line reaches the relay and not the agent, wherever the frames
	// split it; the lines around it reach the agent unchanged.
	b.send(`{"jsonrpc":"2.0","id":1`)
	b.send(`,"method":"x"}` + "\n" + `{"linuxio":"pa`)
	b.send(`ne","conn":1,"id":"p7","request":{"subtype":"ui_panes","client_id":"c"}}` + "\n" + `{"linuxio":"panes!"}` + "\n")
	got, err := relay.line()
	require.NoError(t, err)
	require.JSONEq(t, `{"id":"p7","request":{"subtype":"ui_panes","client_id":"c"}}`, got)
	requireVerbatim(t, `{"jsonrpc":"2.0","id":1,"method":"x"}`+"\n", b.line())
	requireVerbatim(t, `{"linuxio":"panes!"}`+"\n", b.line())

	// Pane lines for an unknown conn, malformed, without a request or over
	// 64 KiB are dropped: neither the relay nor the agent sees them.
	b.send(`{"linuxio":"pane","conn":9,"id":"x","request":{}}` + "\n")
	b.send(`{"linuxio":"pane",oops}` + "\n")
	b.send(`{"linuxio":"pane","conn":1,"id":5,"request":{}}` + "\n")
	b.send(`{"linuxio":"pane","conn":1,"id":"p8"}` + "\n")
	b.send(`{"linuxio":"pane","conn":1,"id":"big","request":{"pad":"` + strings.Repeat("z", 64<<10) + `"}}` + "\n")
	b.send(`{"jsonrpc":"2.0","id":2}` + "\n")
	requireVerbatim(t, `{"jsonrpc":"2.0","id":2}`+"\n", b.line())
	// A request of the wrong shape goes on, so the relay can refuse it.
	b.send(`{"linuxio":"pane","conn":1,"id":"p9","request":"ui_panes"}` + "\n")
	got, err = relay.line()
	require.NoError(t, err)
	require.JSONEq(t, `{"id":"p9","request":"ui_panes"}`, got)
	b.send(`{"linuxio":"pane","conn":1,"id":"p10","request":{"subtype":"ui_panes"}}` + "\n")
	got, err = relay.line()
	require.NoError(t, err)
	require.JSONEq(t, `{"id":"p10","request":{"subtype":"ui_panes"}}`, got)

	// A large agent line arrives whole while relay lines are carried.
	big := `{"jsonrpc":"2.0","id":3,"params":"` + strings.Repeat("q", 1<<20) + `"}` + "\n"
	quarter := len(big) / 4
	for i := range 4 {
		end := (i + 1) * quarter
		if i == 3 {
			end = len(big)
		}
		b.send(big[i*quarter : end])
		relay.send(fmt.Sprintf(`{"type":"system","subtype":"ui_log","n":%d}`, i) + "\n")
		time.Sleep(20 * time.Millisecond)
	}
	var echoed string
	logs := 0
	for echoed == "" || logs < 4 {
		line := b.line()
		if strings.HasPrefix(line, `{"jsonrpc"`) {
			echoed = line
			continue
		}
		require.JSONEq(t, fmt.Sprintf(`{"conn":1,"linuxio":"pane","message":{"type":"system","subtype":"ui_log","n":%d}}`, logs), line)
		logs++
	}
	require.Equal(t, big, echoed)

	// A relay that hangs up is announced; the next one gets a new conn.
	require.NoError(t, relay.conn.Close())
	require.JSONEq(t, `{"conn":1,"event":"close","linuxio":"pane"}`, b.line())
	second := dialRelay(t, env.Socket)
	require.JSONEq(t, `{"conn":2,"event":"open","linuxio":"pane"}`, b.line())

	// The stream ending hangs up the relays before the exit line, and removes
	// the directory.
	lines := b.finish()
	require.Len(t, lines, 2)
	require.JSONEq(t, `{"conn":2,"event":"close","linuxio":"pane"}`, lines[0])
	_, err = second.line()
	require.ErrorIs(t, err, io.EOF)
	_, err = os.Stat(dir)
	require.ErrorIs(t, err, os.ErrNotExist)
	entries, err := os.ReadDir(tmp)
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestHandleAssistantSessionRefusesOtherUsersRelays(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("as root the agent would run as the made-up uid")
	}
	rt, _ := testRuntime(t)
	// Connecting as another user needs root. The same check refuses the test,
	// whose uid is not the session's: as non-root the bridge does not chown,
	// and the agent runs as the test.
	rt.Session.User.UID++
	useRelay(t, installedRelay(t))
	paneAgent(t, true)
	useTempDir(t)

	b := openBrowser(t, rt, apischema.AssistantOpenRequest{Agent: "fake", Panes: true})
	env := b.start()
	relay := dialRelay(t, env.Socket)
	_, err := relay.line()
	require.ErrorIs(t, err, io.EOF, "the bridge hangs up")
	require.Len(t, b.finish(), 1, "no open line")
}

func TestHandleAssistantSessionLimitsRelayConnections(t *testing.T) {
	rt, _ := testRuntime(t)
	useRelay(t, installedRelay(t))
	paneAgent(t, true)
	useTempDir(t)

	b := openBrowser(t, rt, apischema.AssistantOpenRequest{Agent: "fake", Panes: true})
	env := b.start()
	for i := 1; i <= maxPaneConns; i++ {
		dialRelay(t, env.Socket)
		require.JSONEq(t, fmt.Sprintf(`{"conn":%d,"event":"open","linuxio":"pane"}`, i), b.line())
	}
	extra := dialRelay(t, env.Socket)
	_, err := extra.line()
	require.ErrorIs(t, err, io.EOF)
	lines := b.finish()
	require.Len(t, lines, maxPaneConns+1)
}
