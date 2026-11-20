package filebrowser

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/services"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/fsroot"
)

// selfOwner is the test process's own ids. Any process may hand its own
// entries to itself, so an unprivileged test still runs the handle-based
// chown and chmod a root bridge uses.
func selfOwner() sessionOwner {
	return sessionOwner{uid: uint32(os.Getuid()), gid: uint32(os.Getgid()), set: true}
}

func openTestRoot(t *testing.T) *fsroot.FSRoot {
	t.Helper()
	root, err := fsroot.Open()
	require.NoError(t, err)
	t.Cleanup(func() { _ = root.Close() })
	return root
}

// newOwnedUpload builds the transfer runUploadTask builds for target, with
// newOwner already resolved from the request and the session.
func newOwnedUpload(t *testing.T, target string, size int, newOwner sessionOwner, private, overwrite bool) *uploadTransferTask {
	t.Helper()
	req := apischema.FileUploadRequest{TargetPath: target, Size: strconv.Itoa(size)}
	task, _, err := bridgeipc.NewTaskService().ClaimForOwner(routeUpload, req, bridgeipc.TaskOwner{}, bridgeipc.TaskLifetimeSession, bridgeipc.TaskIdentity{})
	require.NoError(t, err)
	return &uploadTransferTask{
		transferLifecycle: &transferLifecycle{task: task},
		ctx:               context.Background(),
		path:              target,
		expectedSize:      int64(size),
		overwrite:         overwrite,
		newOwner:          newOwner,
		linkRule:          newOwner.asOwner(),
		private:           private,
	}
}

// uploadThroughHandles drives prepare, the writes through the buffer the task
// holds and commit, the steps attach runs, for content.
func uploadThroughHandles(t *testing.T, transfer *uploadTransferTask, content string) {
	t.Helper()
	root := openTestRoot(t)
	require.NoError(t, transfer.prepare(root))
	_, err := transfer.buffer.WriteString(content)
	require.NoError(t, err)
	code, err := transfer.commit(root, transfer.path, transfer.tempRel, transfer.finalRel)
	require.NoError(t, err)
	require.Zero(t, code)
}

// registeredUpload is the single-file transfer the task with id runs, once its
// data stream has attached.
func registeredUpload(t *testing.T, id string) *uploadTransferTask {
	t.Helper()
	registered, ok := fileTransferTasks.Load(id)
	require.True(t, ok, "no transfer registered as %s", id)
	transfer, ok := registered.(*uploadTransferTask)
	require.True(t, ok, "%s is not a single-file upload", id)
	return transfer
}

// heldBuffer is the buffer transfer holds open, read under its lock as the
// attach that may still run reads it.
func heldBuffer(transfer *uploadTransferTask) *os.File {
	transfer.mu.Lock()
	defer transfer.mu.Unlock()
	return transfer.buffer
}

// requireClosed checks that f, a buffer an upload held, was closed.
func requireClosed(t *testing.T, f *os.File) {
	t.Helper()
	require.NotNil(t, f)
	_, err := f.Stat()
	require.ErrorIs(t, err, os.ErrClosed)
}

// waitDetached waits for an attach whose stream dropped to park the upload.
func waitDetached(t *testing.T, attachErr <-chan error) {
	t.Helper()
	select {
	case err := <-attachErr:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the attach to detach")
	}
}

// uploadThroughStream runs a whole filebrowser.upload task: the runner, the
// data stream and the commit, on a bridge whose session user is session.
func uploadThroughStream(t *testing.T, req apischema.FileUploadRequest, content string, session sessionOwner) {
	t.Helper()
	req.Size = strconv.Itoa(len(content))
	task, _, err := bridgeipc.NewTaskService().ClaimForOwner(routeUpload, req, bridgeipc.TaskOwner{}, bridgeipc.TaskLifetimeSession, bridgeipc.TaskIdentity{})
	require.NoError(t, err)
	startTransferTask(t, task, func(ctx context.Context, j *bridgeipc.Task, _ any) (any, error) {
		return runUploadTask(ctx, j, req, session)
	})
	conn, results, _ := attachUploadBatchStream(t, task, "")
	writeData(t, conn, []byte(content))
	writeClose(t, conn)
	result := waitResult(t, results)
	require.Equalf(t, "ok", result.Status, "upload of %s answered %+v", req.TargetPath, result)
	require.Equal(t, bridgeipc.TaskStateCompleted, waitTaskDone(t, task).State)
}

