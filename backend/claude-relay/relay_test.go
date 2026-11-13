package main

import (
	"bufio"
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The test binary doubles as a fake `claude` when this variable names a mode:
// "echo" copies stdin to stdout, "claude" answers control requests the way
// Claude Code does and echoes every other line.
const fakeModeEnv = "LINUXIO_FAKE_CLAUDE"

func TestMain(m *testing.M) {
	if mode := os.Getenv(fakeModeEnv); mode != "" {
		os.Exit(fakeClaude(mode))
	}
	os.Exit(m.Run())
}

func fakeClaude(mode string) int {
	code, _ := strconv.Atoi(os.Getenv("FAKE_EXIT"))
	if mode == "echo" {
		_, _ = io.Copy(os.Stdout, os.Stdin)
		return code
	}
	reader := bufio.NewReader(os.Stdin)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			fakeLine(line)
		}
		if err != nil {
			return code
		}
	}
}

func fakeLine(line []byte) {
	var msg struct {
		Type      string         `json:"type"`
		RequestID string         `json:"request_id"`
		Request   jsontext.Value `json:"request"`
		Fake      string         `json:"fake"`
	}
	var head struct {
		Subtype string `json:"subtype"`
	}
	if (!bytes.Contains(line, []byte(`"control_request"`)) && !bytes.Contains(line, []byte(`"fake"`))) || json.Unmarshal(line, &msg) != nil {
		_, _ = os.Stdout.Write(line)
		return
	}
	_ = json.Unmarshal(msg.Request, &head)
	respond := func(body string) {
		fmt.Printf(`{"type":"control_response","response":{"subtype":"success","request_id":%q,"response":%s}}`+"\n", msg.RequestID, body)
	}
	switch {
	case msg.Type == "control_request" && head.Subtype == "initialize":
		respond(`{"capabilities":["ui_surface_v1","prompts_screened_v1"],"pid":1}`)
	case msg.Type == "control_request" && head.Subtype == "ui_silent":
	case msg.Type == "control_request" && head.Subtype == "ui_fail":
		fmt.Printf(`{"type":"control_response","response":{"subtype":"error","request_id":%q,"error":"no such pane"}}`+"\n", msg.RequestID)
	case msg.Type == "control_request" && head.Subtype == "ui_big":
		respond(`{"blob":"` + strings.Repeat("x", 5<<20) + `"}`)
	case msg.Type == "control_request":
		respond(`{"echo":` + string(msg.Request) + `}`)
	case msg.Fake == "push":
		fmt.Print(pushedPanes + "\n" + pushedLog + "\n" + `{"type":"system","subtype":"init","session_id":"s1"}` + "\n")
	case msg.Fake == "env":
		_, hasBin := os.LookupEnv(envClaudeBin)
		_, hasSocket := os.LookupEnv(envPaneSocket)
		out, _ := json.Marshal(map[string]any{"executable": os.Getenv(envExecutable), "has_bin": hasBin, "has_socket": hasSocket})
		fmt.Printf("%s\n", out)
	default:
		_, _ = os.Stdout.Write(line)
	}
}

const (
	pushedPanes = `{"type":"system","subtype":"ui_panes","panes":[{"id":"lot-map","title":"Work map","plugin":"lot-map"}],"shown_id":"lot-map","focused_id":null,"focus_requested_id":null,"uuid":"d88c4e18","session_id":"s1"}`
	pushedLog   = `{"type":"system","subtype":"ui_log","plugin":"lot-map","text":"hook \"skipped\"","uuid":"3aeda32e","session_id":"s1"}`
)

// requireVerbatim compares lines byte for byte: JSONEq would also accept a
// line the relay had re-encoded.
func requireVerbatim(t *testing.T, want, got string) {
	t.Helper()
	require.Equal(t, want, got)
}

var streamArgs = []string{"-p", "--output-format", "stream-json", "--verbose", "--input-format", "stream-json"}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// shortTempDir keeps unix socket paths under the 108-byte limit.
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "lcr")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func testExecutable(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	require.NoError(t, err)
	return exe
}

