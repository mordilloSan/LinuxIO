package jobsclient

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/jobshost"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
	"github.com/mordilloSan/LinuxIO/backend/common/jobs"
	"github.com/mordilloSan/LinuxIO/backend/common/peercred"
)

const (
	jobA      = "00000000-0000-4000-8000-00000000000a"
	ioTimeout = 5 * time.Second
)

type testHost struct {
	socket string
	store  *jobs.Store
}

// startHost serves a job host for uid on a temp socket. Its router holds the
// task-service built-ins and test.job (reports progress, returns {"ok":true}).
func startHost(t *testing.T, uid uint32, configure ...func(*jobshost.Config)) *testHost {
	t.Helper()
	dir := t.TempDir()
	th := &testHost{
		socket: filepath.Join(dir, "host.sock"),
		store:  jobs.NewStore(filepath.Join(dir, "state"), filepath.Join(dir, "run")),
	}
	tasks := bridgeipc.NewTaskService()
	router := bridgeipc.NewRouter(tasks)
	tasks.RegisterRoutes(router)
	router.Mutation("test.job", func(ctx context.Context, _ *bridgeipc.Task, _ any) (any, error) {
		bridgeipc.ReportProgress(ctx, bridgeipc.TaskProgress{Phase: "copy", Message: "halfway"})
		return map[string]bool{"ok": true}, nil
	})

	ready := make(chan struct{})
	cfg := jobshost.Config{
		UID:        uid,
		GID:        uint32(os.Getgid()),
		Username:   "host-user",
		SocketPath: th.socket,
		Store:      th.store,
		Router:     router,
		Tasks:      tasks,
		IdleExit:   time.Hour,
		MaxRunning: 256,
		Notify: func(state string) error {
			if state == "READY=1" {
				close(ready)
			}
			return nil
		},
	}
	if uid != uint32(os.Getuid()) {
		// A host for another uid admits only that uid; the test claims it.
		cfg.Peer = func(*net.UnixConn) (peercred.Cred, error) { return peercred.Cred{UID: uid}, nil }
	}
	for _, fn := range configure {
		fn(&cfg)
	}
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- jobshost.Serve(ctx, cfg) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-served:
			if err != nil {
				t.Errorf("Serve = %v, want nil", err)
			}
		case <-time.After(ioTimeout):
			t.Error("Serve did not return")
		}
	})
	select {
	case <-ready:
	case err := <-served:
		t.Fatalf("Serve = %v before ready", err)
	case <-time.After(ioTimeout):
		t.Fatal("job host not ready")
	}
	return th
}

// asRoot makes Connect take the root path with a fake StartUnit that records
// the units it was asked to start.
func asRoot(t *testing.T, root bool) *[]string {
	t.Helper()
	oldRoot, oldStart := isRoot, startUnit
	t.Cleanup(func() { isRoot, startUnit = oldRoot, oldStart })
	var started []string
	isRoot = func() bool { return root }
	startUnit = func(_ context.Context, unit string) error {
		started = append(started, unit)
		return nil
	}
	return &started
}

func connect(t *testing.T, th *testHost, uid uint32, hello bridgeipc.HelloSession) *Client {
	t.Helper()
	c, err := Connect(t.Context(), uid, hello, th.socket)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func readFrame(t *testing.T, conn net.Conn) *relay.StreamFrame {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(ioTimeout))
	frame, err := relay.ReadRelayFrame(conn)
	if err != nil {
		t.Fatalf("ReadRelayFrame: %v", err)
	}
	return frame
}

func compact(t *testing.T, value jsontext.Value) string {
	t.Helper()
	value = value.Clone()
	if err := value.Compact(); err != nil {
		t.Fatalf("compact %s: %v", value, err)
	}
	return string(value)
}

