package bridgeipc

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io"
	"log/slog"
	"net"
	"slices"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

func TestNoJobsResumeRoute(t *testing.T) {
	service := NewTaskService()
	router := NewRouter(service)
	service.RegisterRoutes(router)
	if _, ok := router.lookup("jobs.resume"); ok {
		t.Fatal("jobs.resume is registered")
	}
}

func TestTaskPrimitiveDecoderEnforcesStrictSingleValuePolicy(t *testing.T) {
	service := NewTaskService()
	router := NewRouter(service)
	service.RegisterRoutes(router)
	route, ok := router.lookup("tasks.get")
	if !ok {
		t.Fatal("tasks.get is not registered")
	}

	tests := []struct {
		name               string
		raw                jsontext.Value
		wantTaskID         string
		wantSemanticError  bool
		wantSyntacticError bool
	}{
		{name: "valid object", raw: jsontext.Value(`{"taskId":"task-1"}`), wantTaskID: "task-1"},
		{name: "unknown field", raw: jsontext.Value(`{"taskId":"task-1","unexpected":true}`), wantSemanticError: true},
		{name: "case-mismatched field", raw: jsontext.Value(`{"TaskId":"task-1"}`), wantSemanticError: true},
		{name: "duplicate field", raw: jsontext.Value(`{"taskId":"task-1","taskId":"task-2"}`), wantSyntacticError: true},
		{name: "invalid UTF-8", raw: jsontext.Value("{\"taskId\":\"\xff\"}"), wantSyntacticError: true},
		{name: "trailing JSON value", raw: jsontext.Value(`{"taskId":"task-1"} {}`), wantSyntacticError: true},
		{name: "scalar type mismatch", raw: jsontext.Value(`{"taskId":123}`), wantSemanticError: true},
		{name: "empty input", raw: nil},
		{name: "null input", raw: jsontext.Value(`null`)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertTaskPrimitiveDecode(t, route.Decode, tc.raw, tc.wantTaskID, tc.wantSemanticError, tc.wantSyntacticError)
		})
	}
}

func assertTaskPrimitiveDecode(
	t *testing.T,
	decode RequestDecoder,
	raw jsontext.Value,
	wantTaskID string,
	wantSemanticError bool,
	wantSyntacticError bool,
) {
	t.Helper()
	decoded, err := decode(raw)
	if wantSemanticError {
		if _, ok := errors.AsType[*json.SemanticError](err); !ok {
			t.Fatalf("Decode() error = %v, want *json.SemanticError", err)
		}
		return
	}
	if wantSyntacticError {
		if _, ok := errors.AsType[*jsontext.SyntacticError](err); !ok {
			t.Fatalf("Decode() error = %v, want *jsontext.SyntacticError", err)
		}
		return
	}
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	request, ok := decoded.(taskIDRequest)
	if !ok {
		t.Fatalf("Decode() result = %T, want taskIDRequest", decoded)
	}
	if request.TaskID != wantTaskID {
		t.Fatalf("taskId = %q, want %q", request.TaskID, wantTaskID)
	}
}

const remoteJobID = "00000000-0000-4000-8000-0000000000aa"

// fakeRemote stands in for the job host behind a bridge.
type fakeRemote struct {
	list     []TaskSnapshot
	canceled chan string
	events   []TaskEvent
	dropped  chan struct{}
	// ended is the one job Get finds.
	ended TaskSnapshot
	// history is what History returns, and asked the request it was last given.
	history TaskHistory
	asked   TaskHistoryRequest
	// output is what Output returns, and outputAsked the request it was last
	// given, nil until it is asked.
	output      TaskOutput
	outputAsked *TaskOutputRequest
	// status is what Status returns.
	status TaskHostStatus
}

func (f *fakeRemote) Get(_ context.Context, id string) (TaskSnapshot, error) {
	if id == f.ended.ID {
		return f.ended, nil
	}
	return TaskSnapshot{}, NewError("task not found: "+id, 404)
}

