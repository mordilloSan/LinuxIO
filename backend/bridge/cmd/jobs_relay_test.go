package cmd

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/jobsclient"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/jobshost"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
	"github.com/mordilloSan/LinuxIO/backend/common/jobs"
	"github.com/mordilloSan/LinuxIO/backend/common/peercred"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

const (
	relayTestJobID = "00000000-0000-4000-8000-0000000000c1"
	// Jobs of the job host the relay waits for, or reads about.
	hostJobOne   = "00000000-0000-4000-8000-0000000000f1"
	hostJobTwo   = "00000000-0000-4000-8000-0000000000f2"
	hostJobThree = "00000000-0000-4000-8000-0000000000f3"
	hostJobFour  = "00000000-0000-4000-8000-0000000000f4"
)

// relayTestSockets points the relay at hostSocket, and at an empty store
// standing for its host's, for the rest of the test: no test reads the records
// of this machine's job host.
func relayTestSockets(t *testing.T, hostSocket string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("as root jobsclient.Connect starts the host's socket unit through systemd; that StartUnit seam is internal to jobsclient, whose tests cover the root path with it stubbed")
	}
	oldHost := jobsHostSocket
	jobsHostSocket = func(uint32) string { return hostSocket }
	t.Cleanup(func() { jobsHostSocket = oldHost })
	relayTestStore(t, t.TempDir())
}

// relayTestStore points the relay at the store under dir, standing for its
// job host's, for the rest of the test, and returns it.
func relayTestStore(t *testing.T, dir string) *jobs.Store {
	t.Helper()
	store := jobs.NewStore(filepath.Join(dir, "state"), filepath.Join(dir, "run"))
	oldStore := jobsHostStore
	jobsHostStore = func(uint32) *jobs.Store { return store }
	t.Cleanup(func() { jobsHostStore = oldStore })
	return store
}

// unreadableRelayTestStore points the relay at a store whose records cannot
// be listed: a file stands where their directory should be.
func unreadableRelayTestStore(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "state"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	relayTestStore(t, dir)
}