func requireOwnerAndMode(t *testing.T, path string, uid, gid uint32, mode os.FileMode) {
	t.Helper()
	info, err := os.Lstat(path)
	require.NoError(t, err)
	st, ok := info.Sys().(*syscall.Stat_t)
	require.True(t, ok)
	require.Equalf(t, [2]uint32{uid, gid}, [2]uint32{st.Uid, st.Gid}, "owner of %s", path)
	require.Equalf(t, mode, info.Mode().Perm(), "mode of %s", path)
}

func requireOwner(t *testing.T, path string, uid, gid uint32) {
	t.Helper()
	info, err := os.Lstat(path)
	require.NoError(t, err)
	st, ok := info.Sys().(*syscall.Stat_t)
	require.True(t, ok)
	require.Equalf(t, [2]uint32{uid, gid}, [2]uint32{st.Uid, st.Gid}, "owner of %s", path)
}

func TestUploadNewOwnerComesOnlyFromARootBridgeSession(t *testing.T) {
	session := sessionOwner{uid: 1000, gid: 1001, set: true}
	for _, test := range []struct {
		name      string
		requested apischema.FileUploadOwner
		session   sessionOwner
		euid      int
		want      sessionOwner
	}{
		{name: "unset keeps entries the bridge's", session: session, euid: 0},
		{name: "a root bridge hands to the session user", requested: uploadOwnerSession, session: session, euid: 0, want: session},
		{name: "an unprivileged bridge already writes as the session user", requested: uploadOwnerSession, session: session, euid: 1000},
		{name: "no session owner known", requested: uploadOwnerSession, euid: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := uploadNewOwner(test.requested, test.session, test.euid)
			require.NoError(t, err)
			require.Equal(t, test.want, got)
		})
	}

	_, err := uploadNewOwner("root", session, 0)
	require.ErrorIs(t, err, bridgeipc.ErrInvalidArgs)
}

func TestUploadTaskRejectsUnknownOwner(t *testing.T) {
	req := apischema.FileUploadRequest{TargetPath: filepath.Join(t.TempDir(), "f.txt"), Size: "1", Owner: "root"}
	task, _, err := bridgeipc.NewTaskService().ClaimForOwner(routeUpload, req, bridgeipc.TaskOwner{}, bridgeipc.TaskLifetimeSession, bridgeipc.TaskIdentity{})
	require.NoError(t, err)

	_, err = runUploadTask(context.Background(), task, req, sessionOwner{uid: 1000, gid: 1000, set: true})
	require.ErrorIs(t, err, bridgeipc.ErrInvalidArgs)
}

// An unprivileged bridge ignores owner: handing a file to another uid would
// fail with EPERM, so the upload completing proves no chown was tried.
func TestUploadIgnoresOwnerOnAnUnprivilegedBridge(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("a root bridge does hand entries to the session user")
	}
	target := filepath.Join(t.TempDir(), "stack", "docker-compose.yml")
	other := sessionOwner{uid: uint32(os.Getuid()) + 1, gid: uint32(os.Getgid()) + 1, set: true}

	uploadThroughStream(t, apischema.FileUploadRequest{TargetPath: target, Owner: uploadOwnerSession}, "services: {}\n", other)

	requireOwner(t, target, uint32(os.Getuid()), uint32(os.Getgid()))
	requireOwner(t, filepath.Dir(target), uint32(os.Getuid()), uint32(os.Getgid()))
}

// New folders get 0755 and the new file 0644 (0600 when private) through their
// handles; the folder that already existed keeps its own mode.
func TestUploadHandsNewEntriesOverWithHandOverModes(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	require.NoError(t, os.Mkdir(home, 0o750))
	compose := filepath.Join(home, "stack", "sub", "docker-compose.yml")
	env := filepath.Join(home, "stack", "sub", ".env")
	owner := selfOwner()

	uploadThroughHandles(t, newOwnedUpload(t, compose, 4, owner, false, false), "a: \n")
	uploadThroughHandles(t, newOwnedUpload(t, env, 4, owner, true, false), "K=v\n")

	requireOwnerAndMode(t, home, owner.uid, owner.gid, 0o750)
	requireOwnerAndMode(t, filepath.Join(home, "stack"), owner.uid, owner.gid, 0o755)
	requireOwnerAndMode(t, filepath.Join(home, "stack", "sub"), owner.uid, owner.gid, 0o755)
	requireOwnerAndMode(t, compose, owner.uid, owner.gid, 0o644)
	requireOwnerAndMode(t, env, owner.uid, owner.gid, 0o600)
}

