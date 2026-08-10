package bridge

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

func TestCanceledQueuedTaskCannotStart(t *testing.T) {
	registry := NewTaskService()
	task, err := registry.Create("test.canceled.queued", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	task.Cancel()
	ran := make(chan struct{}, 1)
	if task.Start(func(context.Context, *Task, any) (any, error) {
		ran <- struct{}{}
		return nil, nil
	}) {
		t.Fatal("Start accepted a canceled queued task")
	}
	select {
	case <-ran:
		t.Fatal("canceled queued task ran")
	default:
	}
	if !task.IsTerminal() {
		t.Fatal("canceled task is not terminal")
	}
}

func TestTaskServiceRegistersPrimitiveRoutes(t *testing.T) {
	service := NewTaskService()
	router := NewRouter(service)
	service.RegisterRoutes(router)

	want := map[string]Mode{
		"tasks.get":    ModeCall,
		"tasks.list":   ModeCall,
		"tasks.cancel": ModeCall,
		"tasks.watch":  ModeDuplex,
		"tasks.data":   ModeDuplex,
		"tasks.events": ModeDuplex,
	}
	for name, mode := range want {
		route, ok := router.lookup(name)
		if !ok {
			t.Errorf("task service route %q is not registered", name)
			continue
		}
		if route.Mode != mode {
			t.Errorf("task service route %q mode = %q, want %q", name, route.Mode, mode)
		}
	}
	if _, ok := router.lookup("tasks.unknown"); ok {
		t.Fatal("unknown task service route was registered")
	}
}

func TestTaskServiceRejectsRegistrationOnDifferentRouter(t *testing.T) {
	service := NewTaskService()
	router := NewRouter(NewTaskService())
	defer func() {
		if recover() == nil {
			t.Fatal("expected mismatched task service registration to panic")
		}
	}()
	service.RegisterRoutes(router)
}

func TestTaskCompletesAndSnapshotsResult(t *testing.T) {
	registry := NewTaskService()
	task, err := startTestTask(registry, "test.complete", nil, TaskOwner{}, func(ctx context.Context, task *Task, _ any) (any, error) {
		task.ReportProgress(map[string]any{"pct": 50})
		return map[string]any{"ok": true}, nil
	})
	if err != nil {
		t.Fatalf("startTestTask returned error: %v", err)
	}

	waitForTaskState(t, task, TaskStateCompleted)
	snapshot := task.Snapshot()
	if snapshot.State != TaskStateCompleted {
		t.Fatalf("state = %q, want %q", snapshot.State, TaskStateCompleted)
	}
	if snapshot.Result == nil {
		t.Fatal("expected result to be stored")
	}
	if snapshot.Progress == nil {
		t.Fatal("expected progress to be stored")
	}
}

func TestSnapshotNeverPublishesRequestAndClearsTerminalReference(t *testing.T) {
	registry := NewTaskService()
	secret := map[string]string{"password": "sentinel-secret"}
	metadata := &TaskMetadata{Identity: []string{"safe"}, Label: "safe label"}
	task, err := registry.CreateForOwner("test.secret", secret, TaskOwner{}, metadata)
	if err != nil {
		t.Fatalf("CreateForOwner: %v", err)
	}
	if got := string(mustJSON(t, task.Snapshot())); strings.Contains(got, "request") || strings.Contains(got, "sentinel-secret") {
		t.Fatalf("queued snapshot leaked request: %s", got)
	}
	if !task.Start(func(_ context.Context, _ *Task, request any) (any, error) {
		got, ok := request.(map[string]string)
		if !ok || got["password"] != secret["password"] {
			t.Fatal("runner did not receive private request")
		}
		return nil, nil
	}) {
		t.Fatal("Start returned false")
	}
	<-task.Done()
	if got := string(mustJSON(t, task.Snapshot())); strings.Contains(got, "request") || strings.Contains(got, "sentinel-secret") {
		t.Fatalf("terminal snapshot leaked request: %s", got)
	}
	task.mu.RLock()
	defer task.mu.RUnlock()
	if task.request != nil {
		t.Fatal("terminal task retained decoded request")
	}
}

// Exercise tasks.get, tasks.list, and tasks.cancel relay payloads with a sentinel
// credential. Start/events/attach have dedicated transport tests below.
func TestPublicTaskSnapshotsNeverExposeDecodedRequest(t *testing.T) {
	registry := NewTaskService()
	router := NewRouter(registry)
	owner := TaskOwner{Username: "alice", UID: 1000}
	registry.RegisterRoutes(router)
	sess := &session.Session{User: session.User{Username: owner.Username, UID: owner.UID}}
	secret := map[string]string{"password": "sentinel-secret"}
	task, err := registry.CreateForOwner("test.public.secret", secret, owner, &TaskMetadata{Label: "safe"})
	if err != nil {
		t.Fatalf("CreateForOwner: %v", err)
	}
	assertNoRequestLeak(t, relayResultData(t, func(stream net.Conn) error {
		return router.Dispatch(context.Background(), stream, Request{Route: "tasks.get", Session: sess, RawRequest: json.RawMessage(`{"taskId":"` + task.ID() + `"}`)})
	}))
	assertNoRequestLeak(t, relayResultData(t, func(stream net.Conn) error {
		return router.Dispatch(context.Background(), stream, Request{Route: "tasks.list", Session: sess, RawRequest: json.RawMessage(`{}`)})
	}))
	assertNoRequestLeak(t, relayResultData(t, func(stream net.Conn) error {
		return router.Dispatch(context.Background(), stream, Request{Route: "tasks.cancel", Session: sess, RawRequest: json.RawMessage(`{"taskId":"` + task.ID() + `"}`)})
	}))
	task.mu.RLock()
	if task.request != nil {
		t.Fatal("queued cancellation retained decoded request")
	}
	task.mu.RUnlock()
}

func TestTaskStartAndEventsSnapshotsNeverExposeDecodedRequest(t *testing.T) {
	registry := NewTaskService()
	router := NewRouter(registry)
	owner := TaskOwner{Username: "alice", UID: 1000}
	secret := map[string]string{"password": "sentinel-secret"}
	started := make(chan struct{})
	release := make(chan struct{})
	router.TaskRunner("test.start.secret", func(_ context.Context, _ *Task, _ any) (any, error) {
		close(started)
		<-release
		return nil, nil
	}, TaskStreamDefault, WithTaskMetadata(func(any) TaskMetadata { return TaskMetadata{Label: "safe"} }))

	server, client := net.Pipe()
	errCh := make(chan error, 1)
	go func() {
		defer server.Close()
		errCh <- router.dispatchTask(context.Background(), server, router.routes["test.start.secret"], Request{Route: "test.start.secret", Owner: owner, DecodedValue: secret})
	}()
	frame, readErr := relay.ReadRelayFrame(client)
	if readErr != nil {
		t.Fatalf("start ReadRelayFrame: %v", readErr)
	}
	var start relay.ResultFrame
	if decodeErr := json.Unmarshal(frame.Payload, &start); decodeErr != nil {
		t.Fatalf("decode start: %v", decodeErr)
	}
	assertNoRequestLeak(t, start.Data)
	if _, closeErr := relay.ReadRelayFrame(client); closeErr != nil {
		t.Fatalf("start close: %v", closeErr)
	}
	_ = client.Close()
	if dispatchErr := <-errCh; dispatchErr != nil {
		t.Fatalf("dispatch task: %v", dispatchErr)
	}
	<-started

	task, ok := registry.Get(registry.ListForOwner(owner)[0].ID)
	if !ok {
		t.Fatal("started task missing from registry")
	}
	eventServer, eventClient := net.Pipe()
	eventErr := make(chan error, 1)
	go func() {
		defer eventServer.Close()
		eventErr <- registry.handleTaskEvents(context.Background(), eventServer, Request{Owner: owner})
	}()
	initial, err := relay.ReadRelayFrame(eventClient)
	if err != nil {
		t.Fatalf("initial event: %v", err)
	}
	assertNoRequestLeak(t, initial.Payload)
	task.ReportProgress(map[string]any{"pct": 1})
	live, err := relay.ReadRelayFrame(eventClient)
	if err != nil {
		t.Fatalf("live event: %v", err)
	}
	assertNoRequestLeak(t, live.Payload)
	_ = eventClient.Close()
	if err := <-eventErr; err != nil {
		t.Fatalf("events: %v", err)
	}
	close(release)
	<-task.Done()
}

func TestTaskEventsCloseInterruptsBlockedSnapshot(t *testing.T) {
	registry := NewTaskService()
	owner := TaskOwner{SessionID: "session-a", Username: "alice", UID: 1000}
	release := make(chan struct{})
	task, err := startTestTask(registry, "test.events.close", nil, owner, func(context.Context, *Task, any) (any, error) {
		<-release
		return map[string]any{"ok": true}, nil
	})
	if err != nil {
		t.Fatalf("startTestTask returned error: %v", err)
	}

	server, client := net.Pipe()
	defer client.Close()
	errCh := make(chan error, 1)
	go func() {
		defer server.Close()
		errCh <- registry.handleTaskEvents(context.Background(), server, Request{Owner: owner})
	}()

	if err := relay.WriteRelayFrame(client, &relay.StreamFrame{Opcode: relay.OpStreamClose}); err != nil {
		t.Fatalf("WriteRelayFrame(close): %v", err)
	}
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("task events returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("task events close did not interrupt the blocked snapshot")
	}

	close(release)
	<-task.Done()
}

func TestAttachReplayAndTerminalNeverExposeDecodedRequest(t *testing.T) {
	registry := NewTaskService()
	secret := map[string]string{"password": "sentinel-secret"}
	task, err := registry.Create("test.attach.secret", secret)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !task.Start(func(_ context.Context, task *Task, _ any) (any, error) {
		task.ReportProgress(map[string]any{"phase": "safe"})
		return map[string]any{"ok": true}, nil
	}) {
		t.Fatal("Start returned false")
	}
	<-task.Done()
	server, client := net.Pipe()
	errCh := make(chan error, 1)
	go func() { defer server.Close(); errCh <- WatchTaskStream(server, task) }()
	for i := range 2 {
		frame, err := relay.ReadRelayFrame(client)
		if err != nil {
			t.Fatalf("attach frame %d: %v", i, err)
		}
		assertNoRequestLeak(t, frame.Payload)
	}
	_ = client.Close()
	if err := <-errCh; err != nil {
		t.Fatalf("attach: %v", err)
	}
}

func relayResultData(t *testing.T, write func(net.Conn) error) []byte {
	t.Helper()
	server, client := net.Pipe()
	defer client.Close()
	errCh := make(chan error, 1)
	go func() { defer server.Close(); errCh <- write(server) }()
	frame, err := relay.ReadRelayFrame(client)
	if err != nil {
		t.Fatalf("ReadRelayFrame: %v", err)
	}
	var result relay.ResultFrame
	if err := json.Unmarshal(frame.Payload, &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result.Status != "ok" {
		t.Fatalf("result status = %q", result.Status)
	}
	if _, err := relay.ReadRelayFrame(client); err != nil {
		t.Fatalf("ReadRelayFrame(close): %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("writer: %v", err)
	}
	return result.Data
}

func assertNoRequestLeak(t *testing.T, data []byte) {
	t.Helper()
	value := string(data)
	if strings.Contains(value, `"request"`) || strings.Contains(value, "sentinel-secret") {
		t.Fatalf("public payload leaked decoded request: %s", value)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return encoded
}

func TestTaskDoneClosesAfterTerminalSnapshotCommitted(t *testing.T) {
	registry := NewTaskService()
	task, err := startTestTask(registry, "test.done.atomic", nil, TaskOwner{}, func(ctx context.Context, task *Task, _ any) (any, error) {
		return map[string]any{"ok": true}, nil
	})
	if err != nil {
		t.Fatalf("startTestTask returned error: %v", err)
	}

	<-task.Done()
	snapshot := task.Snapshot()
	if snapshot.State != TaskStateCompleted {
		t.Fatalf("state after done = %q, want completed", snapshot.State)
	}
	if snapshot.Result == nil {
		t.Fatal("done closed before result was visible in snapshot")
	}
	if snapshot.FinishedAt == nil {
		t.Fatal("done closed before finished_at was visible in snapshot")
	}
}

func TestTaskCancelMarksCanceled(t *testing.T) {
	registry := NewTaskService()
	started := make(chan struct{})
	task, err := startTestTask(registry, "test.cancel", nil, TaskOwner{}, func(ctx context.Context, task *Task, _ any) (any, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	if err != nil {
		t.Fatalf("startTestTask returned error: %v", err)
	}
	<-started
	task.Cancel()

	waitForTaskState(t, task, TaskStateCanceled)
	if snapshot := task.Snapshot(); snapshot.Error == nil || snapshot.Error.Code != 499 {
		t.Fatalf("cancel error = %#v, want code 499", snapshot.Error)
	}
}

func TestCancelQueuedTaskEmitsCanceled(t *testing.T) {
	registry := NewTaskService()
	owner := TaskOwner{SessionID: "session-a", Username: "alice", UID: 1000}
	events, unsubscribe := registry.Subscribe(8)
	defer unsubscribe()
	task, err := registry.CreateForOwner("test.cancel.queued", nil, owner)
	if err != nil {
		t.Fatalf("CreateForOwner returned error: %v", err)
	}

	task.Cancel()

	waitForTaskState(t, task, TaskStateCanceled)
	event := waitForTaskEvent(t, events, task.ID(), TaskEventCanceled)
	if event.Task.State != TaskStateCanceled {
		t.Fatalf("event state = %q, want canceled", event.Task.State)
	}
}

func TestCancelTasksForSessionActiveTask(t *testing.T) {
	registry := NewTaskService()
	owner := TaskOwner{SessionID: "session-a", Username: "alice", UID: 1000}
	events, unsubscribe := registry.Subscribe(8)
	defer unsubscribe()
	started := make(chan struct{})
	task, err := startTestTask(registry, "test.cancel.session.active", nil, owner, func(ctx context.Context, task *Task, _ any) (any, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	if err != nil {
		t.Fatalf("startTestTask returned error: %v", err)
	}
	<-started

	registry.CancelTasksForSession(owner.SessionID)

	waitForTaskState(t, task, TaskStateCanceled)
	waitForTaskEvent(t, events, task.ID(), TaskEventCanceled)
}

func TestCancelTasksForSessionQueuedTask(t *testing.T) {
	registry := NewTaskService()
	owner := TaskOwner{SessionID: "session-a", Username: "alice", UID: 1000}
	events, unsubscribe := registry.Subscribe(8)
	defer unsubscribe()
	task, err := registry.CreateForOwner("test.cancel.session.queued", nil, owner)
	if err != nil {
		t.Fatalf("CreateForOwner returned error: %v", err)
	}

	registry.CancelTasksForSession(owner.SessionID)

	waitForTaskState(t, task, TaskStateCanceled)
	waitForTaskEvent(t, events, task.ID(), TaskEventCanceled)
}

func TestCancelTasksForSessionCompletedTaskIgnored(t *testing.T) {
	registry := NewTaskService()
	owner := TaskOwner{SessionID: "session-a", Username: "alice", UID: 1000}
	task, err := startTestTask(registry, "test.cancel.session.completed", nil, owner, func(ctx context.Context, task *Task, _ any) (any, error) {
		return map[string]any{"ok": true}, nil
	})
	if err != nil {
		t.Fatalf("startTestTask returned error: %v", err)
	}
	waitForTaskState(t, task, TaskStateCompleted)
	events, unsubscribe := registry.Subscribe(8)
	defer unsubscribe()

	registry.CancelTasksForSession(owner.SessionID)

	if snapshot := task.Snapshot(); snapshot.State != TaskStateCompleted {
		t.Fatalf("state after CancelTasksForSession = %q, want completed", snapshot.State)
	}
	assertNoTaskEvent(t, events, task.ID(), TaskEventCanceled)
}

func TestOwnerScopedAccessors(t *testing.T) {
	registry := NewTaskService()
	ownerA := TaskOwner{SessionID: "session-a", Username: "alice", UID: 1000}
	ownerB := TaskOwner{SessionID: "session-b", Username: "bob", UID: 1001}
	block := make(chan struct{})
	task, err := startTestTask(registry, "test.owner", nil, ownerA, func(ctx context.Context, task *Task, _ any) (any, error) {
		<-block
		return map[string]any{"ok": true}, nil
	})
	if err != nil {
		t.Fatalf("startTestTask returned error: %v", err)
	}
	defer close(block)

	if _, ok := registry.GetForOwner(task.ID(), ownerA); !ok {
		t.Fatal("owner should be able to access own task")
	}
	if _, ok := registry.GetForOwner(task.ID(), ownerB); ok {
		t.Fatal("different owner should not be able to access task")
	}
	if got := registry.ListForOwner(ownerA); len(got) != 1 || got[0].ID != task.ID() {
		t.Fatalf("ListForOwner(ownerA) = %#v, want own task", got)
	}
	if got := registry.ListForOwner(ownerB); len(got) != 0 {
		t.Fatalf("ListForOwner(ownerB) = %#v, want empty", got)
	}
}

func TestTaskServiceSubscribeReceivesLiveEvents(t *testing.T) {
	registry := NewTaskService()
	owner := TaskOwner{SessionID: "session-a", Username: "alice", UID: 1000}
	events, unsubscribe := registry.Subscribe(8)
	defer unsubscribe()
	task, err := startTestTask(registry, "test.events", nil, owner, func(ctx context.Context, task *Task, _ any) (any, error) {
		task.ReportProgress(map[string]any{"pct": 50})
		return map[string]any{"ok": true}, nil
	})
	if err != nil {
		t.Fatalf("startTestTask returned error: %v", err)
	}

	seen := map[TaskEventType]bool{}
	deadline := time.After(time.Second)
	for !seen[TaskEventStarted] || !seen[TaskEventProgress] || !seen[TaskEventResult] {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for events; saw %#v", seen)
		case event := <-events:
			if event.Task.ID != task.ID() {
				continue
			}
			if !event.Task.Owner.Matches(owner) {
				t.Fatalf("event owner = %#v, want %#v", event.Task.Owner, owner)
			}
			seen[event.Type] = true
		}
	}
}

func TestSlowTaskServiceSubscriberStillReceivesTerminalEvent(t *testing.T) {
	registry := NewTaskService()
	owner := TaskOwner{SessionID: "session-a", Username: "alice", UID: 1000}
	events, unsubscribe := registry.Subscribe(1)
	defer unsubscribe()

	task, err := startTestTask(registry, "test.slow.registry", nil, owner, func(ctx context.Context, task *Task, _ any) (any, error) {
		for i := range 20 {
			task.ReportProgress(map[string]any{"pct": i})
		}
		return map[string]any{"ok": true}, nil
	})
	if err != nil {
		t.Fatalf("startTestTask returned error: %v", err)
	}

	waitForTaskState(t, task, TaskStateCompleted)
	event := waitForTaskEvent(t, events, task.ID(), TaskEventResult)
	if event.Task.State != TaskStateCompleted {
		t.Fatalf("event state = %q, want completed", event.Task.State)
	}
}

func TestSlowTaskSubscriberStillReceivesTerminalEvent(t *testing.T) {
	registry := NewTaskService()
	block := make(chan struct{})
	task, err := startTestTask(registry, "test.slow.task", nil, TaskOwner{}, func(ctx context.Context, task *Task, _ any) (any, error) {
		<-block
		for i := range 20 {
			task.ReportProgress(map[string]any{"pct": i})
		}
		return map[string]any{"ok": true}, nil
	})
	if err != nil {
		t.Fatalf("startTestTask returned error: %v", err)
	}

	events, unsubscribe := task.Subscribe(1)
	defer unsubscribe()
	close(block)

	waitForTaskState(t, task, TaskStateCompleted)
	event := waitForTaskEvent(t, events, task.ID(), TaskEventResult)
	if event.Task.State != TaskStateCompleted {
		t.Fatalf("event state = %q, want completed", event.Task.State)
	}
}

func TestReportDataDoesNotReachTaskServiceEvents(t *testing.T) {
	registry := NewTaskService()
	owner := TaskOwner{SessionID: "session-a", Username: "alice", UID: 1000}
	registryEvents, registryUnsubscribe := registry.Subscribe(8)
	defer registryUnsubscribe()

	task, err := registry.CreateForOwner("logs.general.follow", nil, owner)
	if err != nil {
		t.Fatalf("CreateForOwner returned error: %v", err)
	}
	taskEvents, taskUnsubscribe := task.Subscribe(8)
	defer taskUnsubscribe()

	task.ReportData("line\n")

	event := waitForTaskEvent(t, taskEvents, task.ID(), TaskEventProgress)
	progress, ok := event.Progress.(map[string]any)
	if !ok {
		t.Fatalf("progress = %#v, want map", event.Progress)
	}
	if progress["type"] != "data" || progress["data"] != "line\n" {
		t.Fatalf("progress = %#v, want data line", progress)
	}
	assertNoTaskEvent(t, registryEvents, task.ID(), TaskEventProgress)
	if snapshot := task.Snapshot(); snapshot.Progress != nil {
		t.Fatalf("snapshot progress = %#v, want nil for transient data", snapshot.Progress)
	}
}

func TestDataProgressMapDoesNotReachTaskServiceEvents(t *testing.T) {
	registry := NewTaskService()
	owner := TaskOwner{SessionID: "session-a", Username: "alice", UID: 1000}
	registryEvents, registryUnsubscribe := registry.Subscribe(8)
	defer registryUnsubscribe()

	task, err := registry.CreateForOwner("logs.service.follow", nil, owner)
	if err != nil {
		t.Fatalf("CreateForOwner returned error: %v", err)
	}
	taskEvents, taskUnsubscribe := task.Subscribe(8)
	defer taskUnsubscribe()

	task.ReportProgress(map[string]any{"type": "data", "data": "legacy\n"})

	event := waitForTaskEvent(t, taskEvents, task.ID(), TaskEventProgress)
	progress, ok := event.Progress.(map[string]any)
	if !ok {
		t.Fatalf("progress = %#v, want map", event.Progress)
	}
	if progress["data"] != "legacy\n" {
		t.Fatalf("progress data = %#v, want legacy line", progress["data"])
	}
	assertNoTaskEvent(t, registryEvents, task.ID(), TaskEventProgress)
	if snapshot := task.Snapshot(); snapshot.Progress != nil {
		t.Fatalf("snapshot progress = %#v, want nil for transient data", snapshot.Progress)
	}
}

func TestTransientProgressDoesNotReachTaskServiceEvents(t *testing.T) {
	registry := NewTaskService()
	owner := TaskOwner{SessionID: "session-a", Username: "alice", UID: 1000}
	registryEvents, registryUnsubscribe := registry.Subscribe(8)
	defer registryUnsubscribe()

	task, err := registry.CreateForOwner("docker.compose", nil, owner)
	if err != nil {
		t.Fatalf("CreateForOwner returned error: %v", err)
	}
	taskEvents, taskUnsubscribe := task.Subscribe(8)
	defer taskUnsubscribe()

	task.ReportTransientProgress(map[string]any{"type": "stdout", "message": "creating container"})

	event := waitForTaskEvent(t, taskEvents, task.ID(), TaskEventProgress)
	progress, ok := event.Progress.(map[string]any)
	if !ok {
		t.Fatalf("progress = %#v, want map", event.Progress)
	}
	if progress["message"] != "creating container" {
		t.Fatalf("progress message = %#v, want compose output", progress["message"])
	}
	assertNoTaskEvent(t, registryEvents, task.ID(), TaskEventProgress)
	if snapshot := task.Snapshot(); snapshot.Progress != nil {
		t.Fatalf("snapshot progress = %#v, want nil for transient progress", snapshot.Progress)
	}
}

func TestProgressReplayShrinksByBytesAfterFirstSubscriber(t *testing.T) {
	registry := NewTaskService()
	task, err := registry.Create("logs.general.follow", nil)
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	chunk := strings.Repeat(
		"x",
		DefaultSubscribedTaskProgressReplayBytes/2+1,
	)
	task.ReportData(chunk)
	task.ReportData(chunk)
	task.ReportData(chunk)

	_, replay, unsubscribe := task.SubscribeWithReplay(8)
	defer unsubscribe()
	if len(replay) != 3 {
		t.Fatalf("initial replay length = %d, want all 3 pre-attach events", len(replay))
	}

	task.mu.RLock()
	retainedBytes := task.progressLogBytes
	retainedEvents := len(task.progressLog)
	task.mu.RUnlock()
	if retainedBytes > DefaultSubscribedTaskProgressReplayBytes {
		t.Fatalf(
			"post-subscribe replay bytes = %d, limit %d",
			retainedBytes,
			DefaultSubscribedTaskProgressReplayBytes,
		)
	}
	if retainedEvents >= len(replay) {
		t.Fatalf(
			"post-subscribe replay retained %d events, want fewer than initial %d",
			retainedEvents,
			len(replay),
		)
	}
}

func TestWatchTaskStreamReplaysProgressBeforeTerminalResult(t *testing.T) {
	registry := NewTaskService()
	task, err := startTestTask(registry, "test.attach.replay", nil, TaskOwner{}, func(ctx context.Context, task *Task, _ any) (any, error) {
		task.ReportData("first\n")
		task.ReportData("second\n")
		return map[string]any{"ok": true}, nil
	})
	if err != nil {
		t.Fatalf("startTestTask returned error: %v", err)
	}
	waitForTaskState(t, task, TaskStateCompleted)

	server, client := net.Pipe()
	defer client.Close()

	errCh := make(chan error, 1)
	go func() {
		defer server.Close()
		errCh <- WatchTaskStream(server, task)
	}()

	if got := readProgressData(t, client); got != "first\n" {
		t.Fatalf("first replay progress = %q, want first line", got)
	}
	if got := readProgressData(t, client); got != "second\n" {
		t.Fatalf("second replay progress = %q, want second line", got)
	}

	frame, err := relay.ReadRelayFrame(client)
	if err != nil {
		t.Fatalf("ReadRelayFrame(result): %v", err)
	}
	if frame.Opcode != relay.OpStreamResult {
		t.Fatalf("opcode = 0x%02x, want OpStreamResult", frame.Opcode)
	}
	var result relay.ResultFrame
	err = json.Unmarshal(frame.Payload, &result)
	if err != nil {
		t.Fatalf("json.Unmarshal(result): %v", err)
	}
	if result.Status != "ok" {
		t.Fatalf("status = %q, want ok", result.Status)
	}
	frame, err = relay.ReadRelayFrame(client)
	if err != nil {
		t.Fatalf("ReadRelayFrame(close): %v", err)
	}
	if frame.Opcode != relay.OpStreamClose {
		t.Fatalf("opcode = 0x%02x, want OpStreamClose", frame.Opcode)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("WatchTaskStream returned error: %v", err)
	}
}

func TestWatchTaskStreamAbortInterruptsBlockedReplay(t *testing.T) {
	registry := NewTaskService()
	task, err := registry.Create("test.attach.abort", nil)
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	task.ReportData("blocked replay\n")

	server, client := net.Pipe()
	defer client.Close()

	errCh := make(chan error, 1)
	go func() {
		defer server.Close()
		errCh <- WatchTaskStream(server, task)
	}()

	deadline := time.Now().Add(time.Second)
	for {
		task.mu.RLock()
		subscribers := len(task.subscribers)
		task.mu.RUnlock()
		if subscribers == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for watch subscriber")
		}
		time.Sleep(time.Millisecond)
	}

	if err := relay.WriteRelayFrame(client, &relay.StreamFrame{Opcode: relay.OpStreamAbort}); err != nil {
		t.Fatalf("WriteRelayFrame(abort): %v", err)
	}
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("WatchTaskStream returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("task watch abort did not interrupt the blocked replay")
	}
	if snapshot := task.Snapshot(); snapshot.State != TaskStateCanceled {
		t.Fatalf("task state = %q, want canceled", snapshot.State)
	}
}

func TestWatchTaskStreamReportsLagInsteadOfSilentlyDroppingData(t *testing.T) {
	registry := NewTaskService()
	task, err := registry.Create("test.attach.lag", nil)
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	task.ReportData("replay\n")

	server, client := net.Pipe()
	defer client.Close()

	errCh := make(chan error, 1)
	go func() {
		defer server.Close()
		errCh <- WatchTaskStream(server, task)
	}()

	deadline := time.Now().Add(time.Second)
	for {
		task.mu.RLock()
		subscribers := len(task.subscribers)
		task.mu.RUnlock()
		if subscribers == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for attach subscriber")
		}
		time.Sleep(time.Millisecond)
	}

	// WatchTaskStream is blocked writing the replay to net.Pipe while the
	// client is not reading. Overflow its bounded live channel during that
	// window and then let the writer continue.
	for index := 0; index <= DefaultTaskProgressReplayLimit; index++ {
		task.ReportData("live\n")
	}

	if got := readProgressData(t, client); got != "replay\n" {
		t.Fatalf("replay progress = %q, want replay line", got)
	}

	frame, err := relay.ReadRelayFrame(client)
	if err != nil {
		t.Fatalf("ReadRelayFrame(lag result): %v", err)
	}
	if frame.Opcode != relay.OpStreamResult {
		t.Fatalf("opcode = 0x%02x, want OpStreamResult", frame.Opcode)
	}
	var result relay.ResultFrame
	if err = json.Unmarshal(frame.Payload, &result); err != nil {
		t.Fatalf("json.Unmarshal(lag result): %v", err)
	}
	if result.Status != "error" || result.Code != 503 {
		t.Fatalf("lag result = %#v, want status error and code 503", result)
	}

	frame, err = relay.ReadRelayFrame(client)
	if err != nil {
		t.Fatalf("ReadRelayFrame(close): %v", err)
	}
	if frame.Opcode != relay.OpStreamClose {
		t.Fatalf("opcode = 0x%02x, want OpStreamClose", frame.Opcode)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("WatchTaskStream returned error: %v", err)
	}
}

func TestSweepTerminalOlderThanRemovesOnlyOldTerminalTasks(t *testing.T) {
	registry := NewTaskService()
	activeBlock := make(chan struct{})
	doneTask, err := startTestTask(registry, "test.sweep.done", nil, TaskOwner{}, func(ctx context.Context, task *Task, _ any) (any, error) {
		return map[string]any{"ok": true}, nil
	})
	if err != nil {
		t.Fatalf("start done returned error: %v", err)
	}
	activeTask, err := startTestTask(registry, "test.sweep.active", nil, TaskOwner{}, func(ctx context.Context, task *Task, _ any) (any, error) {
		<-activeBlock
		return map[string]any{"ok": true}, nil
	})
	if err != nil {
		t.Fatalf("start active returned error: %v", err)
	}
	defer close(activeBlock)

	waitForTaskState(t, doneTask, TaskStateCompleted)
	oldFinishedAt := time.Now().UTC().Add(-time.Hour)
	doneTask.mu.Lock()
	doneTask.finishedAt = &oldFinishedAt
	doneTask.mu.Unlock()

	removed := registry.SweepTerminalOlderThan(time.Now().UTC().Add(-30 * time.Minute))
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if _, ok := registry.Get(doneTask.ID()); ok {
		t.Fatal("old terminal task should be removed")
	}
	if _, ok := registry.Get(activeTask.ID()); !ok {
		t.Fatal("active task should not be removed")
	}
}

func readProgressData(t *testing.T, conn net.Conn) string {
	t.Helper()
	frame, err := relay.ReadRelayFrame(conn)
	if err != nil {
		t.Fatalf("ReadRelayFrame(progress): %v", err)
	}
	if frame.Opcode != relay.OpStreamProgress {
		t.Fatalf("opcode = 0x%02x, want OpStreamProgress", frame.Opcode)
	}
	var progress struct {
		Type string `json:"type"`
		Data string `json:"data"`
	}
	if err := json.Unmarshal(frame.Payload, &progress); err != nil {
		t.Fatalf("json.Unmarshal(progress): %v", err)
	}
	if progress.Type != "data" {
		t.Fatalf("progress type = %q, want data", progress.Type)
	}
	return progress.Data
}

func startTestTask(registry *TaskService, taskType string, request any, owner TaskOwner, runner TaskRunner) (*Task, error) {
	task, err := registry.CreateForOwner(taskType, request, owner)
	if err != nil {
		return nil, err
	}
	task.Start(runner)
	return task, nil
}

func waitForTaskState(t *testing.T, task *Task, want TaskState) {
	t.Helper()
	deadline := time.After(time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for state %q; got %q", want, task.Snapshot().State)
		case <-ticker.C:
			if task.Snapshot().State == want {
				return
			}
		}
	}
}

func waitForTaskEvent(t *testing.T, events <-chan TaskEvent, taskID string, want TaskEventType) TaskEvent {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for event %q on task %s", want, taskID)
		case event := <-events:
			if event.Task.ID == taskID && event.Type == want {
				return event
			}
		}
	}
}

func assertNoTaskEvent(t *testing.T, events <-chan TaskEvent, taskID string, eventType TaskEventType) {
	t.Helper()
	timer := time.NewTimer(25 * time.Millisecond)
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
			return
		case event := <-events:
			if event.Task.ID == taskID && event.Type == eventType {
				t.Fatalf("unexpected event %q for task %s", eventType, taskID)
			}
		}
	}
}
