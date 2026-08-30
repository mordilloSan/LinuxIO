package filebrowser

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/services"
	bridgetasks "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
)

func TestUploadTaskRejectsChangedExpectedVersion(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "note.txt")
	require.NoError(t, os.WriteFile(target, []byte("before"), 0o644))

	opened, err := services.ReadEditorFile(context.Background(), target)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(target, []byte("after"), 0o644))

	root, err := fsroot.Open()
	require.NoError(t, err)
	t.Cleanup(func() { _ = root.Close() })
	transfer := &uploadTransferTask{ctx: context.Background(), expectedVersion: opened.Version}

	err = transfer.verifyExpectedVersion(root, target)
	if !errors.Is(err, errUploadVersionConflict) {
		t.Fatalf("verifyExpectedVersion() error = %v, want conflict", err)
	}
}

func TestUploadTaskSavesThroughSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "bashrc")
	link := filepath.Join(dir, ".bashrc")
	require.NoError(t, os.WriteFile(target, []byte("old"), 0o644))
	require.NoError(t, os.Symlink(target, link))
	opened, err := services.ReadEditorFile(context.Background(), link)
	require.NoError(t, err)

	root, err := fsroot.Open()
	require.NoError(t, err)
	t.Cleanup(func() { _ = root.Close() })
	task, err := bridgetasks.NewTaskService().Create(routeUpload, apischema.FileUploadRequest{TargetPath: link, Size: "3"})
	require.NoError(t, err)
	transfer := &uploadTransferTask{task: task, ctx: context.Background(), path: link, expectedSize: 3, expectedVersion: opened.Version}
	require.NoError(t, transfer.prepare(root))

	file, err := root.Root.OpenFile(transfer.tempRel, os.O_WRONLY, 0)
	require.NoError(t, err)
	_, err = file.Write([]byte("new"))
	require.NoError(t, err)
	code, err := transfer.commit(root, file, link, transfer.tempRel, transfer.finalRel)
	require.NoError(t, err)
	require.Equal(t, 0, code)

	info, err := os.Lstat(link)
	require.NoError(t, err)
	require.NotZero(t, info.Mode()&os.ModeSymlink, "saving must write through the link, not replace it")
	content, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, "new", string(content))
}

func TestUploadTaskPreservesVersionReadErrors(t *testing.T) {
	dir := t.TempDir()
	nul := filepath.Join(dir, "nul")
	large := filepath.Join(dir, "large")
	require.NoError(t, os.WriteFile(nul, []byte("x\x00y"), 0o644))
	require.NoError(t, os.WriteFile(large, make([]byte, services.MaxTextFileBytes), 0o644))

	root, err := fsroot.Open()
	require.NoError(t, err)
	t.Cleanup(func() { _ = root.Close() })
	transfer := &uploadTransferTask{ctx: context.Background(), expectedVersion: "opened-version"}

	for _, test := range []struct {
		name string
		path string
		want error
	}{
		{name: "missing", path: filepath.Join(dir, "missing"), want: os.ErrNotExist},
		{name: "NUL", path: nul, want: services.ErrEditorFileContainsNUL},
		{name: "oversized", path: large, want: services.ErrEditorFileNotEligible},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := transfer.verifyExpectedVersion(root, test.path)
			require.ErrorIs(t, err, test.want)
			require.NotErrorIs(t, err, errUploadVersionConflict)
		})
	}
}

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

func TestArchiveDownloadProgressIncludesArtifactName(t *testing.T) {
	registry := bridgetasks.NewTaskService()
	task, err := registry.Create(routeArchive, apischema.FileArchiveRequest{
		Format: "zip",
		Paths:  []string{"/photos"},
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	transfer := &archiveTransferTask{
		task:        task,
		archiveName: "photos.zip",
		archiveSize: 42,
	}
	server, client := net.Pipe()
	t.Cleanup(func() {
		_ = server.Close()
		_ = client.Close()
	})
	done := make(chan struct{})
	go func() {
		defer close(done)
		transfer.writeProgress(server, "streaming")
	}()

	frame, err := relay.ReadRelayFrame(client)
	if err != nil {
		t.Fatalf("read progress: %v", err)
	}
	var progress downloadStreamProgress
	if err := json.Unmarshal(frame.Payload, &progress); err != nil {
		t.Fatalf("decode progress: %v", err)
	}
	if progress.FileName != "photos.zip" || progress.Total != 42 {
		t.Fatalf("progress = %+v, want filename photos.zip and total 42", progress)
	}
	<-done
}
