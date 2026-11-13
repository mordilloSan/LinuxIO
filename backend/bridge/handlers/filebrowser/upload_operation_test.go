package filebrowser

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/services"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	ipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
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
	task, _, err := bridgeipc.NewTaskService().ClaimForOwnerWithIdentity(routeUpload, apischema.FileUploadRequest{TargetPath: link, Size: "3"}, bridgeipc.TaskOwner{}, bridgeipc.TaskLifetimeSession, bridgeipc.TaskIdentity{})
	require.NoError(t, err)
	transfer := &uploadTransferTask{transferLifecycle: &transferLifecycle{task: task}, ctx: context.Background(), path: link, expectedSize: 3, expectedVersion: opened.Version, overwrite: true}
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

// startOwnedUpload runs a filebrowser.upload task that sets owner, as the
// Compose editor's save does, and attaches its data stream.
func startOwnedUpload(t *testing.T, target, content string, overwrite bool) (net.Conn, <-chan ipc.ResultFrame) {
	t.Helper()
	req := apischema.FileUploadRequest{TargetPath: target, Size: strconv.Itoa(len(content)), Overwrite: &overwrite, Owner: uploadOwnerSession}
	task, _, err := bridgeipc.NewTaskService().ClaimForOwnerWithIdentity(routeUpload, req, bridgeipc.TaskOwner{}, bridgeipc.TaskLifetimeSession, bridgeipc.TaskIdentity{})
	require.NoError(t, err)
	task.Start(func(ctx context.Context, j *bridgeipc.Task, _ any) (any, error) {
		return runUploadTask(ctx, j, req, selfOwner())
	})
	conn, results, _ := attachUploadBatchStream(t, task, "")
	return conn, results
}

// An upload that sets owner writes into the user's folder on their behalf, so
// it never follows a symlink at the target: any process running as the user
// may plant ~/stack/.env -> /etc/environment, and a root bridge would write
// that file for them.
func TestOwnedUploadRefusesASymlinkAtTheTarget(t *testing.T) {
	victim := seedVictim(t)
	env := filepath.Join(t.TempDir(), ".env")
	require.NoError(t, os.Symlink(victim, env))

	_, results := startOwnedUpload(t, env, "new", true)

	result := waitResult(t, results)
	require.Equal(t, "error", result.Status)
	require.Equal(t, 409, result.Code)
	require.Contains(t, result.Error, ".env is a symbolic link; replace it with a regular file")
	requireVictimUntouched(t, victim)
	info, err := os.Lstat(env)
	require.NoError(t, err)
	require.NotZero(t, info.Mode()&os.ModeSymlink, "the link is left alone")
}

// The symlink is refused before the version check, so the bridge never reads
// the link's target and the user is told about the link, not a stale version.
func TestOwnedUploadRefusesASymlinkBeforeCheckingTheVersion(t *testing.T) {
	victim := seedVictim(t)
	env := filepath.Join(t.TempDir(), ".env")
	require.NoError(t, os.Symlink(victim, env))
	transfer := newOwnedUpload(t, env, 3, selfOwner(), true, true)
	transfer.refuseSymlink = true
	transfer.expectedVersion = "loaded-before-the-link"

	err := transfer.prepare(openTestRoot(t))
	require.ErrorIs(t, err, bridgeipc.ErrConflict)
	require.NotErrorIs(t, err, errUploadVersionConflict)
	require.ErrorContains(t, err, ".env is a symbolic link")
	requireVictimUntouched(t, victim)
}

// Uploads without owner keep checking the version before any folder is made,
// so an editor save into a folder deleted since it was opened recreates none.
func TestUploadEditorSaveToADeletedFolderCreatesNoFolders(t *testing.T) {
	gone := filepath.Join(t.TempDir(), "gone")
	transfer := newOwnedUpload(t, filepath.Join(gone, "sub", "note.txt"), 3, sessionOwner{}, false, true)
	transfer.expectedVersion = "opened-before-the-delete"

	require.Error(t, transfer.prepare(openTestRoot(t)))
	require.NoDirExists(t, gone)
}

// A new .env is saved without overwrite. A dangling link there passes the
// existence check up front (it follows the link), so prepare must refuse it.
func TestOwnedUploadWithoutOverwriteRefusesADanglingSymlink(t *testing.T) {
	stack := t.TempDir()
	missing := filepath.Join(t.TempDir(), "environment")
	env := filepath.Join(stack, ".env")
	require.NoError(t, os.Symlink(missing, env))

	_, results := startOwnedUpload(t, env, "new", false)

	result := waitResult(t, results)
	require.Equal(t, "error", result.Status)
	require.Equal(t, 409, result.Code)
	require.Contains(t, result.Error, ".env is a symbolic link; replace it with a regular file")
	_, err := os.Lstat(missing)
	require.ErrorIs(t, err, fs.ErrNotExist)
	target, err := os.Readlink(env)
	require.NoError(t, err)
	require.Equal(t, missing, target, "the link is left alone")
	entries, err := os.ReadDir(stack)
	require.NoError(t, err)
	require.Len(t, entries, 1, "no upload buffer is left behind")
}

