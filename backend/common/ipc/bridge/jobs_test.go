package bridge

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
)

func TestCanceledQueuedJobCannotStart(t *testing.T) {
	registry := NewRegistry()
	job, err := registry.Create("test.canceled.queued", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	job.Cancel()
	ran := make(chan struct{}, 1)
	if job.Start(func(context.Context, *Job, any) (any, error) {
		ran <- struct{}{}
		return nil, nil
	}) {
		t.Fatal("Start accepted a canceled queued job")
	}
	select {
	case <-ran:
		t.Fatal("canceled queued job ran")
	default:
	}
	if !job.IsTerminal() {
		t.Fatal("canceled job is not terminal")
	}
}

func TestJobCompletesAndSnapshotsResult(t *testing.T) {
	registry := NewRegistry()
	job, err := startTestJob(registry, "test.complete", nil, Owner{}, func(ctx context.Context, job *Job, _ any) (any, error) {
		job.ReportProgress(map[string]any{"pct": 50})
		return map[string]any{"ok": true}, nil
	})
	if err != nil {
		t.Fatalf("startTestJob returned error: %v", err)
	}

	waitForState(t, job, StateCompleted)
	snapshot := job.Snapshot()
	if snapshot.State != StateCompleted {
		t.Fatalf("state = %q, want %q", snapshot.State, StateCompleted)
	}
	if snapshot.Result == nil {
		t.Fatal("expected result to be stored")
	}
	if snapshot.Progress == nil {
		t.Fatal("expected progress to be stored")
	}
}

func TestSnapshotNeverPublishesRequestAndClearsTerminalReference(t *testing.T) {
	registry := NewRegistry()
	secret := map[string]string{"password": "sentinel-secret"}
	metadata := &JobMetadata{Identity: []string{"safe"}, Label: "safe label"}
	job, err := registry.CreateForOwner("test.secret", secret, Owner{}, metadata)
	if err != nil {
		t.Fatalf("CreateForOwner: %v", err)
	}
	if got := string(mustJSON(t, job.Snapshot())); strings.Contains(got, "request") || strings.Contains(got, "sentinel-secret") {
		t.Fatalf("queued snapshot leaked request: %s", got)
	}
	if !job.Start(func(_ context.Context, _ *Job, request any) (any, error) {
		got, ok := request.(map[string]string)
		if !ok || got["password"] != secret["password"] {
			t.Fatal("runner did not receive private request")
		}
		return nil, nil
	}) {
		t.Fatal("Start returned false")
	}
	<-job.Done()
	if got := string(mustJSON(t, job.Snapshot())); strings.Contains(got, "request") || strings.Contains(got, "sentinel-secret") {
		t.Fatalf("terminal snapshot leaked request: %s", got)
	}
	job.mu.RLock()
	defer job.mu.RUnlock()
	if job.request != nil {
		t.Fatal("terminal job retained decoded request")
	}
}

// Exercise jobs.get, jobs.list, and jobs.cancel relay payloads with a sentinel
// credential. Start/events/attach have dedicated transport tests below.
func TestPublicJobSnapshotsNeverExposeDecodedRequest(t *testing.T) {
	registry := NewRegistry()
	router := NewRouter(registry)
	owner := Owner{Username: "alice", UID: 1000}
	secret := map[string]string{"password": "sentinel-secret"}
	job, err := registry.CreateForOwner("test.public.secret", secret, owner, &JobMetadata{Label: "safe"})
	if err != nil {
		t.Fatalf("CreateForOwner: %v", err)
	}
	assertNoRequestLeak(t, relayResultData(t, func(stream net.Conn) error {
		return router.handleJobGet(context.Background(), stream, Request{Owner: owner, RawRequest: json.RawMessage(`{"jobId":"` + job.ID() + `"}`)})
	}))
	assertNoRequestLeak(t, relayResultData(t, func(stream net.Conn) error {
		return router.handleJobList(context.Background(), stream, Request{Owner: owner, RawRequest: json.RawMessage(`{}`)})
	}))
	assertNoRequestLeak(t, relayResultData(t, func(stream net.Conn) error {
		return router.handleJobCancel(context.Background(), stream, Request{Owner: owner, RawRequest: json.RawMessage(`{"jobId":"` + job.ID() + `"}`)})
	}))
	job.mu.RLock()
	if job.request != nil {
		t.Fatal("queued cancellation retained decoded request")
	}
	job.mu.RUnlock()
}

func TestJobStartAndEventsSnapshotsNeverExposeDecodedRequest(t *testing.T) {
	registry := NewRegistry()
	router := NewRouter(registry)
	owner := Owner{Username: "alice", UID: 1000}
	secret := map[string]string{"password": "sentinel-secret"}
	started := make(chan struct{})
	release := make(chan struct{})
	router.JobRunner("test.start.secret", func(_ context.Context, _ *Job, _ any) (any, error) {
		close(started)
		<-release
		return nil, nil
	}, StreamDefault, WithJobMetadata(func(any) JobMetadata { return JobMetadata{Label: "safe"} }))

	server, client := net.Pipe()
	errCh := make(chan error, 1)
	go func() {
		defer server.Close()
		errCh <- router.dispatchJob(context.Background(), server, router.routes["test.start.secret"], Request{Route: "test.start.secret", Owner: owner, DecodedValue: secret})
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
		t.Fatalf("dispatch job: %v", dispatchErr)
	}
	<-started

	job, ok := registry.Get(registry.ListForOwner(owner)[0].ID)
	if !ok {
		t.Fatal("started job missing from registry")
	}
	eventServer, eventClient := net.Pipe()
	eventErr := make(chan error, 1)
	go func() {
		defer eventServer.Close()
		eventErr <- router.handleJobEvents(eventServer, Request{Owner: owner})
	}()
	initial, err := relay.ReadRelayFrame(eventClient)
	if err != nil {
		t.Fatalf("initial event: %v", err)
	}
	assertNoRequestLeak(t, initial.Payload)
	job.ReportProgress(map[string]any{"pct": 1})
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
	<-job.Done()
}

func TestAttachReplayAndTerminalNeverExposeDecodedRequest(t *testing.T) {
	registry := NewRegistry()
	secret := map[string]string{"password": "sentinel-secret"}
	job, err := registry.Create("test.attach.secret", secret)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !job.Start(func(_ context.Context, job *Job, _ any) (any, error) {
		job.ReportProgress(map[string]any{"phase": "safe"})
		return map[string]any{"ok": true}, nil
	}) {
		t.Fatal("Start returned false")
	}
	<-job.Done()
	server, client := net.Pipe()
	errCh := make(chan error, 1)
	go func() { defer server.Close(); errCh <- AttachJobStream(server, job) }()
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

func TestJobDoneClosesAfterTerminalSnapshotCommitted(t *testing.T) {
	registry := NewRegistry()
	job, err := startTestJob(registry, "test.done.atomic", nil, Owner{}, func(ctx context.Context, job *Job, _ any) (any, error) {
		return map[string]any{"ok": true}, nil
	})
	if err != nil {
		t.Fatalf("startTestJob returned error: %v", err)
	}

	<-job.Done()
	snapshot := job.Snapshot()
	if snapshot.State != StateCompleted {
		t.Fatalf("state after done = %q, want completed", snapshot.State)
	}
	if snapshot.Result == nil {
		t.Fatal("done closed before result was visible in snapshot")
	}
	if snapshot.FinishedAt == nil {
		t.Fatal("done closed before finished_at was visible in snapshot")
	}
}

func TestJobCancelMarksCanceled(t *testing.T) {
	registry := NewRegistry()
	started := make(chan struct{})
	job, err := startTestJob(registry, "test.cancel", nil, Owner{}, func(ctx context.Context, job *Job, _ any) (any, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	if err != nil {
		t.Fatalf("startTestJob returned error: %v", err)
	}
	<-started
	job.Cancel()

	waitForState(t, job, StateCanceled)
	if snapshot := job.Snapshot(); snapshot.Error == nil || snapshot.Error.Code != 499 {
		t.Fatalf("cancel error = %#v, want code 499", snapshot.Error)
	}
}

func TestCancelQueuedJobEmitsCanceled(t *testing.T) {
	registry := NewRegistry()
	owner := Owner{SessionID: "session-a", Username: "alice", UID: 1000}
	events, unsubscribe := registry.Subscribe(8)
	defer unsubscribe()
	job, err := registry.CreateForOwner("test.cancel.queued", nil, owner)
	if err != nil {
		t.Fatalf("CreateForOwner returned error: %v", err)
	}

	job.Cancel()

	waitForState(t, job, StateCanceled)
	event := waitForJobEvent(t, events, job.ID(), EventCanceled)
	if event.Job.State != StateCanceled {
		t.Fatalf("event state = %q, want canceled", event.Job.State)
	}
}

func TestCancelForSessionActiveJob(t *testing.T) {
	registry := NewRegistry()
	owner := Owner{SessionID: "session-a", Username: "alice", UID: 1000}
	events, unsubscribe := registry.Subscribe(8)
	defer unsubscribe()
	started := make(chan struct{})
	job, err := startTestJob(registry, "test.cancel.session.active", nil, owner, func(ctx context.Context, job *Job, _ any) (any, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	if err != nil {
		t.Fatalf("startTestJob returned error: %v", err)
	}
	<-started

	registry.CancelForSession(owner.SessionID)

	waitForState(t, job, StateCanceled)
	waitForJobEvent(t, events, job.ID(), EventCanceled)
}

func TestCancelForSessionQueuedJob(t *testing.T) {
	registry := NewRegistry()
	owner := Owner{SessionID: "session-a", Username: "alice", UID: 1000}
	events, unsubscribe := registry.Subscribe(8)
	defer unsubscribe()
	job, err := registry.CreateForOwner("test.cancel.session.queued", nil, owner)
	if err != nil {
		t.Fatalf("CreateForOwner returned error: %v", err)
	}

	registry.CancelForSession(owner.SessionID)

	waitForState(t, job, StateCanceled)
	waitForJobEvent(t, events, job.ID(), EventCanceled)
}

func TestCancelForSessionCompletedJobIgnored(t *testing.T) {
	registry := NewRegistry()
	owner := Owner{SessionID: "session-a", Username: "alice", UID: 1000}
	job, err := startTestJob(registry, "test.cancel.session.completed", nil, owner, func(ctx context.Context, job *Job, _ any) (any, error) {
		return map[string]any{"ok": true}, nil
	})
	if err != nil {
		t.Fatalf("startTestJob returned error: %v", err)
	}
	waitForState(t, job, StateCompleted)
	events, unsubscribe := registry.Subscribe(8)
	defer unsubscribe()

	registry.CancelForSession(owner.SessionID)

	if snapshot := job.Snapshot(); snapshot.State != StateCompleted {
		t.Fatalf("state after CancelForSession = %q, want completed", snapshot.State)
	}
	assertNoJobEvent(t, events, job.ID(), EventCanceled)
}

func TestOwnerScopedAccessors(t *testing.T) {
	registry := NewRegistry()
	ownerA := Owner{SessionID: "session-a", Username: "alice", UID: 1000}
	ownerB := Owner{SessionID: "session-b", Username: "bob", UID: 1001}
	block := make(chan struct{})
	job, err := startTestJob(registry, "test.owner", nil, ownerA, func(ctx context.Context, job *Job, _ any) (any, error) {
		<-block
		return map[string]any{"ok": true}, nil
	})
	if err != nil {
		t.Fatalf("startTestJob returned error: %v", err)
	}
	defer close(block)

	if _, ok := registry.GetForOwner(job.ID(), ownerA); !ok {
		t.Fatal("owner should be able to access own job")
	}
	if _, ok := registry.GetForOwner(job.ID(), ownerB); ok {
		t.Fatal("different owner should not be able to access job")
	}
	if got := registry.ListForOwner(ownerA); len(got) != 1 || got[0].ID != job.ID() {
		t.Fatalf("ListForOwner(ownerA) = %#v, want own job", got)
	}
	if got := registry.ListForOwner(ownerB); len(got) != 0 {
		t.Fatalf("ListForOwner(ownerB) = %#v, want empty", got)
	}
}

func TestRegistrySubscribeReceivesLiveEvents(t *testing.T) {
	registry := NewRegistry()
	owner := Owner{SessionID: "session-a", Username: "alice", UID: 1000}
	events, unsubscribe := registry.Subscribe(8)
	defer unsubscribe()
	job, err := startTestJob(registry, "test.events", nil, owner, func(ctx context.Context, job *Job, _ any) (any, error) {
		job.ReportProgress(map[string]any{"pct": 50})
		return map[string]any{"ok": true}, nil
	})
	if err != nil {
		t.Fatalf("startTestJob returned error: %v", err)
	}

	seen := map[EventType]bool{}
	deadline := time.After(time.Second)
	for !seen[EventStarted] || !seen[EventProgress] || !seen[EventResult] {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for events; saw %#v", seen)
		case event := <-events:
			if event.Job.ID != job.ID() {
				continue
			}
			if !event.Job.Owner.Matches(owner) {
				t.Fatalf("event owner = %#v, want %#v", event.Job.Owner, owner)
			}
			seen[event.Type] = true
		}
	}
}

func TestSlowRegistrySubscriberStillReceivesTerminalEvent(t *testing.T) {
	registry := NewRegistry()
	owner := Owner{SessionID: "session-a", Username: "alice", UID: 1000}
	events, unsubscribe := registry.Subscribe(1)
	defer unsubscribe()

	job, err := startTestJob(registry, "test.slow.registry", nil, owner, func(ctx context.Context, job *Job, _ any) (any, error) {
		for i := range 20 {
			job.ReportProgress(map[string]any{"pct": i})
		}
		return map[string]any{"ok": true}, nil
	})
	if err != nil {
		t.Fatalf("startTestJob returned error: %v", err)
	}

	waitForState(t, job, StateCompleted)
	event := waitForJobEvent(t, events, job.ID(), EventResult)
	if event.Job.State != StateCompleted {
		t.Fatalf("event state = %q, want completed", event.Job.State)
	}
}

func TestSlowJobSubscriberStillReceivesTerminalEvent(t *testing.T) {
	registry := NewRegistry()
	block := make(chan struct{})
	job, err := startTestJob(registry, "test.slow.job", nil, Owner{}, func(ctx context.Context, job *Job, _ any) (any, error) {
		<-block
		for i := range 20 {
			job.ReportProgress(map[string]any{"pct": i})
		}
		return map[string]any{"ok": true}, nil
	})
	if err != nil {
		t.Fatalf("startTestJob returned error: %v", err)
	}

	events, unsubscribe := job.Subscribe(1)
	defer unsubscribe()
	close(block)

	waitForState(t, job, StateCompleted)
	event := waitForJobEvent(t, events, job.ID(), EventResult)
	if event.Job.State != StateCompleted {
		t.Fatalf("event state = %q, want completed", event.Job.State)
	}
}

func TestReportDataDoesNotReachRegistryEvents(t *testing.T) {
	registry := NewRegistry()
	owner := Owner{SessionID: "session-a", Username: "alice", UID: 1000}
	registryEvents, registryUnsubscribe := registry.Subscribe(8)
	defer registryUnsubscribe()

	job, err := registry.CreateForOwner("logs.general.follow", nil, owner)
	if err != nil {
		t.Fatalf("CreateForOwner returned error: %v", err)
	}
	jobEvents, jobUnsubscribe := job.Subscribe(8)
	defer jobUnsubscribe()

	job.ReportData("line\n")

	event := waitForJobEvent(t, jobEvents, job.ID(), EventProgress)
	progress, ok := event.Progress.(map[string]any)
	if !ok {
		t.Fatalf("progress = %#v, want map", event.Progress)
	}
	if progress["type"] != "data" || progress["data"] != "line\n" {
		t.Fatalf("progress = %#v, want data line", progress)
	}
	assertNoJobEvent(t, registryEvents, job.ID(), EventProgress)
	if snapshot := job.Snapshot(); snapshot.Progress != nil {
		t.Fatalf("snapshot progress = %#v, want nil for transient data", snapshot.Progress)
	}
}

func TestDataProgressMapDoesNotReachRegistryEvents(t *testing.T) {
	registry := NewRegistry()
	owner := Owner{SessionID: "session-a", Username: "alice", UID: 1000}
	registryEvents, registryUnsubscribe := registry.Subscribe(8)
	defer registryUnsubscribe()

	job, err := registry.CreateForOwner("logs.service.follow", nil, owner)
	if err != nil {
		t.Fatalf("CreateForOwner returned error: %v", err)
	}
	jobEvents, jobUnsubscribe := job.Subscribe(8)
	defer jobUnsubscribe()

	job.ReportProgress(map[string]any{"type": "data", "data": "legacy\n"})

	event := waitForJobEvent(t, jobEvents, job.ID(), EventProgress)
	progress, ok := event.Progress.(map[string]any)
	if !ok {
		t.Fatalf("progress = %#v, want map", event.Progress)
	}
	if progress["data"] != "legacy\n" {
		t.Fatalf("progress data = %#v, want legacy line", progress["data"])
	}
	assertNoJobEvent(t, registryEvents, job.ID(), EventProgress)
	if snapshot := job.Snapshot(); snapshot.Progress != nil {
		t.Fatalf("snapshot progress = %#v, want nil for transient data", snapshot.Progress)
	}
}

func TestTransientProgressDoesNotReachRegistryEvents(t *testing.T) {
	registry := NewRegistry()
	owner := Owner{SessionID: "session-a", Username: "alice", UID: 1000}
	registryEvents, registryUnsubscribe := registry.Subscribe(8)
	defer registryUnsubscribe()

	job, err := registry.CreateForOwner("docker.compose", nil, owner)
	if err != nil {
		t.Fatalf("CreateForOwner returned error: %v", err)
	}
	jobEvents, jobUnsubscribe := job.Subscribe(8)
	defer jobUnsubscribe()

	job.ReportTransientProgress(map[string]any{"type": "stdout", "message": "creating container"})

	event := waitForJobEvent(t, jobEvents, job.ID(), EventProgress)
	progress, ok := event.Progress.(map[string]any)
	if !ok {
		t.Fatalf("progress = %#v, want map", event.Progress)
	}
	if progress["message"] != "creating container" {
		t.Fatalf("progress message = %#v, want compose output", progress["message"])
	}
	assertNoJobEvent(t, registryEvents, job.ID(), EventProgress)
	if snapshot := job.Snapshot(); snapshot.Progress != nil {
		t.Fatalf("snapshot progress = %#v, want nil for transient progress", snapshot.Progress)
	}
}

func TestProgressReplayShrinksByBytesAfterFirstSubscriber(t *testing.T) {
	registry := NewRegistry()
	job, err := registry.Create("logs.general.follow", nil)
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	chunk := strings.Repeat(
		"x",
		DefaultSubscribedJobProgressReplayBytes/2+1,
	)
	job.ReportData(chunk)
	job.ReportData(chunk)
	job.ReportData(chunk)

	_, replay, unsubscribe := job.SubscribeWithReplay(8)
	defer unsubscribe()
	if len(replay) != 3 {
		t.Fatalf("initial replay length = %d, want all 3 pre-attach events", len(replay))
	}

	job.mu.RLock()
	retainedBytes := job.progressLogBytes
	retainedEvents := len(job.progressLog)
	job.mu.RUnlock()
	if retainedBytes > DefaultSubscribedJobProgressReplayBytes {
		t.Fatalf(
			"post-subscribe replay bytes = %d, limit %d",
			retainedBytes,
			DefaultSubscribedJobProgressReplayBytes,
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

func TestAttachJobStreamReplaysProgressBeforeTerminalResult(t *testing.T) {
	registry := NewRegistry()
	job, err := startTestJob(registry, "test.attach.replay", nil, Owner{}, func(ctx context.Context, job *Job, _ any) (any, error) {
		job.ReportData("first\n")
		job.ReportData("second\n")
		return map[string]any{"ok": true}, nil
	})
	if err != nil {
		t.Fatalf("startTestJob returned error: %v", err)
	}
	waitForState(t, job, StateCompleted)

	server, client := net.Pipe()
	defer client.Close()

	errCh := make(chan error, 1)
	go func() {
		defer server.Close()
		errCh <- AttachJobStream(server, job)
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
		t.Fatalf("AttachJobStream returned error: %v", err)
	}
}

func TestAttachJobStreamReportsLagInsteadOfSilentlyDroppingData(t *testing.T) {
	registry := NewRegistry()
	job, err := registry.Create("test.attach.lag", nil)
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	job.ReportData("replay\n")

	server, client := net.Pipe()
	defer client.Close()

	errCh := make(chan error, 1)
	go func() {
		defer server.Close()
		errCh <- AttachJobStream(server, job)
	}()

	deadline := time.Now().Add(time.Second)
	for {
		job.mu.RLock()
		subscribers := len(job.subscribers)
		job.mu.RUnlock()
		if subscribers == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for attach subscriber")
		}
		time.Sleep(time.Millisecond)
	}

	// AttachJobStream is blocked writing the replay to net.Pipe while the
	// client is not reading. Overflow its bounded live channel during that
	// window and then let the writer continue.
	for index := 0; index <= DefaultJobProgressReplayLimit; index++ {
		job.ReportData("live\n")
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
		t.Fatalf("AttachJobStream returned error: %v", err)
	}
}

func TestSweepTerminalOlderThanRemovesOnlyOldTerminalJobs(t *testing.T) {
	registry := NewRegistry()
	activeBlock := make(chan struct{})
	doneJob, err := startTestJob(registry, "test.sweep.done", nil, Owner{}, func(ctx context.Context, job *Job, _ any) (any, error) {
		return map[string]any{"ok": true}, nil
	})
	if err != nil {
		t.Fatalf("start done returned error: %v", err)
	}
	activeJob, err := startTestJob(registry, "test.sweep.active", nil, Owner{}, func(ctx context.Context, job *Job, _ any) (any, error) {
		<-activeBlock
		return map[string]any{"ok": true}, nil
	})
	if err != nil {
		t.Fatalf("start active returned error: %v", err)
	}
	defer close(activeBlock)

	waitForState(t, doneJob, StateCompleted)
	oldFinishedAt := time.Now().UTC().Add(-time.Hour)
	doneJob.mu.Lock()
	doneJob.finishedAt = &oldFinishedAt
	doneJob.mu.Unlock()

	removed := registry.SweepTerminalOlderThan(time.Now().UTC().Add(-30 * time.Minute))
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if _, ok := registry.Get(doneJob.ID()); ok {
		t.Fatal("old terminal job should be removed")
	}
	if _, ok := registry.Get(activeJob.ID()); !ok {
		t.Fatal("active job should not be removed")
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

func startTestJob(registry *Registry, jobType string, request any, owner Owner, runner Runner) (*Job, error) {
	job, err := registry.CreateForOwner(jobType, request, owner)
	if err != nil {
		return nil, err
	}
	job.Start(runner)
	return job, nil
}

func waitForState(t *testing.T, job *Job, want State) {
	t.Helper()
	deadline := time.After(time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for state %q; got %q", want, job.Snapshot().State)
		case <-ticker.C:
			if job.Snapshot().State == want {
				return
			}
		}
	}
}

func waitForJobEvent(t *testing.T, events <-chan Event, jobID string, want EventType) Event {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for event %q on job %s", want, jobID)
		case event := <-events:
			if event.Job.ID == jobID && event.Type == want {
				return event
			}
		}
	}
}

func assertNoJobEvent(t *testing.T, events <-chan Event, jobID string, eventType EventType) {
	t.Helper()
	timer := time.NewTimer(25 * time.Millisecond)
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
			return
		case event := <-events:
			if event.Job.ID == jobID && event.Type == eventType {
				t.Fatalf("unexpected event %q for job %s", eventType, jobID)
			}
		}
	}
}