// harness runs the relay with the test as both the adapter and the bridge.
type harness struct {
	t       *testing.T
	stdin   *io.PipeWriter
	stdout  chan []byte
	bridge  net.Conn
	lines   *bufio.Reader
	signals chan<- os.Signal
	exit    chan int
	stderr  *syncBuffer
}

func startRelay(t *testing.T, mode string, extraEnv ...string) *harness {
	t.Helper()
	socket := filepath.Join(shortTempDir(t), "pane.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })

	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	notified := make(chan chan<- os.Signal, 1)
	h := &harness{t: t, stdin: stdinW, stdout: make(chan []byte, 1024), exit: make(chan int, 1), stderr: &syncBuffer{}}
	env := append(os.Environ(), fakeModeEnv+"="+mode, envClaudeBin+"="+testExecutable(t), envPaneSocket+"="+socket)
	p := process{
		args:   streamArgs,
		env:    append(env, extraEnv...),
		stdin:  stdinR,
		stdout: stdoutW,
		stderr: h.stderr,
		notify: func(c chan<- os.Signal) { notified <- c },
		exec: func(string, []string, []string) error {
			t.Error("relay mode must not exec")
			return errors.New("exec stub")
		},
	}
	go func() {
		code := run(p)
		_ = stdoutW.Close()
		h.exit <- code
	}()
	go func() {
		defer close(h.stdout)
		reader := bufio.NewReader(stdoutR)
		for {
			line, readErr := reader.ReadBytes('\n')
			if len(line) > 0 {
				h.stdout <- line
			}
			if readErr != nil {
				return
			}
		}
	}()
	t.Cleanup(func() { _ = stdinW.Close() })

	require.NoError(t, listener.SetDeadline(time.Now().Add(15*time.Second)))
	conn, err := listener.Accept()
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	h.bridge = conn
	h.lines = bufio.NewReader(conn)
	select {
	case h.signals = <-notified:
	case <-time.After(15 * time.Second):
		t.Fatal("relay did not start Claude Code")
	}
	return h
}

func (h *harness) adapterWrite(data string) {
	h.t.Helper()
	_, err := io.WriteString(h.stdin, data)
	require.NoError(h.t, err)
}

func (h *harness) adapterLine() string {
	h.t.Helper()
	select {
	case line, ok := <-h.stdout:
		require.True(h.t, ok, "relay stdout closed")
		return string(line)
	case <-time.After(15 * time.Second):
		h.t.Fatal("no line on the relay's stdout")
		return ""
	}
}

func (h *harness) bridgeWrite(line string) {
	h.t.Helper()
	require.NoError(h.t, h.bridge.SetWriteDeadline(time.Now().Add(15*time.Second)))
	_, err := io.WriteString(h.bridge, line+"\n")
	require.NoError(h.t, err)
}

func (h *harness) bridgeLine() string {
	h.t.Helper()
	require.NoError(h.t, h.bridge.SetReadDeadline(time.Now().Add(15*time.Second)))
	line, err := h.lines.ReadString('\n')
	require.NoError(h.t, err)
	return line
}

// noBridgeLine asserts that nothing reaches the bridge for a while.
func (h *harness) noBridgeLine(wait time.Duration) {
	h.t.Helper()
	require.NoError(h.t, h.bridge.SetReadDeadline(time.Now().Add(wait)))
	line, err := h.lines.ReadString('\n')
	var netErr net.Error
	require.True(h.t, errors.As(err, &netErr) && netErr.Timeout(), "unexpected line %q (error %v)", line, err)
}

func (h *harness) initialize() {
	h.t.Helper()
	h.adapterWrite(`{"type":"control_request","request_id":"init","request":{"subtype":"initialize"}}` + "\n")
	require.JSONEq(h.t, `{"type":"control_response","response":{"subtype":"success","request_id":"init","response":{"capabilities":["ui_surface_v1","prompts_screened_v1"],"pid":1}}}`, h.adapterLine())
	require.JSONEq(h.t, `{"type":"relay_ready","capabilities":["ui_surface_v1","prompts_screened_v1"]}`, h.bridgeLine())
}

func (h *harness) wait() int {
	h.t.Helper()
	_ = h.stdin.Close()
	select {
	case code := <-h.exit:
		return code
	case <-time.After(15 * time.Second):
		h.t.Fatal("relay did not exit")
		return 0
	}
}

// rest drains the adapter's output until the relay closes it.
func (h *harness) rest() string {
	h.t.Helper()
	var out strings.Builder
	for {
		select {
		case line, ok := <-h.stdout:
			if !ok {
				return out.String()
			}
			out.Write(line)
		case <-time.After(15 * time.Second):
			h.t.Fatal("relay stdout not closed")
		}
	}
}

func TestStreamJSONNeedsBothSides(t *testing.T) {
	cases := map[string]struct {
		args []string
		want bool
	}{
		"two-arg form":      {streamArgs, true},
		"= form":            {[]string{"--input-format=stream-json", "--output-format=stream-json"}, true},
		"mixed forms":       {[]string{"--output-format=stream-json", "--input-format", "stream-json"}, true},
		"output only":       {[]string{"-p", "--output-format", "stream-json"}, false},
		"text input":        {[]string{"--input-format", "text", "--output-format", "stream-json"}, false},
		"value missing":     {[]string{"--output-format", "stream-json", "--input-format"}, false},
		"no args":           {nil, false},
		"last value counts": {[]string{"--input-format=stream-json", "--output-format=stream-json", "--output-format=json"}, false},
	}
	for name, tc := range cases {
		require.Equal(t, tc.want, streamJSON(tc.args), name)
	}
}

func TestChildEnvDropsTheBridgeVariables(t *testing.T) {
	env := []string{"PATH=/bin", envClaudeBin + "=/opt/claude", envPaneSocket + "=/tmp/x/pane.sock", envExecutable + "=/usr/local/bin/linuxio-claude-relay", "HOME=/home/ana"}
	require.Equal(t, []string{"PATH=/bin", "HOME=/home/ana", envExecutable + "=/opt/claude"}, childEnv(env, "/opt/claude"))
}

func TestRunWithoutClaudeBinExitsWithUsage(t *testing.T) {
	var stderr syncBuffer
	code := run(process{args: streamArgs, env: []string{"PATH=/bin"}, stderr: &stderr, exec: func(string, []string, []string) error {
		t.Error("must not exec")
		return nil
	}})
	require.Equal(t, 2, code)
	require.Equal(t, 1, strings.Count(stderr.String(), "\n"))
	require.Contains(t, stderr.String(), envClaudeBin)
}

func TestRunPassesThroughWhenItCannotRelay(t *testing.T) {
	listening := filepath.Join(shortTempDir(t), "pane.sock")
	listener, err := net.Listen("unix", listening)
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	missing := filepath.Join(shortTempDir(t), "gone.sock")

	cases := map[string]struct {
		args   []string
		socket string
	}{
		"text output":        {[]string{"-p", "--output-format", "text"}, listening},
		"no socket variable": {streamArgs, ""},
		"socket unreachable": {streamArgs, missing},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			env := []string{"PATH=/bin", envClaudeBin + "=/opt/claude/bin/claude", envExecutable + "=/usr/local/bin/linuxio-claude-relay"}
			if tc.socket != "" {
				env = append(env, envPaneSocket+"="+tc.socket)
			}
			var gotPath string
			var gotArgv, gotEnv []string
			var stderr syncBuffer
			code := run(process{args: tc.args, env: env, stderr: &stderr, exec: func(path string, argv, envv []string) error {
				gotPath, gotArgv, gotEnv = path, argv, envv
				return syscall.ENOENT
			}})
			require.Equal(t, "/opt/claude/bin/claude", gotPath)
			require.Equal(t, append([]string{"/opt/claude/bin/claude"}, tc.args...), gotArgv)
			require.Equal(t, []string{"PATH=/bin", envExecutable + "=/opt/claude/bin/claude"}, gotEnv)
			// A real exec does not return; a failed one is reported.
			require.Equal(t, exitCannotRun, code)
			require.Contains(t, stderr.String(), "no such file")
		})
	}
}