func TestClientConnectSendsHelloAndOpensMutation(t *testing.T) {
	uid := uint32(os.Getuid())
	th := startHost(t, uid)
	hello := bridgeipc.HelloSession{Username: "alice", UID: uid, SessionRef: "ref-1"}

	// As root the client starts the uid's host unit itself.
	started := asRoot(t, true)
	c := connect(t, th, uid, hello)
	if !slices.Equal(*started, []string{socketUnitName(uid)}) {
		t.Fatalf("started units = %v, want [%s]", *started, socketUnitName(uid))
	}
	if c.Protocol() != jobs.Protocol {
		t.Fatalf("Protocol() = %d, want %d", c.Protocol(), jobs.Protocol)
	}

	raw, err := json.Marshal(relay.StreamOpenEnvelope{Route: "test.job", Request: jsontext.Value(`{}`), ID: jobA})
	if err != nil {
		t.Fatalf("json.Marshal(open): %v", err)
	}
	stream, err := c.Open(t.Context(), raw)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer stream.Close()
	var event bridgeipc.TaskEvent
	if frame := readFrame(t, stream); frame.Opcode != relay.OpStreamProgress || json.Unmarshal(frame.Payload, &event) != nil ||
		event.Type != bridgeipc.TaskEventSnapshot || event.Task.ID != jobA {
		t.Fatalf("first frame = 0x%02x %s, want the task.snapshot of %s", frame.Opcode, frame.Payload, jobA)
	}
	// The job runs for the session the hello introduced.
	if event.Task.Owner.Username != "alice" {
		t.Fatalf("job owner = %q, want alice from the hello", event.Task.Owner.Username)
	}
	for {
		frame := readFrame(t, stream)
		if frame.Opcode != relay.OpStreamResult {
			continue
		}
		var result relay.ResultFrame
		if err = json.Unmarshal(frame.Payload, &result); err != nil || result.Status != "ok" || compact(t, result.Data) != `{"ok":true}` {
			t.Fatalf("result = %s, want ok with {\"ok\":true}", frame.Payload)
		}
		break
	}

	// As a user it only dials: linuxio-auth started the host at login.
	started = asRoot(t, false)
	user, err := Connect(t.Context(), uid, hello, th.socket)
	if err != nil {
		t.Fatalf("Connect as a user: %v", err)
	}
	_ = user.Close()
	if len(*started) != 0 {
		t.Fatalf("started units as a user = %v, want none", *started)
	}

	hello.SessionRef = ""
	if _, err := Connect(t.Context(), uid, hello, th.socket); err == nil {
		t.Fatal("Connect without a session ref succeeded")
	}
}

func TestClientConnectRetriesAStaleSocket(t *testing.T) {
	uid := uint32(os.Getuid())
	th := startHost(t, uid)
	// A socket file nobody listens on, such as one a stopped socket unit left
	// behind, refuses connections until its unit listens again.
	stale := filepath.Join(t.TempDir(), "host.sock")
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: stale, Net: "unix"})
	if err != nil {
		t.Fatalf("listen stale socket: %v", err)
	}
	ln.SetUnlinkOnClose(false)
	_ = ln.Close()

	oldRoot, oldStart := isRoot, startUnit
	t.Cleanup(func() { isRoot, startUnit = oldRoot, oldStart })
	isRoot = func() bool { return true }
	startUnit = func(context.Context, string) error {
		// The queued start replaces the stale socket a little later.
		time.AfterFunc(4*hostPoll, func() {
			_ = os.Remove(stale)
			_ = os.Symlink(th.socket, stale)
		})
		return nil
	}

	hello := bridgeipc.HelloSession{Username: "alice", UID: uid, SessionRef: "ref-stale"}
	c, err := Connect(t.Context(), uid, hello, stale)
	if err != nil {
		t.Fatalf("Connect through a stale socket: %v", err)
	}
	_ = c.Close()
}

