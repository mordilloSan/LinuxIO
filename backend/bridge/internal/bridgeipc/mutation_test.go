package bridgeipc

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

func TestReportProgressReachesTheTaskAndIsNoopOutsideOne(t *testing.T) {
	ReportProgress(context.Background(), testTaskProgress(10))
	if _, ok := TaskFromContext(context.Background()); ok {
		t.Fatal("TaskFromContext found a task in a context that carries none")
	}

	var fromContext *Task
	task, err := startTestTask(NewTaskService(), "test.context_progress", nil, TaskOwner{}, func(ctx context.Context, _ *Task, _ any) (any, error) {
		fromContext, _ = TaskFromContext(ctx)
		ReportProgress(ctx, testTaskProgress(40))
		return nil, nil
	})
	if err != nil {
		t.Fatalf("startTestTask returned error: %v", err)
	}

	waitForTaskState(t, task, TaskStateCompleted)
	if fromContext != task {
		t.Fatalf("TaskFromContext in the runner = %p, want the running task %p", fromContext, task)
	}
	progress := task.Snapshot().Progress
	if progress == nil || progress.Percentage == nil || *progress.Percentage != 40 {
		t.Fatalf("progress = %#v, want the 40%% reported through the context", progress)
	}
}

func TestMutationRouteDefaultsToDurableLifetime(t *testing.T) {
	router := NewRouter(NewTaskService())
	router.Mutation("test.mutation", func(context.Context, *Task, any) (any, error) { return nil, nil })

	route, ok := router.lookup("test.mutation")
	if !ok {
		t.Fatal("mutation route was not registered")
	}
	if route.Mode != ModeMutation || route.Lifetime != TaskLifetimeDurable || route.Policy != TaskDefault {
		t.Fatalf("route mode = %q, lifetime = %q, policy = %q; want %q, %q, %q",
			route.Mode, route.Lifetime, route.Policy.Name, ModeMutation, TaskLifetimeDurable, TaskDefault.Name)
	}
}

func TestMutationOptionsAreAllowedOnlyOnMutationRoutes(t *testing.T) {
	suspend := func(context.Context, jsontext.Value) (jsontext.Value, error) { return nil, nil }
	resume := func(context.Context, jsontext.Value) (any, error) { return nil, nil }
	options := map[string]RouteOption{
		"browser bound": WithBrowserBound(),
		"target":        WithTarget(func(any) []string { return []string{"pool"} }),
		"resume":        WithResume(suspend, resume),
	}
	for name, opt := range options {
		t.Run(name, func(t *testing.T) {
			router := NewRouter(NewTaskService())
			router.Mutation("test.mutation", func(context.Context, *Task, any) (any, error) { return nil, nil }, opt)
			route, _ := router.lookup("test.mutation")
			if !route.BrowserBound && route.Target == nil && route.Suspend == nil && route.Resume == nil {
				t.Fatal("mutation route dropped the option")
			}

			for mode, register := range map[Mode]func(){
				ModeCall: func() {
					router.Call("test.call", func(context.Context, Request) (any, error) { return nil, nil }, opt)
				},
				ModeTask: func() {
					router.TaskRunner("test.task", func(context.Context, *Task, any) (any, error) { return nil, nil }, TaskDefault, opt)
				},
				ModeDuplex: func() {
					router.Duplex("test.duplex", func(context.Context, net.Conn, Request) error { return nil }, opt)
				},
			} {
				func() {
					defer func() {
						if recover() == nil {
							t.Errorf("%s route accepted a mutation option", mode)
						}
					}()
					register()
				}()
			}
		})
	}
}

func TestWithResumeRejectsMissingHooks(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("WithResume accepted a nil hook")
		}
	}()
	WithResume(func(context.Context, jsontext.Value) (jsontext.Value, error) { return nil, nil }, nil)
}

const testMutationID = "00000000-0000-4000-8000-000000000001"

var testMutationSession = &session.Session{SessionID: "session-a", User: session.User{Username: "alice", UID: 1000}}

func mutationRequest(raw string) Request {
	return Request{Route: "test.mutation", RawRequest: jsontext.Value(raw), ID: testMutationID, Session: testMutationSession}
}