// gateRelayConnect holds each of the relay's connection attempts until the
// test lets it through on release; it then connects to the host. connecting
// receives a value when the first attempt is under way.
func gateRelayConnect(t *testing.T) (connecting <-chan struct{}, release chan<- struct{}) {
	t.Helper()
	started, gate := make(chan struct{}, 1), make(chan struct{})
	oldConnect := jobsConnect
	jobsConnect = func(ctx context.Context, uid uint32, hello bridgeipc.HelloSession, hostSocket string) (*jobsclient.Client, error) {
		select {
		case started <- struct{}{}:
		default:
		}
		select {
		case <-gate:
			return jobsclient.Connect(ctx, uid, hello, hostSocket)
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	t.Cleanup(func() { jobsConnect = oldConnect })
	return started, gate
}

// shortHostWait makes a request wait at most d for the host, for the rest of
// the test.
func shortHostWait(t *testing.T, d time.Duration) {
	t.Helper()
	oldWait := hostWaitTimeout
	hostWaitTimeout = d
	t.Cleanup(func() { hostWaitTimeout = oldWait })
}

// liveExecutor is a host executor naming the test's own process, which runs.
// The same pid with another start time names a process that has exited.
func liveExecutor(t *testing.T) jobs.Executor {
	t.Helper()
	pid := int32(os.Getpid())
	start, err := jobs.ProcessStart(pid)
	if err != nil {
		t.Fatalf("ProcessStart: %v", err)
	}
	return jobs.Executor{Kind: "host", PID: pid, PIDStart: start}
}

// runningRelayTestJob records in store a running job labelled label, created
// at created, by the host of an older build, whose process executor names.
func runningRelayTestJob(t *testing.T, store *jobs.Store, id, label string, created time.Time, executor jobs.Executor) jobs.Record {
	t.Helper()
	record := jobs.Record{
		ID: id, Route: "test.job", Owner: jobs.Owner{UID: uint32(os.Getuid()), Username: "alice"},
		State: jobs.StateRunning, CreatedAt: created, StartedAt: created,
		Metadata: jsontext.Value(`{"label":"` + label + `"}`), Request: jsontext.Value(`{"secret":"x"}`),
		Executor: executor, Protocol: jobs.Protocol - 1,
	}
	if err := store.Create(context.Background(), record); err != nil {
		t.Fatalf("Store.Create: %v", err)
	}
	return record
}

// repeatTargetInMetadata gives the record id the paths and packages it acts
// on, repeated in its metadata as an older host wrote them.
func repeatTargetInMetadata(t *testing.T, store *jobs.Store, id, label string) {
	t.Helper()
	if _, err := store.Update(context.Background(), id, func(r *jobs.Record) error {
		r.Target = []string{"/srv/a", "/srv/b"}
		r.Metadata = jsontext.Value(`{"label":"` + label + `","identity":["/srv/a","/srv/b"],"packageIds":["pkg;1"]}`)
		return nil
	}); err != nil {
		t.Fatalf("Store.Update: %v", err)
	}
}

// startHostJob starts a test.job id on the host at socket over a connection
// of its own, as another bridge of the uid does, and leaves it running.
func startHostJob(t *testing.T, socket, id string) {
	t.Helper()
	uid := uint32(os.Getuid())
	c, err := jobsclient.Connect(context.Background(), uid, bridgeipc.HelloSession{Username: "alice", UID: uid, SessionRef: "other-bridge"}, socket)
	if err != nil {
		t.Fatalf("connect another bridge: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	raw, err := json.Marshal(relay.StreamOpenEnvelope{Route: "test.job", Request: []byte(`{}`), ID: id})
	if err != nil {
		t.Fatalf("encode open: %v", err)
	}
	stream, err := c.Open(context.Background(), raw)
	if err != nil {
		t.Fatalf("open test.job: %v", err)
	}
	t.Cleanup(func() { _ = stream.Close() })
	_ = stream.SetReadDeadline(time.Now().Add(5 * time.Second))
	readRelayTestFrame(t, stream, relay.OpStreamProgress)
}

// warnLog keeps the messages logged at Warn and above.
type warnLog struct {
	mu   sync.Mutex
	msgs []string
}

// captureWarnings logs to a warnLog for the rest of the test.
func captureWarnings(t *testing.T) *warnLog {
	t.Helper()
	w := &warnLog{}
	previous := slog.Default()
	slog.SetDefault(slog.New(w))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return w
}

func (w *warnLog) Enabled(_ context.Context, level slog.Level) bool { return level >= slog.LevelWarn }

func (w *warnLog) Handle(_ context.Context, r slog.Record) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.msgs = append(w.msgs, r.Message)
	return nil
}

func (w *warnLog) WithAttrs([]slog.Attr) slog.Handler { return w }
func (w *warnLog) WithGroup(string) slog.Handler      { return w }

// count is how many times msg was logged.
func (w *warnLog) count(msg string) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := 0
	for _, m := range w.msgs {
		if m == msg {
			n++
		}
	}
	return n
}

// startRelayTestHost serves a job host in-process on socket whose "test.job"
// mutation returns {"ok":true}, or with block runs until the host stops. It
// returns a func that stops the host, as SIGTERM does.
func startRelayTestHost(t *testing.T, socket string, block bool, configure ...func(*jobshost.Config)) (stop func()) {
	t.Helper()
	dir := t.TempDir()
	tasks := bridgeipc.NewTaskService()
	router := bridgeipc.NewRouter(tasks)
	router.Mutation("test.job", func(ctx context.Context, _ *bridgeipc.Task, _ any) (any, error) {
		if block {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return map[string]bool{"ok": true}, nil
	})
	tasks.RegisterRoutes(router)
	ready := make(chan struct{})
	cfg := jobshost.Config{
		UID:        uint32(os.Getuid()),
		GID:        uint32(os.Getgid()),
		Username:   "alice",
		SocketPath: socket,
		Store:      jobs.NewStore(filepath.Join(dir, "state"), filepath.Join(dir, "run")),
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
	for _, fn := range configure {
		fn(&cfg)
	}
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- jobshost.Serve(ctx, cfg) }()
	var once sync.Once
	stop = func() {
		once.Do(func() {
			cancel()
			if err := <-served; err != nil {
				t.Errorf("Serve: %v", err)
			}
		})
	}
	t.Cleanup(stop)
	select {
	case <-ready:
	case err := <-served:
		t.Fatalf("Serve: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("job host not ready")
	}
	return stop
}

// startRefusingHost answers every hello with 426, as a host of another
// protocol does.
func startRefusingHost(t *testing.T) string {
	t.Helper()
	socket := filepath.Join(t.TempDir(), "host.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listen host: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				sess, err := relay.NewYamuxServer(conn)
				if err != nil {
					return
				}
				defer sess.Close()
				stream, err := sess.Accept()
				if err != nil {
					return
				}
				if _, err := relay.ReadRelayFrame(stream); err == nil {
					_ = relay.WriteResultErrorAndClose(stream, 0, "unsupported jobs protocol", 426)
					_, _ = relay.ReadRelayFrame(stream)
				}
			}()
		}
	}()
	return socket
}

func newRelayTestBridge(t *testing.T, sessions ...*session.Session) (*bridgeipc.Router, *jobsRelay, *session.Session) {
	t.Helper()
	// Unreachable hosts are logged as warnings; these tests make them on purpose.
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.DiscardHandler))
	t.Cleanup(func() { slog.SetDefault(previous) })
	sess := &session.Session{SessionID: "bridge-session", User: session.User{Username: "alice", UID: uint32(os.Getuid())}}
	if len(sessions) > 0 {
		sess = sessions[0]
	}
	router := bridgeipc.NewRouter(bridgeipc.NewTaskService())
	router.Mutation("test.job", func(context.Context, *bridgeipc.Task, any) (any, error) {
		return nil, errors.New("ran in the bridge")
	})
	router.TaskService().RegisterRoutes(router)
	jr := newJobsRelay(context.Background(), sess)
	t.Cleanup(jr.Close)
	router.SetMutationForwarder(jr)
	router.TaskService().SetRemote(jr)
	return router, jr, sess
}

