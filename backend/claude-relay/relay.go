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
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// The variables the bridge gives the adapter, and so this process.
const (
	envClaudeBin  = "LINUXIO_CLAUDE_BIN"
	envPaneSocket = "LINUXIO_PANE_SOCKET"
	envExecutable = "CLAUDE_CODE_EXECUTABLE"
)

const (
	maxRequestLine = 64 << 10 // a line from the bridge
	maxSocketLine  = 4 << 20  // a line to the bridge
	maxInFlight    = 64       // injected requests Claude Code has not answered
	// maxQueuedBytes bounds the lines waiting for the bridge: a bridge that
	// falls this far behind loses its panes instead of stalling the chat.
	maxQueuedBytes  = 16 << 20
	maxQueuedLines  = 256
	dialTimeout     = 2 * time.Second
	requestIDPrefix = "linuxio-pane-"
	exitCannotRun   = 127
)

var (
	markResponse = []byte(`"control_response"`)
	markUI       = []byte(`"ui_`)
)

// notTakingRequests is the refusal for a request Claude Code's input can no
// longer take.
const notTakingRequests = "Claude Code is not taking requests"

// lenient reads Claude Code's output, so a line that is not strict JSON is
// still checked for the answer to one of the relay's requests.
var lenient = json.JoinOptions(jsontext.AllowDuplicateNames(true), jsontext.AllowInvalidUTF8(true))

// process is what the relay gets from the operating system; tests supply
// their own.
type process struct {
	args   []string // Claude Code's arguments
	env    []string
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
	notify func(chan<- os.Signal)
	exec   func(argv0 string, argv, envv []string) error
}

// run returns the relay's exit status: Claude Code's, or 128+signal when a
// signal killed it.
func run(p process) int {
	claude := lookupEnv(p.env, envClaudeBin)
	if claude == "" {
		fmt.Fprintln(p.stderr, "linuxio-claude-relay: "+envClaudeBin+" is not set")
		return 2
	}
	env := childEnv(p.env, claude)
	socket := lookupEnv(p.env, envPaneSocket)
	if socket == "" || !streamJSON(p.args) {
		return passThrough(p, claude, env)
	}
	conn, err := net.DialTimeout("unix", socket, dialTimeout)
	if err != nil {
		return passThrough(p, claude, env)
	}
	return relayClaude(p, claude, env, conn)
}

// passThrough replaces the relay with Claude Code, so no relay process
// remains.
func passThrough(p process, claude string, env []string) int {
	err := p.exec(claude, append([]string{claude}, p.args...), env)
	// exec returns only when it failed.
	fmt.Fprintf(p.stderr, "linuxio-claude-relay: run %s: %v\n", claude, err)
	return exitCannotRun
}

// streamJSON reports whether args ask Claude Code for stream-JSON on both
// stdin and stdout, the only mode in which it takes control requests.
func streamJSON(args []string) bool {
	var input, output string
	for i, arg := range args {
		name, value, inline := strings.Cut(arg, "=")
		if !inline && i+1 < len(args) {
			value = args[i+1]
		}
		switch name {
		case "--input-format":
			input = value
		case "--output-format":
			output = value
		}
	}
	return input == "stream-json" && output == "stream-json"
}

// childEnv is Claude Code's environment: the relay's, without the bridge's
// variables, and with CLAUDE_CODE_EXECUTABLE naming Claude Code itself, so
// nothing it starts runs the relay again.
func childEnv(env []string, claude string) []string {
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		switch key, _, _ := strings.Cut(kv, "="); key {
		case envClaudeBin, envPaneSocket, envExecutable:
			continue
		}
		out = append(out, kv)
	}
	return append(out, envExecutable+"="+claude)
}

// lookupEnv returns the last value of key in env, as exec would pass it.
func lookupEnv(env []string, key string) string {
	value := ""
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok && k == key {
			value = v
		}
	}
	return value
}