// openMutation dispatches req on an in-process stream and returns the client end.
func openMutation(t *testing.T, router *Router, req Request) (net.Conn, <-chan error) {
	t.Helper()
	server, client := net.Pipe()
	t.Cleanup(func() { _ = client.Close() })
	errCh := make(chan error, 1)
	go func() {
		defer server.Close()
		errCh <- router.Dispatch(context.Background(), server, req)
	}()
	return client, errCh
}

func readFrame(t *testing.T, conn net.Conn, want byte) []byte {
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

func readSnapshotEvent(t *testing.T, conn net.Conn) TaskSnapshot {
	t.Helper()
	var event struct {
		Type TaskEventType `json:"type"`
		Task TaskSnapshot  `json:"task"`
	}
	if err := json.Unmarshal(readFrame(t, conn, relay.OpStreamProgress), &event); err != nil {
		t.Fatalf("json.Unmarshal(snapshot): %v", err)
	}
	if event.Type != TaskEventSnapshot || event.Task.ID == "" {
		t.Fatalf("first frame = %#v, want a task.snapshot event", event)
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

// gatedMutation registers a mutation that waits for proceed, reports 40% and
// returns {"ok":true}, counting its runs.
func gatedMutation(router *Router, proceed <-chan struct{}, runs *atomic.Int32) {
	router.Mutation("test.mutation", func(ctx context.Context, _ *Task, _ any) (any, error) {
		runs.Add(1)
		select {
		case <-proceed:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		ReportProgress(ctx, testTaskProgress(40))
		return map[string]bool{"ok": true}, nil
	})
}

func TestMutationStreamWritesSnapshotProgressAndResult(t *testing.T) {
	router := NewRouter(NewTaskService())
	proceed := make(chan struct{})
	var runs atomic.Int32
	gatedMutation(router, proceed, &runs)

	client, errCh := openMutation(t, router, mutationRequest(`{"pool":"tank"}`))
	snapshot := readSnapshotEvent(t, client)
	if snapshot.ID != testMutationID || snapshot.Type != "test.mutation" {
		t.Fatalf("snapshot id = %q, type = %q; want %q, test.mutation", snapshot.ID, snapshot.Type, testMutationID)
	}
	close(proceed)

	var progress TaskProgress
	if err := json.Unmarshal(readFrame(t, client, relay.OpStreamProgress), &progress); err != nil {
		t.Fatalf("json.Unmarshal(progress): %v", err)
	}
	if progress.Percentage == nil || *progress.Percentage != 40 {
		t.Fatalf("progress = %#v, want 40%%", progress)
	}
	result := readResult(t, client)
	if result.Status != "ok" || string(result.Data) != `{"ok":true}` {
		t.Fatalf("result = %#v, want ok with {\"ok\":true}", result)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("Dispatch returned error: %v", err)
	}
}

func TestMutationResendWithSameIDAttaches(t *testing.T) {
	router := NewRouter(NewTaskService())
	proceed := make(chan struct{})
	var runs atomic.Int32
	gatedMutation(router, proceed, &runs)

	first, firstErr := openMutation(t, router, mutationRequest(`{"pool":"tank"}`))
	firstSnapshot := readSnapshotEvent(t, first)
	second, secondErr := openMutation(t, router, mutationRequest(`{"pool":"tank"}`))
	secondSnapshot := readSnapshotEvent(t, second)
	if secondSnapshot.ID != firstSnapshot.ID {
		t.Fatalf("resend attached to task %q, want %q", secondSnapshot.ID, firstSnapshot.ID)
	}
	close(proceed)

	for _, conn := range []net.Conn{first, second} {
		readFrame(t, conn, relay.OpStreamProgress)
		if result := readResult(t, conn); result.Status != "ok" {
			t.Fatalf("result = %#v, want ok", result)
		}
	}
	for _, errCh := range []<-chan error{firstErr, secondErr} {
		if err := <-errCh; err != nil {
			t.Fatalf("Dispatch returned error: %v", err)
		}
	}
	if got := runs.Load(); got != 1 {
		t.Fatalf("runner calls = %d, want 1", got)
	}
	waitForRouterSettle(t, router, "test.mutation")
}

func TestMutationResendWithDifferentRequestIs409(t *testing.T) {
	router := NewRouter(NewTaskService())
	proceed := make(chan struct{})
	close(proceed)
	var runs atomic.Int32
	gatedMutation(router, proceed, &runs)

	first, firstErr := openMutation(t, router, mutationRequest(`{"pool":"tank"}`))
	readSnapshotEvent(t, first)
	readFrame(t, first, relay.OpStreamProgress)
	readResult(t, first)
	if err := <-firstErr; err != nil {
		t.Fatalf("Dispatch returned error: %v", err)
	}

	result, err := dispatchResult(t, router, mutationRequest(`{"pool":"other"}`))
	if !errors.Is(err, ErrTaskIdentityConflict) || result.Status != "error" || result.Code != 409 {
		t.Fatalf("result = %#v, error = %v; want 409 identity conflict", result, err)
	}
	if got := runs.Load(); got != 1 {
		t.Fatalf("runner calls = %d, want 1", got)
	}
}

func TestMutationWithoutIDIs400(t *testing.T) {
	for _, id := range []string{"", "not-a-uuid", "00000000-0000-0000-0000-000000000000"} {
		router := NewRouter(NewTaskService())
		var runs atomic.Int32
		gatedMutation(router, nil, &runs)
		req := mutationRequest(`{}`)
		req.ID = id

		result, err := dispatchResult(t, router, req)
		if !errors.Is(err, ErrInvalidArgs) || result.Status != "error" || result.Code != 400 {
			t.Fatalf("id %q: result = %#v, error = %v; want 400", id, result, err)
		}
		if got := runs.Load(); got != 0 {
			t.Fatalf("id %q: runner calls = %d, want 0", id, got)
		}
	}
}

func TestMutationCloseDetachesAndAbortCancels(t *testing.T) {
	registry := NewTaskService()
	router := NewRouter(registry)
	var runs atomic.Int32
	gatedMutation(router, nil, &runs)

	first, firstErr := openMutation(t, router, mutationRequest(`{}`))
	readSnapshotEvent(t, first)
	if err := relay.WriteRelayFrame(first, &relay.StreamFrame{Opcode: relay.OpStreamClose}); err != nil {
		t.Fatalf("WriteRelayFrame(close): %v", err)
	}
	if err := <-firstErr; err != nil {
		t.Fatalf("Dispatch after close returned error: %v", err)
	}
	task, ok := registry.Get(testMutationID)
	if !ok || task.IsTerminal() {
		t.Fatalf("task after close: found = %v, want it still running", ok)
	}

	second, secondErr := openMutation(t, router, mutationRequest(`{}`))
	readSnapshotEvent(t, second)
	if err := relay.WriteRelayFrame(second, &relay.StreamFrame{Opcode: relay.OpStreamAbort}); err != nil {
		t.Fatalf("WriteRelayFrame(abort): %v", err)
	}
	if err := <-secondErr; err != nil {
		t.Fatalf("Dispatch after abort returned error: %v", err)
	}
	waitForTaskState(t, task, TaskStateCanceled)
	if got := runs.Load(); got != 1 {
		t.Fatalf("runner calls = %d, want 1", got)
	}
}

// A host stop interrupts a running job: its stream ends with 409, not the
// 499 of a user cancel.
func TestMutationInterruptEndsTheStreamInterrupted(t *testing.T) {
	registry := NewTaskService()
	router := NewRouter(registry)
	var runs atomic.Int32
	gatedMutation(router, nil, &runs)

	client, errCh := openMutation(t, router, mutationRequest(`{}`))
	readSnapshotEvent(t, client)
	task, ok := registry.Get(testMutationID)
	if !ok {
		t.Fatal("mutation task not found")
	}
	waitForTaskState(t, task, TaskStateRunning)
	task.Interrupt()

	if result := readResult(t, client); result.Status != "error" || result.Code != 409 || result.Error != "job was interrupted" {
		t.Fatalf("result = %#v, want 409 job was interrupted", result)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("Dispatch returned error: %v", err)
	}
	if snapshot := task.Snapshot(); snapshot.State != TaskStateInterrupted || snapshot.Error == nil || snapshot.Error.Code != 409 {
		t.Fatalf("snapshot = %s/%#v, want interrupted with 409", snapshot.State, snapshot.Error)
	}
}

type recordedCall struct {
	kind  string
	state TaskState
	raw   string
	pct   int
}

type fakeRecorder struct {
	mu    sync.Mutex
	calls []recordedCall
}

func (f *fakeRecorder) add(call recordedCall) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
}

func (f *fakeRecorder) Started(task *Task, raw jsontext.Value) {
	f.add(recordedCall{kind: "started", state: task.Snapshot().State, raw: string(raw)})
}

func (f *fakeRecorder) Progress(task *Task, p TaskProgress) {
	f.add(recordedCall{kind: "progress", state: task.Snapshot().State, pct: *p.Percentage})
}

func (f *fakeRecorder) Finished(task *Task) {
	f.add(recordedCall{kind: "finished", state: task.Snapshot().State})
}

func (f *fakeRecorder) snapshot() []recordedCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recordedCall(nil), f.calls...)
}

func TestRecorderSeesStartProgressFinish(t *testing.T) {
	registry := NewTaskService()
	recorder := &fakeRecorder{}
	registry.SetRecorder(recorder)
	router := NewRouter(registry)
	proceed := make(chan struct{})
	close(proceed)
	var runs atomic.Int32
	gatedMutation(router, proceed, &runs)
	router.TaskRunner("test.task", func(context.Context, *Task, any) (any, error) { return nil, nil }, TaskDefault)

	client, errCh := openMutation(t, router, mutationRequest(`{"pool":"tank"}`))
	readSnapshotEvent(t, client)
	readFrame(t, client, relay.OpStreamProgress)
	readResult(t, client)
	if err := <-errCh; err != nil {
		t.Fatalf("Dispatch returned error: %v", err)
	}

	want := []recordedCall{
		{kind: "started", state: TaskStateQueued, raw: `{"pool":"tank"}`},
		{kind: "progress", state: TaskStateRunning, pct: 40},
		{kind: "finished", state: TaskStateCompleted},
	}
	if got := recorder.snapshot(); len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("recorder calls = %#v, want %#v", got, want)
	}

	if _, err := dispatchResult(t, router, Request{Route: "test.task", Session: testMutationSession}); err != nil {
		t.Fatalf("Dispatch(task route) returned error: %v", err)
	}
	waitForRouterSettle(t, router, "test.task")
	if got := recorder.snapshot(); len(got) != len(want) {
		t.Fatalf("a task route reached the recorder: %#v", got[len(want):])
	}
}

