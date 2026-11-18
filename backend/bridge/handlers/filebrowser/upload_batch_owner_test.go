package filebrowser

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/fsroot"
)

// newOwnedBatchUpload builds the transfer runUploadBatchTask builds for req,
// with newOwner already resolved from the request and the session, and the
// filesystem root attach opens.
func newOwnedBatchUpload(t *testing.T, req apischema.FileUploadBatchRequest, newOwner sessionOwner) (*uploadBatchTransferTask, *fsroot.FSRoot) {
	t.Helper()
	destination, files, directories, total, err := parseUploadBatchRequest(req)
	require.NoError(t, err)
	task, _, err := bridgeipc.NewTaskService().ClaimForOwnerWithIdentity(routeUploadBatch, req, bridgeipc.TaskOwner{}, bridgeipc.TaskLifetimeSession, bridgeipc.TaskIdentity{})
	require.NoError(t, err)
	transfer := &uploadBatchTransferTask{
		transferLifecycle: &transferLifecycle{task: task},
		ctx:               t.Context(),
		destination:       destination,
		files:             files,
		directories:       directories,
		total:             total,
		overwrite:         req.Overwrite != nil && *req.Overwrite,
		newOwner:          newOwner,
		linkRule:          newOwner.asOwner(),
		private:           req.Private != nil && *req.Private,
	}
	t.Cleanup(func() {
		if transfer.buffer != nil {
			_ = transfer.buffer.Close()
		}
	})
	return transfer, openTestRoot(t)
}

// batchThroughHandles drives what attach and consume run for the manifest:
// the directories, then each file's buffer writes and commit in turn.
func batchThroughHandles(t *testing.T, transfer *uploadBatchTransferTask, root *fsroot.FSRoot, contents ...string) {
	t.Helper()
	transfer.prepareDirectories(root)
	for _, content := range contents {
		if content != "" {
			require.NoError(t, transfer.writeChunk(root, []byte(content)))
		}
		transfer.fileBytes = int64(len(content))
		transfer.settle(root)
	}
	require.Empty(t, transfer.failures)
}

// uploadBatchThroughStream runs a whole filebrowser.upload_batch task, one
// manifest file per content, on a bridge whose session user is session.
func uploadBatchThroughStream(t *testing.T, req apischema.FileUploadBatchRequest, session sessionOwner, contents ...string) {
	t.Helper()
	for i, content := range contents {
		req.Files[i].Size = strconv.Itoa(len(content))
	}
	task := startUploadBatchTaskAs(t, req, session)
	conn, results, _ := attachUploadBatchStream(t, task, "")
	if data := strings.Join(contents, ""); data != "" {
		writeData(t, conn, []byte(data))
	}
	writeClose(t, conn)
	result := waitResult(t, results)
	require.Equalf(t, "ok", result.Status, "batch upload to %s answered %+v", req.Destination, result)
	require.Empty(t, decodeUploadBatchResult(t, result).Failed)
	require.Equal(t, bridgeipc.TaskStateCompleted, waitTaskDone(t, task).State)
}

func batchBufferName(transfer *uploadBatchTransferTask, name string, index int) string {
	return fmt.Sprintf(".%s.linuxio-upload-%s-%d.part", name, transfer.task.ID(), index)
}

func TestUploadBatchRejectsUnknownOwner(t *testing.T) {
	req := apischema.FileUploadBatchRequest{
		Destination: t.TempDir(),
		Files:       []apischema.FileUploadBatchEntry{{Path: "f.txt", Size: "1"}},
		Owner:       "root",
	}
	task, _, err := bridgeipc.NewTaskService().ClaimForOwnerWithIdentity(routeUploadBatch, req, bridgeipc.TaskOwner{}, bridgeipc.TaskLifetimeSession, bridgeipc.TaskIdentity{})
	require.NoError(t, err)

	_, err = runUploadBatchTask(context.Background(), task, req, sessionOwner{uid: 1000, gid: 1000, set: true})
	require.ErrorIs(t, err, bridgeipc.ErrInvalidArgs)
}