func relayClaude(p process, claude string, env []string, conn net.Conn) int {
	cmd := exec.Command(claude, p.args...)
	cmd.Env = env
	cmd.Stderr = p.stderr
	childIn, inErr := cmd.StdinPipe()
	childOut, outErr := cmd.StdoutPipe()
	err := errors.Join(inErr, outErr)
	if err == nil {
		err = cmd.Start()
	}
	if err != nil {
		_ = conn.Close()
		fmt.Fprintf(p.stderr, "linuxio-claude-relay: run %s: %v\n", claude, err)
		return exitCannotRun
	}

	r := &relay{
		stdinLock: make(chan struct{}, 1),
		childIn:   childIn,
		sock:      conn,
		out:       make(chan []byte, maxQueuedLines),
		stopped:   make(chan struct{}),
		pending:   make(map[string]string),
	}
	signals := make(chan os.Signal, 4)
	if p.notify != nil {
		p.notify(signals)
	}
	exited := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() { forwardSignals(cmd.Process, signals, exited) })
	wg.Go(r.writeSocket)
	wg.Go(r.readSocket)
	// The adapter may keep its end open after Claude Code is gone, and a read
	// from it cannot be interrupted, so this pump is not waited for: it ends
	// at the adapter's EOF, at a failed write to Claude Code, or with the
	// process.
	go r.pumpStdin(p.stdin)

	r.pumpStdout(childOut, p.stdout)
	_ = cmd.Wait()
	close(exited)
	r.stopPanes()
	wg.Wait()
	return exitStatus(cmd.ProcessState)
}

func forwardSignals(child *os.Process, signals <-chan os.Signal, exited <-chan struct{}) {
	for {
		select {
		case sig := <-signals:
			_ = child.Signal(sig)
		case <-exited:
			return
		}
	}
}

func exitStatus(state *os.ProcessState) int {
	if state == nil {
		return exitCannotRun
	}
	if status, ok := state.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return 128 + int(status.Signal())
	}
	return state.ExitCode()
}

type relay struct {
	// stdinLock holds one token while an adapter line is partly written to
	// Claude Code, or an injected request is, so the two never interleave.
	stdinLock   chan struct{}
	childIn     io.WriteCloser
	stdinClosed bool // guarded by stdinLock

	sock    net.Conn
	out     chan []byte // lines waiting for the bridge
	queued  atomic.Int64
	stopped chan struct{} // closed when pane work stops
	stop    sync.Once

	ready   atomic.Bool
	mu      sync.Mutex
	pending map[string]string // relay request_id → the bridge's id
	next    uint64
}

// pumpStdin copies the adapter's input to Claude Code byte for byte. An
// injected request gets in between two lines, never inside one.
func (r *relay) pumpStdin(src io.Reader) {
	buf := make([]byte, 64<<10)
	held := false
	defer func() {
		if !held {
			r.stdinLock <- struct{}{}
		}
		r.stdinClosed = true
		_ = r.childIn.Close()
		<-r.stdinLock
	}()
	for {
		n, err := src.Read(buf)
		data := buf[:n]
		for len(data) > 0 {
			if !held {
				r.stdinLock <- struct{}{}
				held = true
			}
			end := len(data)
			if i := bytes.LastIndexByte(data, '\n'); i >= 0 {
				end = i + 1
			}
			if _, werr := r.childIn.Write(data[:end]); werr != nil {
				return
			}
			if data[end-1] == '\n' {
				<-r.stdinLock
				held = false
			}
			data = data[end:]
		}
		if err != nil {
			return
		}
	}
}

// inject writes one request line to Claude Code between two adapter lines.
func (r *relay) inject(line []byte) error {
	select {
	case r.stdinLock <- struct{}{}:
	case <-r.stopped:
		return net.ErrClosed
	}
	defer func() { <-r.stdinLock }()
	if r.stdinClosed {
		return io.ErrClosedPipe
	}
	_, err := r.childIn.Write(line)
	return err
}

