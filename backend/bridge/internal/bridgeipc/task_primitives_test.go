package bridgeipc

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"log/slog"
	"net"
	"slices"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

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
}

func (f *fakeRemote) Get(_ context.Context, id string) (TaskSnapshot, error) {
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

var remoteTestSession = &session.Session{SessionID: "session-a", User: session.User{Username: "alice", UID: 1000}}

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

// dispatchOnPipe dispatches route over a net.Pipe and returns the client end
// and the result of Dispatch.
func dispatchOnPipe(router *Router, route string, request string) (net.Conn, <-chan error) {
	server, client := net.Pipe()
	errCh := make(chan error, 1)
	go func() {
		defer server.Close()
		errCh <- router.Dispatch(context.Background(), server, Request{Route: route, RawRequest: jsontext.Value(request), Session: remoteTestSession})
	}()
	return client, errCh
}

func callOnPipe(t *testing.T, router *Router, route string, request string) relay.ResultFrame {
	t.Helper()
	client, errCh := dispatchOnPipe(router, route, request)
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

func TestTasksEventsMergesStreamsAndSurvivesRemoteDrop(t *testing.T) {
	remote := &fakeRemote{dropped: make(chan struct{})}
	for _, pct := range []int{10, 20} {
		remote.events = append(remote.events, TaskEvent{Type: TaskEventProgress, Task: TaskSnapshot{ID: remoteJobID}, Progress: testTaskProgress(pct)})
	}
	router, service := newRemoteTestRouter(t, remote)
	local := startBlockedTask(t, service)

	client, errCh := dispatchOnPipe(router, "tasks.events", `{}`)
	defer client.Close()
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
	for range 3 {
		counts[readEvent().Task.ID]++
	}
	if counts[local.ID()] != 1 || counts[remoteJobID] != 2 {
		t.Fatalf("events by task = %v, want 1 local snapshot and 2 remote events", counts)
	}
	<-remote.dropped

	local.ReportProgress(testTaskProgress(50))
	if event := readEvent(); event.Task.ID != local.ID() || event.Type != TaskEventProgress {
		t.Fatalf("event after remote drop = %+v, want local progress", event)
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