func (f *fakeRemote) Cancel(_ context.Context, id string) (TaskSnapshot, error) {
	if id != remoteJobID {
		return TaskSnapshot{}, NewError("task not found: "+id, 404)
	}
	f.canceled <- id
	return TaskSnapshot{ID: id, State: TaskStateCanceled}, nil
}

func (f *fakeRemote) Watch(context.Context, net.Conn, string) error { return nil }

func (f *fakeRemote) List(context.Context) ([]TaskSnapshot, error) { return f.list, nil }

func (f *fakeRemote) History(_ context.Context, req TaskHistoryRequest) (TaskHistory, error) {
	f.asked = req
	return f.history, nil
}

func (f *fakeRemote) Output(_ context.Context, req TaskOutputRequest) (TaskOutput, error) {
	f.outputAsked = &req
	return f.output, nil
}

func (f *fakeRemote) Status(context.Context) (TaskHostStatus, error) { return f.status, nil }

// Events writes its events and returns, as a host connection that drops does.
func (f *fakeRemote) Events(_ context.Context, stream net.Conn) error {
	defer close(f.dropped)
	for _, event := range f.events {
		if err := relay.WriteProgress(stream, 0, event); err != nil {
			return err
		}
	}
	return nil
}

var (
	remoteTestSession = &session.Session{SessionID: "session-a", User: session.User{Username: "alice", UID: 1000}}
	// privilegedTestSession is remoteTestSession after sudo.
	privilegedTestSession = &session.Session{SessionID: "session-a", User: session.User{Username: "alice", UID: 1000}, Privileged: true}
)

func newRemoteTestRouter(t *testing.T, remote RemoteTasks) (*Router, *TaskService) {
	t.Helper()
	// The router logs every refused route; these tests refuse some on purpose.
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.DiscardHandler))
	t.Cleanup(func() { slog.SetDefault(previous) })
	service := NewTaskService()
	router := NewRouter(service)
	service.RegisterRoutes(router)
	service.SetRemote(remote)
	return router, service
}

// startBlockedTask starts a local task of remoteTestSession that runs until
// the test ends.
func startBlockedTask(t *testing.T, service *TaskService) *Task {
	t.Helper()
	release := make(chan struct{})
	task, err := startTestTask(service, "test.local", nil, ownerFromSession(remoteTestSession), func(context.Context, *Task, any) (any, error) {
		<-release
		return nil, nil
	})
	if err != nil {
		t.Fatalf("startTestTask: %v", err)
	}
	t.Cleanup(func() {
		close(release)
		<-task.Done()
	})
	return task
}

// dispatchOnPipe dispatches route for remoteTestSession over a net.Pipe and
// returns the client end and the result of Dispatch.
func dispatchOnPipe(router *Router, route string, request string) (net.Conn, <-chan error) {
	return dispatchOnPipeAs(router, remoteTestSession, route, request)
}

// dispatchOnPipeAs is dispatchOnPipe for sess.
func dispatchOnPipeAs(router *Router, sess *session.Session, route string, request string) (net.Conn, <-chan error) {
	server, client := net.Pipe()
	errCh := make(chan error, 1)
	go func() {
		defer server.Close()
		errCh <- router.Dispatch(context.Background(), server, Request{Route: route, RawRequest: jsontext.Value(request), Session: sess})
	}()
	return client, errCh
}

func callOnPipe(t *testing.T, router *Router, route string, request string) relay.ResultFrame {
	t.Helper()
	return callOnPipeAs(t, router, remoteTestSession, route, request)
}