type fakeForwarder struct {
	calls []forwardCall
}

type forwardCall struct {
	open relay.StreamOpenEnvelope
	raw  []byte
}

func (f *fakeForwarder) Forward(_ context.Context, stream net.Conn, open relay.StreamOpenEnvelope, raw []byte) error {
	f.calls = append(f.calls, forwardCall{open: open, raw: raw})
	return relay.WriteResultOKAndClose(stream, 0, "forwarded")
}

func TestMutationForwarderTakesAllButBrowserBoundRoutes(t *testing.T) {
	registry := NewTaskService()
	router := NewRouter(registry)
	forwarder := &fakeForwarder{}
	router.SetMutationForwarder(forwarder)
	var runs atomic.Int32
	router.Mutation("test.mutation", func(context.Context, *Task, any) (any, error) {
		runs.Add(1)
		return nil, nil
	})
	router.Mutation("test.browser", func(context.Context, *Task, any) (any, error) {
		runs.Add(1)
		return map[string]bool{"ok": true}, nil
	}, WithBrowserBound())

	req := mutationRequest(`{"pool":"tank"}`)
	req.OpenRaw = []byte(`{"route":"test.mutation","request":{"pool":"tank"},"id":"` + testMutationID + `"}`)
	result, err := dispatchResult(t, router, req)
	if err != nil || string(result.Data) != `"forwarded"` {
		t.Fatalf("forwarded result = %#v, error = %v", result, err)
	}
	if len(forwarder.calls) != 1 {
		t.Fatalf("forwarder calls = %d, want 1", len(forwarder.calls))
	}
	call := forwarder.calls[0]
	if call.open.Route != req.Route || string(call.open.Request) != string(req.RawRequest) || call.open.ID != req.ID || string(call.raw) != string(req.OpenRaw) {
		t.Fatalf("forwarded open = %#v, raw = %s; want the dispatched route, request, id and payload", call.open, call.raw)
	}
	if _, ok := registry.Get(testMutationID); ok || runs.Load() != 0 {
		t.Fatalf("forwarded mutation ran locally: task found = %v, runs = %d", ok, runs.Load())
	}

	req = mutationRequest(`{}`)
	req.Route = "test.browser"
	client, errCh := openMutation(t, router, req)
	readSnapshotEvent(t, client)
	if result := readResult(t, client); result.Status != "ok" {
		t.Fatalf("browser-bound result = %#v, want ok", result)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("Dispatch(browser-bound) returned error: %v", err)
	}
	if len(forwarder.calls) != 1 || runs.Load() != 1 {
		t.Fatalf("browser-bound route: forwarder calls = %d, runs = %d; want 1 and 1", len(forwarder.calls), runs.Load())
	}
}

