package filebrowser

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/services"
	bridgetasks "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

func lastFileProgress(t *testing.T, task *bridgetasks.Task) FileProgress {
	t.Helper()
	snapshot := task.Snapshot()
	if snapshot.Progress == nil {
		t.Fatal("task reported no progress")
	}
	progress, ok := snapshot.Progress.Detail.(FileProgress)
	if !ok {
		t.Fatalf("progress detail type = %T, want FileProgress", snapshot.Progress.Detail)
	}
	return progress
}

// Compression starts before its size walk finishes, so progress has to be
// reportable with no denominator and has to pick the denominator up mid-flight
// rather than capturing it once at construction.
func TestTaskPhaseCallbacksReportIndeterminateUntilSizeLands(t *testing.T) {
	registry := bridgetasks.NewTaskService()
	req := apischema.FileCompressRequest{TargetPath: "/tmp/x.zip", Format: "zip", Paths: []string{"/tmp"}}
	task, err := registry.Create("filebrowser.compress", req)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	size := &sizeEstimate{}
	callbacks := newTaskPhaseCallbacks(context.Background(), task, nil, size, "compressing")

	callbacks.ReportProgress(500)
	progress := lastFileProgress(t, task)
	if !progress.Indeterminate {
		t.Fatal("progress before the size estimate landed is not marked indeterminate")
	}
	if progress.Total != 0 {
		t.Fatalf("total = %d, want 0 while the estimate is outstanding", progress.Total)
	}
	if progress.Bytes != 500 {
		t.Fatalf("bytes = %d, want 500", progress.Bytes)
	}

	size.set(1000)
	callbacks.ReportProgress(500)
	progress = lastFileProgress(t, task)
	if progress.Indeterminate {
		t.Fatal("progress is still indeterminate after the size estimate landed")
	}
	if progress.Total != 1000 {
		t.Fatalf("total = %d, want 1000", progress.Total)
	}
	if progress.Pct != 100 {
		t.Fatalf("pct = %d, want 100 for 1000/1000", progress.Pct)
	}
}

// A total that is known to be zero is not the same as one that has not been
// computed yet: the first has nothing to report against and stays silent.
func TestTaskPhaseCallbacksStaySilentForKnownZeroTotal(t *testing.T) {
	registry := bridgetasks.NewTaskService()
	req := apischema.FileCompressRequest{TargetPath: "/tmp/x.zip", Format: "zip", Paths: []string{"/tmp"}}
	task, err := registry.Create("filebrowser.compress", req)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	callbacks := newTaskPhaseCallbacks(context.Background(), task, nil, knownSize(0), "compressing")
	callbacks.ReportProgress(500)

	if snapshot := task.Snapshot(); snapshot.Progress != nil {
		t.Fatalf("reported progress %+v for a known-zero total, want none", snapshot.Progress)
	}
}

// The walk runs concurrently with the archive it measures, so a task that ends
// early must be able to call it off instead of waiting out a huge tree.
func TestComputeArchiveSizeHonorsCancellation(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("data"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := services.ComputeArchiveSize(ctx, []string{dir}); err == nil {
		t.Fatal("cancelled walk returned no error")
	}
	// The handler wrapper turns any failure into "total unknown" so a cancelled
	// or failed estimate degrades the progress bar, never the archive.
	if total := computeArchiveSize(ctx, []string{dir}); total != 0 {
		t.Fatalf("computeArchiveSize = %d, want 0 after cancellation", total)
	}
}
