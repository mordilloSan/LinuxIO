package filebrowser

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/services"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	indexerapi "github.com/mordilloSan/LinuxIO/backend/indexer/api"
)

// The session user of the root trash tests, who has no account: their home
// and trash are laid out by hand.
const trashTestUID, trashTestGID = 12345, 12346

// trashTestHome is a root-owned base folder with the session user's home in
// it.
func trashTestHome(t *testing.T) (base, home string) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("acting for another user needs root")
	}
	base = t.TempDir()
	home = filepath.Join(base, "home")
	require.NoError(t, os.Mkdir(home, 0o750))
	require.NoError(t, os.Chown(home, trashTestUID, trashTestGID))
	return base, home
}

// userTrashItem puts name in the session user's trash as any process of
// theirs could: the trash's folders and the .trashinfo are theirs, the item
// is owned by itemUID, and Path= says whatever they like.
func userTrashItem(t *testing.T, home, name, original string, itemUID int) string {
	t.Helper()
	dir := home
	for _, sub := range []string{".local", "share", "Trash"} {
		dir = filepath.Join(dir, sub)
		require.NoError(t, os.MkdirAll(dir, 0o700))
		require.NoError(t, os.Chown(dir, trashTestUID, trashTestGID))
	}
	for _, sub := range []string{"files", "info"} {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, sub), 0o700))
		require.NoError(t, os.Chown(filepath.Join(dir, sub), trashTestUID, trashTestGID))
	}
	item := filepath.Join(dir, "files", name)
	require.NoError(t, os.WriteFile(item, []byte("payload"), 0o644))
	require.NoError(t, os.Chown(item, itemUID, itemUID))
	info := filepath.Join(dir, "info", name+".trashinfo")
	content := "[Trash Info]\nPath=" + (&url.URL{Path: original}).EscapedPath() + "\nDeletionDate=2026-01-02T03:04:05\n"
	require.NoError(t, os.WriteFile(info, []byte(content), 0o600))
	require.NoError(t, os.Chown(info, trashTestUID, trashTestGID))
	return item
}

func requireNoEntries(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Emptyf(t, entries, "entries of %s", dir)
}

// The trash and its .trashinfo are the session user's to edit, so they cannot
// vouch for what a root bridge restores into a folder that user does not own:
// an item the user owns is refused there, and no folder is made on the way.
func TestTrashRestoreRefusesACraftedPathAsRoot(t *testing.T) {
	base, home := trashTestHome(t)
	system := filepath.Join(base, "etc")
	require.NoError(t, os.Mkdir(system, 0o755))
	other := filepath.Join(base, "srv")
	require.NoError(t, os.Mkdir(other, 0o755))
	require.NoError(t, os.Chown(other, 23456, 23457))

	for _, original := range []string{
		filepath.Join(system, "evil.d", "evil.conf"),
		filepath.Join(system, "evil.conf"),
		filepath.Join(other, "evil.d", "evil.conf"),
	} {
		item := userTrashItem(t, home, "evil.conf", original, trashTestUID)

		_, err := services.RestoreFromTrash(t.Context(), item, home, trashTestUID, trashTestGID, true, services.RestoreOptions{})
		require.ErrorIs(t, err, bridgeipc.ErrForbidden, original)
		require.FileExists(t, item)
	}
	requireNoEntries(t, system)
	requireNoEntries(t, other)
}

// What root trashed from a system folder goes back there, root's as before,
// without a temporary name left behind; once the user has swapped the trashed
// item for their own, it is refused and stays in the trash.
func TestTrashRestoreIntoASystemFolderAsRoot(t *testing.T) {
	base, home := trashTestHome(t)
	conf := filepath.Join(base, "etc", "app.d")
	require.NoError(t, os.MkdirAll(conf, 0o755))
	original := filepath.Join(conf, "app.conf")
	require.NoError(t, os.WriteFile(original, []byte("root"), 0o644))

	trashed, err := services.MoveToTrash(t.Context(), original, home, trashTestUID, trashTestGID, true)
	require.NoError(t, err)
	requireOwner(t, trashed, 0, 0)
	restored, err := services.RestoreFromTrash(t.Context(), trashed, home, trashTestUID, trashTestGID, true, services.RestoreOptions{})
	require.NoError(t, err)
	require.Equal(t, original, restored)
	requireOwnerAndMode(t, original, 0, 0, 0o644)
	entries, err := os.ReadDir(conf)
	require.NoError(t, err)
	require.Len(t, entries, 1, "only app.conf, no temporary name")

	trashed, err = services.MoveToTrash(t.Context(), original, home, trashTestUID, trashTestGID, true)
	require.NoError(t, err)
	require.NoError(t, os.Remove(trashed))
	require.NoError(t, os.WriteFile(trashed, []byte("user"), 0o644))
	require.NoError(t, os.Chown(trashed, trashTestUID, trashTestGID))
	_, err = services.RestoreFromTrash(t.Context(), trashed, home, trashTestUID, trashTestGID, true, services.RestoreOptions{})
	require.ErrorIs(t, err, bridgeipc.ErrForbidden)
	require.FileExists(t, trashed)
	requireNoEntries(t, conf)
}