// pumpStdout copies Claude Code's output to the adapter byte for byte,
// leaving out only the answers to the relay's own requests.
func (r *relay) pumpStdout(src io.Reader, dst io.Writer) {
	reader := bufio.NewReaderSize(src, 64<<10)
	adapterGone := false
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 && r.forward(line) && !adapterGone {
			if _, werr := dst.Write(line); werr != nil {
				// Keep draining, so Claude Code is never blocked on its output.
				adapterGone = true
			}
		}
		if err != nil {
			return
		}
	}
}

// forward reports whether a line of Claude Code's output goes on to the
// adapter, and copies what the bridge needs from it. Only a line that can
// matter is parsed.
func (r *relay) forward(line []byte) bool {
	if !bytes.Contains(line, markResponse) && !bytes.Contains(line, markUI) {
		return true
	}
	var msg struct {
		Type     string         `json:"type"`
		Subtype  string         `json:"subtype"`
		Response jsontext.Value `json:"response"`
	}
	if json.Unmarshal(line, &msg, lenient) != nil {
		return true
	}
	switch {
	case msg.Type == "control_response":
		return r.controlResponse(msg.Response)
	case msg.Type == "system" && strings.HasPrefix(msg.Subtype, "ui_"):
		// Nothing writes to line once it is read, so the socket writer can
		// share it. A line cut off by Claude Code's exit is not copied.
		if len(line) <= maxSocketLine+1 && line[len(line)-1] == '\n' {
			r.send(line)
		}
	}
	return true
}

// controlResponse answers the bridge when the response is to one of the
// relay's requests, and reports whether the adapter gets it.
func (r *relay) controlResponse(raw jsontext.Value) bool {
	var resp struct {
		Subtype   string         `json:"subtype"`
		RequestID string         `json:"request_id"`
		Response  jsontext.Value `json:"response"`
		Error     jsontext.Value `json:"error"`
	}
	if json.Unmarshal(raw, &resp, lenient) != nil {
		return true
	}
	if client, ok := r.take(resp.RequestID); ok {
		r.answer(client, resp.Subtype == "success", resp.Response, resp.Error)
		return false
	}
	if !r.ready.Load() {
		// The adapter's initialize: Claude Code is up and says what it serves.
		var init struct {
			Capabilities jsontext.Value `json:"capabilities"`
		}
		if json.Unmarshal(resp.Response, &init, lenient) == nil && init.Capabilities.Kind() == '[' && r.ready.CompareAndSwap(false, true) {
			r.sendJSON(relayReady{Type: "relay_ready", Capabilities: init.Capabilities})
		}
	}
	return true
}

type relayReady struct {
	Type         string         `json:"type"`
	Capabilities jsontext.Value `json:"capabilities"`
}

type paneResponse struct {
	Type     string         `json:"type"`
	ID       string         `json:"id"`
	OK       bool           `json:"ok"`
	Response jsontext.Value `json:"response,omitzero"`
	Error    string         `json:"error,omitzero"`
}

type controlRequest struct {
	Type      string         `json:"type"`
	RequestID string         `json:"request_id"`
	Request   jsontext.Value `json:"request"`
}

func (r *relay) answer(client string, ok bool, response, errValue jsontext.Value) {
	reply := paneResponse{Type: "pane_response", ID: client, OK: ok}
	if ok {
		reply.Response = response
		if len(reply.Response) == 0 {
			reply.Response = jsontext.Value("{}")
		}
	} else if json.Unmarshal(errValue, &reply.Error) != nil || reply.Error == "" {
		reply.Error = "Claude Code refused the request"
	}
	line, err := json.Marshal(reply)
	switch {
	case err != nil:
		r.refuse(client, "the response is not valid JSON")
	case len(line) > maxSocketLine:
		r.refuse(client, "the response is over 4 MiB")
	default:
		r.send(append(line, '\n'))
	}
}

func (r *relay) refuse(client, reason string) {
	r.sendJSON(paneResponse{Type: "pane_response", ID: client, Error: reason})
}