func TestRelayCopiesBothDirectionsByteForByte(t *testing.T) {
	h := startRelay(t, "echo")
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize"}`,
		"caf\xe9 is not UTF-8",
		`"control_response" is not JSON here {`,
		`{"type":"control_response","response":{"subtype":"success","request_id":"linuxio-pane-1","response":{}}}`,
		pushedPanes,
		"",
		`{"type":"assistant","message":{"content":[{"type":"text","text":"` + strings.Repeat("y", 300<<10) + `"}]}}`,
		"no newline at the end",
	}, "\n")
	h.adapterWrite(input)
	require.Equal(t, 0, h.wait())
	require.Equal(t, input, h.rest())
	// The ui_ system line went to the bridge as well.
	requireVerbatim(t, pushedPanes+"\n", h.bridgeLine())
}

func TestRelayCarriesPaneRequests(t *testing.T) {
	h := startRelay(t, "claude")
	h.initialize()

	h.bridgeWrite(`{"id":"p1","request":{"subtype":"ui_panes","client_id":"linuxio-1"}}`)
	require.JSONEq(t, `{"type":"pane_response","id":"p1","ok":true,"response":{"echo":{"subtype":"ui_panes","client_id":"linuxio-1"}}}`, h.bridgeLine())

	h.bridgeWrite(`{"id":"p2","request":{"subtype":"ui_fail"}}`)
	require.JSONEq(t, `{"type":"pane_response","id":"p2","ok":false,"error":"no such pane"}`, h.bridgeLine())

	h.bridgeWrite(`{"id":"p3","request":{"subtype":"ui_big"}}`)
	require.JSONEq(t, `{"type":"pane_response","id":"p3","ok":false,"error":"the response is over 4 MiB"}`, h.bridgeLine())

	// Pushed ui_ messages reach both the bridge, verbatim, and the adapter.
	h.adapterWrite(`{"fake":"push"}` + "\n")
	requireVerbatim(t, pushedPanes+"\n", h.bridgeLine())
	requireVerbatim(t, pushedLog+"\n", h.bridgeLine())
	requireVerbatim(t, pushedPanes+"\n", h.adapterLine())
	requireVerbatim(t, pushedLog+"\n", h.adapterLine())
	require.Contains(t, h.adapterLine(), `"subtype":"init"`)
	h.noBridgeLine(200 * time.Millisecond)

	// The answers to the relay's own requests never reach the adapter.
	h.adapterWrite(`{"type":"user","text":"after"}` + "\n")
	require.JSONEq(t, `{"type":"user","text":"after"}`, h.adapterLine())
	require.Equal(t, 0, h.wait())
	require.NotContains(t, h.rest(), requestIDPrefix)
}