// The trash's folders a root bridge makes in the session user's home, the
// .trashinfo and the parents a restore remakes there are handed to that user
// through their handles; a folder that already existed is left as it was, and
// a symlink planted at the .trashinfo name is never followed.
func TestTrashHandsNewEntriesToSessionUserAsRoot(t *testing.T) {
	_, home := trashTestHome(t)
	share := filepath.Join(home, ".local", "share")
	require.NoError(t, os.MkdirAll(share, 0o750))
	for _, dir := range []string{filepath.Dir(share), share} {
		require.NoError(t, os.Chown(dir, trashTestUID, trashTestGID))
		require.NoError(t, os.Chmod(dir, 0o750))
	}
	docs := filepath.Join(home, "docs")
	require.NoError(t, os.Mkdir(docs, 0o750))
	require.NoError(t, os.Chown(docs, trashTestUID, trashTestGID))
	notes := filepath.Join(docs, "notes.txt")
	require.NoError(t, os.WriteFile(notes, []byte("notes"), 0o644))
	require.NoError(t, os.Chown(notes, trashTestUID, trashTestGID))

	trashed, err := services.MoveToTrash(t.Context(), notes, home, trashTestUID, trashTestGID, true)
	require.NoError(t, err)
	requireOwnerAndMode(t, share, trashTestUID, trashTestGID, 0o750)
	trash := filepath.Join(share, "Trash")
	for _, dir := range []string{trash, filepath.Join(trash, "files"), filepath.Join(trash, "info")} {
		requireOwnerAndMode(t, dir, trashTestUID, trashTestGID, 0o700)
	}
	requireOwnerAndMode(t, filepath.Join(trash, "info", "notes.txt.trashinfo"), trashTestUID, trashTestGID, 0o600)

	require.NoError(t, os.Remove(docs))
	restored, err := services.RestoreFromTrash(t.Context(), trashed, home, trashTestUID, trashTestGID, true, services.RestoreOptions{})
	require.NoError(t, err)
	require.Equal(t, notes, restored)
	requireOwnerAndMode(t, docs, trashTestUID, trashTestGID, 0o755)
	requireOwnerAndMode(t, notes, trashTestUID, trashTestGID, 0o644)

	victim := seedVictim(t)
	plant(t, victim, filepath.Join(trash, "info", "notes.txt.trashinfo"))
	trashed, err = services.MoveToTrash(t.Context(), notes, home, trashTestUID, trashTestGID, true)
	require.NoError(t, err)
	require.Equal(t, "notes.txt.2", filepath.Base(trashed))
	requireVictimUntouched(t, victim)
	requireOwnerAndMode(t, filepath.Join(trash, "info", "notes.txt.2.trashinfo"), trashTestUID, trashTestGID, 0o600)
}