// callOnPipeAs is callOnPipe for sess.
func callOnPipeAs(t *testing.T, router *Router, sess *session.Session, route string, request string) relay.ResultFrame {
	t.Helper()
	client, errCh := dispatchOnPipeAs(router, sess, route, request)
	defer client.Close()
	_ = client.SetReadDeadline(time.Now().Add(5 * time.Second))
	frame, err := relay.ReadRelayFrame(client)
	if err != nil || frame.Opcode != relay.OpStreamResult {
		t.Fatalf("%s: frame %+v, error %v; want a result", route, frame, err)
	}
	var result relay.ResultFrame
	if err := json.Unmarshal(frame.Payload, &result); err != nil {
		t.Fatalf("decode %s result: %v", route, err)
	}
	if frame, err := relay.ReadRelayFrame(client); err != nil || frame.Opcode != relay.OpStreamClose {
		t.Fatalf("%s: frame %+v, error %v; want a close", route, frame, err)
	}
	if err := <-errCh; (err == nil) != (result.Status == "ok") {
		t.Fatalf("%s: Dispatch error %v disagrees with result %+v", route, err, result)
	}
	return result
}

func TestTasksListMergesLocalAndRemote(t *testing.T) {
	router, service := newRemoteTestRouter(t, &fakeRemote{list: []TaskSnapshot{{ID: remoteJobID, State: TaskStateRunning}}})
	local := startBlockedTask(t, service)

	result := callOnPipe(t, router, "tasks.list", `{}`)
	var snapshots []TaskSnapshot
	if result.Status != "ok" || json.Unmarshal(result.Data, &snapshots) != nil {
		t.Fatalf("tasks.list = %+v, want ok", result)
	}
	var ids []string
	for _, snapshot := range snapshots {
		ids = append(ids, snapshot.ID)
	}
	if want := []string{local.ID(), remoteJobID}; !slices.Equal(ids, want) {
		t.Fatalf("tasks.list ids = %v, want %v", ids, want)
	}
}

func TestTasksCancelRoutesUUIDToRemote(t *testing.T) {
	remote := &fakeRemote{canceled: make(chan string, 1)}
	router, _ := newRemoteTestRouter(t, remote)

	result := callOnPipe(t, router, "tasks.cancel", `{"taskId":"`+remoteJobID+`"}`)
	var snapshot TaskSnapshot
	if result.Status != "ok" || json.Unmarshal(result.Data, &snapshot) != nil || snapshot.State != TaskStateCanceled {
		t.Fatalf("tasks.cancel = %+v, want the remote's canceled snapshot", result)
	}
	if got := <-remote.canceled; got != remoteJobID {
		t.Fatalf("remote canceled %q, want %q", got, remoteJobID)
	}

	// A remote 404 stays a 404.
	other := "00000000-0000-4000-8000-0000000000bb"
	if result := callOnPipe(t, router, "tasks.cancel", `{"taskId":"`+other+`"}`); result.Code != 404 {
		t.Fatalf("tasks.cancel unknown job = %+v, want 404", result)
	}
	// A bridge-local id never reaches the host.
	if result := callOnPipe(t, router, "tasks.cancel", `{"taskId":"task-99"}`); result.Code != 404 || len(remote.canceled) != 0 {
		t.Fatalf("tasks.cancel task-99 = %+v, want a local 404", result)
	}
}

func TestTasksEventsSendsTheEndOfTheJobsItIsAskedAbout(t *testing.T) {
	remote := &fakeRemote{dropped: make(chan struct{}), ended: TaskSnapshot{ID: remoteJobID, State: TaskStateInterrupted}}
	router, _ := newRemoteTestRouter(t, remote)

	client, _ := dispatchOnPipe(router, "tasks.events", `{"ids":["`+remoteJobID+`","00000000-0000-4000-8000-0000000000ab"]}`)
	defer client.Close()
	_ = client.SetReadDeadline(time.Now().Add(5 * time.Second))
	frame, err := relay.ReadRelayFrame(client)
	if err != nil || frame.Opcode != relay.OpStreamProgress {
		t.Fatalf("events frame %+v, error %v; want a progress frame", frame, err)
	}
	var event TaskEvent
	if err := json.Unmarshal(frame.Payload, &event); err != nil || event.Type != TaskEventSnapshot ||
		event.Task.ID != remoteJobID || event.Task.State != TaskStateInterrupted {
		t.Fatalf("first event = %+v, %v; want the interrupted snapshot of %s", event, err, remoteJobID)
	}
}