// New folders, manifest folders included, get 0755 and new files 0644 (0600
// when private) through their handles; the folder that already existed keeps
// its own mode.
func TestUploadBatchHandsNewEntriesOverWithHandOverModes(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	require.NoError(t, os.Mkdir(home, 0o750))
	owner := selfOwner()

	transfer, root := newOwnedBatchUpload(t, apischema.FileUploadBatchRequest{
		Destination: home,
		Files:       []apischema.FileUploadBatchEntry{{Path: "stack/sub/docker-compose.yml", Size: "4"}, {Path: "stack/empty.txt", Size: "0"}},
		Directories: []string{"stack/data/cache"},
	}, owner)
	batchThroughHandles(t, transfer, root, "a: \n", "")
	private := true
	transfer, root = newOwnedBatchUpload(t, apischema.FileUploadBatchRequest{
		Destination: home,
		Files:       []apischema.FileUploadBatchEntry{{Path: "stack/.env", Size: "4"}},
		Private:     &private,
	}, owner)
	batchThroughHandles(t, transfer, root, "K=v\n")

	requireOwnerAndMode(t, home, owner.uid, owner.gid, 0o750)
	for _, dir := range []string{"stack", "stack/sub", "stack/data", "stack/data/cache"} {
		requireOwnerAndMode(t, filepath.Join(home, dir), owner.uid, owner.gid, 0o755)
	}
	requireOwnerAndMode(t, filepath.Join(home, "stack/sub/docker-compose.yml"), owner.uid, owner.gid, 0o644)
	requireOwnerAndMode(t, filepath.Join(home, "stack/empty.txt"), owner.uid, owner.gid, 0o644)
	requireOwnerAndMode(t, filepath.Join(home, "stack/.env"), owner.uid, owner.gid, 0o600)
}

// Nothing is handed to a user inside a folder that is not already theirs. An
// unprivileged test would fail with EPERM if a chown to the other uid ran.
func TestUploadBatchDoesNotHandOverInAFolderTheUserDoesNotOwn(t *testing.T) {
	base := t.TempDir()
	other := sessionOwner{uid: uint32(os.Getuid()) + 1, gid: uint32(os.Getgid()) + 1, set: true}

	transfer, root := newOwnedBatchUpload(t, apischema.FileUploadBatchRequest{
		Destination: base,
		Files:       []apischema.FileUploadBatchEntry{{Path: "stack/docker-compose.yml", Size: "4"}},
		Directories: []string{"data"},
	}, other)
	batchThroughHandles(t, transfer, root, "a: \n")

	requireOwner(t, filepath.Join(base, "stack"), uint32(os.Geteuid()), uint32(os.Getegid()))
	requireOwner(t, filepath.Join(base, "data"), uint32(os.Geteuid()), uint32(os.Getegid()))
	requireOwnerAndMode(t, filepath.Join(base, "stack/docker-compose.yml"), uint32(os.Geteuid()), uint32(os.Getegid()), 0o664)
}

// A file already at a buffer's name was not made by this upload: the create
// fails with EEXIST and leaves it as it was, and the rest of the batch lands.
func TestUploadBatchLeavesAFileAtTheBufferNameAlone(t *testing.T) {
	dest := t.TempDir()
	req := apischema.FileUploadBatchRequest{
		Destination: dest,
		Files:       []apischema.FileUploadBatchEntry{{Path: "a.txt", Size: "3"}, {Path: "b.txt", Size: "3"}},
	}
	task := startUploadBatchTask(t, req)
	planted := filepath.Join(dest, fmt.Sprintf(".a.txt.linuxio-upload-%s-0.part", task.ID()))
	require.NoError(t, os.WriteFile(planted, []byte("planted"), 0o640))

	conn, results, _ := attachUploadBatchStream(t, task, "")
	writeData(t, conn, []byte("newnew"))
	writeClose(t, conn)
	result := waitResult(t, results)
	require.Equal(t, "ok", result.Status)
	decoded := decodeUploadBatchResult(t, result)
	require.Equal(t, 1, decoded.Succeeded)
	require.Len(t, decoded.Failed, 1)
	require.Equal(t, "a.txt", decoded.Failed[0].Path)
	waitTaskDone(t, task)

	content, err := os.ReadFile(planted)
	require.NoError(t, err)
	require.Equal(t, "planted", string(content))
	requireOwnerAndMode(t, planted, uint32(os.Geteuid()), uint32(os.Getegid()), 0o640)
	require.NoFileExists(t, filepath.Join(dest, "a.txt"))
	content, err = os.ReadFile(filepath.Join(dest, "b.txt"))
	require.NoError(t, err)
	require.Equal(t, "new", string(content))
}