// A trash folder another user owns, say one they made in a shared .Trash
// ahead of time, would hand them what root moves in or out of it: it is
// refused either way.
func TestTrashRefusesAFolderAnotherUserOwnsAsRoot(t *testing.T) {
	_, home := trashTestHome(t)
	item := userTrashItem(t, home, "notes.txt", filepath.Join(home, "notes.txt"), trashTestUID)
	files := filepath.Dir(item)
	require.NoError(t, os.Chown(files, 23456, 23457))

	_, err := services.RestoreFromTrash(t.Context(), item, home, trashTestUID, trashTestGID, true, services.RestoreOptions{})
	require.ErrorIs(t, err, bridgeipc.ErrForbidden)
	require.FileExists(t, item)

	// The trash folder itself counts too, as .Trash/<uid> in a shared .Trash.
	require.NoError(t, os.Chown(files, trashTestUID, trashTestGID))
	require.NoError(t, os.Chown(filepath.Dir(files), 23456, 23457))
	_, err = services.RestoreFromTrash(t.Context(), item, home, trashTestUID, trashTestGID, true, services.RestoreOptions{})
	require.ErrorIs(t, err, bridgeipc.ErrForbidden)
	require.FileExists(t, item)

	report := filepath.Join(home, "report.txt")
	require.NoError(t, os.WriteFile(report, []byte("report"), 0o644))
	require.NoError(t, os.Chown(report, trashTestUID, trashTestGID))
	_, err = services.MoveToTrash(t.Context(), report, home, trashTestUID, trashTestGID, true)
	require.ErrorIs(t, err, bridgeipc.ErrForbidden)
	require.FileExists(t, report)
	require.NoFileExists(t, filepath.Join(files, "report.txt"))
}

// An item the session user neither owns nor can write cannot carry content a
// process of theirs put there, so it goes back into a folder they do not own,
// as a www-data file into /var/www does; one they can write is refused. The
// bridge has no record of trashing either, so the destination must be
// confirmed.
func TestTrashRestoreTakesAnItemTheUserCouldNotWriteAsRoot(t *testing.T) {
	base, home := trashTestHome(t)
	www := filepath.Join(base, "var", "www")
	require.NoError(t, os.MkdirAll(www, 0o755))
	const wwwData = 33

	item := userTrashItem(t, home, "index.html", filepath.Join(www, "index.html"), wwwData)
	_, err := services.RestoreFromTrash(t.Context(), item, home, trashTestUID, trashTestGID, true, services.RestoreOptions{})
	require.ErrorIs(t, err, bridgeipc.ErrForbidden)
	require.FileExists(t, item)
	restored, err := services.RestoreFromTrash(t.Context(), item, home, trashTestUID, trashTestGID, true, services.RestoreOptions{Confirmed: []string{filepath.Join(www, "index.html")}})
	require.NoError(t, err)
	require.Equal(t, filepath.Join(www, "index.html"), restored)
	requireOwnerAndMode(t, restored, wwwData, wwwData, 0o644)

	shared := userTrashItem(t, home, "shared.html", filepath.Join(www, "shared.html"), wwwData)
	require.NoError(t, os.Chmod(shared, 0o664))
	_, err = services.RestoreFromTrash(t.Context(), shared, home, trashTestUID, trashTestGID, true, services.RestoreOptions{})
	require.ErrorIs(t, err, bridgeipc.ErrForbidden)
	require.FileExists(t, shared)
	require.NoFileExists(t, filepath.Join(www, "shared.html"))
}

// A per-filesystem trash (.Trash-<uid> in the mount's top folder) is the
// session user's, as the XDG trash spec has it, even in a root-owned top:
// the folder this request makes there is handed over, and what is in it
// follows. Root's item in it stays root's and goes back where it was.
func TestTrashHandsAPerFilesystemTrashToSessionUserAsRoot(t *testing.T) {
	_, home := trashTestHome(t)
	shm, err := os.MkdirTemp("/dev/shm", "linuxio-trash-test-")
	if err != nil {
		t.Skip("needs /dev/shm")
	}
	t.Cleanup(func() { _ = os.RemoveAll(shm) })
	var shmStat, homeStat syscall.Stat_t
	require.NoError(t, syscall.Stat(shm, &shmStat))
	require.NoError(t, syscall.Stat(home, &homeStat))
	trash := filepath.Join("/dev/shm", ".Trash-"+strconv.Itoa(trashTestUID))
	if shmStat.Dev == homeStat.Dev {
		t.Skip("/dev/shm is on the same filesystem as the test home")
	}
	for _, existing := range []string{trash, "/dev/shm/.Trash"} {
		if _, statErr := os.Lstat(existing); statErr == nil {
			t.Skipf("%s already exists", existing)
		}
	}
	t.Cleanup(func() { _ = os.RemoveAll(trash) })
	report := filepath.Join(shm, "report.txt")
	require.NoError(t, os.WriteFile(report, []byte("report"), 0o644))

	trashed, err := services.MoveToTrash(t.Context(), report, home, trashTestUID, trashTestGID, true)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(trash, "files", "report.txt"), trashed)
	for _, dir := range []string{trash, filepath.Join(trash, "files"), filepath.Join(trash, "info")} {
		requireOwnerAndMode(t, dir, trashTestUID, trashTestGID, 0o700)
	}
	requireOwnerAndMode(t, filepath.Join(trash, "info", "report.txt.trashinfo"), trashTestUID, trashTestGID, 0o600)
	requireOwner(t, trashed, 0, 0)

	restored, err := services.RestoreFromTrash(t.Context(), trashed, home, trashTestUID, trashTestGID, true, services.RestoreOptions{})
	require.NoError(t, err)
	require.Equal(t, report, restored)
	requireOwnerAndMode(t, report, 0, 0, 0o644)
}