// Nothing is handed to a user inside a folder that is not already theirs, so
// the user never gets an entry they could not have created themselves. An
// unprivileged test would fail with EPERM if a chown to the other uid ran.
func TestUploadDoesNotHandOverInAFolderTheUserDoesNotOwn(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "stack", "docker-compose.yml")
	other := sessionOwner{uid: uint32(os.Getuid()) + 1, gid: uint32(os.Getgid()) + 1, set: true}

	uploadThroughHandles(t, newOwnedUpload(t, target, 4, other, false, false), "a: \n")

	requireOwner(t, filepath.Join(base, "stack"), uint32(os.Geteuid()), uint32(os.Getegid()))
	requireOwnerAndMode(t, target, uint32(os.Geteuid()), uint32(os.Getegid()), 0o664)
}

// A folder that already exists when mkdirat runs (EEXIST) was not made by
// this request and is opened as it is, never chowned or chmodded.
func TestUploadDirLeavesAFolderSomeoneElseMadeAlone(t *testing.T) {
	base := t.TempDir()
	existing := filepath.Join(base, "stack")
	require.NoError(t, os.Mkdir(existing, 0o700))
	parent, err := os.Open(base)
	require.NoError(t, err)
	defer parent.Close()

	dir, err := services.MkdirAt(parent, "stack", services.PermDir, selfOwner().asOwner())
	require.NoError(t, err)
	require.NoError(t, dir.Close())

	requireOwnerAndMode(t, existing, uint32(os.Geteuid()), uint32(os.Getegid()), 0o700)
}

// Between mkdirat and openat a process that can write the parent could put
// another folder under the new name, so only a folder the bridge owns that
// is still empty, as one it has just made is, is handed over.
func TestUploadHandsOverOnlyAFreshFolder(t *testing.T) {
	base := t.TempDir()
	fresh := filepath.Join(base, "fresh")
	used := filepath.Join(base, "used")
	require.NoError(t, os.Mkdir(fresh, 0o700))
	require.NoError(t, os.Mkdir(used, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(used, "docker-compose.yml"), nil, 0o600))
	want := map[string]bool{fresh: true, used: false}
	if os.Geteuid() == 0 {
		foreign := filepath.Join(base, "foreign")
		require.NoError(t, os.Mkdir(foreign, 0o700))
		require.NoError(t, os.Chown(foreign, 12345, 12346))
		want[foreign] = false
	}

	for path, fresh := range want {
		dir, err := os.Open(path)
		require.NoError(t, err)
		info, err := dir.Stat()
		require.NoError(t, err)
		got, err := services.IsFreshDir(dir, info)
		require.NoError(t, err)
		require.NoError(t, dir.Close())
		require.Equalf(t, fresh, got, "fresh %s", path)
	}
}

// setgidTestGroup is a group the test may give a folder: any group as root,
// otherwise one of the process's supplementary groups.
func setgidTestGroup(t *testing.T) int {
	t.Helper()
	if os.Geteuid() == 0 {
		return 4242
	}
	groups, err := os.Getgroups()
	require.NoError(t, err)
	for _, group := range groups {
		if group != os.Getegid() {
			return group
		}
	}
	t.Skip("needs a supplementary group to set on a folder")
	return 0
}

// In a setgid folder, what the upload hands over keeps the group it inherited,
// and a new folder keeps the setgid bit, as if the user had created them there.
func TestUploadKeepsTheGroupOfASetgidFolder(t *testing.T) {
	group := setgidTestGroup(t)
	home := filepath.Join(t.TempDir(), "home")
	require.NoError(t, os.Mkdir(home, 0o750))
	require.NoError(t, os.Chown(home, -1, group))
	require.NoError(t, os.Chmod(home, 0o750|os.ModeSetgid))
	stack := filepath.Join(home, "stack")
	compose := filepath.Join(stack, "docker-compose.yml")
	owner := selfOwner()

	uploadThroughHandles(t, newOwnedUpload(t, compose, 4, owner, false, false), "a: \n")

	requireOwnerAndMode(t, stack, owner.uid, uint32(group), 0o755)
	info, err := os.Stat(stack)
	require.NoError(t, err)
	require.NotZero(t, info.Mode()&os.ModeSetgid, "setgid bit of %s", stack)
	requireOwnerAndMode(t, compose, owner.uid, uint32(group), 0o644)
}