// The batch keeps each buffer the way the single upload does: created
// exclusively, held open and written and settled only through that handle,
// and committed in the folder it was created in only while its name is still
// that file.
func TestUploadBatchBufferNameIsNeverFollowed(t *testing.T) {
	composeBatch := func(t *testing.T, dest string, owner sessionOwner) (*uploadBatchTransferTask, *fsroot.FSRoot) {
		return newOwnedBatchUpload(t, apischema.FileUploadBatchRequest{
			Destination: dest,
			Files:       []apischema.FileUploadBatchEntry{{Path: "docker-compose.yml", Size: "3"}},
		}, owner)
	}

	t.Run("planted before the buffer is created", func(t *testing.T) {
		victim := seedVictim(t)
		dest := t.TempDir()
		transfer, root := composeBatch(t, dest, selfOwner())
		plant(t, victim, filepath.Join(dest, batchBufferName(transfer, "docker-compose.yml", 0)))

		require.ErrorIs(t, transfer.writeChunk(root, []byte("new")), syscall.EEXIST)
		requireVictimUntouched(t, victim)
	})

	t.Run("deleted and replaced between chunks", func(t *testing.T) {
		victim := seedVictim(t)
		dest := t.TempDir()
		transfer, root := composeBatch(t, dest, selfOwner())
		_, err := transfer.ensurePrepared(root)
		require.NoError(t, err)
		buffer := "/" + transfer.tempRel

		require.NoError(t, os.Remove(buffer))
		plant(t, victim, buffer)
		require.NoError(t, transfer.writeChunk(root, []byte("new")))
		requireVictimUntouched(t, victim)
		transfer.fileBytes = 3
		require.ErrorIs(t, transfer.finalizeCurrent(root), errUploadBufferReplaced)
		requireVictimUntouched(t, victim)
		require.NoFileExists(t, filepath.Join(dest, "docker-compose.yml"))
	})

	t.Run("swapped while the buffer is open", func(t *testing.T) {
		victim := seedVictim(t)
		dest := t.TempDir()
		owner := selfOwner()
		if os.Geteuid() == 0 {
			owner = sessionOwner{uid: 12345, gid: 12346, set: true}
			require.NoError(t, os.Chown(dest, 12345, 12346))
		}
		transfer, root := composeBatch(t, dest, owner)
		require.NoError(t, transfer.writeChunk(root, []byte("new")))
		transfer.fileBytes = 3

		buffer := "/" + transfer.tempRel
		moved := buffer + ".moved"
		require.NoError(t, os.Rename(buffer, moved))
		plant(t, victim, buffer)
		require.ErrorIs(t, transfer.finalizeCurrent(root), errUploadBufferReplaced)

		requireVictimUntouched(t, victim)
		// Refused before it is handed over: the moved buffer keeps the
		// bridge's owner and its private mode, not owner's and 0644.
		requireOwnerAndMode(t, moved, uint32(os.Geteuid()), uint32(os.Getegid()), 0o600)
		require.NoFileExists(t, filepath.Join(dest, "docker-compose.yml"), "the link at the buffer's name is not committed")
	})

	t.Run("a folder swapped before the commit is refused", func(t *testing.T) {
		base := t.TempDir()
		stack := filepath.Join(base, "stack")
		prod := filepath.Join(base, "prod")
		require.NoError(t, os.Mkdir(prod, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(prod, ".env"), []byte("secret"), 0o600))
		overwrite := true
		transfer, root := newOwnedBatchUpload(t, apischema.FileUploadBatchRequest{
			Destination: base,
			Files:       []apischema.FileUploadBatchEntry{{Path: "stack/.env", Size: "3"}},
			Overwrite:   &overwrite,
		}, selfOwner())
		require.NoError(t, transfer.writeChunk(root, []byte("new")))
		transfer.fileBytes = 3

		// Both names now resolve into prod, buffer included.
		partName := filepath.Base(transfer.tempRel)
		require.NoError(t, os.Rename(stack, stack+".moved"))
		require.NoError(t, os.Symlink("prod", stack))
		require.NoError(t, os.Rename(filepath.Join(stack+".moved", partName), filepath.Join(prod, partName)))

		require.ErrorIs(t, transfer.finalizeCurrent(root), errUploadFolderReplaced)
		content, err := os.ReadFile(filepath.Join(prod, ".env"))
		require.NoError(t, err)
		require.Equal(t, "secret", string(content))
	})
}

