package filebrowser

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgejobs "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

func TestUploadJobRejectsExistingDestinationWithoutOverwrite(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	registry := bridgejobs.NewRegistry()
	req := apischema.FileUploadRequest{TargetPath: target, Size: "3"}
	job, err := registry.Create(routeUpload, req)
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	job.Start(func(ctx context.Context, j *bridgejobs.Job, _ any) (any, error) {
		return runUploadJob(ctx, j, req)
	})

	snapshot := waitJobDone(t, job)
	if snapshot.State != bridgejobs.StateFailed {
		t.Fatalf("job state = %q, want failed", snapshot.State)
	}
	if snapshot.Error == nil || snapshot.Error.Code != 409 {
		t.Fatalf("job error = %+v, want 409", snapshot.Error)
	}
	if got, readErr := os.ReadFile(target); readErr != nil || string(got) != "old" {
		t.Fatalf("existing file was touched: %q, err %v", got, readErr)
	}
}

func TestUploadJobOverwriteAcceptsExistingDestination(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	registry := bridgejobs.NewRegistry()
	overwrite := true
	req := apischema.FileUploadRequest{TargetPath: target, Size: "3", Overwrite: &overwrite}
	job, err := registry.Create(routeUpload, req)
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	job.Start(func(ctx context.Context, j *bridgejobs.Job, _ any) (any, error) {
		return runUploadJob(ctx, j, req)
	})

	// The job must get past the conflict check and park waiting for a client
	// stream; cancel it to finish the test.
	waitForTransfer := func() bool {
		_, ok := waitForFileTransferJob(context.Background(), job.ID())
		return ok
	}
	if !waitForTransfer() {
		t.Fatal("upload job did not reach waiting_for_client")
	}
	job.Cancel()
	snapshot := waitJobDone(t, job)
	if snapshot.State != bridgejobs.StateCanceled {
		t.Fatalf("job state = %q, want canceled", snapshot.State)
	}
}