func TestClientConnectFailsWhenNoHostListens(t *testing.T) {
	oldWait := hostWait
	t.Cleanup(func() { hostWait = oldWait })
	hostWait = 4 * hostPoll
	asRoot(t, false)
	uid := uint32(os.Getuid())
	hello := bridgeipc.HelloSession{Username: "alice", UID: uid, SessionRef: "ref-none"}

	started := time.Now()
	_, err := Connect(t.Context(), uid, hello, filepath.Join(t.TempDir(), "host.sock"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Connect with no host = %v, want a missing-socket error", err)
	}
	if elapsed := time.Since(started); elapsed < hostWait {
		t.Fatalf("Connect gave up after %v, want the %v dial window", elapsed, hostWait)
	}
}

func TestClientReturnsUpdatePendingOnProtocolMismatch(t *testing.T) {
	uid := uint32(os.Getuid())
	th := startHost(t, uid)
	asRoot(t, true)
	old := protocol
	protocol = jobs.Protocol + 1
	t.Cleanup(func() { protocol = old })

	c, err := Connect(t.Context(), uid, bridgeipc.HelloSession{Username: "alice", UID: uid, SessionRef: "ref-1"}, th.socket)
	if !errors.Is(err, ErrUpdatePending) || c != nil {
		t.Fatalf("Connect = %v, %v; want ErrUpdatePending", c, err)
	}
}

func TestUserHostRefusesARootPeer(t *testing.T) {
	uid := uint32(os.Getuid()) + 1 // a user host, never root's
	th := startHost(t, uid, func(cfg *jobshost.Config) {
		cfg.Peer = func(*net.UnixConn) (peercred.Cred, error) { return peercred.Cred{UID: 0}, nil }
	})
	asRoot(t, false)
	hello := bridgeipc.HelloSession{Username: "alice", UID: uid, Privileged: true, SessionRef: "root-bridge"}
	if c, err := Connect(t.Context(), uid, hello, th.socket); err == nil {
		_ = c.Close()
		t.Fatal("a user host accepted a hello from uid 0")
	}
}

func TestRelayCopiesFramesBothWaysUntilAClose(t *testing.T) {
	relayed := func(ctx context.Context) (bridgeSide, hostSide net.Conn, done chan error) {
		bridgeSide, from := net.Pipe()
		to, hostSide := net.Pipe()
		t.Cleanup(func() { _ = bridgeSide.Close(); _ = hostSide.Close() })
		done = make(chan error, 1)
		go func() { done <- (&Client{}).Relay(ctx, from, to) }()
		return bridgeSide, hostSide, done
	}
	pass := func(src, dst net.Conn, opcode byte) {
		t.Helper()
		if err := relay.WriteRelayFrame(src, &relay.StreamFrame{Opcode: opcode, Payload: []byte("x")}); err != nil {
			t.Fatalf("write 0x%02x: %v", opcode, err)
		}
		if frame := readFrame(t, dst); frame.Opcode != opcode || string(frame.Payload) != "x" {
			t.Fatalf("relayed frame = 0x%02x %q, want 0x%02x x", frame.Opcode, frame.Payload, opcode)
		}
	}
	finished := func(done chan error) error {
		t.Helper()
		select {
		case err := <-done:
			return err
		case <-time.After(ioTimeout):
			t.Fatal("Relay did not return")
			return nil
		}
	}

	bridgeSide, hostSide, done := relayed(t.Context())
	pass(bridgeSide, hostSide, relay.OpStreamOpen)
	pass(hostSide, bridgeSide, relay.OpStreamProgress)
	pass(bridgeSide, hostSide, relay.OpStreamAbort) // an abort reaches the host as a frame
	_ = hostSide.Close()
	if err := finished(done); err != nil {
		t.Fatalf("Relay after a close = %v, want nil", err)
	}
	if _, err := bridgeSide.Read(make([]byte, 1)); err == nil {
		t.Fatal("bridge side still open after the host side closed")
	}

	ctx, cancel := context.WithCancel(t.Context())
	_, _, done = relayed(ctx)
	cancel()
	if err := finished(done); !errors.Is(err, context.Canceled) {
		t.Fatalf("Relay after cancel = %v, want context.Canceled", err)
	}
}
