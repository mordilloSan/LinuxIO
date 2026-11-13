package assistant

import (
	"bufio"
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/acpagents"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/streamrelay"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/usercmd"
	"github.com/mordilloSan/LinuxIO/backend/common/peercred"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

// paneRelayPath is the relay the adapter runs in place of the agent's CLI. A
// variable so tests can point it elsewhere.
var paneRelayPath = filepath.Join(version.BinDir, "linuxio-claude-relay")

const (
	maxPaneConns     = 16
	maxPaneRequest   = 64 << 10 // a browser line for a relay
	maxPaneMessage   = 4 << 20  // a relay line for the browser
	paneWriteTimeout = 2 * time.Second
	paneBufSize      = 32 * 1024
)

// panePrefix starts every browser line meant for a relay.
var panePrefix = []byte(`{"linuxio":"pane",`)

// paneHost carries plugin panes for one assistant.open stream. Claude Code
// serves them over control requests on its stream-JSON stdio, which
// claude-agent-acp does not carry, so the adapter runs linuxio-claude-relay
// in place of the CLI. Each relay (a chat, a fork and a resumed chat each
// start one) connects to a unix socket private to this stream, and its lines
// travel in-band:
//
//	to the browser:   {"conn":1,"event":"open","linuxio":"pane"}
//	                  {"conn":1,"linuxio":"pane","message":<relay line>}
//	                  {"conn":1,"event":"close","linuxio":"pane"}
//	from the browser: {"linuxio":"pane","conn":1,"id":"p7","request":{...}}
//
// A pane line is only ever written between two agent lines. The socket
// carries no root action, and only the session user can reach it.
type paneHost struct {
	dir      string
	socket   string
	uid      uint32
	listener *net.UnixListener
	stream   net.Conn
	// out is held from the first byte of an agent line to its newline, and
	// while a pane line is written, so the two never interleave.
	out sync.Mutex

	mu     sync.Mutex
	conns  map[int]*net.UnixConn
	next   int
	closed bool

	wg        sync.WaitGroup
	closeOnce sync.Once
}

// paneLine is an in-band line to the browser.
type paneLine struct {
	Conn    int            `json:"conn"`
	Event   string         `json:"event,omitzero"`
	LinuxIO string         `json:"linuxio"`
	Message jsontext.Value `json:"message,omitzero"`
}

// relayRequest is a browser request as the relay takes it.
type relayRequest struct {
	ID      string         `json:"id"`
	Request jsontext.Value `json:"request"`
}

// openPanes makes the pane socket for a stream that asked for panes. It
// returns nil when the stream runs without them, exactly as before panes.
func openPanes(stream net.Conn, req apischema.AssistantOpenRequest, agent acpagents.Agent, user session.User) *paneHost {
	if !req.Panes {
		return nil
	}
	if !agent.Panes || agent.CLI == "" {
		slog.Debug("assistant panes off", "agent", req.Agent, "reason", "agent has no panes")
		return nil
	}
	if _, err := usercmd.LookPathIn("", paneRelayPath); err != nil {
		slog.Debug("assistant panes off", "agent", req.Agent, "reason", "relay not installed", "error", err)
		return nil
	}
	host, err := listenPanes(stream, user)
	if err != nil {
		slog.Warn("assistant panes off", "agent", req.Agent, "error", err)
		return nil
	}
	slog.Debug("assistant panes on", "agent", req.Agent, "socket", host.socket)
	return host
}

// listenPanes creates a fresh 0700 directory holding one 0600 socket, both
// the session user's.
func listenPanes(stream net.Conn, user session.User) (*paneHost, error) {
	dir, err := os.MkdirTemp("", "linuxio-panes-*")
	if err != nil {
		return nil, fmt.Errorf("create pane socket directory: %w", err)
	}
	socket := filepath.Join(dir, "pane.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("listen on pane socket: %w", err)
	}
	// The directory is removed whole. Unlinking the socket by a path through a
	// directory the user owns would follow whatever the user put there.
	listener.SetUnlinkOnClose(false)
	if err := ownPaneSocket(dir, socket, user); err != nil {
		_ = listener.Close()
		_ = os.RemoveAll(dir)
		return nil, err
	}
	return &paneHost{dir: dir, socket: socket, uid: user.UID, listener: listener, stream: stream, conns: make(map[int]*net.UnixConn)}, nil
}

func ownPaneSocket(dir, socket string, user session.User) error {
	if err := os.Chmod(socket, 0o600); err != nil {
		return fmt.Errorf("restrict pane socket: %w", err)
	}
	if os.Geteuid() != 0 {
		return nil
	}
	// The socket first: until the directory is the user's, only root can
	// reach it.
	if err := os.Lchown(socket, int(user.UID), int(user.GID)); err != nil {
		return fmt.Errorf("hand pane socket to %s: %w", user.Username, err)
	}
	if err := os.Chown(dir, int(user.UID), int(user.GID)); err != nil {
		return fmt.Errorf("hand pane socket directory to %s: %w", user.Username, err)
	}
	return nil
}

// agentEnv points the adapter at the relay in place of the CLI at cli, whose
// variable is cliEnv. It goes after the launch environment, since exec keeps
// the last value of a repeated variable.
func (h *paneHost) agentEnv(cliEnv, cli string) []string {
	return []string{
		cliEnv + "=" + paneRelayPath,
		"LINUXIO_CLAUDE_BIN=" + cli,
		"LINUXIO_PANE_SOCKET=" + h.socket,
	}
}

// serve accepts relay connections until close.
func (h *paneHost) serve() {
	h.wg.Go(h.accept)
}

func (h *paneHost) accept() {
	for {
		conn, err := h.listener.AcceptUnix()
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				slog.Warn("assistant pane socket stopped accepting", "error", err)
			}
			return
		}
		h.admit(conn)
	}
}