// ext4 gives a deleted file's inode number to the next file created, but not
// while the deleted file is still open: a file created at the name of the
// buffer the upload holds open never has its device and inode.
func TestCheckUploadBufferNeedsTheHeldFile(t *testing.T) {
	dirPath := t.TempDir()
	dir, err := os.Open(dirPath)
	require.NoError(t, err)
	defer dir.Close()
	buffer, err := createUploadBuffer(dir, "buffer")
	require.NoError(t, err)
	defer buffer.Close()
	require.NoError(t, checkUploadBuffer(dir, "buffer", buffer))

	require.NoError(t, os.Remove(filepath.Join(dirPath, "buffer")))
	recreated, err := createUploadBuffer(dir, "buffer")
	require.NoError(t, err)
	require.NoError(t, recreated.Close())
	require.ErrorIs(t, checkUploadBuffer(dir, "buffer", buffer), errUploadBufferReplaced)

	require.NoError(t, os.Remove(filepath.Join(dirPath, "buffer")))
	require.NoError(t, os.Symlink("elsewhere", filepath.Join(dirPath, "buffer")))
	require.ErrorIs(t, checkUploadBuffer(dir, "buffer", buffer), errUploadBufferReplaced)

	require.NoError(t, os.Remove(filepath.Join(dirPath, "buffer")))
	require.ErrorIs(t, checkUploadBuffer(dir, "buffer", buffer), fs.ErrNotExist)
}

// seedVictim is a file a swapped buffer name points at, standing in for
// /etc/passwd.
func seedVictim(t *testing.T) string {
	t.Helper()
	victim := filepath.Join(t.TempDir(), "victim")
	require.NoError(t, os.WriteFile(victim, []byte("victim"), 0o640))
	return victim
}

// plant points link at victim relatively: os.Root refuses absolute targets
// as escaping, so a relative link is what a path-based call would follow.
func plant(t *testing.T, victim, link string) {
	t.Helper()
	target, err := filepath.Rel(filepath.Dir(link), victim)
	require.NoError(t, err)
	require.NoError(t, os.Symlink(target, link))
}

func requireVictimUntouched(t *testing.T, victim string) {
	t.Helper()
	content, err := os.ReadFile(victim)
	require.NoError(t, err)
	require.Equal(t, "victim", string(content))
	requireOwnerAndMode(t, victim, uint32(os.Geteuid()), uint32(os.Getegid()), 0o640)
}

// The buffer's name sits in a folder the session user may write to, so the
// bridge never follows or trusts that name: it creates the buffer
// exclusively, writes and settles it only through the handle it holds open,
// and commits only while the name is still that file.
func TestUploadBufferNameIsNeverFollowed(t *testing.T) {
	t.Run("planted before the buffer is created", func(t *testing.T) {
		victim := seedVictim(t)
		target := filepath.Join(t.TempDir(), "docker-compose.yml")
		transfer := newOwnedUpload(t, target, 3, selfOwner(), false, false)
		partName := "." + filepath.Base(target) + ".linuxio-upload-" + transfer.task.ID() + ".part"
		plant(t, victim, filepath.Join(filepath.Dir(target), partName))

		require.Error(t, transfer.prepare(openTestRoot(t)))
		requireVictimUntouched(t, victim)
	})

	t.Run("deleted and replaced between chunks", func(t *testing.T) {
		victim := seedVictim(t)
		target := filepath.Join(t.TempDir(), "docker-compose.yml")
		transfer := newOwnedUpload(t, target, 3, selfOwner(), false, false)
		root := openTestRoot(t)
		require.NoError(t, transfer.prepare(root))
		buffer := "/" + transfer.tempRel

		require.NoError(t, os.Remove(buffer))
		plant(t, victim, buffer)
		_, err := transfer.buffer.WriteString("new")
		require.NoError(t, err)
		_, err = transfer.commit(root, target, transfer.tempRel, transfer.finalRel)
		require.ErrorIs(t, err, errUploadBufferReplaced)
		requireVictimUntouched(t, victim)
		require.NoFileExists(t, target)
	})

	t.Run("swapped while the buffer is open", func(t *testing.T) {
		victim := seedVictim(t)
		target := filepath.Join(t.TempDir(), "docker-compose.yml")
		owner := selfOwner()
		if os.Geteuid() == 0 {
			owner = sessionOwner{uid: 12345, gid: 12346, set: true}
			require.NoError(t, os.Chown(filepath.Dir(target), 12345, 12346))
		}
		transfer := newOwnedUpload(t, target, 3, owner, false, false)
		root := openTestRoot(t)
		require.NoError(t, transfer.prepare(root))
		_, err := transfer.buffer.WriteString("new")
		require.NoError(t, err)

		buffer := "/" + transfer.tempRel
		moved := buffer + ".moved"
		require.NoError(t, os.Rename(buffer, moved))
		plant(t, victim, buffer)
		_, err = transfer.commit(root, target, transfer.tempRel, transfer.finalRel)
		require.ErrorIs(t, err, errUploadBufferReplaced)

		requireVictimUntouched(t, victim)
		// Refused before it is handed over: the moved buffer keeps the
		// bridge's owner and its private mode, not owner's and 0644.
		requireOwnerAndMode(t, moved, uint32(os.Geteuid()), uint32(os.Getegid()), 0o600)
		require.NoFileExists(t, target, "the link at the buffer's name is not committed")
	})
}