// A symlink swapped in after prepare is not followed either: the commit's
// renameat replaces the link itself.
func TestOwnedUploadReplacesASymlinkSwappedInAfterPrepare(t *testing.T) {
	victim := seedVictim(t)
	env := filepath.Join(t.TempDir(), ".env")
	require.NoError(t, os.WriteFile(env, []byte("old"), 0o600))

	conn, results := startOwnedUpload(t, env, "new", true)
	writeData(t, conn, []byte("new")) // returns once attach has prepared and reads frames
	require.NoError(t, os.Remove(env))
	require.NoError(t, os.Symlink(victim, env))
	writeClose(t, conn)

	result := waitResult(t, results)
	require.Equalf(t, "ok", result.Status, "upload answered %+v", result)
	requireVictimUntouched(t, victim)
	info, err := os.Lstat(env)
	require.NoError(t, err)
	require.True(t, info.Mode().IsRegular(), "the link itself is replaced")
	content, err := os.ReadFile(env)
	require.NoError(t, err)
	require.Equal(t, "new", string(content))
}

// commitTestUpload prepares an upload to target and commits content through
// the real prepare/commit path, returning the commit result.
func commitTestUpload(t *testing.T, target, content string, overwrite bool, mutate func(*uploadTransferTask)) (int, error) {
	t.Helper()
	root, err := fsroot.Open()
	require.NoError(t, err)
	t.Cleanup(func() { _ = root.Close() })
	task, _, err := bridgeipc.NewTaskService().ClaimForOwnerWithIdentity(routeUpload, apischema.FileUploadRequest{TargetPath: target, Size: strconv.Itoa(len(content))}, bridgeipc.TaskOwner{}, bridgeipc.TaskLifetimeSession, bridgeipc.TaskIdentity{})
	require.NoError(t, err)
	transfer := &uploadTransferTask{transferLifecycle: &transferLifecycle{task: task}, ctx: context.Background(), path: target, expectedSize: int64(len(content)), overwrite: overwrite}
	require.NoError(t, transfer.prepare(root))
	if mutate != nil {
		mutate(transfer)
	}

	file, err := root.Root.OpenFile(transfer.tempRel, os.O_WRONLY, 0)
	require.NoError(t, err)
	_, err = file.WriteString(content)
	require.NoError(t, err)
	return transfer.commit(root, file, target, transfer.tempRel, transfer.finalRel)
}

func TestUploadTaskCommitKeepsRestrictiveTargetMode(t *testing.T) {
	target := filepath.Join(t.TempDir(), "authorized_keys")
	require.NoError(t, os.WriteFile(target, []byte("old"), 0o600))

	_, err := commitTestUpload(t, target, "new", true, nil)
	require.NoError(t, err)

	info, err := os.Stat(target)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	content, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, "new", string(content))
}

func TestUploadTaskCommitIgnoresOwnershipPermissionError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can chown to any owner")
	}
	target := filepath.Join(t.TempDir(), "shared.env")
	require.NoError(t, os.WriteFile(target, []byte("old"), 0o640))

	code, err := commitTestUpload(t, target, "new", true, func(transfer *uploadTransferTask) {
		transfer.attrs.uid = 0
	})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	info, err := os.Stat(target)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o640), info.Mode().Perm())
	content, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, "new", string(content))
}

func TestUploadTaskCommitDoesNotReplaceFileCreatedAfterStart(t *testing.T) {
	target := filepath.Join(t.TempDir(), "f.txt")

	code, err := commitTestUpload(t, target, "new", false, func(*uploadTransferTask) {
		require.NoError(t, os.WriteFile(target, []byte("appeared"), 0o644))
	})
	require.ErrorIs(t, err, fs.ErrExist)
	require.Equal(t, 409, code)
	content, readErr := os.ReadFile(target)
	require.NoError(t, readErr)
	require.Equal(t, "appeared", string(content))
}

func TestUploadTaskCommitWithoutOverwriteLinksNewFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "f.txt")

	_, err := commitTestUpload(t, target, "new", false, nil)
	require.NoError(t, err)
	content, readErr := os.ReadFile(target)
	require.NoError(t, readErr)
	require.Equal(t, "new", string(content))
	entries, readErr := os.ReadDir(dir)
	require.NoError(t, readErr)
	require.Len(t, entries, 1, "buffer must be removed after linking")
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
	task, _, err := registry.ClaimForOwnerWithIdentity(routeUpload, req, bridgeipc.TaskOwner{}, bridgeipc.TaskLifetimeSession, bridgeipc.TaskIdentity{})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	task.Start(func(ctx context.Context, j *bridgeipc.Task, _ any) (any, error) {
		return runUploadTask(ctx, j, req, sessionOwner{})
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

	// The browser opens tasks.data after a start that still read running:
	// the attach answers with the refusal, not with a timeout.
	server, client := net.Pipe()
	defer client.Close()
	go func() {
		defer server.Close()
		_ = attachFileTransferData(context.Background(), task, server, bridgeipc.TaskDataAttachRequest{})
	}()
	_ = client.SetReadDeadline(time.Now().Add(5 * time.Second))
	frame, err := ipc.ReadRelayFrame(client)
	var result ipc.ResultFrame
	if err != nil || json.Unmarshal(frame.Payload, &result) != nil || result.Code != 409 {
		t.Fatalf("attach answer = %+v (%s), %v; want the 409 refusal", result, frame.Payload, err)
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
	task, _, err := registry.ClaimForOwnerWithIdentity(routeUpload, req, bridgeipc.TaskOwner{}, bridgeipc.TaskLifetimeSession, bridgeipc.TaskIdentity{})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	task.Start(func(ctx context.Context, j *bridgeipc.Task, _ any) (any, error) {
		return runUploadTask(ctx, j, req, sessionOwner{})
	})

	// The task must get past the conflict check and park waiting for a client
	// stream; cancel it to finish the test.
	waitForTransfer := func() bool {
		_, ok := waitForFileTransferTask(context.Background(), task)
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