func TestRelayRefusesRequests(t *testing.T) {
	h := startRelay(t, "claude")

	h.bridgeWrite(`{"id":"early","request":{"subtype":"ui_panes"}}`)
	require.JSONEq(t, `{"type":"pane_response","id":"early","ok":false,"error":"Claude Code is not ready"}`, h.bridgeLine())

	h.initialize()

	h.bridgeWrite(`{"id":"p1","request":{"subtype":"interrupt"}}`)
	require.JSONEq(t, `{"type":"pane_response","id":"p1","ok":false,"error":"only ui_ requests are relayed"}`, h.bridgeLine())

	h.bridgeWrite(`{"id":"p2","request":"ui_panes"}`)
	require.JSONEq(t, `{"type":"pane_response","id":"p2","ok":false,"error":"the request is not {\"id\",\"request\"}"}`, h.bridgeLine())

	h.bridgeWrite(`not json`)
	require.JSONEq(t, `{"type":"pane_response","id":"","ok":false,"error":"the request is not {\"id\",\"request\"}"}`, h.bridgeLine())

	h.bridgeWrite(`{"id":"big","request":{"subtype":"ui_panes","pad":"` + strings.Repeat("z", 64<<10) + `"}}`)
	require.JSONEq(t, `{"type":"pane_response","id":"","ok":false,"error":"the request is over 64 KiB"}`, h.bridgeLine())

	// The relay keeps reading after a refusal.
	h.bridgeWrite(`{"id":"p3","request":{"subtype":"ui_panes"}}`)
	require.JSONEq(t, `{"type":"pane_response","id":"p3","ok":true,"response":{"echo":{"subtype":"ui_panes"}}}`, h.bridgeLine())

	for i := range maxInFlight {
		h.bridgeWrite(fmt.Sprintf(`{"id":"s%d","request":{"subtype":"ui_silent"}}`, i))
	}
	h.bridgeWrite(`{"id":"one-too-many","request":{"subtype":"ui_panes"}}`)
	require.JSONEq(t, `{"type":"pane_response","id":"one-too-many","ok":false,"error":"too many requests in flight"}`, h.bridgeLine())
	require.Equal(t, 0, h.wait())
}