// The upload holds its buffer open from prepare to the commit, so a process
// that can write the folder and, between two attaches of the data stream,
// deletes the buffer and keeps a file of its own open at the name receives
// none of the rest of the upload. The commit finds another file at the
// buffer's name and fails: the file root wrote has no name left to commit,
// and the one at the name is not root's.
func TestUploadBufferReplacedBetweenAttachesGetsNothing(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "notes.txt")
	req := apischema.FileUploadRequest{TargetPath: target, Size: "6"}
	task, _, err := bridgeipc.NewTaskService().ClaimForOwner(routeUpload, req, bridgeipc.TaskOwner{}, bridgeipc.TaskLifetimeSession, bridgeipc.TaskIdentity{})
	require.NoError(t, err)
	startTransferTask(t, task, func(ctx context.Context, j *bridgeipc.Task, _ any) (any, error) {
		return runUploadTask(ctx, j, req, sessionOwner{})
	})

	conn, _, attachErr := attachUploadBatchStream(t, task, "")
	writeData(t, conn, []byte("abc"))
	_ = conn.Close()
	waitDetached(t, attachErr)
	held := heldBuffer(registeredUpload(t, task.ID()))
	require.NotNil(t, held, "a parked upload keeps its buffer open")

	buffer := filepath.Join(dir, ".notes.txt.linuxio-upload-"+task.ID()+".part")
	require.NoError(t, os.Remove(buffer))
	planted, err := os.OpenFile(buffer, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	require.NoError(t, err)
	defer planted.Close()

	conn, results, _ := attachUploadBatchStream(t, task, "3")
	writeData(t, conn, []byte("def"))
	writeClose(t, conn)
	result := waitResult(t, results)
	require.Equal(t, "error", result.Status)
	require.Equal(t, 500, result.Code)
	require.Contains(t, result.Error, errUploadBufferReplaced.Error())
	require.Equal(t, bridgeipc.TaskStateFailed, waitTaskDone(t, task).State)

	info, err := planted.Stat()
	require.NoError(t, err)
	require.Zero(t, info.Size(), "the planted file receives none of the upload")
	require.NoFileExists(t, target)
	requireClosed(t, held)
}

// The task is the buffer's one owner: the commit closes it whether it lands
// or not, and the cleanup of a failed or cancelled upload closes it, so no
// upload leaves it open.
func TestUploadClosesItsBuffer(t *testing.T) {
	t.Run("on commit", func(t *testing.T) {
		target := filepath.Join(t.TempDir(), "notes.txt")
		transfer := newOwnedUpload(t, target, 3, sessionOwner{}, false, false)
		root := openTestRoot(t)
		require.NoError(t, transfer.prepare(root))
		held := transfer.buffer
		_, err := held.WriteString("new")
		require.NoError(t, err)
		_, err = transfer.commit(root, target, transfer.tempRel, transfer.finalRel)
		require.NoError(t, err)

		require.Nil(t, transfer.buffer)
		requireClosed(t, held)
		content, err := os.ReadFile(target)
		require.NoError(t, err)
		require.Equal(t, "new", string(content))
	})

	t.Run("on a failed commit", func(t *testing.T) {
		target := filepath.Join(t.TempDir(), "notes.txt")
		transfer := newOwnedUpload(t, target, 3, sessionOwner{}, false, false)
		root := openTestRoot(t)
		require.NoError(t, transfer.prepare(root))
		held := transfer.buffer
		require.NoError(t, os.WriteFile(target, []byte("appeared"), 0o644))
		code, err := transfer.commit(root, target, transfer.tempRel, transfer.finalRel)
		require.ErrorIs(t, err, fs.ErrExist)
		require.Equal(t, 409, code)

		require.Nil(t, transfer.buffer)
		requireClosed(t, held)
		transfer.cleanupPartial()
		require.Len(t, dirEntries(t, filepath.Dir(target)), 1, "the cleanup removes the buffer")
	})

	t.Run("on cancel", func(t *testing.T) {
		target := filepath.Join(t.TempDir(), "notes.txt")
		transfer := newOwnedUpload(t, target, 3, sessionOwner{}, false, false)
		require.NoError(t, transfer.prepare(openTestRoot(t)))
		held := transfer.buffer
		transfer.cleanupPartial()

		require.Nil(t, transfer.buffer)
		requireClosed(t, held)
		require.Empty(t, dirEntries(t, filepath.Dir(target)))
	})

	t.Run("when cancelled before the buffer is made", func(t *testing.T) {
		base := t.TempDir()
		transfer := newOwnedUpload(t, filepath.Join(base, "missing", "notes.txt"), 3, sessionOwner{}, false, false)
		transfer.cleanupPartial()

		require.ErrorIs(t, transfer.prepare(openTestRoot(t)), errUploadEnded)
		require.Nil(t, transfer.buffer)
		require.Empty(t, dirEntries(t, base), "no folder and no buffer is made once the upload ended")
	})

	t.Run("on a failed chunk", func(t *testing.T) {
		dir := t.TempDir()
		req := apischema.FileUploadRequest{TargetPath: filepath.Join(dir, "notes.txt"), Size: "3"}
		task, _, err := bridgeipc.NewTaskService().ClaimForOwner(routeUpload, req, bridgeipc.TaskOwner{}, bridgeipc.TaskLifetimeSession, bridgeipc.TaskIdentity{})
		require.NoError(t, err)
		startTransferTask(t, task, func(ctx context.Context, j *bridgeipc.Task, _ any) (any, error) {
			return runUploadTask(ctx, j, req, sessionOwner{})
		})
		conn, results, _ := attachUploadBatchStream(t, task, "")
		writeData(t, conn, []byte("ab"))
		transfer := registeredUpload(t, task.ID())
		held := heldBuffer(transfer)

		writeData(t, conn, []byte("cd")) // one byte past the size
		result := waitResult(t, results)
		require.Equal(t, "error", result.Status)
		require.Equal(t, 400, result.Code)
		require.Equal(t, bridgeipc.TaskStateFailed, waitTaskDone(t, task).State)

		require.Nil(t, heldBuffer(transfer))
		requireClosed(t, held)
		require.Empty(t, dirEntries(t, dir))
	})
}

