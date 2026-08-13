package filebrowser

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgetasks "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

func TestUploadTaskRejectsExistingDestinationWithoutOverwrite(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	registry := bridgetasks.NewTaskService()
	req := apischema.FileUploadRequest{TargetPath: target, Size: "3"}
	task, err := registry.Create(routeUpload, req)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	task.Start(func(ctx context.Context, j *bridgetasks.Task, _ any) (any, error) {
		return runUploadTask(ctx, j, req)
	})

	snapshot := waitTaskDone(t, task)
	if snapshot.State != bridgetasks.TaskStateFailed {
		t.Fatalf("task state = %q, want failed", snapshot.State)
	}
	if snapshot.Error == nil || snapshot.Error.Code != 409 {
		t.Fatalf("task error = %+v, want 409", snapshot.Error)
	}
	if got, readErr := os.ReadFile(target); readErr != nil || string(got) != "old" {
		t.Fatalf("existing file was touched: %q, err %v", got, readErr)
	}
}

func TestUploadTaskOverwriteAcceptsExistingDestination(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	registry := bridgetasks.NewTaskService()
	overwrite := true
	req := apischema.FileUploadRequest{TargetPath: target, Size: "3", Overwrite: &overwrite}
	task, err := registry.Create(routeUpload, req)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	task.Start(func(ctx context.Context, j *bridgetasks.Task, _ any) (any, error) {
		return runUploadTask(ctx, j, req)
	})

	// The task must get past the conflict check and park waiting for a client
	// stream; cancel it to finish the test.
	waitForTransfer := func() bool {
		_, ok := waitForFileTransferTask(context.Background(), task.ID())
		return ok
	}
	if !waitForTransfer() {
		t.Fatal("upload task did not reach waiting_for_client")
	}
	task.Cancel()
	snapshot := waitTaskDone(t, task)
	if snapshot.State != bridgetasks.TaskStateCanceled {
		t.Fatalf("task state = %q, want canceled", snapshot.State)
	}
}

// The client opens its data stream as soon as the start reply lands, so the
// archive task must register its transfer before anything slow: the
// heavy-archive slot and the size walk both outlast transferAttachWaitTimeout
// on a large folder, and registering after them handed the client a bogus
// "transfer task not ready" 404 while the task archived on regardless.
// Occupying the single heavy-archive slot reproduces that delay exactly.
func TestArchiveTaskRegistersTransferBeforeSlowStartup(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("data"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	release, err := heavyArchiveLimiter.acquire(context.Background(), 1)
	if err != nil {
		t.Fatalf("occupy heavy archive slot: %v", err)
	}
	defer release()

	registry := bridgetasks.NewTaskService()
	req := apischema.FileArchiveRequest{Format: "zip", Paths: []string{dir}}
	task, err := registry.Create(routeArchive, req)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	task.Start(func(ctx context.Context, j *bridgetasks.Task, _ any) (any, error) {
		return runArchiveTask(ctx, j, nil, req)
	})

	entry, ok := waitForFileTransferTask(context.Background(), task.ID())
	if !ok {
		t.Fatal("archive transfer not registered while the task waited for the heavy-archive slot")
	}
	transfer, ok := entry.(*archiveTransferTask)
	if !ok {
		t.Fatalf("registered transfer type = %T, want *archiveTransferTask", entry)
	}

	// Cancelling while the task is still blocked on the slot must also release
	// a client already parked in attach() on <-ready, rather than stranding it.
	task.Cancel()
	select {
	case <-transfer.ready:
	case <-time.After(5 * time.Second):
		t.Fatal("ready gate never resolved after the task was cancelled")
	}
	if transfer.readyErr == nil {
		t.Fatal("ready gate resolved without an error after cancellation")
	}

	snapshot := waitTaskDone(t, task)
	if snapshot.State != bridgetasks.TaskStateCanceled {
		t.Fatalf("task state = %q, want canceled", snapshot.State)
	}
}