// A destination is confirmed for one item of the request: another item,
// whose Path= the session user rewrote to that destination, gets none.
func TestConfirmedRestoreIsPerItem(t *testing.T) {
	req := apischema.TrashRestoreRequest{
		Paths:                 []string{"/home/u/.local/share/Trash/files/a.conf", "/home/u/.local/share/Trash/files/b.conf"},
		ConfirmedDestinations: map[string]string{"/home/u/.local/share/Trash/files/./a.conf": "/etc/app.d/../a.conf"},
	}
	require.Equal(t, []string{"/etc/a.conf"}, confirmedRestores(req)[req.Paths[0]])
	require.Nil(t, confirmedRestores(req)[req.Paths[1]])
	require.Nil(t, confirmedRestores(apischema.TrashRestoreRequest{Paths: req.Paths})[req.Paths[0]])
}

// What root trashed from a system folder goes back over what took its name
// only when the request says replace, and beside it with keep both: root's,
// as before, with nothing left behind in the folder or the trash. An item the
// session user could have written is refused there whatever the request
// chose, so a replace never takes away what is there.
func TestTrashRestoreChoicesIntoASystemFolderAsRoot(t *testing.T) {
	base, home := trashTestHome(t)
	conf := filepath.Join(base, "etc", "app.d")
	require.NoError(t, os.MkdirAll(conf, 0o755))
	original := filepath.Join(conf, "app.conf")
	require.NoError(t, os.WriteFile(original, []byte("root"), 0o644))
	restore := func(trashed string, opts services.RestoreOptions) (string, error) {
		return services.RestoreFromTrash(t.Context(), trashed, home, trashTestUID, trashTestGID, true, opts)
	}

	trashed, err := services.MoveToTrash(t.Context(), original, home, trashTestUID, trashTestGID, true)
	require.NoError(t, err)
	trashFiles := filepath.Dir(trashed)
	require.NoError(t, os.WriteFile(original, []byte("newer"), 0o600))
	_, err = restore(trashed, services.RestoreOptions{})
	require.ErrorIs(t, err, services.ErrNameTaken)
	restored, err := restore(trashed, services.RestoreOptions{Replace: true})
	require.NoError(t, err)
	require.Equal(t, original, restored)
	require.Equal(t, "root", readString(t, original))
	requireOwnerAndMode(t, original, 0, 0, 0o644)
	entries, err := os.ReadDir(conf)
	require.NoError(t, err)
	require.Len(t, entries, 1, "only app.conf, nothing set aside")
	requireNoEntries(t, trashFiles)

	trashed, err = services.MoveToTrash(t.Context(), original, home, trashTestUID, trashTestGID, true)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(original, []byte("newer"), 0o600))
	restored, err = restore(trashed, services.RestoreOptions{KeepBoth: true})
	require.NoError(t, err)
	require.Equal(t, filepath.Join(conf, "app (copy).conf"), restored)
	require.Equal(t, "root", readString(t, restored))
	requireOwnerAndMode(t, restored, 0, 0, 0o644)
	require.Equal(t, "newer", readString(t, original))
	requireNoEntries(t, trashFiles)

	item := userTrashItem(t, home, "evil.conf", original, trashTestUID)
	for _, opts := range []services.RestoreOptions{
		{Confirmed: []string{original}, Replace: true},
		{Confirmed: []string{original}, KeepBoth: true},
	} {
		_, err = restore(item, opts)
		require.ErrorIs(t, err, bridgeipc.ErrForbidden)
		require.Equal(t, "newer", readString(t, original))
		require.FileExists(t, item)
	}

	// Root's own folder, recorded, still does not merge into the system
	// folder that took its name.
	trashed, err = services.MoveToTrash(t.Context(), conf, home, trashTestUID, trashTestGID, true)
	require.NoError(t, err)
	require.NoError(t, os.Mkdir(conf, 0o755))
	_, err = restore(trashed, services.RestoreOptions{Replace: true})
	require.ErrorIs(t, err, bridgeipc.ErrForbidden)
	requireNoEntries(t, conf)
	require.DirExists(t, trashed)

	// Nor does the user's folder merge into a folder root owns inside one of
	// the user's own folders: the merge would write where the user may not.
	docs := filepath.Join(home, "docs")
	require.NoError(t, os.Mkdir(docs, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(docs, "a.txt"), []byte("user"), 0o644))
	require.NoError(t, os.Chown(filepath.Join(docs, "a.txt"), trashTestUID, trashTestGID))
	require.NoError(t, os.Chown(docs, trashTestUID, trashTestGID))
	trashed, err = services.MoveToTrash(t.Context(), docs, home, trashTestUID, trashTestGID, true)
	require.NoError(t, err)
	require.NoError(t, os.Mkdir(docs, 0o755))
	_, err = restore(trashed, services.RestoreOptions{Replace: true})
	require.ErrorIs(t, err, bridgeipc.ErrForbidden)
	requireNoEntries(t, docs)
	require.DirExists(t, trashed)
	require.NoError(t, os.Chown(docs, trashTestUID, trashTestGID))
	restored, err = restore(trashed, services.RestoreOptions{Replace: true})
	require.NoError(t, err)
	require.Equal(t, docs, restored)
	require.Equal(t, "user", readString(t, filepath.Join(docs, "a.txt")))
}

