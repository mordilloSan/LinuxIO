package jobs

import (
	"context"
	"testing"
	"time"
)

func TestJobCompletesAndSnapshotsResult(t *testing.T) {
	registry := NewRegistry()
	registry.RegisterRunner("test.complete", func(ctx context.Context, job *Job, args []string) (any, error) {
		job.ReportProgress(map[string]any{"pct": 50})
		return map[string]any{"ok": true}, nil
	})

	job, err := registry.Start("test.complete", nil)
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
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

func TestJobCancelMarksCanceled(t *testing.T) {
	registry := NewRegistry()
	started := make(chan struct{})
	registry.RegisterRunner("test.cancel", func(ctx context.Context, job *Job, args []string) (any, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	})

	job, err := registry.Start("test.cancel", nil)
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	<-started
	job.Cancel()

	waitForState(t, job, StateCanceled)
	if snapshot := job.Snapshot(); snapshot.Error == nil || snapshot.Error.Code != 499 {
		t.Fatalf("cancel error = %#v, want code 499", snapshot.Error)
	}
}

func TestRecoverReturnsExistingActiveJob(t *testing.T) {
	registry := NewRegistry()
	started := make(chan struct{})
	registry.RegisterRunner("test.recover.active", func(ctx context.Context, job *Job, args []string) (any, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	registry.RegisterRecoverer("test.recover.active", func(registry *Registry, owner Owner) (*Job, error) {
		t.Fatal("recoverer should not be called when an active job already exists")
		return nil, nil
	})

	job, err := registry.Start("test.recover.active", nil)
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	<-started
	recovered, err := registry.Recover("test.recover.active")
	if err != nil {
		t.Fatalf("Recover returned error: %v", err)
	}
	if recovered.ID() != job.ID() {
		t.Fatalf("recovered job = %q, want %q", recovered.ID(), job.ID())
	}
	job.Cancel()
	waitForState(t, job, StateCanceled)
}

func TestRecoverUsesRegisteredRecoverer(t *testing.T) {
	registry := NewRegistry()
	registry.RegisterRecoverer("test.recover.external", func(registry *Registry, owner Owner) (*Job, error) {
		return registry.StartWithRunnerForOwner("test.recover.external", []string{"external"}, owner, func(ctx context.Context, job *Job, args []string) (any, error) {
			return map[string]any{"args": args}, nil
		})
	})

	job, err := registry.Recover("test.recover.external")
	if err != nil {
		t.Fatalf("Recover returned error: %v", err)
	}
	if job.Type() != "test.recover.external" {
		t.Fatalf("job type = %q, want %q", job.Type(), "test.recover.external")
	}
	waitForState(t, job, StateCompleted)
}

func TestOwnerScopedAccessors(t *testing.T) {
	registry := NewRegistry()
	ownerA := Owner{SessionID: "session-a", Username: "alice", UID: 1000}
	ownerB := Owner{SessionID: "session-b", Username: "bob", UID: 1001}
	block := make(chan struct{})
	registry.RegisterRunner("test.owner", func(ctx context.Context, job *Job, args []string) (any, error) {
		<-block
		return map[string]any{"ok": true}, nil
	})

	job, err := registry.StartForOwner("test.owner", nil, ownerA)
	if err != nil {
		t.Fatalf("StartForOwner returned error: %v", err)
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
	registry.RegisterRunner("test.events", func(ctx context.Context, job *Job, args []string) (any, error) {
		job.ReportProgress(map[string]any{"pct": 50})
		return map[string]any{"ok": true}, nil
	})

	job, err := registry.StartForOwner("test.events", nil, owner)
	if err != nil {
		t.Fatalf("StartForOwner returned error: %v", err)
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

func TestSweepTerminalOlderThanRemovesOnlyOldTerminalJobs(t *testing.T) {
	registry := NewRegistry()
	activeBlock := make(chan struct{})
	registry.RegisterRunner("test.sweep.done", func(ctx context.Context, job *Job, args []string) (any, error) {
		return map[string]any{"ok": true}, nil
	})
	registry.RegisterRunner("test.sweep.active", func(ctx context.Context, job *Job, args []string) (any, error) {
		<-activeBlock
		return map[string]any{"ok": true}, nil
	})

	doneJob, err := registry.Start("test.sweep.done", nil)
	if err != nil {
		t.Fatalf("Start done returned error: %v", err)
	}
	activeJob, err := registry.Start("test.sweep.active", nil)
	if err != nil {
		t.Fatalf("Start active returned error: %v", err)
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