// The buffer and its target share one folder, opened once and checked to be
// the folder prepare used, so the commit never lands anywhere else, even when
// that folder is swapped for a symlink (RENAME_EXCHANGE between the two path
// resolutions of a rename does the same).
func TestUploadCommitStaysInThePreparedFolder(t *testing.T) {
	t.Run("a folder swapped before the commit is refused", func(t *testing.T) {
		base := t.TempDir()
		stack := filepath.Join(base, "stack")
		prod := filepath.Join(base, "prod")
		require.NoError(t, os.Mkdir(prod, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(prod, ".env"), []byte("secret"), 0o600))
		env := filepath.Join(stack, ".env")
		transfer := newOwnedUpload(t, env, 3, selfOwner(), true, true)
		root := openTestRoot(t)
		require.NoError(t, transfer.prepare(root))
		_, err := transfer.buffer.WriteString("new")
		require.NoError(t, err)

		// Both names now resolve into prod, buffer included.
		partName := filepath.Base(transfer.tempRel)
		require.NoError(t, os.Rename(stack, stack+".moved"))
		require.NoError(t, os.Symlink("prod", stack))
		require.NoError(t, os.Rename(filepath.Join(stack+".moved", partName), filepath.Join(prod, partName)))

		_, err = transfer.commit(root, env, transfer.tempRel, transfer.finalRel)
		require.ErrorIs(t, err, errUploadFolderReplaced)
		content, err := os.ReadFile(filepath.Join(prod, ".env"))
		require.NoError(t, err)
		require.Equal(t, "secret", string(content))
	})

	t.Run("a folder swapped once open keeps the commit in it", func(t *testing.T) {
		base := t.TempDir()
		stack := filepath.Join(base, "stack")
		prod := filepath.Join(base, "prod")
		require.NoError(t, os.Mkdir(stack, 0o755))
		require.NoError(t, os.Mkdir(prod, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(stack, ".env.part"), []byte("new"), 0o600))
		dir, err := os.Open(stack)
		require.NoError(t, err)
		defer dir.Close()

		require.NoError(t, os.Rename(stack, stack+".moved"))
		require.NoError(t, os.Symlink("prod", stack))
		require.NoError(t, commitUploadBuffer(dir, ".env.part", ".env", true))

		content, err := os.ReadFile(filepath.Join(stack+".moved", ".env"))
		require.NoError(t, err)
		require.Equal(t, "new", string(content))
		require.NoFileExists(t, filepath.Join(prod, ".env"))
	})
}