func (h *paneHost) admit(conn *net.UnixConn) {
	cred, err := peercred.Read(conn)
	if err != nil {
		slog.Warn("assistant pane connection refused", "error", err)
		_ = conn.Close()
		return
	}
	if cred.UID != h.uid {
		slog.Warn("assistant pane connection refused", "reason", "peer is not the session user", "peer_uid", cred.UID, "uid", h.uid)
		_ = conn.Close()
		return
	}
	h.mu.Lock()
	if h.closed || len(h.conns) >= maxPaneConns {
		h.mu.Unlock()
		// Any process of the session user can connect, so this stays below the
		// level a flood of connections could fill the journal at.
		slog.Debug("assistant pane connection refused", "reason", "too many connections", "limit", maxPaneConns)
		_ = conn.Close()
		return
	}
	h.next++
	id := h.next
	h.conns[id] = conn
	h.mu.Unlock()

	// The open line goes out before the reader starts, so it precedes every
	// message of the connection.
	if err := h.writeLine(paneLine{Conn: id, Event: "open", LinuxIO: "pane"}); err != nil {
		slog.Debug("assistant pane open line not sent", "conn", id, "error", err)
	}
	h.wg.Go(func() { h.read(id, conn) })
}

// read carries one relay's lines to the browser until the relay hangs up or
// the stream ends.
func (h *paneHost) read(id int, conn *net.UnixConn) {
	defer func() {
		h.mu.Lock()
		delete(h.conns, id)
		h.mu.Unlock()
		_ = conn.Close()
		if err := h.writeLine(paneLine{Conn: id, Event: "close", LinuxIO: "pane"}); err != nil {
			slog.Debug("assistant pane close line not sent", "conn", id, "error", err)
		}
	}()
	reader := bufio.NewReaderSize(conn, paneBufSize)
	for {
		line, tooLong, err := readLine(reader, maxPaneMessage)
		if err != nil {
			return
		}
		switch {
		case tooLong:
			slog.Debug("assistant pane line dropped", "conn", id, "reason", "over 4 MiB")
		case !jsontext.Value(line).IsValid():
			slog.Debug("assistant pane line dropped", "conn", id, "reason", "not one JSON value")
		default:
			if err := h.writeLine(paneLine{Conn: id, LinuxIO: "pane", Message: line}); err != nil {
				slog.Debug("assistant pane line not sent", "conn", id, "error", err)
				return
			}
		}
	}
}

func (h *paneHost) writeLine(line paneLine) error {
	data, err := json.Marshal(line)
	if err != nil {
		return err
	}
	h.out.Lock()
	defer h.out.Unlock()
	return streamrelay.WriteData(h.stream, append(data, '\n'))
}

// request hands one browser pane line to the relay connection it names.
func (h *paneHost) request(line []byte) {
	var req struct {
		Conn    int            `json:"conn"`
		ID      string         `json:"id"`
		Request jsontext.Value `json:"request"`
	}
	if err := json.Unmarshal(line, &req); err != nil {
		slog.Debug("assistant pane request dropped", "reason", "malformed", "error", err)
		return
	}
	// The relay checks the request itself and answers a bad one, so the
	// browser hears back; one with no request has nothing to answer.
	if len(req.Request) == 0 {
		slog.Debug("assistant pane request dropped", "reason", "no request")
		return
	}
	h.mu.Lock()
	conn := h.conns[req.Conn]
	h.mu.Unlock()
	if conn == nil {
		slog.Debug("assistant pane request dropped", "reason", "unknown conn", "conn", req.Conn)
		return
	}
	data, err := json.Marshal(relayRequest{ID: req.ID, Request: req.Request})
	if err != nil {
		slog.Debug("assistant pane request dropped", "conn", req.Conn, "error", err)
		return
	}
	// A relay that stops reading loses its pane, never stalls the chat.
	_ = conn.SetWriteDeadline(time.Now().Add(paneWriteTimeout))
	if _, err := conn.Write(append(data, '\n')); err != nil {
		slog.Debug("assistant pane connection dropped", "conn", req.Conn, "error", err)
		_ = conn.Close()
	}
}