func TestTasksEventsMergesStreamsAndSurvivesRemoteDrop(t *testing.T) {
	const remoteEvents = 50
	remote := &fakeRemote{dropped: make(chan struct{})}
	for pct := range remoteEvents {
		remote.events = append(remote.events, TaskEvent{Type: TaskEventProgress, Task: TaskSnapshot{ID: remoteJobID}, Progress: testTaskProgress(pct)})
	}
	router, service := newRemoteTestRouter(t, remote)
	local := startBlockedTask(t, service)

	client, errCh := dispatchOnPipe(router, "tasks.events", `{}`)
	defer client.Close()
	// Local tasks start while the remote writes, so both write the stream at once.
	locals := []*Task{local}
	for range 4 {
		locals = append(locals, startBlockedTask(t, service))
	}
	readEvent := func() TaskEvent {
		t.Helper()
		_ = client.SetReadDeadline(time.Now().Add(5 * time.Second))
		frame, err := relay.ReadRelayFrame(client)
		if err != nil || frame.Opcode != relay.OpStreamProgress {
			t.Fatalf("events frame %+v, error %v; want a progress frame", frame, err)
		}
		var event TaskEvent
		if err := json.Unmarshal(frame.Payload, &event); err != nil {
			t.Fatalf("torn or invalid event %q: %v", frame.Payload, err)
		}
		return event
	}

	counts := map[string]int{}
	seen := func() bool {
		return counts[remoteJobID] == remoteEvents && !slices.ContainsFunc(locals, func(task *Task) bool { return counts[task.ID()] == 0 })
	}
	for !seen() {
		counts[readEvent().Task.ID]++
	}
	<-remote.dropped

	local.ReportProgress(testTaskProgress(50))
	event := readEvent()
	for event.Task.ID != local.ID() || event.Type != TaskEventProgress {
		event = readEvent() // a local task's second start event may still be queued
	}

	if err := relay.WriteRelayFrame(client, &relay.StreamFrame{Opcode: relay.OpStreamClose}); err != nil {
		t.Fatalf("write close: %v", err)
	}
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("tasks.events: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("tasks.events did not end when the client closed")
	}
}

func TestTasksHistoryAsksTheRemote(t *testing.T) {
	at := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	remote := &fakeRemote{history: TaskHistory{
		Entries: []TaskHistoryEntry{{Task: TaskSnapshot{ID: remoteJobID, State: TaskStateCompleted}, Reason: "reboot", Unit: "a.service", Output: true}},
		More:    true,
	}}
	router, _ := newRemoteTestRouter(t, remote)

	result := callOnPipe(t, router, "tasks.history", `{"before":"2026-11-10T12:00:00Z","limit":5,"unit":"a.service"}`)
	var history TaskHistory
	if result.Status != "ok" || json.Unmarshal(result.Data, &history) != nil {
		t.Fatalf("tasks.history = %+v, want ok", result)
	}
	if len(history.Entries) != 1 || history.Entries[0] != remote.history.Entries[0] || !history.More {
		t.Fatalf("tasks.history = %+v, want the remote's %+v", history, remote.history)
	}
	if remote.asked.Before == nil || !remote.asked.Before.Equal(at) || remote.asked.Limit != 5 || remote.asked.Unit != "a.service" {
		t.Fatalf("remote was asked %+v, want before %v, limit 5, unit a.service", remote.asked, at)
	}

	// Without a job host there is no history, and no error.
	router, _ = newRemoteTestRouter(t, nil)
	result = callOnPipe(t, router, "tasks.history", `{}`)
	if result.Status != "ok" || string(result.Data) != `{"entries":[],"more":false}` {
		t.Fatalf("tasks.history without a remote = %+v, want an empty history", result)
	}
}