// On a root bridge a session upload hands every entry it creates inside the
// user's own folders to the session user; everything else keeps item 110:
// folders that existed, entries made in a root-owned folder, overwrites and
// uploads that do not ask.
func TestUploadHandsNewEntriesToSessionUserAsRoot(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("handing entries to another user needs root")
	}
	const uid, gid = 12345, 12346
	session := sessionOwner{uid: uid, gid: gid, set: true}
	base := t.TempDir()
	home := filepath.Join(base, "home")
	srv := filepath.Join(base, "srv")
	require.NoError(t, os.Mkdir(home, 0o750))
	require.NoError(t, os.Chown(home, uid, gid))
	require.NoError(t, os.Mkdir(srv, 0o755))
	overwrite := true

	compose := filepath.Join(home, "stack", "docker-compose.yml")
	env := filepath.Join(home, "stack", ".env")
	private := true
	uploadThroughStream(t, apischema.FileUploadRequest{TargetPath: compose, Owner: uploadOwnerSession}, "services: {}\n", session)
	uploadThroughStream(t, apischema.FileUploadRequest{TargetPath: env, Owner: uploadOwnerSession, Private: &private, Overwrite: &overwrite}, "K=v\n", session)
	requireOwnerAndMode(t, home, uid, gid, 0o750)
	requireOwnerAndMode(t, filepath.Join(home, "stack"), uid, gid, 0o755)
	requireOwnerAndMode(t, compose, uid, gid, 0o644)
	requireOwnerAndMode(t, env, uid, gid, 0o600)

	shared := filepath.Join(base, "shared")
	require.NoError(t, os.Mkdir(shared, 0o750))
	require.NoError(t, os.Chown(shared, uid, 4242))
	require.NoError(t, os.Chmod(shared, 0o750|os.ModeSetgid))
	sharedCompose := filepath.Join(shared, "stack", "docker-compose.yml")
	uploadThroughStream(t, apischema.FileUploadRequest{TargetPath: sharedCompose, Owner: uploadOwnerSession}, "services: {}\n", session)
	requireOwnerAndMode(t, filepath.Join(shared, "stack"), uid, 4242, 0o755)
	info, err := os.Stat(filepath.Join(shared, "stack"))
	require.NoError(t, err)
	require.NotZero(t, info.Mode()&os.ModeSetgid)
	requireOwnerAndMode(t, sharedCompose, uid, 4242, 0o644)

	existing := filepath.Join(home, "stack", "existing.yml")
	require.NoError(t, os.WriteFile(existing, []byte("old"), 0o640))
	require.NoError(t, os.Chown(existing, 23456, 23457))
	uploadThroughStream(t, apischema.FileUploadRequest{TargetPath: existing, Owner: uploadOwnerSession, Overwrite: &overwrite}, "new", session)
	requireOwnerAndMode(t, existing, 23456, 23457, 0o640)

	system := filepath.Join(srv, "stack", "docker-compose.yml")
	uploadThroughStream(t, apischema.FileUploadRequest{TargetPath: system, Owner: uploadOwnerSession}, "services: {}\n", session)
	requireOwner(t, filepath.Join(srv, "stack"), 0, uint32(os.Getegid()))
	requireOwnerAndMode(t, system, 0, uint32(os.Getegid()), 0o664)

	plain := filepath.Join(home, "plain", "notes.txt")
	uploadThroughStream(t, apischema.FileUploadRequest{TargetPath: plain}, "notes", session)
	requireOwner(t, filepath.Join(home, "plain"), 0, uint32(os.Getegid()))
	requireOwnerAndMode(t, plain, 0, uint32(os.Getegid()), 0o664)
}

// An upload with an owner, the Compose save, reaches its folder, and makes
// any missing one, without following a link in a folder the session user can
// write: a process of theirs could have replaced ~/docker/web with a link to
// another folder root can write. On a bridge that is not root an upload acts
// as the user's own save and follows the user's link.
func TestOwnedUploadNeverGoesThroughALinkTheUserCouldPlant(t *testing.T) {
	home, victim := t.TempDir(), t.TempDir()
	plant(t, victim, filepath.Join(home, "docker"))
	root := openTestRoot(t)

	for _, target := range []string{filepath.Join(home, "docker", "compose.yml"), filepath.Join(home, "docker", "web", "compose.yml")} {
		err := newOwnedUpload(t, target, 3, selfOwner(), false, false).prepare(root)
		require.ErrorIs(t, err, bridgeipc.ErrForbidden, target)
		_, err = openUploadDir(root, fsroot.ToRel(filepath.Dir(target)), selfOwner().asOwner(), selfOwner())
		require.ErrorIs(t, err, bridgeipc.ErrForbidden, target)
		require.Empty(t, dirEntries(t, victim))
	}

	uploadThroughHandles(t, newOwnedUpload(t, filepath.Join(home, "docker", "compose.yml"), 3, sessionOwner{}, false, false), "abc")
	require.FileExists(t, filepath.Join(victim, "compose.yml"))
}