// pumpAgentOutput is streamrelay.PumpToStream for a stream that also carries
// pane lines: it holds out from the first byte of an agent line to its
// newline, and frames end at a newline where they can, so a pane line waits
// at most for the agent line in progress.
func (h *paneHost) pumpAgentOutput(src io.Reader) error {
	buf := make([]byte, paneBufSize)
	held := false
	defer func() {
		if held {
			h.out.Unlock()
		}
	}()
	for {
		n, err := src.Read(buf)
		data := buf[:n]
		for len(data) > 0 {
			if !held {
				h.out.Lock()
				held = true
			}
			end := len(data)
			if i := bytes.LastIndexByte(data, '\n'); i >= 0 {
				end = i + 1
			}
			if werr := streamrelay.WriteData(h.stream, data[:end]); werr != nil {
				return fmt.Errorf("%w: %w", streamrelay.ErrStreamWrite, werr)
			}
			if data[end-1] == '\n' {
				h.out.Unlock()
				held = false
			}
			data = data[end:]
		}
		if err != nil {
			return err
		}
	}
}

// close stops accepting, hangs up every relay, waits for their close lines
// and removes the socket directory. A nil host has nothing to close.
func (h *paneHost) close() {
	if h == nil {
		return
	}
	h.closeOnce.Do(func() {
		_ = h.listener.Close()
		h.mu.Lock()
		h.closed = true
		for _, conn := range h.conns {
			_ = conn.Close()
		}
		h.mu.Unlock()
		h.wg.Wait()
		// The session user owns the directory by now: remove the two names
		// the bridge made and never walk a tree that user can fill.
		_ = os.Remove(h.socket)
		if err := os.Remove(h.dir); err != nil && !errors.Is(err, fs.ErrNotExist) {
			slog.Warn("assistant pane socket directory not removed", "dir", h.dir, "error", err)
		}
	})
}

// splitState is where a paneSplitter is within the current browser line.
type splitState int

const (
	stateStart splitState = iota // held holds a prefix of panePrefix
	stateAgent                   // the rest of the line goes to the agent
	statePane                    // held collects a pane line
	stateSkip                    // the rest of an oversized pane line is dropped
)

// paneSplitter is the agent's stdin on a stream with panes: a browser line
// that starts with panePrefix goes to a relay, every other byte to the agent
// unchanged. A line is classified from its first bytes, so an agent line is
// passed on as it arrives, never held whole.
type paneSplitter struct {
	host  *paneHost
	agent io.Writer
	state splitState
	held  []byte
}

func (h *paneHost) splitter(agent io.Writer) *paneSplitter {
	return &paneSplitter{host: h, agent: agent}
}

func (s *paneSplitter) Write(p []byte) (int, error) {
	total := len(p)
	for len(p) > 0 {
		var err error
		switch s.state {
		case stateStart:
			p, err = s.classify(p)
		case stateAgent:
			end := len(p)
			if i := bytes.IndexByte(p, '\n'); i >= 0 {
				end = i + 1
				s.state = stateStart
			}
			_, err = s.agent.Write(p[:end])
			p = p[end:]
		case statePane:
			p = s.collect(p)
		case stateSkip:
			i := bytes.IndexByte(p, '\n')
			if i < 0 {
				return total, nil
			}
			p = p[i+1:]
			s.state = stateStart
		}
		if err != nil {
			return total - len(p), err
		}
	}
	return total, nil
}

// classify takes bytes while they still match panePrefix. On the first byte
// that does not, the bytes held back go to the agent and the line is the
// agent's.
func (s *paneSplitter) classify(p []byte) ([]byte, error) {
	for len(p) > 0 {
		if p[0] != panePrefix[len(s.held)] {
			s.state = stateAgent
			if len(s.held) == 0 {
				return p, nil
			}
			_, err := s.agent.Write(s.held)
			s.held = s.held[:0]
			return p, err
		}
		s.held = append(s.held, p[0])
		p = p[1:]
		if len(s.held) == len(panePrefix) {
			s.state = statePane
			return p, nil
		}
	}
	return p, nil
}

// collect gathers a pane line and hands it over at its newline.
func (s *paneSplitter) collect(p []byte) []byte {
	end := bytes.IndexByte(p, '\n')
	if end < 0 {
		end = len(p)
	}
	if len(s.held)+end > maxPaneRequest {
		slog.Debug("assistant pane request dropped", "reason", "over 64 KiB")
		s.held = s.held[:0]
		s.state = stateSkip
		return p
	}
	s.held = append(s.held, p[:end]...)
	if end == len(p) {
		return nil
	}
	s.host.request(s.held)
	s.held = s.held[:0]
	s.state = stateStart
	return p[end+1:]
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
