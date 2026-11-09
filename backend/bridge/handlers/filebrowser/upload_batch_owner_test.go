package filebrowser

import (
	"context"
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
)

// newOwnedBatchUpload builds the transfer runUploadBatchTask builds for req,
// with newOwner already resolved from the request and the session, and the
// per-attach session attach opens.
func newOwnedBatchUpload(t *testing.T, req apischema.FileUploadBatchRequest, newOwner sessionOwner) (*uploadBatchTransferTask, *uploadBatchSession) {
	t.Helper()
	destination, files, directories, total, err := parseUploadBatchRequest(req)
	require.NoError(t, err)
	task, _, err := bridgeipc.NewTaskService().ClaimForOwnerWithIdentity(routeUploadBatch, req, bridgeipc.TaskOwner{}, bridgeipc.TaskLifetimeSession, bridgeipc.TaskIdentity{})
	require.NoError(t, err)
	transfer := &uploadBatchTransferTask{
		transferLifecycle: &transferLifecycle{task: task},
		destination:       destination,
		files:             files,
		directories:       directories,
		total:             total,
		overwrite:         req.Overwrite != nil && *req.Overwrite,
		newOwner:          newOwner,
		private:           req.Private != nil && *req.Private,
	}
	session := &uploadBatchSession{root: openTestRoot(t)}
	t.Cleanup(session.closeFile)
	return transfer, session
}

// batchThroughHandles drives what attach and consume run for the manifest:
// the directories, then each file's buffer writes and commit in turn.
func batchThroughHandles(t *testing.T, transfer *uploadBatchTransferTask, session *uploadBatchSession, contents ...string) {
	t.Helper()
	transfer.prepareDirectories(session.root)
	for _, content := range contents {
		if content != "" {
			require.NoError(t, transfer.writeChunk(session, []byte(content)))
		}
		transfer.fileBytes = int64(len(content))
		transfer.settle(session)
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

	transfer, session := newOwnedBatchUpload(t, apischema.FileUploadBatchRequest{
		Destination: home,
		Files:       []apischema.FileUploadBatchEntry{{Path: "stack/sub/docker-compose.yml", Size: "4"}, {Path: "stack/empty.txt", Size: "0"}},
		Directories: []string{"stack/data/cache"},
	}, owner)
	batchThroughHandles(t, transfer, session, "a: \n", "")
	private := true
	transfer, session = newOwnedBatchUpload(t, apischema.FileUploadBatchRequest{
		Destination: home,
		Files:       []apischema.FileUploadBatchEntry{{Path: "stack/.env", Size: "4"}},
		Private:     &private,
	}, owner)
	batchThroughHandles(t, transfer, session, "K=v\n")

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

	transfer, session := newOwnedBatchUpload(t, apischema.FileUploadBatchRequest{
		Destination: base,
		Files:       []apischema.FileUploadBatchEntry{{Path: "stack/docker-compose.yml", Size: "4"}},
		Directories: []string{"data"},
	}, other)
	batchThroughHandles(t, transfer, session, "a: \n")

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
// exclusively, reopened without following a symlink and only while it is the
// same file, settled through the open handle, and committed in the folder it
// was created in.
func TestUploadBatchBufferNameIsNeverFollowed(t *testing.T) {
	composeBatch := func(t *testing.T, dest string, owner sessionOwner) (*uploadBatchTransferTask, *uploadBatchSession) {
		private := true
		return newOwnedBatchUpload(t, apischema.FileUploadBatchRequest{
			Destination: dest,
			Files:       []apischema.FileUploadBatchEntry{{Path: "docker-compose.yml", Size: "3"}},
			Private:     &private,
		}, owner)
	}

	t.Run("planted before the buffer is created", func(t *testing.T) {
		victim := seedVictim(t)
		dest := t.TempDir()
		transfer, session := composeBatch(t, dest, selfOwner())
		plant(t, victim, filepath.Join(dest, batchBufferName(transfer, "docker-compose.yml", 0)))

		require.ErrorIs(t, transfer.writeChunk(session, []byte("new")), syscall.EEXIST)
		requireVictimUntouched(t, victim)
	})

	t.Run("swapped before a reopen", func(t *testing.T) {
		victim := seedVictim(t)
		transfer, session := composeBatch(t, t.TempDir(), selfOwner())
		require.NoError(t, transfer.ensurePrepared(session))
		buffer := "/" + transfer.tempRel

		require.NoError(t, os.Remove(buffer))
		plant(t, victim, buffer)
		require.ErrorIs(t, transfer.writeChunk(session, []byte("new")), syscall.ELOOP)
		requireVictimUntouched(t, victim)

		require.NoError(t, os.Remove(buffer))
		require.NoError(t, os.WriteFile(buffer, nil, 0o600))
		require.ErrorIs(t, transfer.writeChunk(session, []byte("new")), errUploadBufferReplaced)
	})

	t.Run("swapped while the buffer is open", func(t *testing.T) {
		victim := seedVictim(t)
		dest := t.TempDir()
		owner := selfOwner()
		if os.Geteuid() == 0 {
			owner = sessionOwner{uid: 12345, gid: 12346, set: true}
			require.NoError(t, os.Chown(dest, 12345, 12346))
		}
		transfer, session := composeBatch(t, dest, owner)
		require.NoError(t, transfer.writeChunk(session, []byte("new")))
		transfer.fileBytes = 3

		buffer := "/" + transfer.tempRel
		moved := buffer + ".moved"
		require.NoError(t, os.Rename(buffer, moved))
		plant(t, victim, buffer)
		require.NoError(t, transfer.finalizeCurrent(session))

		requireVictimUntouched(t, victim)
		requireOwnerAndMode(t, moved, owner.uid, owner.gid, 0o600)
	})

	t.Run("a folder swapped before the commit is refused", func(t *testing.T) {
		base := t.TempDir()
		stack := filepath.Join(base, "stack")
		prod := filepath.Join(base, "prod")
		require.NoError(t, os.Mkdir(prod, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(prod, ".env"), []byte("secret"), 0o600))
		overwrite := true
		transfer, session := newOwnedBatchUpload(t, apischema.FileUploadBatchRequest{
			Destination: base,
			Files:       []apischema.FileUploadBatchEntry{{Path: "stack/.env", Size: "3"}},
			Overwrite:   &overwrite,
		}, selfOwner())
		require.NoError(t, transfer.writeChunk(session, []byte("new")))
		transfer.fileBytes = 3

		// Both names now resolve into prod, buffer included.
		partName := filepath.Base(transfer.tempRel)
		require.NoError(t, os.Rename(stack, stack+".moved"))
		require.NoError(t, os.Symlink("prod", stack))
		require.NoError(t, os.Rename(filepath.Join(stack+".moved", partName), filepath.Join(prod, partName)))

		require.ErrorIs(t, transfer.finalizeCurrent(session), errUploadFolderReplaced)
		content, err := os.ReadFile(filepath.Join(prod, ".env"))
		require.NoError(t, err)
		require.Equal(t, "secret", string(content))
	})
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