// Into a folder the admin chose an item goes as a move there would, even one
// the session user wrote into a folder only root may write: the folder and the
// name are the admin's choice in this request, not a claim read from the
// user's trash. The item keeps its owner, and the folder is reached as a move
// reaches it: through a link in a folder only root may write, never through
// one the user could have planted.
func TestTrashRestoreIntoAChosenFolderAsRoot(t *testing.T) {
	base, home := trashTestHome(t)
	srv := filepath.Join(base, "srv")
	require.NoError(t, os.Mkdir(srv, 0o755))
	restore := func(trashed, into, name string) (string, error) {
		return services.RestoreFromTrash(t.Context(), trashed, home, trashTestUID, trashTestGID, true, services.RestoreOptions{Into: into, Name: name})
	}

	item := userTrashItem(t, home, "notes.txt.2", filepath.Join(home, "notes.txt"), trashTestUID)
	restored, err := restore(item, srv, "notes.txt")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(srv, "notes.txt"), restored)
	requireOwnerAndMode(t, restored, trashTestUID, trashTestUID, 0o644)
	require.NoFileExists(t, item)

	item = userTrashItem(t, home, "report.txt", filepath.Join(home, "report.txt"), trashTestUID)
	planted := filepath.Join(home, "docs")
	require.NoError(t, os.Symlink(srv, planted))
	_, err = restore(item, planted, "")
	require.ErrorIs(t, err, bridgeipc.ErrForbidden)
	require.FileExists(t, item)
	require.NoFileExists(t, filepath.Join(srv, "report.txt"))

	require.NoError(t, os.Symlink(srv, filepath.Join(base, "alias")))
	_, err = restore(item, filepath.Join(base, "alias"), "")
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(srv, "report.txt"))
}

// The page tells two failures apart without reading the message: a restore
// whose name is taken, and a move to the trash on a filesystem without one.
func TestTrashFailureReasons(t *testing.T) {
	require.Equal(t, apischema.FileBatchFailureExists, failureReason(fmt.Errorf("restore a: %w", services.ErrNameTaken)))
	require.Equal(t, apischema.FileBatchFailureNoTrash, failureReason(fmt.Errorf("trash a: %w: %w", services.ErrNoTrash, syscall.EACCES)))
	require.Empty(t, failureReason(fmt.Errorf("%w: a is a link", bridgeipc.ErrForbidden)))
}