// A batch holds the buffer of the file it is receiving and no other: the
// commit of one file closes its buffer before the next file's is made, and
// every chunk of a file goes to the buffer made for it.
func TestUploadBatchHoldsOneBufferAtATime(t *testing.T) {
	dest := t.TempDir()
	transfer, root := newOwnedBatchUpload(t, apischema.FileUploadBatchRequest{
		Destination: dest,
		Files:       []apischema.FileUploadBatchEntry{{Path: "a.txt", Size: "3"}, {Path: "b.txt", Size: "3"}},
	}, sessionOwner{})

	require.NoError(t, transfer.writeChunk(root, []byte("abc")))
	first := transfer.buffer
	require.NotNil(t, first)
	transfer.fileBytes = 3
	transfer.settle(root)
	require.Nil(t, transfer.buffer)
	requireClosed(t, first)

	require.NoError(t, transfer.writeChunk(root, []byte("de")))
	second := transfer.buffer
	require.NotNil(t, second)
	require.NotEqual(t, first.Name(), second.Name())
	require.NoError(t, transfer.writeChunk(root, []byte("f")))
	require.Same(t, second, transfer.buffer)
	transfer.fileBytes = 3
	transfer.settle(root)
	require.Nil(t, transfer.buffer)
	requireClosed(t, second)

	require.Empty(t, transfer.failures)
	for name, want := range map[string]string{"a.txt": "abc", "b.txt": "def"} {
		content, err := os.ReadFile(filepath.Join(dest, name))
		require.NoError(t, err)
		require.Equal(t, want, string(content))
	}
	require.Len(t, dirEntries(t, dest), 2, "no buffer is left behind")
}

// The cleanup of a failed item and of a failed or cancelled batch closes the
// buffer the batch holds, and an attach still running after the cleanup
// makes no new one.
func TestUploadBatchClosesItsBuffer(t *testing.T) {
	newBatch := func(t *testing.T) (*uploadBatchTransferTask, *fsroot.FSRoot, string) {
		dest := t.TempDir()
		transfer, root := newOwnedBatchUpload(t, apischema.FileUploadBatchRequest{
			Destination: dest,
			Files:       []apischema.FileUploadBatchEntry{{Path: "a.txt", Size: "3"}, {Path: "b.txt", Size: "3"}},
		}, sessionOwner{})
		return transfer, root, dest
	}

	t.Run("when an item fails", func(t *testing.T) {
		transfer, root, dest := newBatch(t)
		require.NoError(t, transfer.writeChunk(root, []byte("ab")))
		held := transfer.buffer
		transfer.failCurrent(root, errors.New("disk full"))

		require.Nil(t, transfer.buffer)
		requireClosed(t, held)
		require.Empty(t, dirEntries(t, dest))
		require.NoError(t, transfer.writeChunk(root, []byte("c")), "the rest of a failed item is discarded")
		require.Nil(t, transfer.buffer)
	})

	t.Run("makes no folder or buffer once the batch ended", func(t *testing.T) {
		dest := t.TempDir()
		transfer, root := newOwnedBatchUpload(t, apischema.FileUploadBatchRequest{
			Destination: dest,
			Files:       []apischema.FileUploadBatchEntry{{Path: "sub/a.txt", Size: "3"}},
			Directories: []string{"made"},
		}, sessionOwner{})
		transfer.cleanupPartial()

		transfer.prepareDirectories(root)
		_, err := transfer.ensurePrepared(root)
		require.ErrorIs(t, err, errUploadEnded)
		require.ErrorIs(t, transfer.writeChunk(root, []byte("abc")), errUploadEnded)
		require.Nil(t, transfer.buffer)
		require.Empty(t, dirEntries(t, dest))
	})

	t.Run("when the batch is cancelled", func(t *testing.T) {
		transfer, root, dest := newBatch(t)
		require.NoError(t, transfer.writeChunk(root, []byte("ab")))
		held := transfer.buffer
		transfer.cleanupPartial()

		require.Nil(t, transfer.buffer)
		requireClosed(t, held)
		require.Empty(t, dirEntries(t, dest))
		require.ErrorIs(t, transfer.writeChunk(root, []byte("c")), errUploadEnded)
		require.Nil(t, transfer.buffer)
		require.Empty(t, dirEntries(t, dest))
	})
}

