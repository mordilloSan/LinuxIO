package filebrowser

import (
	"context"
	"os"
	"path/filepath"
	"testing"

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
