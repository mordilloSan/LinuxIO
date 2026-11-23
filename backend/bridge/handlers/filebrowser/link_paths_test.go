package filebrowser

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json/v2"
	"net"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/services"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/linksafe"
	ipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
	indexerapi "github.com/mordilloSan/LinuxIO/backend/indexer/api"
)

// absLinkTree is a home with data, a folder link whose target is absolute,
// as /home/u/data -> /mnt/data, leading to real, a folder elsewhere that
// holds sub/notes.txt. base is the real path of the folder holding both, so
// the paths the bridge resolves can be compared with it.
type absLinkTree struct {
	base, home, link, real string
}

func newAbsLinkTree(t *testing.T, base string) absLinkTree {
	t.Helper()
	base, err := filepath.EvalSymlinks(base)
	require.NoError(t, err)
	tree := absLinkTree{base: base, home: filepath.Join(base, "home"), real: filepath.Join(base, "disk", "data")}
	require.NoError(t, os.MkdirAll(tree.home, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(tree.real, "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tree.real, "sub", "notes.txt"), []byte("notes"), 0o644))
	tree.link = filepath.Join(tree.home, "data")
	require.NoError(t, os.Symlink(tree.real, tree.link))
	return tree
}

// through names rel below the link; real names it below the link's target.
func (tree absLinkTree) through(rel string) string { return filepath.Join(tree.link, rel) }
func (tree absLinkTree) target(rel string) string  { return filepath.Join(tree.real, rel) }

// downloadThrough runs the single-file download of path and returns what it
// streamed, or the error the stream answered with.
func downloadThrough(t *testing.T, path string) (string, ipc.ResultFrame) {
	t.Helper()
	server, client := net.Pipe()
	t.Cleanup(func() {
		_ = server.Close()
		_ = client.Close()
	})
	go func() { _ = streamFileDownload(context.Background(), server, apischema.PathRequest{Path: path}) }()
	var body bytes.Buffer
	for {
		frame, err := ipc.ReadRelayFrame(client)
		require.NoError(t, err)
		switch frame.Opcode {
		case ipc.OpStreamData:
			body.Write(frame.Payload)
		case ipc.OpStreamResult:
			var result ipc.ResultFrame
			require.NoError(t, json.Unmarshal(frame.Payload, &result))
			return body.String(), result
		}
	}
}

// uploadThrough runs a filebrowser.upload of content to target for session
// and returns what the stream answered. edit, when set, makes it an editor
// save, which replaces the file it read.
func uploadThrough(t *testing.T, target, content string, session sessionOwner, edit ...func(*apischema.FileUploadRequest)) ipc.ResultFrame {
	t.Helper()
	req := apischema.FileUploadRequest{TargetPath: target, Size: strconv.Itoa(len(content))}
	for _, change := range edit {
		change(&req)
	}
	task, _, err := bridgeipc.NewTaskService().ClaimForOwner(routeUpload, req, bridgeipc.TaskOwner{}, bridgeipc.TaskLifetimeSession, bridgeipc.TaskIdentity{})
	require.NoError(t, err)
	startTransferTask(t, task, func(ctx context.Context, j *bridgeipc.Task, _ any) (any, error) {
		return runUploadTask(ctx, j, req, session)
	})
	conn, results, _ := attachUploadBatchStream(t, task, "")
	// An upload refused before it is ready answers without reading, so the
	// data is sent where a write that blocks cannot hold the test.
	go func() {
		_ = ipc.WriteRelayFrame(conn, &ipc.StreamFrame{Opcode: ipc.OpStreamData, Payload: []byte(content)})
		_ = ipc.WriteStreamClose(conn, 0)
	}()
	return waitResult(t, results)
}

// uploadBatchThrough runs a filebrowser.upload_batch of req for session,
// sending content as the manifest's bytes, and returns what the stream
// answered.
func uploadBatchThrough(t *testing.T, req apischema.FileUploadBatchRequest, content string, session sessionOwner) ipc.ResultFrame {
	t.Helper()
	task := startUploadBatchTaskAs(t, req, session)
	conn, results, _ := attachUploadBatchStream(t, task, "")
	// A batch refused before it is ready answers without reading.
	go func() {
		_ = ipc.WriteRelayFrame(conn, &ipc.StreamFrame{Opcode: ipc.OpStreamData, Payload: []byte(content)})
		_ = ipc.WriteStreamClose(conn, 0)
	}()
	return waitResult(t, results)
}

// zipTo writes a zip at path holding name with content.
func zipTo(t *testing.T, path, name, content string) {
	t.Helper()
	out, err := os.Create(path)
	require.NoError(t, err)
	writer := zip.NewWriter(out)
	entry, err := writer.Create(name)
	require.NoError(t, err)
	_, err = entry.Write([]byte(content))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	require.NoError(t, out.Close())
}

// Everything that reads goes below a folder link whose target is absolute:
// os.Root refuses such a link, so these failed with "path escapes from
// parent" until the folders of a path were resolved where it enters.
func TestReadsGoBelowAFolderLinkWithAnAbsoluteTarget(t *testing.T) {
	tree := newAbsLinkTree(t, t.TempDir())
	ctx := t.Context()

	listing, err := listOpenedDirectory(ctx, apischema.PathRequest{Path: tree.through("sub")}, sessionOwner{})
	require.NoError(t, err)
	require.Len(t, listing.Files, 1)
	require.Equal(t, "notes.txt", listing.Files[0].Name)

	children, err := directoryChildren(ctx, apischema.DirectoryChildrenRequest{Path: tree.through("sub"), IncludeFiles: true})
	require.NoError(t, err)
	require.Equal(t, []string{"notes.txt"}, children.Files)

	stat, err := resourceStat(ctx, apischema.PathRequest{Path: tree.through("sub/notes.txt")})
	require.NoError(t, err)
	require.Equal(t, "-rw-r--r--", stat.Mode)

	text, err := readText(ctx, apischema.PathRequest{Path: tree.through("sub/notes.txt")}, services.Owner{})
	require.NoError(t, err)
	require.Equal(t, "notes", text.Content)
	require.True(t, text.CanSave)

	// The page asks with the path it navigated and matches the answer by it.
	exists, err := existsBatch(ctx, apischema.BatchPathRequest{Paths: []string{tree.through("sub/notes.txt"), tree.through("sub/missing.txt")}})
	require.NoError(t, err)
	require.Len(t, exists.Existing, 1)
	require.Equal(t, tree.through("sub/notes.txt"), exists.Existing[0].Path)

	body, result := downloadThrough(t, tree.through("sub/notes.txt"))
	require.Equalf(t, "ok", result.Status, "download answered %+v", result)
	require.Equal(t, "notes", body)
}

// Everything that changes files goes below such a link for a bridge that
// acts as the session user, who may follow their own links.
func TestChangesGoBelowAFolderLinkWithAnAbsoluteTarget(t *testing.T) {
	recordIndexerRequests(t)
	tree := newAbsLinkTree(t, t.TempDir())
	ctx := t.Context()
	session := sessionOwner{}
	outside := filepath.Join(tree.base, "outside")
	require.NoError(t, os.Mkdir(outside, 0o755))

	created, err := resourcePost(ctx, apischema.FileResourcePostRequest{Path: tree.through("sub/new.txt")}, actingFor(session))
	require.NoError(t, err, "create a file")
	require.Equal(t, map[string]any{"message": "created"}, created)
	require.FileExists(t, tree.target("sub/new.txt"))
	_, err = resourcePost(ctx, apischema.FileResourcePostRequest{Path: tree.through("sub/made/inner") + "/"}, actingFor(session))
	require.NoError(t, err, "create a folder")
	require.DirExists(t, tree.target("sub/made/inner"))

	rename := apischema.ActionSourceDestinationRequest{Action: "rename", Source: tree.through("sub/new.txt"), Dest: tree.through("sub/renamed.txt")}
	_, err = resourcePatch(ctx, rename, newBatchTask(t, "filebrowser.resource_patch", rename), session)
	detachedIndexerUpdates.Wait()
	require.NoError(t, err, "rename")
	require.FileExists(t, tree.target("sub/renamed.txt"))

	copyOut := apischema.BatchTransferRequest{Sources: []string{tree.through("sub/notes.txt")}, Destination: outside}
	result, err := runCopyBatchTask(ctx, newBatchTask(t, "filebrowser.copy_batch", copyOut), nil, copyOut, session)
	detachedIndexerUpdates.Wait() // the recorder takes one indexer update at a time
	require.NoError(t, err)
	require.Equal(t, 1, result.Succeeded, result.Failed)
	require.Equal(t, "notes", readString(t, filepath.Join(outside, "notes.txt")))

	moveIn := apischema.BatchTransferRequest{Sources: []string{filepath.Join(outside, "notes.txt")}, Destination: tree.through("sub/made")}
	result, err = runMoveBatchTask(ctx, newBatchTask(t, "filebrowser.move_batch", moveIn), nil, moveIn, session)
	detachedIndexerUpdates.Wait()
	require.NoError(t, err)
	require.Equal(t, 1, result.Succeeded, result.Failed)
	require.Equal(t, "notes", readString(t, tree.target("sub/made/notes.txt")))

	moveOut := apischema.BatchTransferRequest{Sources: []string{tree.through("sub/made/notes.txt")}, Destination: outside}
	result, err = runMoveBatchTask(ctx, newBatchTask(t, "filebrowser.move_batch", moveOut), nil, moveOut, session)
	detachedIndexerUpdates.Wait()
	require.NoError(t, err)
	require.Equal(t, 1, result.Succeeded, result.Failed)
	require.NoFileExists(t, tree.target("sub/made/notes.txt"))

	chmod := apischema.FileChmodBatchRequest{Paths: []string{tree.through("sub/notes.txt")}, Mode: "600"}
	result, err = runChmodBatchTask(ctx, newBatchTask(t, "filebrowser.chmod_batch", chmod), nil, chmod, session)
	require.NoError(t, err)
	require.Equal(t, 1, result.Succeeded, result.Failed)
	stat, err := resourceStat(ctx, apischema.PathRequest{Path: tree.through("sub/notes.txt")})
	require.NoError(t, err)
	require.Equal(t, "-rw-------", stat.Mode, "the Permissions dialog reads the new mode")

	compress := apischema.FileCompressRequest{Format: "zip", TargetPath: tree.through("sub/out.zip"), Paths: []string{tree.through("sub/notes.txt"), tree.through("sub/made")}}
	compressed, err := runCompressTask(ctx, newBatchTask(t, "filebrowser.compress", compress), nil, compress, session)
	detachedIndexerUpdates.Wait()
	require.NoError(t, err)
	require.Equal(t, tree.through("sub/out.zip"), compressed.Path, "the result names the path the page sent")
	require.Empty(t, compressed.Failed)
	zipped, err := zip.OpenReader(tree.target("sub/out.zip"))
	require.NoError(t, err)
	names := make([]string, 0, len(zipped.File))
	for _, file := range zipped.File {
		names = append(names, file.Name)
	}
	require.NoError(t, zipped.Close())
	require.ElementsMatch(t, []string{"notes.txt", "made/inner/"}, names)

	out := tree.through("sub/out")
	extract := apischema.FileExtractRequest{ArchivePath: tree.through("sub/out.zip"), Destination: &out}
	extracted, err := runExtractTask(ctx, newBatchTask(t, "filebrowser.extract", extract), nil, extract, session)
	detachedIndexerUpdates.Wait()
	require.NoError(t, err)
	require.Equal(t, out, extracted.Destination)
	require.Equal(t, "notes", readString(t, tree.target("sub/out/notes.txt")))

	upload := uploadThrough(t, tree.through("sub/up.txt"), "up", session)
	detachedIndexerUpdates.Wait()
	require.Equalf(t, "ok", upload.Status, "upload answered %+v", upload)
	require.Equal(t, "up", readString(t, tree.target("sub/up.txt")))

	opened, err := readText(ctx, apischema.PathRequest{Path: tree.through("sub/up.txt")}, actingFor(session))
	require.NoError(t, err)
	save := uploadThrough(t, tree.through("sub/up.txt"), "saved", session, func(req *apischema.FileUploadRequest) {
		overwrite := true
		req.Overwrite, req.ExpectedVersion = &overwrite, &opened.Version
	})
	detachedIndexerUpdates.Wait()
	require.Equalf(t, "ok", save.Status, "editor save answered %+v", save)
	require.Equal(t, "saved", readString(t, tree.target("sub/up.txt")))

	batch := apischema.FileUploadBatchRequest{Destination: tree.link, Files: []apischema.FileUploadBatchEntry{{Path: "sub/batch.txt", Size: "5"}}}
	task := startUploadBatchTaskAs(t, batch, session)
	conn, results, _ := attachUploadBatchStream(t, task, "")
	writeData(t, conn, []byte("batch"))
	writeClose(t, conn)
	batchResult := waitResult(t, results)
	detachedIndexerUpdates.Wait()
	require.Equalf(t, "ok", batchResult.Status, "batch upload answered %+v", batchResult)
	require.Empty(t, decodeUploadBatchResult(t, batchResult).Failed)
	require.Equal(t, tree.link, decodeUploadBatchResult(t, batchResult).Destination, "the answer names the folder the page sent")
	require.Equal(t, "batch", readString(t, tree.target("sub/batch.txt")))

	remove := apischema.BatchPathRequest{Paths: []string{tree.through("sub/renamed.txt")}}
	result, err = runDeleteBatchTask(ctx, newBatchTask(t, "filebrowser.delete_batch", remove), nil, remove, session)
	require.NoError(t, err)
	require.Equal(t, 1, result.Succeeded, result.Failed)
	require.NoFileExists(t, tree.target("sub/renamed.txt"))
	detachedIndexerUpdates.Wait()
}

// The trash takes an item from below such a link and puts it back there.
func TestTrashGoesBelowAFolderLinkWithAnAbsoluteTarget(t *testing.T) {
	home, _ := trashTaskHome(t)
	tree := newAbsLinkTree(t, home)
	ctx := t.Context()

	trash := apischema.BatchPathRequest{Paths: []string{tree.through("sub/notes.txt")}}
	result, err := runTrashBatchTask(ctx, newBatchTask(t, "filebrowser.trash_batch", trash), nil, trash, sessionOwner{})
	detachedIndexerUpdates.Wait()
	require.NoError(t, err)
	require.Equal(t, 1, result.Succeeded, result.Failed)
	require.NoFileExists(t, tree.target("sub/notes.txt"))

	listed, err := listTrash(ctx, sessionOwner{})
	require.NoError(t, err)
	require.Len(t, listed.Entries, 1)
	restore := apischema.TrashRestoreRequest{Paths: []string{listed.Entries[0].Path}}
	result, err = runTrashRestoreTask(ctx, newBatchTask(t, "filebrowser.trash_restore", restore), nil, restore, sessionOwner{})
	detachedIndexerUpdates.Wait()
	require.NoError(t, err)
	require.Equal(t, 1, result.Succeeded, result.Failed)
	require.Equal(t, "notes", readString(t, tree.target("sub/notes.txt")))
}

// The trash works for a user whose home folder's own path leads through a
// link with an absolute target, as /home -> /data/home: an item goes into the
// home trash and back out of it, and the indexer hears of both moves by the
// real paths it knows the files by.
func TestTrashWorksForAHomeBelowAFolderLinkWithAnAbsoluteTarget(t *testing.T) {
	requests := recordIndexerRequests(t)
	tree := newAbsLinkTree(t, t.TempDir())
	home := tree.through("u")
	require.NoError(t, os.Mkdir(tree.target("u"), 0o755))
	t.Setenv("HOME", home)
	notes := seedFile(t, filepath.Join(home, "notes.txt"), "notes")
	realNotes, realTrashed := tree.target("u/notes.txt"), tree.target("u/.local/share/Trash/files/notes.txt")
	ctx := t.Context()

	trash := apischema.BatchPathRequest{Paths: []string{notes}}
	result, err := runTrashBatchTask(ctx, newBatchTask(t, "filebrowser.trash_batch", trash), nil, trash, sessionOwner{})
	detachedIndexerUpdates.Wait()
	require.NoError(t, err)
	require.Equal(t, 1, result.Succeeded, result.Failed)
	require.FileExists(t, realTrashed)
	require.Contains(t, *requests, indexerMove(realNotes, realTrashed))

	listed, err := listTrash(ctx, sessionOwner{})
	require.NoError(t, err)
	require.Len(t, listed.Entries, 1)
	require.Equal(t, filepath.Join(home, ".local/share/Trash/files/notes.txt"), listed.Entries[0].Path)
	restore := apischema.TrashRestoreRequest{Paths: []string{listed.Entries[0].Path}}
	result, err = runTrashRestoreTask(ctx, newBatchTask(t, "filebrowser.trash_restore", restore), nil, restore, sessionOwner{})
	detachedIndexerUpdates.Wait()
	require.NoError(t, err)
	require.Equal(t, 1, result.Succeeded, result.Failed)
	require.Equal(t, "notes", readString(t, realNotes))
	require.NoFileExists(t, realTrashed)
	require.Contains(t, *requests, indexerMove(realTrashed, realNotes))
}

// A trash move names the item to the indexer under the session user's link
// rule: on a root bridge a link that user swapped into their trash's folders
// is not followed, and the indexer is not told of that item rather than told
// of where the link leads. Without root every folder of the test is one the
// session user could have planted a link in.
func TestATrashMoveIsNamedToTheIndexerUnderTheLinkRule(t *testing.T) {
	tree := newAbsLinkTree(t, t.TempDir())
	trashed := tree.through("sub/notes.txt")

	name, ok := indexedName(t.Context(), trashed, services.Owner{})
	require.True(t, ok)
	require.Equal(t, tree.target("sub/notes.txt"), name)
	_, ok = indexedName(t.Context(), trashed, selfOwner().asOwner())
	require.False(t, ok, "the link is not followed for the indexer")
}

// indexerMove is the request that tells the indexer what was at from is now
// at to.
func indexerMove(from, to string) recordedIndexerRequest {
	return recordedIndexerRequest{method: http.MethodPost, path: indexerapi.RouteMove, move: &indexerapi.MoveRequest{From: from, To: to}}
}

// The editor offers to save a file only where a save for the session user
// can write. A root bridge's save refuses a link that user could have planted
// on the way, so a file below one opens read-only; without root every folder
// of the test is such a folder for a session user set.
func TestTheEditorOffersSaveOnlyWhereASaveCanWrite(t *testing.T) {
	tree := newAbsLinkTree(t, t.TempDir())
	notes := tree.through("sub/notes.txt")

	read, err := services.ReadEditorFile(t.Context(), notes, services.Owner{})
	require.NoError(t, err)
	require.True(t, read.CanSave)
	read, err = services.ReadEditorFile(t.Context(), notes, selfOwner().asOwner())
	require.NoError(t, err)
	require.Equal(t, "notes", string(read.Content), "it still reads")
	require.False(t, read.CanSave)
}

// A download reads only regular files. A FIFO, or a link to a device, that a
// user puts in a folder an admin downloads is left out of the archive without
// being opened, rather than hanging it or streaming the device into it, and a
// download of one alone is refused.
func TestDownloadsReadOnlyRegularFiles(t *testing.T) {
	dir := t.TempDir()
	share := filepath.Join(dir, "share")
	require.NoError(t, os.Mkdir(share, 0o755))
	seedFile(t, filepath.Join(share, "notes.txt"), "notes")
	fifo := filepath.Join(share, "fifo")
	require.NoError(t, syscall.Mkfifo(fifo, 0o644))
	t.Cleanup(func() {
		// Lets an open that waits for a writer return, should one hang.
		if f, err := os.OpenFile(fifo, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
			_ = f.Close()
		}
	})
	null := filepath.Join(share, "null")
	require.NoError(t, os.Symlink("/dev/null", null))
	alias := filepath.Join(share, "alias")
	require.NoError(t, os.Symlink("notes.txt", alias))

	out, err := os.Create(filepath.Join(dir, "out.zip"))
	require.NoError(t, err)
	defer out.Close()
	done := make(chan error, 1)
	go func() {
		done <- services.CreateZip(t.Context(), out, nil, "", services.ArchiveSources{}, share, fifo, null)
	}()
	select {
	case err = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the archive waits on the FIFO")
	}
	require.NoError(t, err)
	zipped, err := zip.OpenReader(out.Name())
	require.NoError(t, err)
	defer zipped.Close()
	names := []string{}
	for _, file := range zipped.File {
		names = append(names, file.Name)
	}
	require.ElementsMatch(t, []string{"share/alias", "share/notes.txt"}, names, "a link to a file adds the file")

	body, result := downloadThrough(t, alias)
	require.Equalf(t, "ok", result.Status, "download answered %+v", result)
	require.Equal(t, "notes", body)

	for _, path := range []string{fifo, null} {
		_, result := downloadThrough(t, path)
		require.Equal(t, "error", result.Status, path)
		require.Equal(t, 400, result.Code, path)
	}
}

// A delete or a move judges where a link leads, not the name it was asked
// by: a link to / does not make /srv anything but a system folder, nor a link
// to /dev /dev/shm anything but a mount point.
func TestProtectedFoldersAreJudgedWhereALinkLeads(t *testing.T) {
	tree := newAbsLinkTree(t, t.TempDir())
	top := filepath.Join(tree.home, "top")
	require.NoError(t, os.Symlink("/", top))
	ctx := t.Context()

	_, _, failures := planDeleteBatch(ctx, []string{filepath.Join(top, "srv")}, services.Owner{})
	require.Len(t, failures, 1)
	require.Equal(t, "/srv is a system folder", failures[0].Error)

	root := openTestRoot(t)
	move := apischema.BatchTransferRequest{Sources: []string{filepath.Join(top, "srv")}, Destination: tree.home}
	_, _, failures = planBatchTransfer(ctx, root, tree.home, move, "move", services.Owner{})
	require.Len(t, failures, 1)
	require.Equal(t, "/srv is a system folder", failures[0].Error)

	var devStat, shmStat syscall.Stat_t
	if syscall.Stat("/dev", &devStat) != nil || syscall.Stat("/dev/shm", &shmStat) != nil || devStat.Dev == shmStat.Dev {
		t.Skip("/dev/shm is not a mount point here")
	}
	dev := filepath.Join(tree.home, "dev")
	require.NoError(t, os.Symlink("/dev", dev))
	_, _, failures = planDeleteBatch(ctx, []string{filepath.Join(dev, "shm")}, services.Owner{})
	require.Len(t, failures, 1)
	require.Equal(t, "/dev/shm is a mount point", failures[0].Error)
}

// A chain of links resolves, a link whose target names .. resolves where the
// kernel would put it, and a .. the page sends after a link stays on the
// path it navigated, as the breadcrumbs show it. A loop fails rather than
// spinning, and a link to nothing is not found.
func TestLinkChainsLoopsAndDanglingLinks(t *testing.T) {
	tree := newAbsLinkTree(t, t.TempDir())
	ctx := t.Context()
	listedNames := func(path string) []string {
		t.Helper()
		listing, err := listOpenedDirectory(ctx, apischema.PathRequest{Path: path}, sessionOwner{})
		require.NoError(t, err, path)
		names := []string{}
		for _, folder := range listing.Folders {
			names = append(names, folder.Name)
		}
		for _, file := range listing.Files {
			names = append(names, file.Name)
		}
		return names
	}

	chain := filepath.Join(tree.home, "chain")
	require.NoError(t, os.Symlink(tree.link, chain))
	require.Equal(t, []string{"notes.txt"}, listedNames(filepath.Join(chain, "sub")))

	up := filepath.Join(tree.home, "up")
	require.NoError(t, os.Symlink(tree.target("sub/.."), up))
	require.Equal(t, []string{"notes.txt"}, listedNames(filepath.Join(up, "sub")))

	require.ElementsMatch(t, []string{"chain", "data", "up"}, listedNames(tree.through("..")))

	a, b := filepath.Join(tree.home, "a"), filepath.Join(tree.home, "b")
	require.NoError(t, os.Symlink(b, a))
	require.NoError(t, os.Symlink(a, b))
	_, err := listOpenedDirectory(ctx, apischema.PathRequest{Path: filepath.Join(a, "x")}, sessionOwner{})
	require.ErrorIs(t, err, syscall.ELOOP)

	gone := filepath.Join(tree.home, "gone")
	require.NoError(t, os.Symlink(filepath.Join(tree.base, "nowhere"), gone))
	_, err = listOpenedDirectory(ctx, apischema.PathRequest{Path: filepath.Join(gone, "x")}, sessionOwner{})
	require.ErrorIs(t, err, bridgeipc.ErrNotFound)
}

// A path resolved for a root bridge is a name only: the change reaches it
// again by handles under the link rule, so a folder on it swapped for a link
// after it was resolved is refused, not followed. Without root every folder
// of the test is one a user could plant a link in, so the rule refuses it
// here as it refuses one in a session user's folder on a root bridge.
func TestAPathResolvedForAChangeIsReachedAgainUnderTheLinkRule(t *testing.T) {
	tree := newAbsLinkTree(t, t.TempDir())
	victim := filepath.Join(tree.base, "victim")
	require.NoError(t, os.Mkdir(victim, 0o755))
	kept := seedFile(t, filepath.Join(victim, "kept.txt"), "kept")
	docs := filepath.Join(tree.home, "docs")
	require.NoError(t, os.Mkdir(docs, 0o755))
	seedFile(t, filepath.Join(docs, "kept.txt"), "mine")
	session := selfOwner().asOwner()

	resolved, err := linksafe.ResolveFolders(t.Context(), filepath.Join(docs, "kept.txt"), session)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(docs, "kept.txt"), resolved)

	require.NoError(t, os.Rename(docs, docs+".old"))
	require.NoError(t, os.Symlink(victim, docs))
	_, err = services.DeleteFiles(t.Context(), resolved, services.DeleteOptions{Owner: session})
	require.ErrorIs(t, err, bridgeipc.ErrForbidden)
	require.Equal(t, "kept", readString(t, kept))
}

// absLinkSessionTree is absLinkTree in the home of the root tests' session
// user, which that user owns, so the link is one they could have planted.
func absLinkSessionTree(t *testing.T) (absLinkTree, sessionOwner) {
	t.Helper()
	base, home := trashTestHome(t)
	tree := newAbsLinkTree(t, base)
	require.Equal(t, home, tree.home)
	return tree, sessionOwner{uid: trashTestUID, gid: trashTestGID, set: true}
}

// A root bridge acting for the session user reads below a link in that
// user's folder, as it reads below any link, but changes nothing there: the
// link could send it anywhere, so each change is refused with the error that
// names where the link leads, and nothing below it is touched.
func TestChangesBelowAUsersFolderLinkAreRefusedAsRoot(t *testing.T) {
	recordIndexerRequests(t)
	tree, session := absLinkSessionTree(t)
	ctx := t.Context()
	refusal := tree.link + " is a link to " + tree.real + ". Open that folder to make this change"
	outside := filepath.Join(tree.base, "outside")
	require.NoError(t, os.Mkdir(outside, 0o755))
	loose := seedFile(t, filepath.Join(outside, "loose.txt"), "loose")

	listing, err := listOpenedDirectory(ctx, apischema.PathRequest{Path: tree.through("sub")}, sessionOwner{})
	require.NoError(t, err)
	require.Len(t, listing.Files, 1)
	text, err := readText(ctx, apischema.PathRequest{Path: tree.through("sub/notes.txt")}, actingFor(session))
	require.NoError(t, err)
	require.Equal(t, "notes", text.Content)
	require.False(t, text.CanSave, "the editor opens it read-only: a save there would be refused")
	body, download := downloadThrough(t, tree.through("sub/notes.txt"))
	require.Equalf(t, "ok", download.Status, "download answered %+v", download)
	require.Equal(t, "notes", body)
	stat, err := resourceStat(ctx, apischema.PathRequest{Path: tree.through("sub/notes.txt")})
	require.NoError(t, err)
	require.Equal(t, "-rw-r--r--", stat.Mode)

	_, err = resourcePost(ctx, apischema.FileResourcePostRequest{Path: tree.through("sub/new.txt")}, actingFor(session))
	require.ErrorIs(t, err, bridgeipc.ErrForbidden)
	require.ErrorContains(t, err, refusal)

	rename := apischema.ActionSourceDestinationRequest{Action: "rename", Source: tree.through("sub/notes.txt"), Dest: tree.through("sub/renamed.txt")}
	_, err = resourcePatch(ctx, rename, newBatchTask(t, "filebrowser.resource_patch", rename), session)
	require.ErrorIs(t, err, bridgeipc.ErrForbidden)
	require.ErrorContains(t, err, refusal)

	copyIn := apischema.BatchTransferRequest{Sources: []string{loose}, Destination: tree.through("sub")}
	_, err = runCopyBatchTask(ctx, newBatchTask(t, "filebrowser.copy_batch", copyIn), nil, copyIn, session)
	require.ErrorIs(t, err, bridgeipc.ErrForbidden)
	require.ErrorContains(t, err, refusal)

	for name, run := range map[string]func() (FileBatchResult, error){
		"move out": func() (FileBatchResult, error) {
			req := apischema.BatchTransferRequest{Sources: []string{tree.through("sub/notes.txt")}, Destination: outside}
			return runMoveBatchTask(ctx, newBatchTask(t, "filebrowser.move_batch", req), nil, req, session)
		},
		"delete": func() (FileBatchResult, error) {
			req := apischema.BatchPathRequest{Paths: []string{tree.through("sub/notes.txt")}}
			return runDeleteBatchTask(ctx, newBatchTask(t, "filebrowser.delete_batch", req), nil, req, session)
		},
		"chmod": func() (FileBatchResult, error) {
			req := apischema.FileChmodBatchRequest{Paths: []string{tree.through("sub/notes.txt")}, Mode: "600"}
			return runChmodBatchTask(ctx, newBatchTask(t, "filebrowser.chmod_batch", req), nil, req, session)
		},
		"copy out": func() (FileBatchResult, error) {
			req := apischema.BatchTransferRequest{Sources: []string{tree.through("sub/notes.txt")}, Destination: outside}
			return runCopyBatchTask(ctx, newBatchTask(t, "filebrowser.copy_batch", req), nil, req, session)
		},
		"trash": func() (FileBatchResult, error) {
			// The trash needs the session user's account for their home; the
			// rule does not depend on whose folder holds the link, only on its
			// not being root's alone, so any account shows the refusal.
			account, lookupErr := user.Lookup("nobody")
			require.NoError(t, lookupErr, "an account to trash for")
			uid, _ := strconv.Atoi(account.Uid)
			gid, _ := strconv.Atoi(account.Gid)
			req := apischema.BatchPathRequest{Paths: []string{tree.through("sub/notes.txt")}}
			return runTrashBatchTask(ctx, newBatchTask(t, "filebrowser.trash_batch", req), nil, req, sessionOwner{uid: uint32(uid), gid: uint32(gid), set: true})
		},
	} {
		result, runErr := run()
		require.NoError(t, runErr, name)
		require.Len(t, result.Failed, 1, name)
		require.Equal(t, tree.through("sub/notes.txt"), result.Failed[0].Path, name)
		require.Contains(t, result.Failed[0].Error, refusal, name)
	}

	compress := apischema.FileCompressRequest{Format: "zip", TargetPath: tree.through("sub/out.zip"), Paths: []string{loose}}
	_, err = runCompressTask(ctx, newBatchTask(t, "filebrowser.compress", compress), nil, compress, session)
	require.ErrorIs(t, err, bridgeipc.ErrForbidden)
	require.ErrorContains(t, err, refusal)

	archive := filepath.Join(outside, "loose.zip")
	zipTo(t, archive, "loose.txt", "loose")
	out := tree.through("sub/out")
	extract := apischema.FileExtractRequest{ArchivePath: archive, Destination: &out}
	_, err = runExtractTask(ctx, newBatchTask(t, "filebrowser.extract", extract), nil, extract, session)
	require.ErrorIs(t, err, bridgeipc.ErrForbidden)
	require.ErrorContains(t, err, refusal)

	upload := uploadThrough(t, tree.through("sub/up.txt"), "up", session)
	require.Equal(t, "error", upload.Status)
	require.Contains(t, upload.Error, refusal)

	restore := apischema.TrashRestoreRequest{Paths: []string{loose}, Destination: tree.through("sub")}
	_, err = runTrashRestoreTask(ctx, newBatchTask(t, "filebrowser.trash_restore", restore), nil, restore, session)
	require.ErrorIs(t, err, bridgeipc.ErrForbidden, "restore into a chosen folder")
	require.ErrorContains(t, err, refusal)

	batch := apischema.FileUploadBatchRequest{Destination: tree.through("sub"), Files: []apischema.FileUploadBatchEntry{{Path: "batch.txt", Size: "5"}}}
	batchResult := uploadBatchThrough(t, batch, "batch", session)
	require.Equal(t, "error", batchResult.Status, "batch upload into a folder below the link")
	require.Contains(t, batchResult.Error, refusal)

	batch = apischema.FileUploadBatchRequest{Destination: tree.home, Files: []apischema.FileUploadBatchEntry{{Path: "data/sub/batch.txt", Size: "5"}}}
	batchResult = uploadBatchThrough(t, batch, "batch", session)
	require.Equalf(t, "ok", batchResult.Status, "batch upload of a name below the link answered %+v", batchResult)
	failed := decodeUploadBatchResult(t, batchResult).Failed
	require.Len(t, failed, 1)
	require.Contains(t, failed[0].Error, refusal)

	entries, err := os.ReadDir(tree.target("sub"))
	require.NoError(t, err)
	require.Len(t, entries, 1, "nothing was made below the link")
	require.Equal(t, "notes", readString(t, tree.target("sub/notes.txt")))
	info, err := os.Stat(tree.target("sub/notes.txt"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o644), info.Mode().Perm())
	detachedIndexerUpdates.Wait()
}

// A link in a folder only root can write is root's own, so a root bridge
// follows it for a change as for a read, whatever its target.
func TestChangesBelowARootLinkWithAnAbsoluteTargetAsRoot(t *testing.T) {
	recordIndexerRequests(t)
	tree, session := absLinkSessionTree(t)
	ctx := t.Context()
	rootLink := filepath.Join(tree.base, "rootdata")
	require.NoError(t, os.Symlink(tree.real, rootLink))

	text, err := readText(ctx, apischema.PathRequest{Path: filepath.Join(rootLink, "sub", "notes.txt")}, actingFor(session))
	require.NoError(t, err)
	require.True(t, text.CanSave, "a save reaches it through root's own link")

	_, err = resourcePost(ctx, apischema.FileResourcePostRequest{Path: filepath.Join(rootLink, "sub", "new.txt")}, actingFor(session))
	require.NoError(t, err)
	require.FileExists(t, tree.target("sub/new.txt"))

	remove := apischema.BatchPathRequest{Paths: []string{filepath.Join(rootLink, "sub", "new.txt")}}
	result, err := runDeleteBatchTask(ctx, newBatchTask(t, "filebrowser.delete_batch", remove), nil, remove, session)
	require.NoError(t, err)
	require.Equal(t, 1, result.Succeeded, result.Failed)
	require.NoFileExists(t, tree.target("sub/new.txt"))
	detachedIndexerUpdates.Wait()
}

// The rule a root bridge applies to a resolved path: a folder of the session
// user's on it swapped for a link after it was resolved is refused.
func TestAPathResolvedForAChangeIsReachedAgainUnderTheLinkRuleAsRoot(t *testing.T) {
	tree, session := absLinkSessionTree(t)
	docs := filepath.Join(tree.home, "docs")
	require.NoError(t, os.Mkdir(docs, 0o755))
	require.NoError(t, os.Chown(docs, trashTestUID, trashTestGID))
	seedFile(t, filepath.Join(docs, "notes.txt"), "mine")
	owner := actingFor(session)

	resolved, err := linksafe.ResolveFolders(t.Context(), filepath.Join(docs, "notes.txt"), owner)
	require.NoError(t, err)
	require.NoError(t, os.Rename(docs, docs+".old"))
	require.NoError(t, os.Symlink(tree.target("sub"), docs))
	_, err = services.DeleteFiles(t.Context(), resolved, services.DeleteOptions{Owner: owner})
	require.ErrorIs(t, err, bridgeipc.ErrForbidden)
	require.Equal(t, "notes", readString(t, tree.target("sub/notes.txt")))
}

// The .. in the target of a link at the last name is taken as the kernel
// takes it for a root bridge acting for the session user too: dir/L ->
// c/../x, with dir/c -> ../other/sub, leads to other/x when root owns dir.
func TestALinkTargetWithDotDotIsTakenAsTheKernelTakesItAsRoot(t *testing.T) {
	tree, session := absLinkSessionTree(t)
	dir, other := filepath.Join(tree.base, "dir"), filepath.Join(tree.base, "other")
	require.NoError(t, os.Mkdir(dir, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(other, "sub"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(other, "x"), 0o755))
	require.NoError(t, os.Symlink("../other/sub", filepath.Join(dir, "c")))
	link := filepath.Join(dir, "L")
	require.NoError(t, os.Symlink("c/../x", link))
	owner := actingFor(session)
	require.True(t, owner.Set)

	got, err := linksafe.Resolve(t.Context(), link, owner)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(other, "x"), got)
	got, err = linksafe.ResolveFolders(t.Context(), filepath.Join(link, "y"), owner)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(other, "x", "y"), got)
}