type poolRequest struct {
	Pool string `json:"pool"`
}

func TestMutationTargetRefusesASecondRunOnTheSameTarget(t *testing.T) {
	router := NewRouter(NewTaskService())
	proceed := make(chan struct{})
	router.Mutation("test.mutation", func(ctx context.Context, _ *Task, _ any) (any, error) {
		select {
		case <-proceed:
			return nil, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	},
		WithRequestDecoder(JSONRequestDecoder[poolRequest]()),
		WithTarget(func(request any) []string {
			pool, _ := request.(poolRequest)
			return []string{pool.Pool}
		}),
	)

	first, firstErr := openMutation(t, router, mutationRequest(`{"pool":"tank"}`))
	readSnapshotEvent(t, first)

	sameTarget := mutationRequest(`{"pool":"tank"}`)
	sameTarget.ID = "00000000-0000-4000-8000-000000000002"
	result, err := dispatchResult(t, router, sameTarget)
	if !errors.Is(err, ErrDuplicateActive) || result.Status != "error" || result.Code != 409 {
		t.Fatalf("same-target result = %#v, error = %v; want 409 duplicate active", result, err)
	}

	otherTarget := mutationRequest(`{"pool":"other"}`)
	otherTarget.ID = "00000000-0000-4000-8000-000000000003"
	other, otherErr := openMutation(t, router, otherTarget)
	if snapshot := readSnapshotEvent(t, other); snapshot.ID != otherTarget.ID {
		t.Fatalf("other-target snapshot id = %q, want %q", snapshot.ID, otherTarget.ID)
	}

	close(proceed)
	for _, conn := range []net.Conn{first, other} {
		if result := readResult(t, conn); result.Status != "ok" {
			t.Fatalf("result = %#v, want ok", result)
		}
	}
	for _, errCh := range []<-chan error{firstErr, otherErr} {
		if err := <-errCh; err != nil {
			t.Fatalf("Dispatch returned error: %v", err)
		}
	}
	waitForRouterSettle(t, router, "test.mutation")
}

func TestInterruptedIsATerminalTaskState(t *testing.T) {
	for state, want := range map[TaskState]bool{
		TaskStateQueued:      false,
		TaskStateRunning:     false,
		TaskStateCompleted:   true,
		TaskStateFailed:      true,
		TaskStateCanceled:    true,
		TaskStateInterrupted: true,
	} {
		if got := state.Terminal(); got != want {
			t.Errorf("%s.Terminal() = %v, want %v", state, got, want)
		}
	}

	// A watch on an interrupted snapshot ends with its error, as for a cancel.
	server, client := net.Pipe()
	defer client.Close()
	go func() {
		defer server.Close()
		writeTerminalTaskSnapshot(server, TaskSnapshot{State: TaskStateInterrupted, Error: NewError("job was interrupted", 409)})
	}()
	if result := readResult(t, client); result.Status != "error" || result.Code != 409 || result.Error != "job was interrupted" {
		t.Fatalf("interrupted snapshot result = %#v, want 409 job was interrupted", result)
	}
}