// As the single upload does, a batch writes every chunk of a file through the
// buffer it holds, so a file put at the buffer's name between two attaches
// receives none of it; that item fails and the rest of the batch, whose next
// file gets a buffer of its own, lands.
func TestUploadBatchBufferReplacedBetweenAttachesGetsNothing(t *testing.T) {
	dest := t.TempDir()
	req := apischema.FileUploadBatchRequest{
		Destination: dest,
		Files:       []apischema.FileUploadBatchEntry{{Path: "a.txt", Size: "6"}, {Path: "b.txt", Size: "3"}},
	}
	task := startUploadBatchTask(t, req)

	conn, _, attachErr := attachUploadBatchStream(t, task, "")
	writeData(t, conn, []byte("abc"))
	_ = conn.Close()
	waitDetached(t, attachErr)

	buffer := filepath.Join(dest, fmt.Sprintf(".a.txt.linuxio-upload-%s-0.part", task.ID()))
	require.NoError(t, os.Remove(buffer))
	planted, err := os.OpenFile(buffer, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	require.NoError(t, err)
	defer planted.Close()

	conn, results, _ := attachUploadBatchStream(t, task, "3")
	writeData(t, conn, []byte("defghi"))
	writeClose(t, conn)
	result := waitResult(t, results)
	require.Equal(t, "ok", result.Status)
	decoded := decodeUploadBatchResult(t, result)
	require.Equal(t, 1, decoded.Succeeded)
	require.Len(t, decoded.Failed, 1)
	require.Equal(t, "a.txt", decoded.Failed[0].Path)
	require.Contains(t, decoded.Failed[0].Error, errUploadBufferReplaced.Error())
	waitTaskDone(t, task)

	info, err := planted.Stat()
	require.NoError(t, err)
	require.Zero(t, info.Size(), "the planted file receives none of the upload")
	require.NoFileExists(t, filepath.Join(dest, "a.txt"))
	content, err := os.ReadFile(filepath.Join(dest, "b.txt"))
	require.NoError(t, err)
	require.Equal(t, "ghi", string(content))
}

// On a root bridge a session batch upload hands every entry it creates inside
// the user's own folders to the session user; everything else keeps item
// 110: folders that existed, entries made in a root-owned folder, overwrites
// and uploads that do not ask.
func TestUploadBatchHandsNewEntriesToSessionUserAsRoot(t *testing.T) {
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
	overwrite, private := true, true
	files := func(paths ...string) []apischema.FileUploadBatchEntry {
		entries := make([]apischema.FileUploadBatchEntry, len(paths))
		for i, path := range paths {
			entries[i] = apischema.FileUploadBatchEntry{Path: path}
		}
		return entries
	}

	uploadBatchThroughStream(t, apischema.FileUploadBatchRequest{
		Destination: home, Owner: uploadOwnerSession,
		Files:       files("stack/docker-compose.yml", "stack/sub/notes.txt"),
		Directories: []string{"stack/data"},
	}, session, "services: {}\n", "notes")
	uploadBatchThroughStream(t, apischema.FileUploadBatchRequest{
		Destination: home, Owner: uploadOwnerSession, Private: &private, Overwrite: &overwrite,
		Files: files("stack/.env"),
	}, session, "K=v\n")
	requireOwnerAndMode(t, home, uid, gid, 0o750)
	for _, dir := range []string{"stack", "stack/sub", "stack/data"} {
		requireOwnerAndMode(t, filepath.Join(home, dir), uid, gid, 0o755)
	}
	requireOwnerAndMode(t, filepath.Join(home, "stack/docker-compose.yml"), uid, gid, 0o644)
	requireOwnerAndMode(t, filepath.Join(home, "stack/sub/notes.txt"), uid, gid, 0o644)
	requireOwnerAndMode(t, filepath.Join(home, "stack/.env"), uid, gid, 0o600)

	shared := filepath.Join(base, "shared")
	require.NoError(t, os.Mkdir(shared, 0o750))
	require.NoError(t, os.Chown(shared, uid, 4242))
	require.NoError(t, os.Chmod(shared, 0o750|os.ModeSetgid))
	uploadBatchThroughStream(t, apischema.FileUploadBatchRequest{
		Destination: shared, Owner: uploadOwnerSession, Files: files("stack/docker-compose.yml"),
	}, session, "services: {}\n")
	requireOwnerAndMode(t, filepath.Join(shared, "stack"), uid, 4242, 0o755)
	info, err := os.Stat(filepath.Join(shared, "stack"))
	require.NoError(t, err)
	require.NotZero(t, info.Mode()&os.ModeSetgid)
	requireOwnerAndMode(t, filepath.Join(shared, "stack/docker-compose.yml"), uid, 4242, 0o644)

	existing := filepath.Join(home, "stack", "existing.yml")
	require.NoError(t, os.WriteFile(existing, []byte("old"), 0o640))
	require.NoError(t, os.Chown(existing, 23456, 23457))
	uploadBatchThroughStream(t, apischema.FileUploadBatchRequest{
		Destination: home, Owner: uploadOwnerSession, Overwrite: &overwrite, Files: files("stack/existing.yml"),
	}, session, "new")
	requireOwnerAndMode(t, existing, 23456, 23457, 0o640)

	uploadBatchThroughStream(t, apischema.FileUploadBatchRequest{
		Destination: srv, Owner: uploadOwnerSession, Files: files("stack/docker-compose.yml"),
	}, session, "services: {}\n")
	requireOwner(t, filepath.Join(srv, "stack"), 0, uint32(os.Getegid()))
	requireOwnerAndMode(t, filepath.Join(srv, "stack/docker-compose.yml"), 0, uint32(os.Getegid()), 0o664)

	uploadBatchThroughStream(t, apischema.FileUploadBatchRequest{
		Destination: home, Files: files("plain/notes.txt"),
	}, session, "notes")
	requireOwner(t, filepath.Join(home, "plain"), 0, uint32(os.Getegid()))
	requireOwnerAndMode(t, filepath.Join(home, "plain/notes.txt"), 0, uint32(os.Getegid()), 0o664)
}

// The commit's renameat replaces a symlink at the target, not the file it
// points at, so that file must lend the upload neither its owner nor its mode:
// a link planted at ~/dl/tool -> /usr/bin/sudo must not turn an admin's
// overwrite into a setuid-root copy of the uploaded content.
func TestUploadBatchOverwriteOfASymlinkTakesNothingFromItsTarget(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "sudo")
	require.NoError(t, os.WriteFile(target, []byte("victim"), 0o755))
	require.NoError(t, os.Chmod(target, 0o755|os.ModeSetuid))
	dl := filepath.Join(base, "dl")
	require.NoError(t, os.Mkdir(dl, 0o755))
	require.NoError(t, os.Symlink("../sudo", filepath.Join(dl, "tool")))
	overwrite := true

	uploadBatchThroughStream(t, apischema.FileUploadBatchRequest{
		Destination: dl,
		Files:       []apischema.FileUploadBatchEntry{{Path: "tool"}},
		Overwrite:   &overwrite,
	}, sessionOwner{}, "new")

	tool := filepath.Join(dl, "tool")
	info, err := os.Lstat(tool)
	require.NoError(t, err)
	require.True(t, info.Mode().IsRegular(), "the link itself is replaced")
	require.Zero(t, info.Mode()&(os.ModeSetuid|os.ModeSetgid), "mode of %s", tool)
	requireOwnerAndMode(t, tool, uint32(os.Geteuid()), uint32(os.Getegid()), 0o664)
	content, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, "victim", string(content))
	info, err = os.Stat(target)
	require.NoError(t, err)
	require.Equal(t, 0o755|os.ModeSetuid, info.Mode()&(os.ModePerm|os.ModeSetuid))
}

