package bridge

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
)

func TestJobCompletesAndSnapshotsResult(t *testing.T) {
	registry := NewRegistry()
	job, err := startTestJob(registry, "test.complete", nil, Owner{}, func(ctx context.Context, job *Job, args []string) (any, error) {
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

func TestJobDoneClosesAfterTerminalSnapshotCommitted(t *testing.T) {
	registry := NewRegistry()
	job, err := startTestJob(registry, "test.done.atomic", nil, Owner{}, func(ctx context.Context, job *Job, args []string) (any, error) {
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
	job, err := startTestJob(registry, "test.cancel", nil, Owner{}, func(ctx context.Context, job *Job, args []string) (any, error) {
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
	job, err := startTestJob(registry, "test.cancel.session.active", nil, owner, func(ctx context.Context, job *Job, args []string) (any, error) {
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
	job, err := startTestJob(registry, "test.cancel.session.completed", nil, owner, func(ctx context.Context, job *Job, args []string) (any, error) {
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
	job, err := startTestJob(registry, "test.owner", nil, ownerA, func(ctx context.Context, job *Job, args []string) (any, error) {
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
	job, err := startTestJob(registry, "test.events", nil, owner, func(ctx context.Context, job *Job, args []string) (any, error) {
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

func TestAttachJobStreamReplaysProgressBeforeTerminalResult(t *testing.T) {
	registry := NewRegistry()
	job, err := startTestJob(registry, "test.attach.replay", nil, Owner{}, func(ctx context.Context, job *Job, args []string) (any, error) {
		job.ReportProgress(map[string]any{"type": "data", "data": "first\n"})
		job.ReportProgress(map[string]any{"type": "data", "data": "second\n"})
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

func TestSweepTerminalOlderThanRemovesOnlyOldTerminalJobs(t *testing.T) {
	registry := NewRegistry()
	activeBlock := make(chan struct{})
	doneJob, err := startTestJob(registry, "test.sweep.done", nil, Owner{}, func(ctx context.Context, job *Job, args []string) (any, error) {
		return map[string]any{"ok": true}, nil
	})
	if err != nil {
		t.Fatalf("start done returned error: %v", err)
	}
	activeJob, err := startTestJob(registry, "test.sweep.active", nil, Owner{}, func(ctx context.Context, job *Job, args []string) (any, error) {
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

func startTestJob(registry *Registry, jobType string, args []string, owner Owner, runner Runner) (*Job, error) {
	job, err := registry.CreateForOwner(jobType, args, owner)
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
