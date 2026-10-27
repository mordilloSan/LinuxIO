package jobshost

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"math"
	"net"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/common/alerts"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
	"github.com/mordilloSan/LinuxIO/backend/common/jobs"
	"github.com/mordilloSan/LinuxIO/backend/common/peercred"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

const (
	jobA = "00000000-0000-4000-8000-00000000000a"
	jobB = "00000000-0000-4000-8000-00000000000b"
	jobC = "00000000-0000-4000-8000-00000000000c"
	jobD = "00000000-0000-4000-8000-00000000000d"

	ioTimeout = 5 * time.Second
)

// testHost serves a router whose "test.job" mutation waits for an outcome on
// release: nil completes it with {"ok":true}, an error fails it.
type testHost struct {
	t        *testing.T
	cfg      Config
	host     *Host
	release  chan error
	runs     atomic.Int32
	notified chan string
	cancel   context.CancelFunc
	served   chan error

	mu     sync.Mutex
	alerts []alerts.Observation

	waitOnce sync.Once
	serveErr error
}

func newTestHost(t *testing.T, configure func(*Config)) *testHost {
	t.Helper()
	dir := t.TempDir()
	tasks := bridgeipc.NewTaskService()
	th := &testHost{
		t:        t,
		release:  make(chan error),
		notified: make(chan string, 8),
		served:   make(chan error, 1),
	}
	router := bridgeipc.NewRouter(tasks)
	router.Mutation("test.job", th.run)
	th.cfg = Config{
		UID:        uint32(os.Getuid()),
		GID:        uint32(os.Getgid()),
		Username:   "alice",
		SocketPath: filepath.Join(dir, "host.sock"),
		Store:      jobs.NewStore(filepath.Join(dir, "state"), filepath.Join(dir, "run")),
		Router:     router,
		Tasks:      tasks,
		IdleExit:   time.Hour,
		MaxRunning: 256,
		Notify: func(state string) error {
			th.notified <- state
			return nil
		},
		Alert: func(_ context.Context, obs alerts.Observation) error {
			th.mu.Lock()
			defer th.mu.Unlock()
			th.alerts = append(th.alerts, obs)
			return nil
		},
		Now: time.Now,
	}
	if configure != nil {
		configure(&th.cfg)
	}
	th.host = newHost(th.cfg)
	return th
}

func (th *testHost) run(ctx context.Context, _ *bridgeipc.Task, _ any) (any, error) {
	th.runs.Add(1)
	select {
	case err := <-th.release:
		if err != nil {
			return nil, err
		}
		return map[string]bool{"ok": true}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (th *testHost) start() {
	th.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	th.cancel = cancel
	go func() { th.served <- th.host.serve(ctx) }()
	th.t.Cleanup(func() { _ = th.stop() })
	th.waitNotify("READY=1")
}

// stop cancels Serve, as SIGTERM does, and returns its result.
func (th *testHost) stop() error {
	th.cancel()
	return th.wait()
}

func (th *testHost) wait() error {
	th.waitOnce.Do(func() {
		select {
		case th.serveErr = <-th.served:
		case <-time.After(ioTimeout):
			th.serveErr = errors.New("Serve did not return")
		}
	})
	return th.serveErr
}

func (th *testHost) waitNotify(want string) {
	th.t.Helper()
	select {
	case got := <-th.notified:
		if got != want {
			th.t.Fatalf("notify = %q, want %q", got, want)
		}
	case <-time.After(ioTimeout):
		th.t.Fatalf("no %q notification", want)
	}
}

func (th *testHost) observed() []alerts.Observation {
	th.mu.Lock()
	defer th.mu.Unlock()
	return slices.Clone(th.alerts)
}

func (th *testHost) record(id string) jobs.Record {
	th.t.Helper()
	record, err := th.cfg.Store.Get(context.Background(), id)
	if err != nil {
		th.t.Fatalf("Store.Get(%s): %v", id, err)
	}
	return record
}

func (th *testHost) dial() *relay.YamuxSession {
	th.t.Helper()
	conn, err := net.Dial("unix", th.cfg.SocketPath)
	if err != nil {
		th.t.Fatalf("dial host: %v", err)
	}
	sess, err := relay.NewYamuxClient(conn)
	if err != nil {
		th.t.Fatalf("NewYamuxClient: %v", err)
	}
	th.t.Cleanup(func() { _ = sess.Close() })
	return sess
}

// connect dials and says hello as a bridge claiming another uid.
func (th *testHost) connect() *relay.YamuxSession {
	th.t.Helper()
	sess := th.dial()
	result := hello(th.t, sess, jobs.Protocol)
	var answer bridgeipc.Hello
	if result.Status != "ok" || json.Unmarshal(result.Data, &answer) != nil || answer.Protocol != jobs.Protocol {
		th.t.Fatalf("hello result = %#v, want ok with protocol %d", result, jobs.Protocol)
	}
	return sess
}

// startJob opens a mutation stream and returns it after the task snapshot.
func (th *testHost) startJob(sess *relay.YamuxSession, route, id string) net.Conn {
	th.t.Helper()
	stream := openStream(th.t, sess, route, struct{}{}, id)
	if snapshot := readSnapshot(th.t, stream); snapshot.ID != id {
		th.t.Fatalf("snapshot id = %q, want %q", snapshot.ID, id)
	}
	return stream
}

func hello(t *testing.T, sess *relay.YamuxSession, protocol int) relay.ResultFrame {
	t.Helper()
	stream := openStream(t, sess, bridgeipc.HelloRoute, bridgeipc.Hello{
		Protocol: protocol,
		Session:  bridgeipc.HelloSession{Username: "alice", UID: 4242, Privileged: true, SessionRef: "ref-1"},
	}, "")
	defer stream.Close()
	return readResult(t, stream)
}

func openStream(t *testing.T, sess *relay.YamuxSession, route string, request any, id string) net.Conn {
	t.Helper()
	stream, err := sess.Open(context.Background())
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	t.Cleanup(func() { _ = stream.Close() })
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("json.Marshal(request): %v", err)
	}
	payload, err := json.Marshal(relay.StreamOpenEnvelope{Route: route, Request: raw, ID: id})
	if err != nil {
		t.Fatalf("json.Marshal(open): %v", err)
	}
	if err := relay.WriteRelayFrame(stream, &relay.StreamFrame{Opcode: relay.OpStreamOpen, Payload: payload}); err != nil {
		t.Fatalf("write stream open: %v", err)
	}
	return stream
}

func readFrame(t *testing.T, conn net.Conn, want byte) []byte {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(ioTimeout))
	frame, err := relay.ReadRelayFrame(conn)
	if err != nil {
		t.Fatalf("ReadRelayFrame: %v", err)
	}
	if frame.Opcode != want {
		t.Fatalf("opcode = 0x%02x, want 0x%02x (payload %s)", frame.Opcode, want, frame.Payload)
	}
	return frame.Payload
}