// A batch that sets owner refuses a symlink at a file's target like the single
// upload does, instead of following or silently replacing it.
func TestUploadBatchWithOwnerRefusesASymlinkAtTheTarget(t *testing.T) {
	victim := seedVictim(t)
	stack := t.TempDir()
	env := filepath.Join(stack, ".env")
	plant(t, victim, env)
	overwrite := true

	task := startUploadBatchTaskAs(t, apischema.FileUploadBatchRequest{
		Destination: stack,
		Files:       []apischema.FileUploadBatchEntry{{Path: ".env", Size: "3"}},
		Overwrite:   &overwrite,
		Owner:       uploadOwnerSession,
	}, selfOwner())
	conn, results, _ := attachUploadBatchStream(t, task, "")
	writeData(t, conn, []byte("new"))
	writeClose(t, conn)

	result := waitResult(t, results)
	require.Equalf(t, "ok", result.Status, "batch answered %+v", result)
	failed := decodeUploadBatchResult(t, result).Failed
	require.Len(t, failed, 1)
	require.Contains(t, failed[0].Error, ".env is a symbolic link; replace it with a regular file")
	requireVictimUntouched(t, victim)
	info, err := os.Lstat(env)
	require.NoError(t, err)
	require.NotZero(t, info.Mode()&os.ModeSymlink, "the link is left alone")
	entries, err := os.ReadDir(stack)
	require.NoError(t, err)
	require.Len(t, entries, 1, "no upload buffer is left behind")
	require.Equal(t, ".env", entries[0].Name())
}
