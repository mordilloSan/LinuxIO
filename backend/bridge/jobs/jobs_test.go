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
	registry.RegisterRecoverer("test.recover.active", func(registry *Registry) (*Job, error) {
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
	registry.RegisterRecoverer("test.recover.external", func(registry *Registry) (*Job, error) {
		return registry.StartWithRunner("test.recover.external", []string{"external"}, func(ctx context.Context, job *Job, args []string) (any, error) {
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