func TestTasksHistoryRejectsABadRequest(t *testing.T) {
	router, _ := newRemoteTestRouter(t, &fakeRemote{})
	client, errCh := dispatchOnPipe(router, "tasks.history", `{"limit":-1}`)
	defer client.Close()
	go func() { _, _ = io.Copy(io.Discard, client) }()
	if err := <-errCh; !errors.Is(err, ErrInvalidArgs) {
		t.Fatalf("tasks.history with limit -1 = %v, want ErrInvalidArgs", err)
	}
}

func TestTasksHostWithoutARemote(t *testing.T) {
	router, _ := newRemoteTestRouter(t, nil)
	result := callOnPipe(t, router, "tasks.host", `{}`)
	if result.Status != "ok" || string(result.Data) != `{"update_pending":false,"running":0}` {
		t.Fatalf("tasks.host without a remote = %+v, want no update pending and no job running", result)
	}

	// With a remote, an unprivileged session gets the remote's status as it is.
	remote := &fakeRemote{status: TaskHostStatus{UpdatePending: true, Running: 1, Jobs: []TaskSnapshot{{ID: remoteJobID, State: TaskStateRunning}}}}
	router, _ = newRemoteTestRouter(t, remote)
	result = callOnPipe(t, router, "tasks.host", `{}`)
	var status TaskHostStatus
	if result.Status != "ok" || json.Unmarshal(result.Data, &status) != nil ||
		!status.UpdatePending || status.Running != 1 || len(status.Jobs) != 1 || status.Jobs[0].ID != remoteJobID {
		t.Fatalf("tasks.host = %+v, want the remote's %+v", result, remote.status)
	}
}

func TestTasksOutputIsPrivileged(t *testing.T) {
	remote := &fakeRemote{output: TaskOutput{Content: "line\n", Offset: 3, Size: 8}}
	router, _ := newRemoteTestRouter(t, remote)
	request := `{"taskId":"` + remoteJobID + `","offset":3,"limit":5}`

	if result := callOnPipe(t, router, "tasks.output", request); result.Code != 403 || remote.outputAsked != nil {
		t.Fatalf("tasks.output of an unprivileged session = %+v (remote asked %+v), want 403 before the remote is asked", result, remote.outputAsked)
	}

	result := callOnPipeAs(t, router, privilegedTestSession, "tasks.output", request)
	var output TaskOutput
	if result.Status != "ok" || json.Unmarshal(result.Data, &output) != nil || output != remote.output {
		t.Fatalf("tasks.output of a privileged session = %+v, want the remote's %+v", result, remote.output)
	}
	if asked := remote.outputAsked; asked == nil || asked.TaskID != remoteJobID || asked.Offset == nil || *asked.Offset != 3 || asked.Limit != 5 {
		t.Fatalf("remote was asked %+v, want task %s, offset 3, limit 5", asked, remoteJobID)
	}

	// A bridge-local task has no saved output, and its ID never reaches the host.
	remote.outputAsked = nil
	if result := callOnPipeAs(t, router, privilegedTestSession, "tasks.output", `{"taskId":"task-99"}`); result.Code != 404 || remote.outputAsked != nil {
		t.Fatalf("tasks.output of task-99 = %+v (remote asked %+v), want a local 404", result, remote.outputAsked)
	}
}

func TestTasksOutputRejectsABadRequest(t *testing.T) {
	remote := &fakeRemote{}
	router, _ := newRemoteTestRouter(t, remote)
	for _, request := range []string{
		`{}`,
		`{"taskId":""}`,
		`{"taskId":"` + remoteJobID + `","limit":-1}`,
		`{"taskId":"` + remoteJobID + `","offset":-1}`,
	} {
		client, errCh := dispatchOnPipeAs(router, privilegedTestSession, "tasks.output", request)
		go func() { _, _ = io.Copy(io.Discard, client) }()
		err := <-errCh
		_ = client.Close()
		if !errors.Is(err, ErrInvalidArgs) || remote.outputAsked != nil {
			t.Fatalf("tasks.output %s = %v (remote asked %+v), want ErrInvalidArgs before the remote is asked", request, err, remote.outputAsked)
		}
	}
}