// trashTaskHome is the home of a task test's session user, who is the user
// running the test: the bridge's own home is $HOME when no session is set.
// It returns the indexer requests the task makes.
func trashTaskHome(t *testing.T) (string, *[]recordedIndexerRequest) {
	t.Helper()
	requests := recordIndexerRequests(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home, requests
}

// A restore fails the items whose names are taken with reason "exists"; sent
// again with the user's choices, an item to keep both gets a "name (copy)"
// name and the others replace what is there, which the indexer drops first.
func TestTrashRestoreTaskAsksAboutTakenNames(t *testing.T) {
	home, requests := trashTaskHome(t)
	docs := filepath.Join(home, "docs")
	require.NoError(t, os.Mkdir(docs, 0o755))
	var items []string
	for _, name := range []string{"a.txt", "b.txt"} {
		path := seedFile(t, filepath.Join(docs, name), "trashed "+name)
		item, err := services.MoveToTrash(t.Context(), path, home, os.Geteuid(), os.Getegid(), false)
		require.NoError(t, err)
		seedFile(t, path, "newer "+name)
		items = append(items, item)
	}

	req := apischema.TrashRestoreRequest{Paths: items}
	result, err := runTrashRestoreTask(t.Context(), newBatchTask(t, "filebrowser.trash_restore", req), nil, req, sessionOwner{})
	require.NoError(t, err)
	require.Zero(t, result.Succeeded)
	require.Len(t, result.Failed, 2)
	for _, failure := range result.Failed {
		require.Equal(t, apischema.FileBatchFailureExists, failure.Reason)
		require.Contains(t, failure.Error, "already exists")
	}

	overwrite := true
	req = apischema.TrashRestoreRequest{Paths: items, Overwrite: &overwrite, KeepBoth: []string{items[0]}}
	result, err = runTrashRestoreTask(t.Context(), newBatchTask(t, "filebrowser.trash_restore", req), nil, req, sessionOwner{})
	detachedIndexerUpdates.Wait()
	require.NoError(t, err)
	require.Equal(t, 2, result.Succeeded, result.Failed)
	require.Equal(t, "newer a.txt", readString(t, filepath.Join(docs, "a.txt")))
	require.Equal(t, "trashed a.txt", readString(t, filepath.Join(docs, "a (copy).txt")))
	require.Equal(t, "trashed b.txt", readString(t, filepath.Join(docs, "b.txt")))
	dropped := recordedIndexerRequest{method: http.MethodDelete, path: indexerapi.RouteDelete, query: url.Values{"path": {filepath.Join(docs, "b.txt")}}.Encode()}
	require.Contains(t, *requests, dropped)
}

// A restore into a chosen folder takes an existing folder, checked once for
// the whole request as a move's destination is, and the name the request
// gives each item.
func TestTrashRestoreTaskIntoAChosenFolder(t *testing.T) {
	home, _ := trashTaskHome(t)
	report := seedFile(t, filepath.Join(home, "report.txt"), "report")
	item, err := services.MoveToTrash(t.Context(), report, home, os.Geteuid(), os.Getegid(), false)
	require.NoError(t, err)
	chosen := filepath.Join(home, "chosen")
	require.NoError(t, os.Mkdir(chosen, 0o755))

	for destination, want := range map[string]error{filepath.Join(home, "missing"): bridgeipc.ErrNotFound, item: bridgeipc.ErrInvalidArgs} {
		req := apischema.TrashRestoreRequest{Paths: []string{item}, Destination: destination}
		_, err = runTrashRestoreTask(t.Context(), newBatchTask(t, "filebrowser.trash_restore", req), nil, req, sessionOwner{})
		require.ErrorIs(t, err, want, destination)
		require.FileExists(t, item)
	}

	req := apischema.TrashRestoreRequest{Paths: []string{item}, Destination: chosen, Names: map[string]string{item: "renamed.txt"}}
	result, err := runTrashRestoreTask(t.Context(), newBatchTask(t, "filebrowser.trash_restore", req), nil, req, sessionOwner{})
	detachedIndexerUpdates.Wait()
	require.NoError(t, err)
	require.Equal(t, 1, result.Succeeded, result.Failed)
	require.Equal(t, "report", readString(t, filepath.Join(chosen, "renamed.txt")))
}

// A move to the trash on a filesystem that cannot hold one fails with reason
// "no_trash", so the page can offer to delete the item for good, and leaves
// it where it is; a missing item fails without a reason.
func TestTrashBatchTaskReportsAFilesystemWithoutATrash(t *testing.T) {
	home, _ := trashTaskHome(t)
	seedFile(t, filepath.Join(home, ".local"), "not a folder")
	report := seedFile(t, filepath.Join(home, "report.txt"), "report")
	missing := filepath.Join(home, "missing.txt")

	req := apischema.BatchPathRequest{Paths: []string{report, missing}}
	result, err := runTrashBatchTask(t.Context(), newBatchTask(t, "filebrowser.trash_batch", req), nil, req, sessionOwner{})
	require.NoError(t, err)
	require.Len(t, result.Failed, 2)
	require.Equal(t, FileBatchItemFailure{Path: report, Error: result.Failed[0].Error, Reason: apischema.FileBatchFailureNoTrash}, result.Failed[0])
	require.Equal(t, missing, result.Failed[1].Path)
	require.Empty(t, result.Failed[1].Reason)
	require.FileExists(t, report)
}

// filebrowser.trash_size reads the session user's own trash, as the list
// does: a Call any session may make, and safe to repeat.
func TestTrashSizeRouteIsAnUnprivilegedRead(t *testing.T) {
	found := false
	for _, route := range Routes {
		if route.Route != "filebrowser.trash_size" {
			continue
		}
		found = true
		require.Equal(t, bridgeipc.ModeCall, route.Mode)
		require.False(t, route.Privileged)
		require.True(t, route.RetrySafe)
	}
	require.True(t, found, "filebrowser.trash_size is a route")
}

// A trashed folder's properties read its size. What is not a folder in the
// session user's trash is the caller's mistake, and a missing one is not
// found.
func TestTrashSizeOfATrashedFolder(t *testing.T) {
	home, _ := trashTaskHome(t)
	photos := filepath.Join(home, "photos")
	require.NoError(t, os.Mkdir(photos, 0o755))
	seedFile(t, filepath.Join(photos, "a.jpg"), "jpeg")
	require.NoError(t, os.Mkdir(filepath.Join(photos, "2026"), 0o755))
	item, err := services.MoveToTrash(t.Context(), photos, home, os.Geteuid(), os.Getegid(), false)
	require.NoError(t, err)

	size, err := trashSize(t.Context(), sessionOwner{}, apischema.PathRequest{Path: item})
	require.NoError(t, err)
	// Size on disk, as the folder listing gives it.
	onDisk := int64(0)
	for _, path := range []string{item, filepath.Join(item, "2026"), filepath.Join(item, "a.jpg")} {
		var st syscall.Stat_t
		require.NoError(t, syscall.Lstat(path, &st))
		onDisk += st.Blocks * 512
	}
	require.Equal(t, apischema.DirectorySizeData{Size: onDisk, FileCount: 1, FolderCount: 1}, size)

	for path, want := range map[string]error{
		"":     bridgeipc.ErrInvalidArgs,
		photos: bridgeipc.ErrInvalidArgs,
		filepath.Join(filepath.Dir(item), "gone"): bridgeipc.ErrNotFound,
	} {
		_, err = trashSize(t.Context(), sessionOwner{}, apischema.PathRequest{Path: path})
		require.ErrorIs(t, err, want, path)
	}
}

// A trashed folder the session cannot read is a refusal, not a size of 0 B.
func TestTrashSizeOfAnUnreadableFolderIsForbidden(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads any folder")
	}
	home, _ := trashTaskHome(t)
	locked := filepath.Join(home, "locked")
	require.NoError(t, os.Mkdir(locked, 0o755))
	item, err := services.MoveToTrash(t.Context(), locked, home, os.Geteuid(), os.Getegid(), false)
	require.NoError(t, err)
	require.NoError(t, os.Chmod(item, 0))
	t.Cleanup(func() { _ = os.Chmod(item, 0o700) })

	_, err = trashSize(t.Context(), sessionOwner{}, apischema.PathRequest{Path: item})
	require.ErrorIs(t, err, bridgeipc.ErrForbidden)
}
