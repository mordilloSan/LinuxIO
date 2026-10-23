package filebrowser

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/services"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
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
	task, err := bridgeipc.NewTaskService().Create(routeUpload, apischema.FileUploadRequest{TargetPath: link, Size: "3"})
	require.NoError(t, err)
	transfer := &uploadTransferTask{transferLifecycle: &transferLifecycle{task: task}, ctx: context.Background(), path: link, expectedSize: 3, expectedVersion: opened.Version}
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

	registry := bridgeipc.NewTaskService()
	req := apischema.FileUploadRequest{TargetPath: target, Size: "3"}
	task, err := registry.Create(routeUpload, req)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	task.Start(func(ctx context.Context, j *bridgeipc.Task, _ any) (any, error) {
		return runUploadTask(ctx, j, req, uploadOwner{})
	})

	snapshot := waitTaskDone(t, task)
	if snapshot.State != bridgeipc.TaskStateFailed {
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

	registry := bridgeipc.NewTaskService()
	overwrite := true
	req := apischema.FileUploadRequest{TargetPath: target, Size: "3", Overwrite: &overwrite}
	task, err := registry.Create(routeUpload, req)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	task.Start(func(ctx context.Context, j *bridgeipc.Task, _ any) (any, error) {
		return runUploadTask(ctx, j, req, uploadOwner{})
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
	if snapshot.State != bridgeipc.TaskStateCanceled {
		t.Fatalf("task state = %q, want canceled", snapshot.State)
	}
}