// waitRelay waits until the relay's client lookup satisfies ok.
func waitRelay(t *testing.T, jr *jobsRelay, ok func(error) bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, err := jr.current()
		if ok(err) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("relay state = %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// dispatchMutation sends a "test.job" mutation of job id through router and
// returns the client end of its stream.
func dispatchMutation(t *testing.T, router *bridgeipc.Router, sess *session.Session, id string) net.Conn {
	t.Helper()
	return dispatchRoute(t, router, sess, "test.job", id)
}

// dispatchRoute sends route with an empty request, as job id when id is set,
// through router and returns the client end of its stream.
func dispatchRoute(t *testing.T, router *bridgeipc.Router, sess *session.Session, route, id string) net.Conn {
	t.Helper()
	request := []byte(`{}`)
	if id == "" {
		request = []byte(`{"taskId":"` + relayTestJobID + `"}`)
	}
	raw, err := json.Marshal(relay.StreamOpenEnvelope{Route: route, Request: request, ID: id})
	if err != nil {
		t.Fatalf("encode open: %v", err)
	}
	server, client := net.Pipe()
	t.Cleanup(func() { _ = client.Close() })
	go func() {
		defer server.Close()
		_ = router.Dispatch(context.Background(), server, bridgeipc.Request{
			Route: route, RawRequest: request, ID: id, OpenRaw: raw, Session: sess,
		})
	}()
	_ = client.SetReadDeadline(time.Now().Add(5 * time.Second))
	return client
}

func readRelayTestFrame(t *testing.T, conn net.Conn, want byte) []byte {
	t.Helper()
	frame, err := relay.ReadRelayFrame(conn)
	if err != nil {
		t.Fatalf("ReadRelayFrame: %v", err)
	}
	if frame.Opcode != want {
		t.Fatalf("opcode = 0x%02x, want 0x%02x (payload %s)", frame.Opcode, want, frame.Payload)
	}
	return frame.Payload
}

func readRelayTestResult(t *testing.T, conn net.Conn) relay.ResultFrame {
	t.Helper()
	var result relay.ResultFrame
	if err := json.Unmarshal(readRelayTestFrame(t, conn, relay.OpStreamResult), &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	return result
}

func TestBridgeForwardsMutationToHost(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "host.sock")
	startRelayTestHost(t, socket, false)
	relayTestSockets(t, socket)
	router, jr, sess := newRelayTestBridge(t)
	waitRelay(t, jr, func(err error) bool { return err == nil })

	client := dispatchMutation(t, router, sess, relayTestJobID)
	var event bridgeipc.TaskEvent
	if err := json.Unmarshal(readRelayTestFrame(t, client, relay.OpStreamProgress), &event); err != nil ||
		event.Type != bridgeipc.TaskEventSnapshot || event.Task.ID != relayTestJobID {
		t.Fatalf("first frame = %+v (%v), want the host's snapshot of %s", event, err, relayTestJobID)
	}
	if result := readRelayTestResult(t, client); result.Status != "ok" || string(result.Data) != `{"ok":true}` {
		t.Fatalf("result = %+v, want the host's ok", result)
	}

	// The job is the host's, so tasks.get reaches it through the bridge.
	snapshot, err := jr.Get(context.Background(), relayTestJobID)
	if err != nil || snapshot.ID != relayTestJobID || snapshot.State != bridgeipc.TaskStateCompleted {
		t.Fatalf("remote Get = %+v, %v; want the completed job", snapshot, err)
	}
}

func TestBridgeFailsMutationWhenHostUnavailable(t *testing.T) {
	oldConnect := jobsConnect
	jobsConnect = func(context.Context, uint32, bridgeipc.HelloSession, string) (*jobsclient.Client, error) {
		return nil, errors.New("dial job host: no such file or directory")
	}
	t.Cleanup(func() { jobsConnect = oldConnect })
	router, jr, sess := newRelayTestBridge(t)
	waitRelay(t, jr, func(err error) bool { return err != nil && !errors.Is(err, errJobHostConnecting) })

	client := dispatchMutation(t, router, sess, relayTestJobID)
	if result := readRelayTestResult(t, client); result.Code != 503 || result.Error != "job host unavailable" {
		t.Fatalf("result = %+v, want 503 job host unavailable", result)
	}
	// Reads still answer without a host.
	if snapshots, err := jr.List(context.Background()); err != nil || snapshots != nil {
		t.Fatalf("remote List = %v, %v; want nothing", snapshots, err)
	}
	// A host job's state is unknown, not missing.
	oldWait := hostWaitTimeout
	hostWaitTimeout = 50 * time.Millisecond
	t.Cleanup(func() { hostWaitTimeout = oldWait })
	if _, err := jr.Get(context.Background(), relayTestJobID); bridgeipc.StatusCode(err) != 503 {
		t.Fatalf("remote Get = %v, want 503", err)
	}
	// A saved output is unknown too, not empty.
	if _, err := jr.Output(context.Background(), bridgeipc.TaskOutputRequest{TaskID: relayTestJobID}); bridgeipc.StatusCode(err) != 503 {
		t.Fatalf("remote Output = %v, want 503", err)
	}
	watch := dispatchRoute(t, router, sess, "tasks.watch", "")
	if result := readRelayTestResult(t, watch); result.Code != 503 {
		t.Fatalf("tasks.watch = %+v, want 503", result)
	}
}

// storedRelayTestJob is a store holding job relayTestJobID, completed an
// hour ago with {"ok":true}.
func storedRelayTestJob(t *testing.T) *jobs.Store {
	t.Helper()
	dir := t.TempDir()
	store := jobs.NewStore(filepath.Join(dir, "state"), filepath.Join(dir, "run"))
	at := time.Now().UTC().Add(-time.Hour)
	if err := store.Create(context.Background(), jobs.Record{
		ID: relayTestJobID, Route: "test.job", Owner: jobs.Owner{UID: uint32(os.Getuid()), Username: "alice"},
		State: jobs.StateCompleted, CreatedAt: at, StartedAt: at, FinishedAt: at, Result: []byte(`{"ok":true}`),
		Request: []byte(`{}`), Executor: jobs.Executor{Kind: "host"}, Protocol: jobs.Protocol,
	}); err != nil {
		t.Fatalf("Store.Create: %v", err)
	}
	return store
}

func TestBridgeWatchWaitsForTheHostToReturn(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "host.sock")
	relayTestSockets(t, socket)
	router, _, sess := newRelayTestBridge(t)

	watch := dispatchRoute(t, router, sess, "tasks.watch", "")
	store := storedRelayTestJob(t)
	startRelayTestHost(t, socket, false, func(c *jobshost.Config) { c.Store = store })

	_ = watch.SetReadDeadline(time.Now().Add(hostWaitTimeout))
	var event bridgeipc.TaskEvent
	if err := json.Unmarshal(readRelayTestFrame(t, watch, relay.OpStreamProgress), &event); err != nil || event.Task.State != bridgeipc.TaskStateCompleted {
		t.Fatalf("watch snapshot = %+v, %v; want the stored completed job", event, err)
	}
	if result := readRelayTestResult(t, watch); result.Status != "ok" || string(result.Data) != `{"ok":true}` {
		t.Fatalf("watch result = %+v, want ok with the stored result", result)
	}
}

func TestBridgeEventsSendAJobThatEndedWhileTheHostWasAway(t *testing.T) {
	release := make(chan struct{})
	socket := filepath.Join(t.TempDir(), "host.sock")
	startRelayTestHost(t, socket, false, func(c *jobshost.Config) {
		c.Router.Mutation("test.release", func(ctx context.Context, _ *bridgeipc.Task, _ any) (any, error) {
			select {
			case <-release:
				return map[string]bool{"ok": true}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		})
	})
	relayTestSockets(t, socket)
	router, jr, sess := newRelayTestBridge(t)
	router.Mutation("test.release", func(context.Context, *bridgeipc.Task, any) (any, error) {
		return nil, errors.New("ran in the bridge")
	})
	waitRelay(t, jr, func(err error) bool { return err == nil })
	server, events := net.Pipe()
	t.Cleanup(func() { _ = events.Close() })
	go func() {
		defer server.Close()
		_ = router.Dispatch(context.Background(), server, bridgeipc.Request{Route: "tasks.events", RawRequest: []byte(`{}`), Session: sess})
	}()
	dispatchRoute(t, router, sess, "test.release", relayTestJobID)
	waitForHostEvent(t, events, relayTestJobID)

	// The job ends while the bridge has no host connection, so no event
	// carries its end; the reconnected stream sends it.
	first, _ := jr.current()
	_ = first.Close()
	close(release)
	_ = events.SetReadDeadline(time.Now().Add(hostWaitTimeout))
	for {
		var event bridgeipc.TaskEvent
		if err := json.Unmarshal(readRelayTestFrame(t, events, relay.OpStreamProgress), &event); err != nil {
			t.Fatalf("decode event: %v", err)
		}
		if event.Task.ID == relayTestJobID && event.Task.State == bridgeipc.TaskStateCompleted {
			return
		}
	}
}

func TestBridgeReportsUpdatePending(t *testing.T) {
	relayTestSockets(t, startRefusingHost(t))
	router, jr, sess := newRelayTestBridge(t)
	waitRelay(t, jr, func(err error) bool { return errors.Is(err, jobsclient.ErrUpdatePending) })

	client := dispatchMutation(t, router, sess, relayTestJobID)
	if result := readRelayTestResult(t, client); result.Code != 409 || !strings.HasPrefix(result.Error, "update_pending:") {
		t.Fatalf("result = %+v, want 409 update_pending", result)
	}
}

// waitForHostEvent reads the events stream until it carries an event of job id.
func waitForHostEvent(t *testing.T, events net.Conn, id string) {
	t.Helper()
	_ = events.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		var event bridgeipc.TaskEvent
		if err := json.Unmarshal(readRelayTestFrame(t, events, relay.OpStreamProgress), &event); err != nil {
			t.Fatalf("decode event: %v", err)
		}
		if event.Task.ID == id {
			return
		}
	}
}

func TestBridgeEventsFollowHostRestart(t *testing.T) {
	const firstJob, secondJob = "00000000-0000-4000-8000-0000000000d1", "00000000-0000-4000-8000-0000000000d2"
	socket := filepath.Join(t.TempDir(), "host.sock")
	stopFirst := startRelayTestHost(t, socket, true)
	relayTestSockets(t, socket)
	router, jr, sess := newRelayTestBridge(t)
	waitRelay(t, jr, func(err error) bool { return err == nil })
	first, _ := jr.current()

	server, events := net.Pipe()
	t.Cleanup(func() { _ = events.Close() })
	go func() {
		defer server.Close()
		_ = router.Dispatch(context.Background(), server, bridgeipc.Request{Route: "tasks.events", RawRequest: []byte(`{}`), Session: sess})
	}()
	dispatchMutation(t, router, sess, firstJob)
	waitForHostEvent(t, events, firstJob)

	stopFirst()
	startRelayTestHost(t, socket, true)
	waitRelay(t, jr, func(error) bool {
		c, _ := jr.current()
		return c != nil && c != first
	})
	dispatchMutation(t, router, sess, secondJob)
	waitForHostEvent(t, events, secondJob)
}

func TestPrivilegedBridgeUsesRootHost(t *testing.T) {
	const jobID = "00000000-0000-4000-8000-0000000000e1"
	socket := filepath.Join(t.TempDir(), "host.sock")
	var store *jobs.Store
	// The job runs until the host stops: a job done in under 1 s leaves no record.
	startRelayTestHost(t, socket, true, func(cfg *jobshost.Config) {
		cfg.UID, cfg.GID = 0, 0
		cfg.Peer = func(*net.UnixConn) (peercred.Cred, error) { return peercred.Cred{UID: 0}, nil }
		store = cfg.Store
	})
	relayTestSockets(t, socket)

	// The bridge runs as root for alice (uid 1000) after sudo.
	oldUID, oldConnect, oldHostSocket := effectiveUID, jobsConnect, jobsHostSocket
	t.Cleanup(func() { effectiveUID, jobsConnect, jobsHostSocket = oldUID, oldConnect, oldHostSocket })
	effectiveUID = func() uint32 { return 0 }
	type connectArgs struct {
		uid        uint32
		hostSocket string
		hello      bridgeipc.HelloSession
	}
	connects := make(chan connectArgs, 16)
	jobsHostSocket = func(uid uint32) string {
		if uid != 0 {
			return fmt.Sprintf("host-of-uid-%d.sock", uid)
		}
		return socket
	}
	jobsConnect = func(ctx context.Context, uid uint32, hello bridgeipc.HelloSession, hostSocket string) (*jobsclient.Client, error) {
		connects <- connectArgs{uid: uid, hostSocket: hostSocket, hello: hello}
		return jobsclient.Connect(ctx, uid, hello, hostSocket)
	}

	alice := &session.Session{SessionID: "alice-session", User: session.User{Username: "alice", UID: 1000}, Privileged: true}
	router, jr, sess := newRelayTestBridge(t, alice)
	waitRelay(t, jr, func(err error) bool { return err == nil })

	got := <-connects
	wantHello := bridgeipc.HelloSession{Username: "alice", UID: 1000, Privileged: true, SessionRef: session.DiagnosticRef("alice-session")}
	if got.uid != 0 || got.hostSocket != socket || got.hello != wantHello {
		t.Fatalf("connect(uid %d, host %q, hello %+v), want uid 0, the root host %q and hello %+v", got.uid, got.hostSocket, got.hello, socket, wantHello)
	}

	client := dispatchMutation(t, router, sess, jobID)
	var event bridgeipc.TaskEvent
	if err := json.Unmarshal(readRelayTestFrame(t, client, relay.OpStreamProgress), &event); err != nil || event.Task.ID != jobID {
		t.Fatalf("first frame = %+v (%v), want the root host's snapshot of %s", event, err, jobID)
	}
	record, err := store.Get(context.Background(), jobID)
	if err != nil {
		t.Fatalf("root host record: %v", err)
	}
	if record.Owner != (jobs.Owner{UID: 1000, Username: "alice"}) {
		t.Fatalf("record owner = %+v, want alice as uid 1000", record.Owner)
	}
}

func TestRelayForwardsHistory(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "host.sock")
	startRelayTestHost(t, socket, false, func(c *jobshost.Config) { c.Store = storedRelayTestJob(t) })
	relayTestSockets(t, socket)
	_, jr, _ := newRelayTestBridge(t)
	waitRelay(t, jr, func(err error) bool { return err == nil })

	history, err := jr.History(context.Background(), bridgeipc.TaskHistoryRequest{Limit: 5})
	if err != nil || history.More || len(history.Entries) != 1 ||
		history.Entries[0].Task.ID != relayTestJobID || history.Entries[0].Task.State != bridgeipc.TaskStateCompleted {
		t.Fatalf("remote History = %+v, %v; want the host's completed job", history, err)
	}
}

