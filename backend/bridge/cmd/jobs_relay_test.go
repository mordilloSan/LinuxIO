package cmd

import (
	"context"
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

const relayTestJobID = "00000000-0000-4000-8000-0000000000c1"

// relayTestSockets points the relay at hostSocket for the rest of the test.
func relayTestSockets(t *testing.T, hostSocket string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("as root jobsclient.Connect starts the host's socket unit through systemd; that StartUnit seam is internal to jobsclient, whose tests cover the root path with it stubbed")
	}
	oldHost := jobsHostSocket
	jobsHostSocket = func(uint32) string { return hostSocket }
	t.Cleanup(func() { jobsHostSocket = oldHost })
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