func readSnapshot(t *testing.T, conn net.Conn) bridgeipc.TaskSnapshot {
	t.Helper()
	var event struct {
		Type bridgeipc.TaskEventType `json:"type"`
		Task bridgeipc.TaskSnapshot  `json:"task"`
	}
	if err := json.Unmarshal(readFrame(t, conn, relay.OpStreamProgress), &event); err != nil {
		t.Fatalf("json.Unmarshal(snapshot): %v", err)
	}
	if event.Type != bridgeipc.TaskEventSnapshot {
		t.Fatalf("first frame type = %q, want %q", event.Type, bridgeipc.TaskEventSnapshot)
	}
	return event.Task
}

func readResult(t *testing.T, conn net.Conn) relay.ResultFrame {
	t.Helper()
	var result relay.ResultFrame
	if err := json.Unmarshal(readFrame(t, conn, relay.OpStreamResult), &result); err != nil {
		t.Fatalf("json.Unmarshal(result): %v", err)
	}
	readFrame(t, conn, relay.OpStreamClose)
	return result
}

func compact(t *testing.T, value jsontext.Value) string {
	t.Helper()
	value = value.Clone()
	if err := value.Compact(); err != nil {
		t.Fatalf("compact %s: %v", value, err)
	}
	return string(value)
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(ioTimeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// asUserHost makes a host a user's, uid 1000, with its peers that uid, so a
// user-host test holds when it runs as root.
func asUserHost(c *Config) {
	c.UID, c.GID = 1000, 1000
	c.Peer = func(*net.UnixConn) (peercred.Cred, error) { return peercred.Cred{UID: 1000}, nil }
}

func TestHostServesMutationOverYamuxAndRecords(t *testing.T) {
	th := newTestHost(t, asUserHost)
	th.start()
	sess := th.connect()
	if bridges, _ := th.host.counts(); bridges != 1 {
		t.Fatalf("bridges after hello = %d, want 1", bridges)
	}

	quick := th.startJob(sess, "test.job", jobA)
	record := th.record(jobA)
	pidStart, err := jobs.ProcessStart(int32(os.Getpid()))
	if err != nil {
		t.Fatalf("ProcessStart: %v", err)
	}
	boot, err := jobs.BootID()
	if err != nil {
		t.Fatalf("BootID: %v", err)
	}
	wantExecutor := jobs.Executor{Kind: "host", PID: int32(os.Getpid()), PIDStart: pidStart, BootID: boot}
	if record.State != jobs.StateRunning || record.Route != "test.job" || record.Executor != wantExecutor ||
		record.Protocol != jobs.Protocol || compact(t, record.Request) != `{}` {
		t.Fatalf("running record = %#v, want a running host record of the request", record)
	}
	// A user host records its own uid, never the one the hello claims.
	if record.Owner != (jobs.Owner{UID: th.cfg.UID, Username: "alice"}) {
		t.Fatalf("record owner = %#v, want uid %d from the host", record.Owner, th.cfg.UID)
	}
	if _, running := th.host.counts(); running != 1 {
		t.Fatalf("running = %d, want 1", running)
	}

	th.release <- nil
	if result := readResult(t, quick); result.Status != "ok" || string(result.Data) != `{"ok":true}` {
		t.Fatalf("result = %#v, want ok with {\"ok\":true}", result)
	}
	if _, err := th.cfg.Store.Get(context.Background(), jobA); !errors.Is(err, jobs.ErrNotFound) {
		t.Fatalf("Store.Get(job under 1 s) error = %v, want ErrNotFound", err)
	}

	slow := th.startJob(sess, "test.job", jobB)
	time.Sleep(jobs.TransientMaxDuration + 100*time.Millisecond)
	th.release <- nil
	readResult(t, slow)
	record = th.record(jobB)
	if record.State != jobs.StateCompleted || compact(t, record.Result) != `{"ok":true}` || record.FinishedAt.IsZero() {
		t.Fatalf("finished record = %#v, want completed with the result", record)
	}
	if _, running := th.host.counts(); running != 0 {
		t.Fatalf("running after both jobs = %d, want 0", running)
	}
}

func TestHostRejectsWrongPeerUID(t *testing.T) {
	var asked atomic.Int32
	th := newTestHost(t, func(cfg *Config) {
		cfg.Peer = func(*net.UnixConn) (peercred.Cred, error) {
			asked.Add(1)
			return peercred.Cred{UID: cfg.UID + 1}, nil
		}
	})
	th.start()

	conn, err := net.Dial("unix", th.cfg.SocketPath)
	if err != nil {
		t.Fatalf("dial host: %v", err)
	}
	defer conn.Close()
	// Nothing is written: the host must close without waiting for a hello.
	_ = conn.SetReadDeadline(time.Now().Add(ioTimeout))
	if n, err := conn.Read(make([]byte, 1)); n != 0 || err == nil || errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("read from refused connection = %d, %v; want it closed", n, err)
	}
	if asked.Load() != 1 {
		t.Fatalf("peer credentials read %d times, want 1", asked.Load())
	}
	if bridges, _ := th.host.counts(); bridges != 0 {
		t.Fatalf("bridges = %d, want 0", bridges)
	}
}

func TestHostRejectsProtocolMismatch(t *testing.T) {
	th := newTestHost(t, nil)
	th.start()
	sess := th.dial()

	if result := hello(t, sess, jobs.Protocol+1); result.Status != "error" || result.Code != 426 || result.Error != "unsupported jobs protocol" {
		t.Fatalf("hello result = %#v, want 426 unsupported jobs protocol", result)
	}
	select {
	case <-sess.CloseChan():
	case <-time.After(ioTimeout):
		t.Fatal("session still open after a protocol mismatch")
	}
	if bridges, _ := th.host.counts(); bridges != 0 {
		t.Fatalf("bridges = %d, want 0", bridges)
	}
}

func TestHostIdleExitAfterGrace(t *testing.T) {
	base := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	var offset atomic.Int64
	th := newTestHost(t, func(cfg *Config) {
		cfg.IdleExit = time.Minute
		cfg.Now = func() time.Time { return base.Add(time.Duration(offset.Load())) }
	})
	th.start()
	sess := th.connect()

	offset.Add(int64(2 * time.Minute))
	time.Sleep(1500 * time.Millisecond)
	select {
	case err := <-th.served:
		t.Fatalf("Serve returned %v with a bridge connected", err)
	default:
	}

	_ = sess.Close()
	waitFor(t, "the bridge to disconnect", func() bool {
		bridges, _ := th.host.counts()
		return bridges == 0
	})
	offset.Add(int64(2 * time.Minute))
	th.waitNotify("STOPPING=1")
	if err := th.wait(); err != nil {
		t.Fatalf("Serve after idle exit = %v, want nil", err)
	}
	if _, err := os.Stat(th.cfg.SocketPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket after idle exit: %v, want it removed", err)
	}
}

func TestHostSIGTERMMarksRunningJobsInterrupted(t *testing.T) {
	suspended := make(chan jsontext.Value, 2)
	th := newTestHost(t, nil)
	th.cfg.Router.Mutation("test.resumable", th.run, bridgeipc.WithResume(
		func(_ context.Context, record jsontext.Value) (jsontext.Value, error) {
			suspended <- record
			return jsontext.Value(`{"offset":7}`), nil
		},
		func(context.Context, jsontext.Value) (any, error) { return nil, nil },
	))
	th.start()
	sess := th.connect()

	streams := map[string]net.Conn{
		jobA: th.startJob(sess, "test.job", jobA),
		jobB: th.startJob(sess, "test.resumable", jobB),
		jobC: th.startJob(sess, "test.resumable", jobC),
	}
	if _, err := th.cfg.Store.Update(context.Background(), jobC, func(r *jobs.Record) error {
		r.CancelRequested = true
		return nil
	}); err != nil {
		t.Fatalf("Store.Update(cancel requested): %v", err)
	}

	th.cancel() // SIGTERM
	for id, stream := range streams {
		readInterrupted(t, id, stream)
	}
	if err := th.wait(); err != nil {
		t.Fatalf("Serve after SIGTERM = %v, want nil", err)
	}
	for id, checkpoint := range map[string]string{jobA: "", jobB: `{"offset":7}`, jobC: ""} {
		record := th.record(id)
		if record.State != jobs.StateInterrupted || record.Reason != jobs.ReasonShutdown || record.FinishedAt.IsZero() {
			t.Fatalf("record %s = %s/%s, want interrupted/shutdown", id, record.State, record.Reason)
		}
		if got := string(record.Checkpoint); checkpoint != "" {
			got = compact(t, record.Checkpoint)
			if got != checkpoint {
				t.Fatalf("record %s checkpoint = %s, want %s", id, got, checkpoint)
			}
		} else if got != "" {
			t.Fatalf("record %s checkpoint = %s, want none", id, got)
		}
		if task, ok := th.cfg.Tasks.Get(id); !ok || task.Snapshot().State != bridgeipc.TaskStateInterrupted {
			t.Fatalf("task %s was not interrupted", id)
		}
	}
	if len(suspended) != 1 {
		t.Fatalf("Suspend called %d times, want once (only the job without a cancel request)", len(suspended))
	}
	var record jobs.Record
	if err := json.Unmarshal(<-suspended, &record); err != nil || record.ID != jobB || !record.Resumable {
		t.Fatalf("Suspend record = %#v, %v; want the resumable record of %s", record, err, jobB)
	}
}

// readInterrupted reads the end of the stream of a job its host interrupted:
// the interruption, not a user cancel. It then closes the stream as the
// bridge does.
func readInterrupted(t *testing.T, id string, stream net.Conn) {
	t.Helper()
	if result := readResult(t, stream); result.Status != "error" || result.Code != 409 || result.Error != "job was interrupted" {
		t.Fatalf("job %s stream result = %#v, want 409 job was interrupted", id, result)
	}
	_ = stream.Close()
}

func TestHostReconcilesCrashedRecordsAtStart(t *testing.T) {
	th := newTestHost(t, nil)
	now := time.Now().UTC()
	pidStart, err := jobs.ProcessStart(int32(os.Getpid()))
	if err != nil {
		t.Fatalf("ProcessStart: %v", err)
	}
	running := func(id string, executor jobs.Executor) jobs.Record {
		return jobs.Record{
			ID: id, Route: "test.job", Owner: jobs.Owner{UID: th.cfg.UID, Username: "alice"},
			State: jobs.StateRunning, CreatedAt: now, StartedAt: now, Request: jsontext.Value(`{}`),
			Executor: executor, Protocol: jobs.Protocol,
		}
	}
	expired := running(jobC, jobs.Executor{Kind: "host"})
	expired.State, expired.CreatedAt, expired.FinishedAt = jobs.StateCompleted, now.Add(-40*24*time.Hour), now.Add(-31*24*time.Hour)
	for _, record := range []jobs.Record{
		running(jobA, jobs.Executor{Kind: "host", PID: math.MaxInt32, PIDStart: 1}),
		running(jobB, jobs.Executor{Kind: "host", PID: int32(os.Getpid()), PIDStart: pidStart}),
		expired,
	} {
		if err := th.cfg.Store.Create(context.Background(), record); err != nil {
			t.Fatalf("Store.Create(%s): %v", record.ID, err)
		}
	}

	th.start()
	if record := th.record(jobA); record.State != jobs.StateInterrupted || record.Reason != jobs.ReasonCrash {
		t.Fatalf("crashed record = %s/%s, want interrupted/crash", record.State, record.Reason)
	}
	if record := th.record(jobB); record.State != jobs.StateRunning {
		t.Fatalf("record of a live executor = %s, want running", record.State)
	}
	if _, err := th.cfg.Store.Get(context.Background(), jobC); !errors.Is(err, jobs.ErrNotFound) {
		t.Fatalf("expired record: %v, want pruned", err)
	}
	got := th.observed()
	if len(got) != 1 || got[0].Key != jobA || got[0].Title != "test.job was interrupted" {
		t.Fatalf("alerts = %#v, want one interrupted alert for %s", got, jobA)
	}
}

// addResumableRoute registers "test.resumable", whose Resume hook sends the
// job's ID on resumed and completes.
func addResumableRoute(c *Config, resumed chan<- string) {
	c.Router.Mutation("test.resumable",
		func(context.Context, *bridgeipc.Task, any) (any, error) { return nil, nil },
		bridgeipc.WithResume(
			func(context.Context, jsontext.Value) (jsontext.Value, error) { return nil, nil },
			func(_ context.Context, raw jsontext.Value) (any, error) {
				var record jobs.Record
				if err := json.Unmarshal(raw, &record); err != nil {
					return nil, err
				}
				resumed <- record.ID
				return map[string]bool{"ok": true}, nil
			}))
}

// earlierBootRecord is a resumable job record an hour old, so a resumed run is
// not dropped as transient, whose last host ran in an earlier boot.
func earlierBootRecord(id, route string, state jobs.State) jobs.Record {
	started := time.Now().UTC().Add(-time.Hour)
	r := jobs.Record{
		ID: id, Route: route, Owner: jobs.Owner{UID: 1000, Username: "alice"},
		State: state, CreatedAt: started, StartedAt: started, Request: jsontext.Value(`{}`),
		Executor:  jobs.Executor{Kind: "host", PID: math.MaxInt32, PIDStart: 1, BootID: "an-earlier-boot"},
		Resumable: true, Protocol: jobs.Protocol,
	}
	if state == jobs.StateInterrupted {
		r.Reason, r.FinishedAt = jobs.ReasonShutdown, started
	}
	return r
}

func TestUserHostMarksAnEarlierBootsJobWithoutResumingIt(t *testing.T) {
	resumed := make(chan string, 1)
	th := newTestHost(t, func(c *Config) {
		asUserHost(c)
		addResumableRoute(c, resumed)
	})
	if err := th.cfg.Store.Create(context.Background(), earlierBootRecord(jobA, "test.resumable", jobs.StateRunning)); err != nil {
		t.Fatalf("Store.Create: %v", err)
	}

	th.start()
	if r := th.record(jobA); r.State != jobs.StateInterrupted || r.Reason != jobs.ReasonReboot {
		t.Fatalf("record = %s/%s, want interrupted/reboot", r.State, r.Reason)
	}
	select {
	case id := <-resumed:
		t.Fatalf("a user host resumed %s; only the root host resumes", id)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestRootHostAlertsAndKeepsAJobItCannotResume(t *testing.T) {
	th := newTestHost(t, func(c *Config) { c.UID = 0 })
	boot, err := jobs.BootID()
	if err != nil {
		t.Fatalf("BootID: %v", err)
	}
	// Its route is gone after an update, so the resume fails.
	if err := th.cfg.Store.Create(context.Background(), earlierBootRecord(jobA, "test.gone", jobs.StateInterrupted)); err != nil {
		t.Fatalf("Store.Create: %v", err)
	}

	th.start()
	waitFor(t, "the failed resume alert", func() bool { return len(th.observed()) == 1 })
	if got := th.observed(); got[0].Key != jobA || got[0].Title != "test.gone was interrupted" {
		t.Fatalf("alert = %#v, want one for %s", got[0], jobA)
	}
	// Stamped as this boot's, so a later host start does not retry it.
	if r := th.record(jobA); r.State != jobs.StateInterrupted || r.Executor.BootID != boot {
		t.Fatalf("record = %s with boot %q, want interrupted and stamped with %q", r.State, r.Executor.BootID, boot)
	}
}

func TestRootHostResumesOnlyTheJobsAnEarlierBootInterrupted(t *testing.T) {
	resumed := make(chan string, 4)
	th := newTestHost(t, func(c *Config) {
		c.UID = 0
		addResumableRoute(c, resumed)
	})
	boot, err := jobs.BootID()
	if err != nil {
		t.Fatalf("BootID: %v", err)
	}
	thisBoot := earlierBootRecord(jobC, "test.resumable", jobs.StateInterrupted)
	thisBoot.Executor.BootID = boot
	notResumable := earlierBootRecord(jobD, "test.job", jobs.StateRunning)
	notResumable.Resumable = false
	for _, r := range []jobs.Record{
		// A power loss left it running: marked reboot, then resumed.
		earlierBootRecord(jobA, "test.resumable", jobs.StateRunning),
		// An orderly shutdown of the earlier boot interrupted it: resumed.
		earlierBootRecord(jobB, "test.resumable", jobs.StateInterrupted),
		// Stopped or crashed in this boot: stays interrupted.
		thisBoot,
		// Not resumable: marked reboot and alerted.
		notResumable,
	} {
		if err := th.cfg.Store.Create(context.Background(), r); err != nil {
			t.Fatalf("Store.Create(%s): %v", r.ID, err)
		}
	}

	th.start()
	if got := receiveIDs(t, resumed, 2); !slices.Equal(got, []string{jobA, jobB}) {
		t.Fatalf("resumed %v, want %s and %s", got, jobA, jobB)
	}
	waitFor(t, "the resumed jobs to complete", func() bool {
		return th.record(jobA).State == jobs.StateCompleted && th.record(jobB).State == jobs.StateCompleted
	})
	// The resumed task is the host's for permission checks; the record keeps
	// naming the user who started the job.
	if task, ok := th.cfg.Tasks.Get(jobA); !ok || task.Owner().UID != 0 || task.Owner().Username != "alice" {
		t.Fatalf("resumed task owner = %#v (found %v), want alice as uid 0", task.Owner(), ok)
	}
	if r := th.record(jobA); r.Owner != (jobs.Owner{UID: 1000, Username: "alice"}) {
		t.Fatalf("resumed record owner = %#v, want alice as uid 1000", r.Owner)
	}
	select {
	case id := <-resumed:
		t.Fatalf("also resumed %s", id)
	default:
	}
	if r := th.record(jobC); r.State != jobs.StateInterrupted || r.Reason != jobs.ReasonShutdown {
		t.Fatalf("job interrupted in this boot = %s/%s, want interrupted/shutdown", r.State, r.Reason)
	}
	if r := th.record(jobD); r.State != jobs.StateInterrupted || r.Reason != jobs.ReasonReboot {
		t.Fatalf("job a reboot interrupted = %s/%s, want interrupted/reboot", r.State, r.Reason)
	}
	if got := th.observed(); len(got) != 1 || got[0].Key != jobD {
		t.Fatalf("alerts = %#v, want one for %s", got, jobD)
	}
}

// receiveIDs reads n job IDs from ch and returns them sorted.
func receiveIDs(t *testing.T, ch <-chan string, n int) []string {
	t.Helper()
	var ids []string
	for range n {
		select {
		case id := <-ch:
			ids = append(ids, id)
		case <-time.After(ioTimeout):
			t.Fatalf("received %v, want %d job IDs", ids, n)
		}
	}
	slices.Sort(ids)
	return ids
}

func TestHostServesOnTheSocketSystemdPassedAndLeavesIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "passed.sock")
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	// A socket systemd passes is not unlinked when the host closes its copy.
	ln.SetUnlinkOnClose(false)
	th := newTestHost(t, func(c *Config) {
		c.Listener = ln
		c.SocketPath = path
	})
	th.start()
	_ = th.connect().Close()
	if err := th.stop(); err != nil {
		t.Fatalf("Serve = %v, want nil", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("passed socket after the host stopped: %v, want it left for systemd", err)
	}
}

func TestHostRefusesAbove256Running(t *testing.T) {
	th := newTestHost(t, func(cfg *Config) { cfg.MaxRunning = 2 })
	th.start()
	sess := th.connect()

	th.startJob(sess, "test.job", jobA)
	th.startJob(sess, "test.job", jobB)
	refused := openStream(t, sess, "test.job", struct{}{}, jobC)
	if result := readResult(t, refused); result.Status != "error" || result.Code != 409 || result.Error != "too many running jobs" {
		t.Fatalf("result above the cap = %#v, want 409 too many running jobs", result)
	}
	if _, ok := th.cfg.Tasks.Get(jobC); ok {
		t.Fatal("a job above the cap was started")
	}
	// A resend of a running job attaches to it; it starts nothing.
	th.startJob(sess, "test.job", jobA)

	th.release <- nil
	waitFor(t, "a job to finish", func() bool {
		_, running := th.host.counts()
		return running == 1
	})
	th.startJob(sess, "test.job", jobC)
}

func TestHostRaisesAlertForFailedAndInterruptedJobs(t *testing.T) {
	th := newTestHost(t, nil)
	th.start()
	sess := th.connect()

	completed := th.startJob(sess, "test.job", jobA)
	th.release <- nil
	readResult(t, completed)

	failed := th.startJob(sess, "test.job", jobB)
	th.release <- errors.New("boom")
	if result := readResult(t, failed); result.Status != "error" || result.Error != "boom" {
		t.Fatalf("failed result = %#v, want error boom", result)
	}

	interrupted := th.startJob(sess, "test.job", jobC)
	th.cancel() // SIGTERM; the host waits for the client to close the stream
	readInterrupted(t, jobC, interrupted)
	if err := th.wait(); err != nil {
		t.Fatalf("Serve after SIGTERM = %v, want nil", err)
	}

	want := []alerts.Observation{
		{Source: "jobs", Key: jobB, Severity: alerts.SeverityError, Title: "test.job failed", Message: "boom", Link: "/"},
		{Source: "jobs", Key: jobC, Severity: alerts.SeverityError, Title: "test.job was interrupted", Message: "The job host stopped before the job finished (shutdown).", Link: "/"},
	}
	if got := th.observed(); !slices.Equal(got, want) {
		t.Fatalf("alerts = %#v, want %#v", got, want)
	}
}

// claim creates a queued durable task without the router, so a test drives
// the recorder directly.
func claim(t *testing.T, th *testHost, id string) *bridgeipc.Task {
	t.Helper()
	task, _, err := th.cfg.Tasks.ClaimForOwnerWithIdentity("test.job", nil,
		bridgeipc.TaskOwner{SessionID: "s", Username: "alice", UID: th.cfg.UID},
		bridgeipc.TaskLifetimeDurable, bridgeipc.TaskIdentity{ID: id, Fingerprint: "f"})
	if err != nil {
		t.Fatalf("ClaimForOwnerWithIdentity: %v", err)
	}
	return task
}

func progress(percentage int) bridgeipc.TaskProgress {
	return bridgeipc.TaskProgress{Percentage: &percentage}
}

func (th *testHost) progressFile(id string) string {
	th.t.Helper()
	data, err := th.cfg.Store.ReadProgress(id)
	if errors.Is(err, jobs.ErrNotFound) {
		return ""
	}
	if err != nil {
		th.t.Fatalf("ReadProgress(%s): %v", id, err)
	}
	return string(data)
}

func TestRecorderTreatsATerminalSnapshotAtStartedAsFinal(t *testing.T) {
	th := newTestHost(t, nil)
	task := claim(t, th, jobA)
	task.Cancel() // canceled while queued, before the recorder saw it

	th.host.rec.Started(task, jsontext.Value(`{}`))
	if record := th.record(jobA); record.State != jobs.StateCanceled || record.FinishedAt.IsZero() {
		t.Fatalf("record = %s finished %v, want canceled and finished", record.State, record.FinishedAt)
	}
	if _, running := th.host.counts(); running != 0 {
		t.Fatalf("running = %d, want 0", running)
	}
	th.host.rec.Finished(task)
	if record := th.record(jobA); record.State != jobs.StateCanceled {
		t.Fatalf("record after Finished = %s, want canceled", record.State)
	}
}

func TestRecorderIgnoresProgressAfterFinished(t *testing.T) {
	th := newTestHost(t, nil)
	task := claim(t, th, jobA)
	th.host.rec.Started(task, jsontext.Value(`{}`))
	task.Cancel()
	th.host.rec.Finished(task)

	th.host.rec.Progress(task, progress(90))
	time.Sleep(2 * progressInterval)
	if got := th.progressFile(jobA); got != "" {
		t.Fatalf("progress after Finished = %s, want none", got)
	}
	if record := th.record(jobA); record.State != jobs.StateCanceled {
		t.Fatalf("record = %s, want canceled", record.State)
	}
}

func TestRecorderCoalescesProgressWrites(t *testing.T) {
	th := newTestHost(t, nil)
	task := claim(t, th, jobA)
	th.host.rec.Started(task, jsontext.Value(`{}`))

	th.host.rec.Progress(task, progress(10))
	if got := th.progressFile(jobA); got != `{"percentage":10}` {
		t.Fatalf("first progress = %s, want written at once", got)
	}
	th.host.rec.Progress(task, progress(20))
	th.host.rec.Progress(task, progress(30))
	if got := th.progressFile(jobA); got != `{"percentage":10}` {
		t.Fatalf("progress within the interval = %s, want the first value kept", got)
	}
	waitFor(t, "the coalesced progress", func() bool { return th.progressFile(jobA) == `{"percentage":30}` })

	th.host.rec.Progress(task, progress(40))
	task.Cancel()
	th.host.rec.Finished(task)
	if got := th.progressFile(jobA); got != `{"percentage":40}` {
		t.Fatalf("progress after Finished = %s, want the last value flushed", got)
	}
}

func TestRecorderStopFlushesAndCancelsProgressTimers(t *testing.T) {
	th := newTestHost(t, nil)
	task := claim(t, th, jobA)
	th.host.rec.Started(task, jsontext.Value(`{}`))
	th.host.rec.Progress(task, progress(10))
	th.host.rec.Progress(task, progress(20)) // waits for a timer

	th.host.rec.stop() // the host shut down
	if got := th.progressFile(jobA); got != `{"percentage":20}` {
		t.Fatalf("progress after stop = %s, want the pending value written", got)
	}
	th.host.rec.Progress(task, progress(30))
	time.Sleep(2 * progressInterval)
	if got := th.progressFile(jobA); got != `{"percentage":20}` {
		t.Fatalf("progress after stop = %s, want no timed write", got)
	}
	task.Cancel()
	th.host.rec.Finished(task)
	if got := th.progressFile(jobA); got != `{"percentage":30}` {
		t.Fatalf("progress after Finished = %s, want the last value flushed", got)
	}
}

func TestHostAnswersAResendOfAStoredJobFromItsRecord(t *testing.T) {
	th := newTestHost(t, nil)
	now := time.Now().UTC()
	stored := func(id string, state jobs.State, result string, jobErr *jobs.Error) jobs.Record {
		record := jobs.Record{
			ID: id, Route: "test.job", Owner: jobs.Owner{UID: th.cfg.UID, Username: "alice"},
			State: state, CreatedAt: now.Add(-2 * time.Second), StartedAt: now.Add(-2 * time.Second), FinishedAt: now,
			Request: jsontext.Value(`{ }`), Error: jobErr, Executor: jobs.Executor{Kind: "host"}, Protocol: jobs.Protocol,
		}
		if result != "" {
			record.Result = jsontext.Value(result)
		}
		return record
	}
	for _, record := range []jobs.Record{
		stored(jobA, jobs.StateCompleted, `{"ok":true}`, nil),
		stored(jobB, jobs.StateFailed, "", &jobs.Error{Code: 422, Message: "boom"}),
		stored(jobC, jobs.StateInterrupted, "", nil),
	} {
		if err := th.cfg.Store.Create(context.Background(), record); err != nil {
			t.Fatalf("Store.Create(%s): %v", record.ID, err)
		}
	}
	th.start()
	sess := th.connect()

	for _, tc := range []struct {
		id    string
		state bridgeipc.TaskState
		want  relay.ResultFrame
	}{
		{jobA, bridgeipc.TaskStateCompleted, relay.ResultFrame{Status: "ok", Data: jsontext.Value(`{"ok":true}`)}},
		{jobB, bridgeipc.TaskStateFailed, relay.ResultFrame{Status: "error", Error: "boom", Code: 422}},
		{jobC, bridgeipc.TaskStateInterrupted, relay.ResultFrame{Status: "error", Error: "job was interrupted", Code: 409}},
	} {
		stream := openStream(t, sess, "test.job", struct{}{}, tc.id)
		snapshot := readSnapshot(t, stream)
		if snapshot.ID != tc.id || snapshot.Type != "test.job" || snapshot.State != tc.state ||
			snapshot.Owner.Username != "alice" || snapshot.FinishedAt == nil {
			t.Fatalf("snapshot of %s = %#v, want the stored %s job", tc.id, snapshot, tc.state)
		}
		result := readResult(t, stream)
		if len(result.Data) > 0 {
			result.Data = jsontext.Value(compact(t, result.Data))
		}
		if result.Status != tc.want.Status || result.Error != tc.want.Error || result.Code != tc.want.Code || string(result.Data) != string(tc.want.Data) {
			t.Fatalf("result of %s = %#v, want %#v", tc.id, result, tc.want)
		}
	}
	if runs := th.runs.Load(); runs != 0 {
		t.Fatalf("handler ran %d times for resends of stored jobs, want 0", runs)
	}
	if _, running := th.host.counts(); running != 0 {
		t.Fatalf("running = %d, want 0", running)
	}
}

func TestHostRefusesAResendOfAStoredJobWithAnotherRequest(t *testing.T) {
	th := newTestHost(t, nil)
	now := time.Now().UTC()
	if err := th.cfg.Store.Create(context.Background(), jobs.Record{
		ID: jobA, Route: "test.job", Owner: jobs.Owner{UID: th.cfg.UID, Username: "alice"},
		State: jobs.StateCompleted, CreatedAt: now, StartedAt: now, FinishedAt: now,
		Request: jsontext.Value(`{"pool":"tank"}`), Executor: jobs.Executor{Kind: "host"}, Protocol: jobs.Protocol,
	}); err != nil {
		t.Fatalf("Store.Create: %v", err)
	}
	th.start()
	sess := th.connect()

	stream := openStream(t, sess, "test.job", struct{}{}, jobA)
	if result := readResult(t, stream); result.Status != "error" || result.Code != 409 || result.Error != "job id was used for another request" {
		t.Fatalf("result = %#v, want 409 job id was used for another request", result)
	}
	if runs := th.runs.Load(); runs != 0 {
		t.Fatalf("handler ran %d times, want 0", runs)
	}
}

func TestRootHostRecordsTheClaimedOwnerUID(t *testing.T) {
	th := newTestHost(t, func(cfg *Config) {
		cfg.UID = 0
		cfg.Peer = func(*net.UnixConn) (peercred.Cred, error) { return peercred.Cred{UID: 0}, nil }
	})
	th.start()
	sess := th.connect() // alice, claiming uid 4242

	th.startJob(sess, "test.job", jobA)
	if record := th.record(jobA); record.Owner != (jobs.Owner{UID: 4242, Username: "alice"}) {
		t.Fatalf("record owner = %#v, want alice as uid 4242", record.Owner)
	}
	// Permission checks keep the host uid: the task is root's, so every
	// admin's connection sees it.
	task, ok := th.cfg.Tasks.Get(jobA)
	if !ok || task.Owner().UID != 0 {
		t.Fatalf("task owner uid = %d (found %v), want 0", task.Owner().UID, ok)
	}

	_ = sess.Close()
	waitFor(t, "the claim to be released", func() bool {
		th.host.mu.Lock()
		defer th.host.mu.Unlock()
		return len(th.host.owners) == 0
	})
	th.release <- nil
}

// serveOneStream serves one stream opening route test.job with id, under ctx,
// and returns its result.
func serveOneStream(t *testing.T, th *testHost, ctx context.Context, id string) relay.ResultFrame {
	t.Helper()
	server, client := net.Pipe()
	defer client.Close()
	served := make(chan struct{})
	go func() {
		defer close(served)
		defer server.Close()
		th.host.serveStream(ctx, server, &session.Session{SessionID: "s", User: session.User{Username: "alice", UID: th.cfg.UID}})
	}()
	payload, err := json.Marshal(relay.StreamOpenEnvelope{Route: "test.job", Request: jsontext.Value(`{}`), ID: id})
	if err != nil {
		t.Fatalf("json.Marshal(open): %v", err)
	}
	if err := relay.WriteRelayFrame(client, &relay.StreamFrame{Opcode: relay.OpStreamOpen, Payload: payload}); err != nil {
		t.Fatalf("write stream open: %v", err)
	}
	result := readResult(t, client)
	_ = client.Close()
	<-served
	return result
}

func TestStoppingHostRefusesANewJobButAnInvalidIDFirst(t *testing.T) {
	th := newTestHost(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // SIGTERM

	if result := serveOneStream(t, th, ctx, jobA); result.Status != "error" || result.Code != 503 || result.Error != "job host is stopping" {
		t.Fatalf("new job on a stopping host = %#v, want 503 job host is stopping", result)
	}
	if result := serveOneStream(t, th, ctx, "not-a-job-id"); result.Status != "error" || result.Code != 400 {
		t.Fatalf("invalid job id on a stopping host = %#v, want 400", result)
	}
	if runs := th.runs.Load(); runs != 0 {
		t.Fatalf("handler ran %d times, want 0", runs)
	}
}

func TestClosingHostDropsANewBridge(t *testing.T) {
	th := newTestHost(t, nil)
	th.start()
	th.host.mu.Lock()
	th.host.closing = true
	th.host.mu.Unlock()

	body, err := json.Marshal(bridgeipc.Hello{Protocol: jobs.Protocol})
	if err != nil {
		t.Fatalf("json.Marshal(hello): %v", err)
	}
	payload, err := json.Marshal(relay.StreamOpenEnvelope{Route: bridgeipc.HelloRoute, Request: body})
	if err != nil {
		t.Fatalf("json.Marshal(open): %v", err)
	}
	sess := th.dial()
	// The hello may or may not get out before the host closes the session.
	if stream, err := sess.Open(context.Background()); err == nil {
		defer stream.Close()
		_ = relay.WriteRelayFrame(stream, &relay.StreamFrame{Opcode: relay.OpStreamOpen, Payload: payload})
	}
	select {
	case <-sess.CloseChan():
	case <-time.After(ioTimeout):
		t.Fatal("session still open on a closing host")
	}
	if bridges, _ := th.host.counts(); bridges != 0 {
		t.Fatalf("bridges = %d, want 0", bridges)
	}
}