func (r *relay) sendJSON(v any) {
	line, err := json.Marshal(v)
	if err != nil {
		return
	}
	r.send(append(line, '\n'))
}

// send queues a line for the bridge without ever blocking the chat: a
// bridge that falls behind loses its panes.
func (r *relay) send(line []byte) {
	select {
	case <-r.stopped:
		return
	default:
	}
	if r.queued.Add(int64(len(line))) > maxQueuedBytes {
		r.stopPanes()
		return
	}
	select {
	case r.out <- line:
	default:
		r.stopPanes()
	}
}

func (r *relay) writeSocket() {
	for {
		select {
		case <-r.stopped:
			return
		case line := <-r.out:
			if _, err := r.sock.Write(line); err != nil {
				r.stopPanes()
				return
			}
			r.queued.Add(-int64(len(line)))
		}
	}
}

// stopPanes ends pane work; the chat goes on.
func (r *relay) stopPanes() {
	r.stop.Do(func() {
		close(r.stopped)
		_ = r.sock.Close()
	})
}

func (r *relay) readSocket() {
	defer r.stopPanes()
	reader := bufio.NewReader(r.sock)
	for {
		line, tooLong, err := readLine(reader, maxRequestLine)
		if err != nil {
			return
		}
		if tooLong {
			r.refuse("", "the request is over 64 KiB")
			continue
		}
		r.request(line)
	}
}

// request injects one bridge request into Claude Code's input:
// {"id":"<client id>","request":{"subtype":"ui_...",...}}.
func (r *relay) request(line []byte) {
	var req struct {
		ID      string         `json:"id"`
		Request jsontext.Value `json:"request"`
	}
	if json.Unmarshal(line, &req) != nil {
		r.refuse("", "the request is not {\"id\",\"request\"}")
		return
	}
	var head struct {
		Subtype string `json:"subtype"`
	}
	if req.Request.Kind() != '{' || json.Unmarshal(req.Request, &head) != nil {
		r.refuse(req.ID, "the request is not {\"id\",\"request\"}")
		return
	}
	if !strings.HasPrefix(head.Subtype, "ui_") {
		r.refuse(req.ID, "only ui_ requests are relayed")
		return
	}
	if !r.ready.Load() {
		r.refuse(req.ID, "Claude Code is not ready")
		return
	}
	id, ok := r.track(req.ID)
	if !ok {
		r.refuse(req.ID, "too many requests in flight")
		return
	}
	// The line came from one newline-delimited line, so it holds no newline.
	ctrl, err := json.Marshal(controlRequest{Type: "control_request", RequestID: id, Request: req.Request})
	if err == nil {
		err = r.inject(append(ctrl, '\n'))
	}
	if err != nil {
		r.take(id)
		r.refuse(req.ID, notTakingRequests)
	}
}

// track allocates a relay request_id for a bridge request.
func (r *relay) track(client string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.pending) >= maxInFlight {
		return "", false
	}
	r.next++
	id := requestIDPrefix + strconv.FormatUint(r.next, 10)
	r.pending[id] = client
	return id, true
}

// take forgets a relay request_id and returns the bridge's id for it.
func (r *relay) take(id string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	client, ok := r.pending[id]
	delete(r.pending, id)
	return client, ok
}

// readLine reads one newline-terminated line of at most limit bytes and
// returns it without the newline. A longer line is consumed and reported as
// too long; a line cut off by EOF is dropped.
func readLine(reader *bufio.Reader, limit int) ([]byte, bool, error) {
	var line []byte
	tooLong := false
	for {
		chunk, err := reader.ReadSlice('\n')
		if !tooLong {
			if len(line)+len(chunk) > limit+1 {
				line, tooLong = nil, true
			} else {
				line = append(line, chunk...)
			}
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if err != nil {
			return nil, false, err
		}
		if tooLong {
			return nil, true, nil
		}
		return line[:len(line)-1], false, nil
	}
}