func TestRelayForwardsOutput(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "host.sock")
	dir := t.TempDir()
	scheduled := jobs.NewStore(filepath.Join(dir, "state"), filepath.Join(dir, "run"))
	if err := os.MkdirAll(filepath.Dir(scheduled.OutputPath(relayTestJobID)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scheduled.OutputPath(relayTestJobID), []byte("line one\nline two\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Only the root host, which privileged bridges share, keeps scheduled runs.
	startRelayTestHost(t, socket, false, func(c *jobshost.Config) {
		c.UID, c.GID = 0, 0
		c.Peer = func(*net.UnixConn) (peercred.Cred, error) { return peercred.Cred{UID: 0}, nil }
		c.Scheduled = scheduled
	})
	relayTestSockets(t, socket)
	admin := &session.Session{SessionID: "bridge-session", User: session.User{Username: "alice", UID: uint32(os.Getuid())}, Privileged: true}
	_, jr, _ := newRelayTestBridge(t, admin)
	waitRelay(t, jr, func(err error) bool { return err == nil })

	offset := int64(5)
	output, err := jr.Output(context.Background(), bridgeipc.TaskOutputRequest{TaskID: relayTestJobID, Offset: &offset, Limit: 3})
	if want := (bridgeipc.TaskOutput{Content: "one", Offset: 5, Size: 18}); err != nil || output != want {
		t.Fatalf("remote Output = %+v, %v; want the host's %+v", output, err, want)
	}
	// The host's error comes back as it is.
	other := "00000000-0000-4000-8000-0000000000c2"
	if _, err := jr.Output(context.Background(), bridgeipc.TaskOutputRequest{TaskID: other}); bridgeipc.StatusCode(err) != 404 {
		t.Fatalf("remote Output of a run with no output = %v, want the host's 404", err)
	}
}

func TestRelayHistoryWhileAnUpdateWaits(t *testing.T) {
	relayTestSockets(t, startRefusingHost(t))
	_, jr, _ := newRelayTestBridge(t)
	waitRelay(t, jr, func(err error) bool { return errors.Is(err, jobsclient.ErrUpdatePending) })

	// The older host cannot be asked, and no host of this build will come
	// while it runs: the answer does not wait for one.
	history, err := jr.History(context.Background(), bridgeipc.TaskHistoryRequest{})
	if err != nil || len(history.Entries) != 0 || history.More {
		t.Fatalf("remote History = %+v, %v; want an empty history", history, err)
	}
}

func TestRelayHistoryWaitsForTheHost(t *testing.T) {
	t.Run("connected", func(t *testing.T) {
		socket := filepath.Join(t.TempDir(), "host.sock")
		startRelayTestHost(t, socket, false, func(c *jobshost.Config) { c.Store = storedRelayTestJob(t) })
		relayTestSockets(t, socket)
		connecting, release := gateRelayConnect(t)
		_, jr, _ := newRelayTestBridge(t)
		<-connecting

		type answer struct {
			history bridgeipc.TaskHistory
			err     error
		}
		answered := make(chan answer, 1)
		go func() {
			history, err := jr.History(context.Background(), bridgeipc.TaskHistoryRequest{})
			answered <- answer{history, err}
		}()
		release <- struct{}{}
		select {
		case got := <-answered:
			if got.err != nil || len(got.history.Entries) != 1 || got.history.Entries[0].Task.ID != relayTestJobID {
				t.Fatalf("remote History = %+v, %v; want the host's job", got.history, got.err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("History did not answer once the host was connected")
		}
	})
	t.Run("no host", func(t *testing.T) {
		shortHostWait(t, 50*time.Millisecond)
		gateRelayConnect(t)
		_, jr, _ := newRelayTestBridge(t)

		// No host is not the same as no history.
		if history, err := jr.History(context.Background(), bridgeipc.TaskHistoryRequest{}); bridgeipc.StatusCode(err) != 503 {
			t.Fatalf("remote History = %+v, %v; want 503", history, err)
		}
	})
}

func TestRelayStatusWhileAnUpdateWaits(t *testing.T) {
	t.Run("running jobs", func(t *testing.T) {
		relayTestSockets(t, startRefusingHost(t))
		store := relayTestStore(t, t.TempDir())
		now := time.Now().UTC().Truncate(time.Second)
		live := liveExecutor(t)
		gone := live
		gone.PIDStart++
		netplan := live
		netplan.Kind = "netplan"
		first := runningRelayTestJob(t, store, hostJobOne, "Scrubbing pool-1", now.Add(-3*time.Minute), live)
		repeatTargetInMetadata(t, store, first.ID, "Scrubbing pool-1")
		second := runningRelayTestJob(t, store, hostJobTwo, "Balancing pool-2", now.Add(-2*time.Minute), live)
		// The next host start marks a job whose process is gone interrupted,
		// and a record no host ran is no host job.
		runningRelayTestJob(t, store, hostJobThree, "Lost job", now.Add(-time.Minute), gone)
		runningRelayTestJob(t, store, hostJobFour, "Network change", now, netplan)
		if err := store.WriteProgress(first.ID, jsontext.Value(`{"percentage":40,"message":"Scrubbing"}`)); err != nil {
			t.Fatalf("WriteProgress: %v", err)
		}
		_, jr, _ := newRelayTestBridge(t)

		status, err := jr.Status(context.Background())
		if err != nil || !status.UpdatePending || status.Running != 2 || len(status.Jobs) != 2 {
			t.Fatalf("remote Status = %+v, %v; want an update waiting for 2 running jobs", status, err)
		}
		got := status.Jobs[0]
		if got.ID != first.ID || got.Type != "test.job" || got.State != bridgeipc.TaskStateRunning ||
			got.Metadata == nil || got.Metadata.Label != "Scrubbing pool-1" ||
			got.Metadata.Identity != nil || got.Metadata.PackageIDs != nil ||
			got.Progress == nil || got.Progress.Percentage == nil || *got.Progress.Percentage != 40 || got.Progress.Message != "Scrubbing" {
			t.Fatalf("first job = %+v, want %s running, labelled, without its paths or packages, with its last progress", got, first.ID)
		}
		got = status.Jobs[1]
		if got.ID != second.ID || got.State != bridgeipc.TaskStateRunning || got.Metadata == nil || got.Metadata.Label != "Balancing pool-2" || got.Progress != nil {
			t.Fatalf("second job = %+v, want %s running, labelled, with no progress", got, second.ID)
		}
		// A record's request, which may hold secrets, never reaches the answer.
		if data, err := json.Marshal(status); err != nil || strings.Contains(string(data), "secret") {
			t.Fatalf("remote Status = %s (%v), want no record's request in it", data, err)
		}
	})
	t.Run("unreadable store", func(t *testing.T) {
		relayTestSockets(t, startRefusingHost(t))
		unreadableRelayTestStore(t)
		_, jr, _ := newRelayTestBridge(t)
		warnings := captureWarnings(t)

		status, err := jr.Status(context.Background())
		if err != nil || !status.UpdatePending || status.Running != 0 || status.Jobs != nil {
			t.Fatalf("remote Status = %+v, %v; want an update waiting and no job read", status, err)
		}
		if got := warnings.count("job host records unreadable while an update waits"); got != 1 {
			t.Fatalf("store read failure logged %d times at Warn, want once", got)
		}
	})
}

func TestRelayStatusWithACurrentHost(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "host.sock")
	startRelayTestHost(t, socket, true)
	relayTestSockets(t, socket)
	router, jr, sess := newRelayTestBridge(t)
	waitRelay(t, jr, func(err error) bool { return err == nil })
	for _, id := range []string{hostJobOne, hostJobTwo} {
		readRelayTestFrame(t, dispatchMutation(t, router, sess, id), relay.OpStreamProgress)
	}

	status, err := jr.Status(context.Background())
	if err != nil || status.UpdatePending || status.Running != 2 || status.Jobs != nil {
		t.Fatalf("remote Status = %+v, %v; want the host's 2 running jobs and no update waiting", status, err)
	}
}

func TestRelayStatusWaitsForTheFirstConnection(t *testing.T) {
	type answer struct {
		status bridgeipc.TaskHostStatus
		err    error
	}
	// ask asks Status while the relay's first connection attempt is under
	// way, then lets the attempt reach the host and returns the answer.
	ask := func(t *testing.T) answer {
		t.Helper()
		connecting, release := gateRelayConnect(t)
		_, jr, _ := newRelayTestBridge(t)
		<-connecting
		answered := make(chan answer, 1)
		go func() {
			status, err := jr.Status(context.Background())
			answered <- answer{status, err}
		}()
		release <- struct{}{}
		select {
		case got := <-answered:
			return got
		case <-time.After(5 * time.Second):
			t.Fatal("Status did not answer once the connection attempt ended")
			return answer{}
		}
	}

	t.Run("refused", func(t *testing.T) {
		relayTestSockets(t, startRefusingHost(t))
		store := relayTestStore(t, t.TempDir())
		runningRelayTestJob(t, store, hostJobOne, "Scrubbing pool-1", time.Now().UTC(), liveExecutor(t))

		got := ask(t)
		if got.err != nil || !got.status.UpdatePending || got.status.Running != 1 || len(got.status.Jobs) != 1 {
			t.Fatalf("remote Status = %+v, %v; want an update waiting for 1 running job", got.status, got.err)
		}
	})
	t.Run("connected", func(t *testing.T) {
		socket := filepath.Join(t.TempDir(), "host.sock")
		startRelayTestHost(t, socket, true)
		relayTestSockets(t, socket)
		startHostJob(t, socket, hostJobOne)

		got := ask(t)
		if got.err != nil || got.status.UpdatePending || got.status.Running != 1 || got.status.Jobs != nil {
			t.Fatalf("remote Status = %+v, %v; want the host's 1 running job", got.status, got.err)
		}
	})
	t.Run("no host", func(t *testing.T) {
		shortHostWait(t, 100*time.Millisecond)
		gateRelayConnect(t)
		_, jr, _ := newRelayTestBridge(t)

		asked := time.Now()
		status, err := jr.Status(context.Background())
		if err != nil || status.UpdatePending || status.Running != 0 || status.Jobs != nil {
			t.Fatalf("remote Status = %+v, %v; want nothing waiting and no error", status, err)
		}
		if waited := time.Since(asked); waited < hostWaitTimeout {
			t.Fatalf("Status answered after %v, want it to wait the host wait of %v", waited, hostWaitTimeout)
		}
	})
}

func TestRelayListIsEmptyWhileAnUpdateWaits(t *testing.T) {
	relayTestSockets(t, startRefusingHost(t))
	store := relayTestStore(t, t.TempDir())
	runningRelayTestJob(t, store, hostJobOne, "Scrubbing pool-1", time.Now().UTC(), liveExecutor(t))
	_, jr, _ := newRelayTestBridge(t)
	waitRelay(t, jr, func(err error) bool { return errors.Is(err, jobsclient.ErrUpdatePending) })

	// The jobs waited for travel in tasks.host; no stream could follow them.
	if snapshots, err := jr.List(context.Background()); err != nil || snapshots != nil {
		t.Fatalf("remote List = %+v, %v; want nothing", snapshots, err)
	}
}

func TestUpdatePendingMessageCarriesTheCount(t *testing.T) {
	for _, tc := range []struct {
		name       string
		running    int
		unreadable bool
		want       string
	}{
		{name: "two jobs", running: 2, want: "update_pending: LinuxIO update waits for 2 running jobs"},
		{name: "one job", running: 1, want: "update_pending: LinuxIO update waits for 1 running job"},
		{name: "no job", want: "update_pending: LinuxIO was updated; new jobs can start shortly"},
		{name: "unreadable store", unreadable: true, want: "update_pending: LinuxIO update waits for running jobs"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			relayTestSockets(t, startRefusingHost(t))
			if tc.unreadable {
				unreadableRelayTestStore(t)
			} else {
				store := relayTestStore(t, t.TempDir())
				now := time.Now().UTC()
				live := liveExecutor(t)
				for i, id := range []string{hostJobOne, hostJobTwo}[:tc.running] {
					runningRelayTestJob(t, store, id, "Job", now.Add(time.Duration(i)*time.Second), live)
				}
				gone := live
				gone.PIDStart++
				runningRelayTestJob(t, store, hostJobThree, "Lost job", now, gone)
			}
			router, jr, sess := newRelayTestBridge(t)
			waitRelay(t, jr, func(err error) bool { return errors.Is(err, jobsclient.ErrUpdatePending) })

			client := dispatchMutation(t, router, sess, relayTestJobID)
			if result := readRelayTestResult(t, client); result.Code != 409 || result.Error != tc.want {
				t.Fatalf("result = %+v, want 409 %q", result, tc.want)
			}
		})
	}
}