func TestRelayKeepsAdapterLinesWhole(t *testing.T) {
	h := startRelay(t, "claude")
	h.initialize()

	big := `{"type":"user","message":{"content":"` + strings.Repeat("b", 6<<20) + `"}}` + "\n"
	half := len(big) / 2
	h.adapterWrite(big[:half])
	// The request waits until the adapter's line is complete.
	h.bridgeWrite(`{"id":"p1","request":{"subtype":"ui_panes"}}`)
	h.noBridgeLine(300 * time.Millisecond)
	h.adapterWrite(big[half:])

	require.JSONEq(t, `{"type":"pane_response","id":"p1","ok":true,"response":{"echo":{"subtype":"ui_panes"}}}`, h.bridgeLine())
	require.Equal(t, big, h.adapterLine())
	require.Equal(t, 0, h.wait())
}

func TestRelaySurvivesTheSocketGoingAway(t *testing.T) {
	h := startRelay(t, "claude")
	h.initialize()
	require.NoError(t, h.bridge.Close())

	h.adapterWrite(`{"type":"user","text":"still here"}` + "\n")
	require.JSONEq(t, `{"type":"user","text":"still here"}`, h.adapterLine())
	h.adapterWrite(`{"fake":"push"}` + "\n")
	requireVerbatim(t, pushedPanes+"\n", h.adapterLine())
	requireVerbatim(t, pushedLog+"\n", h.adapterLine())
	require.Contains(t, h.adapterLine(), `"subtype":"init"`)
	require.Equal(t, 0, h.wait())
}

func TestRelayExitsWithClaudesStatus(t *testing.T) {
	h := startRelay(t, "echo", "FAKE_EXIT=3")
	require.Equal(t, 3, h.wait())
}

func TestRelayForwardsSignals(t *testing.T) {
	h := startRelay(t, "claude")
	h.signals <- syscall.SIGTERM
	select {
	case code := <-h.exit:
		require.Equal(t, 128+int(syscall.SIGTERM), code)
	case <-time.After(15 * time.Second):
		t.Fatal("Claude Code did not get the signal")
	}
}

func TestRelayScrubsClaudesEnvironment(t *testing.T) {
	h := startRelay(t, "claude")
	h.adapterWrite(`{"fake":"env"}` + "\n")
	var env struct {
		Executable string `json:"executable"`
		HasBin     bool   `json:"has_bin"`
		HasSocket  bool   `json:"has_socket"`
	}
	require.NoError(t, json.Unmarshal([]byte(h.adapterLine()), &env))
	require.Equal(t, testExecutable(t), env.Executable)
	require.False(t, env.HasBin)
	require.False(t, env.HasSocket)
	require.Equal(t, 0, h.wait())
}