// An upload without an owner, a plain upload or an editor save, on a root
// bridge reaches its folder under the same rule: a process of the session
// user could have replaced ~/docs with a link. A save writes through a link
// at the file only where that user cannot have planted it, and the refusal
// names where the link points.
func TestUploadWithoutOwnerNeverGoesThroughALinkTheUserCouldPlant(t *testing.T) {
	home, victim := t.TempDir(), t.TempDir()
	secret := filepath.Join(victim, "secret.txt")
	require.NoError(t, os.WriteFile(secret, []byte("secret"), 0o600))
	docs := filepath.Join(home, "docs")
	plant(t, victim, docs)
	plant(t, secret, filepath.Join(home, "notes.txt"))
	root := openTestRoot(t)
	session := selfOwner().asOwner()

	for _, target := range []string{filepath.Join(docs, "new.txt"), filepath.Join(docs, "sub", "new.txt"), filepath.Join(home, "notes.txt")} {
		transfer := newOwnedUpload(t, target, 3, sessionOwner{}, false, true)
		transfer.linkRule = session
		err := transfer.prepare(root)
		require.ErrorIs(t, err, bridgeipc.ErrForbidden, target)
		require.ErrorContains(t, err, " is a link to ", target)
	}
	transfer := newOwnedUpload(t, filepath.Join(docs, "new.txt"), 3, sessionOwner{}, false, false)
	transfer.linkRule = session
	require.ErrorContains(t, transfer.prepare(root), docs+" is a link to "+victim+". Open that folder to make this change")

	batch, batchRoot := newOwnedBatchUpload(t, apischema.FileUploadBatchRequest{
		Destination: home,
		Files:       []apischema.FileUploadBatchEntry{{Path: "docs/new.txt", Size: "3"}},
		Directories: []string{"docs/made"},
	}, sessionOwner{})
	batch.linkRule = session
	batch.prepareDirectories(batchRoot)
	require.Len(t, batch.failures, 1)
	_, err := batch.ensurePrepared(batchRoot)
	require.ErrorIs(t, err, bridgeipc.ErrForbidden)

	entries := dirEntries(t, victim)
	require.Len(t, entries, 1)
	require.Equal(t, "secret.txt", entries[0].Name())
	content, err := os.ReadFile(secret)
	require.NoError(t, err)
	require.Equal(t, "secret", string(content))
}

// A link the session user cannot have planted, one in a folder only root can
// write such as /home -> /var/home, still takes an owned upload; one in a
// folder of theirs does not.
func TestOwnedUploadFollowsOnlyALinkInARootFolderAsRoot(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("handing entries to another user needs root")
	}
	const uid, gid = 12345, 12346
	session := sessionOwner{uid: uid, gid: gid, set: true}
	base := t.TempDir()
	home := filepath.Join(base, "home")
	require.NoError(t, os.Mkdir(home, 0o750))
	require.NoError(t, os.Chown(home, uid, gid))
	actual := filepath.Join(base, "real")
	require.NoError(t, os.Mkdir(actual, 0o755))
	plant(t, actual, filepath.Join(base, "alias"))
	plant(t, actual, filepath.Join(home, "docker"))
	root := openTestRoot(t)

	err := newOwnedUpload(t, filepath.Join(home, "docker", "compose.yml"), 3, session, false, false).prepare(root)
	require.ErrorIs(t, err, bridgeipc.ErrForbidden)
	require.Empty(t, dirEntries(t, actual))

	uploadThroughStream(t, apischema.FileUploadRequest{TargetPath: filepath.Join(base, "alias", "stack", "compose.yml"), Owner: uploadOwnerSession}, "abc", session)
	require.FileExists(t, filepath.Join(actual, "stack", "compose.yml"))

	// An upload without owner is reached for the session user the same way.
	plain := newOwnedUpload(t, filepath.Join(home, "docker", "plain.txt"), 3, sessionOwner{}, false, false)
	plain.linkRule = session.asOwner()
	require.ErrorIs(t, plain.prepare(root), bridgeipc.ErrForbidden)
	uploadThroughStream(t, apischema.FileUploadRequest{TargetPath: filepath.Join(base, "alias", "plain.txt")}, "abc", session)
	require.FileExists(t, filepath.Join(actual, "plain.txt"))
}

func dirEntries(t *testing.T, dir string) []os.DirEntry {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	return entries
}
