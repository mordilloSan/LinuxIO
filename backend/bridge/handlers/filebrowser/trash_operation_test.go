package filebrowser

import (
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/services"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
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

		_, err := services.RestoreFromTrash(t.Context(), item, home, trashTestUID, trashTestGID, true, nil)
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
	restored, err := services.RestoreFromTrash(t.Context(), trashed, home, trashTestUID, trashTestGID, true, nil)
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
	_, err = services.RestoreFromTrash(t.Context(), trashed, home, trashTestUID, trashTestGID, true, nil)
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
	restored, err := services.RestoreFromTrash(t.Context(), trashed, home, trashTestUID, trashTestGID, true, nil)
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

	_, err := services.RestoreFromTrash(t.Context(), item, home, trashTestUID, trashTestGID, true, nil)
	require.ErrorIs(t, err, bridgeipc.ErrForbidden)
	require.FileExists(t, item)

	// The trash folder itself counts too, as .Trash/<uid> in a shared .Trash.
	require.NoError(t, os.Chown(files, trashTestUID, trashTestGID))
	require.NoError(t, os.Chown(filepath.Dir(files), 23456, 23457))
	_, err = services.RestoreFromTrash(t.Context(), item, home, trashTestUID, trashTestGID, true, nil)
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
	_, err := services.RestoreFromTrash(t.Context(), item, home, trashTestUID, trashTestGID, true, nil)
	require.ErrorIs(t, err, bridgeipc.ErrForbidden)
	require.FileExists(t, item)
	restored, err := services.RestoreFromTrash(t.Context(), item, home, trashTestUID, trashTestGID, true, []string{filepath.Join(www, "index.html")})
	require.NoError(t, err)
	require.Equal(t, filepath.Join(www, "index.html"), restored)
	requireOwnerAndMode(t, restored, wwwData, wwwData, 0o644)

	shared := userTrashItem(t, home, "shared.html", filepath.Join(www, "shared.html"), wwwData)
	require.NoError(t, os.Chmod(shared, 0o664))
	_, err = services.RestoreFromTrash(t.Context(), shared, home, trashTestUID, trashTestGID, true, nil)
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

	restored, err := services.RestoreFromTrash(t.Context(), trashed, home, trashTestUID, trashTestGID, true, nil)
	require.NoError(t, err)
	require.Equal(t, report, restored)
	requireOwnerAndMode(t, report, 0, 0, 0o644)
}
