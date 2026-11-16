package services

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

// trashInfoOf is the .trashinfo of the trashed item at path.
func trashInfoOf(path string) string {
	return filepath.Join(filepath.Dir(filepath.Dir(path)), "info", filepath.Base(path)+".trashinfo")
}

// trashedThenRetaken trashes docs/report.txt from a new home and puts another
// report.txt where it was, as when the name is taken again before a restore.
func trashedThenRetaken(t *testing.T) (home, original, item string) {
	t.Helper()
	home = t.TempDir()
	original = createTestFile(t, createTestDir(t, home, "docs"), "report.txt", []byte("trashed"))
	item, err := MoveToTrash(t.Context(), original, home, testTrashUID, testTrashUID, false)
	require.NoError(t, err)
	createTestFile(t, filepath.Dir(original), "report.txt", []byte("newer"))
	return home, original, item
}

func restoreTestItem(t *testing.T, home, item string, opts RestoreOptions) (string, error) {
	t.Helper()
	return RestoreFromTrash(t.Context(), item, home, testTrashUID, testTrashUID, false, opts)
}

// A restore whose name is taken fails, recognisably, and leaves the item in
// the trash and what took its name alone. Keep both puts it in under the next
// free "name (copy)" name; replace puts it over what is there, as a move with
// overwrite does, and never puts a file over a folder.
func TestRestoreWhenItsNameIsTaken(t *testing.T) {
	t.Run("fails_without_a_choice", func(t *testing.T) {
		home, original, item := trashedThenRetaken(t)
		_, err := restoreTestItem(t, home, item, RestoreOptions{})
		require.ErrorIs(t, err, ErrNameTaken)
		require.ErrorIs(t, err, bridgeipc.ErrConflict)
		require.ErrorContains(t, err, original+" already exists")
		assert.Equal(t, "newer", readString(t, original))
		assert.FileExists(t, item)
		assert.FileExists(t, trashInfoOf(item))
	})
	t.Run("keep_both", func(t *testing.T) {
		home, original, item := trashedThenRetaken(t)
		docs := filepath.Dir(original)
		createTestFile(t, docs, "report (copy).txt", []byte("earlier copy"))
		restored, err := restoreTestItem(t, home, item, RestoreOptions{KeepBoth: true})
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(docs, "report (copy 2).txt"), restored)
		assert.Equal(t, "trashed", readString(t, restored))
		assert.Equal(t, "newer", readString(t, original))
		assert.NoFileExists(t, item)
		assert.NoFileExists(t, trashInfoOf(item))
	})
	t.Run("replace_a_file", func(t *testing.T) {
		home, original, item := trashedThenRetaken(t)
		restored, err := restoreTestItem(t, home, item, RestoreOptions{Replace: true})
		require.NoError(t, err)
		assert.Equal(t, original, restored)
		assert.Equal(t, "trashed", readString(t, original))
		requireEntries(t, filepath.Dir(original), "report.txt")
		assert.NoFileExists(t, item)
		assert.NoFileExists(t, trashInfoOf(item))
	})
	t.Run("replace_merges_a_folder", func(t *testing.T) {
		home := t.TempDir()
		docs := createTestDir(t, home, "docs")
		photos := createTestDir(t, docs, "photos")
		createTestFile(t, photos, "a.jpg", []byte("trashed a"))
		createTestFile(t, photos, "b.jpg", []byte("b"))
		item, err := MoveToTrash(t.Context(), photos, home, testTrashUID, testTrashUID, false)
		require.NoError(t, err)
		createTestFile(t, createTestDir(t, docs, "photos"), "a.jpg", []byte("newer a"))
		createTestFile(t, photos, "c.jpg", []byte("c"))

		restored, err := restoreTestItem(t, home, item, RestoreOptions{Replace: true})
		require.NoError(t, err)
		assert.Equal(t, photos, restored)
		requireEntries(t, photos, "a.jpg", "b.jpg", "c.jpg")
		assert.Equal(t, "trashed a", readString(t, filepath.Join(photos, "a.jpg")))
		assert.NoDirExists(t, item)
		assert.NoFileExists(t, trashInfoOf(item))
	})
	t.Run("replace_never_puts_a_file_over_a_folder", func(t *testing.T) {
		home, original, item := trashedThenRetaken(t)
		require.NoError(t, os.Remove(original))
		createTestDir(t, filepath.Dir(original), "report.txt")
		_, err := restoreTestItem(t, home, item, RestoreOptions{Replace: true})
		require.ErrorContains(t, err, "different type")
		assert.DirExists(t, original)
		assert.FileExists(t, item)
		assert.FileExists(t, trashInfoOf(item))
	})
}

// A replace goes through handles: a link the user put in the folder it
// merges into is replaced, never written through, and what it pointed at is
// left alone.
func TestRestoreReplaceNeverWritesThroughALink(t *testing.T) {
	home := t.TempDir()
	victim := createTestFile(t, t.TempDir(), "victim.conf", []byte("victim"))
	docs := createTestDir(t, home, "docs")
	config := createTestDir(t, docs, "config")
	createTestFile(t, config, "app.conf", []byte("trashed"))
	item, err := MoveToTrash(t.Context(), config, home, testTrashUID, testTrashUID, false)
	require.NoError(t, err)
	plantTrashLink(t, victim, filepath.Join(createTestDir(t, docs, "config"), "app.conf"))

	_, err = restoreTestItem(t, home, item, RestoreOptions{Replace: true})
	require.NoError(t, err)
	assert.Equal(t, "victim", readString(t, victim))
	info, err := os.Lstat(filepath.Join(config, "app.conf"))
	require.NoError(t, err)
	assert.True(t, info.Mode().IsRegular(), "the link is replaced by the restored file")
	assert.Equal(t, "trashed", readString(t, filepath.Join(config, "app.conf")))
}

// Restored into a folder the user chose, an item goes in under the name the
// request gives it, whatever its .trashinfo says, or else under its name in
// the trash. A taken name there gets the same choices, and a folder that is
// missing or not a folder, or a name that is not one, fails the item, which
// stays.
func TestRestoreIntoAChosenFolder(t *testing.T) {
	home := t.TempDir()
	first := createTestFile(t, createTestDir(t, home, "a"), "notes.txt", []byte("first"))
	second := createTestFile(t, createTestDir(t, home, "b"), "notes.txt", []byte("second"))
	_, err := MoveToTrash(t.Context(), first, home, testTrashUID, testTrashUID, false)
	require.NoError(t, err)
	item, err := MoveToTrash(t.Context(), second, home, testTrashUID, testTrashUID, false)
	require.NoError(t, err)
	require.Equal(t, "notes.txt.2", filepath.Base(item))
	chosen := createTestDir(t, home, "chosen")
	// The user's own edit after the page listed the item names nothing.
	writeTestTrashInfo(t, filepath.Join(home, ".local/share/Trash"), "notes.txt.2", filepath.Join(home, "b", "index.php"))

	restored, err := restoreTestItem(t, home, item, RestoreOptions{Into: chosen, Name: "notes.txt"})
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(chosen, "notes.txt"), restored)
	assert.Equal(t, "second", readString(t, restored))
	assert.NoFileExists(t, trashInfoOf(item))
	assert.NoFileExists(t, second, "not where it came from")

	item = filepath.Join(filepath.Dir(item), "notes.txt")
	_, err = restoreTestItem(t, home, item, RestoreOptions{Into: chosen})
	require.ErrorIs(t, err, ErrNameTaken)
	assert.FileExists(t, item)
	restored, err = restoreTestItem(t, home, item, RestoreOptions{Into: chosen, KeepBoth: true})
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(chosen, "notes (copy).txt"), restored)
	assert.Equal(t, "first", readString(t, restored))

	orphan := createTestFile(t, filepath.Join(home, ".local/share/Trash/files"), "orphan.txt", []byte("orphan"))
	restored, err = restoreTestItem(t, home, orphan, RestoreOptions{Into: chosen})
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(chosen, "orphan.txt"), restored)

	stays := createTestFile(t, filepath.Join(home, ".local/share/Trash/files"), "stays.txt", []byte("stays"))
	for _, into := range []string{filepath.Join(home, "missing"), restored} {
		_, err = restoreTestItem(t, home, stays, RestoreOptions{Into: into})
		require.Error(t, err, into)
		assert.FileExists(t, stays)
	}
	for _, name := range []string{"..", ".", "../escape.txt", "a/b"} {
		_, err = restoreTestItem(t, home, stays, RestoreOptions{Into: chosen, Name: name})
		require.ErrorIs(t, err, bridgeipc.ErrInvalidArgs, name)
		assert.FileExists(t, stays)
	}
	requireEntries(t, chosen, "notes (copy).txt", "notes.txt", "orphan.txt")
}

// Back where it came from, a root bridge replaces only what the request saw:
// an item without a record goes over what took its name only when the
// request confirms that destination, so a Path= rewritten after the page
// asked replaces nothing. A folder never merges into a folder the session
// user does not own, as the could-have-written check vouches only for the
// top of the item; keep both still works there.
func TestRestoreReplaceNeedsAFixedDestination(t *testing.T) {
	t.Run("unrecorded_item", func(t *testing.T) {
		uid, gid := os.Getuid(), os.Getgid()
		home, original, item := trashedThenRetaken(t)
		other := createTestFile(t, filepath.Dir(original), "other.txt", []byte("other"))
		restore := func(opts RestoreOptions) error {
			_, err := RestoreFromTrash(t.Context(), item, home, uid, gid, true, opts)
			return err
		}

		require.ErrorIs(t, restore(RestoreOptions{Replace: true}), bridgeipc.ErrForbidden)
		writeTestTrashInfo(t, filepath.Join(home, ".local/share/Trash"), "report.txt", other)
		require.ErrorIs(t, restore(RestoreOptions{Replace: true, Confirmed: []string{original}}), bridgeipc.ErrForbidden)
		assert.Equal(t, "newer", readString(t, original))
		assert.Equal(t, "other", readString(t, other))
		assert.FileExists(t, item)

		writeTestTrashInfo(t, filepath.Join(home, ".local/share/Trash"), "report.txt", original)
		require.NoError(t, restore(RestoreOptions{Replace: true, Confirmed: []string{original}}))
		assert.Equal(t, "trashed", readString(t, original))
	})
	t.Run("folder_into_a_system_folder", func(t *testing.T) {
		_, uid, gid := otherSession()
		useTestTrashRecords(t, uid)
		home, system := t.TempDir(), t.TempDir()
		confd := createTestDir(t, system, "conf.d")
		createTestFile(t, confd, "a.conf", []byte("trashed"))
		item, err := MoveToTrash(t.Context(), confd, home, uid, gid, true)
		require.NoError(t, err)
		createTestFile(t, createTestDir(t, system, "conf.d"), "a.conf", []byte("newer"))

		_, err = RestoreFromTrash(t.Context(), item, home, uid, gid, true, RestoreOptions{Replace: true})
		require.ErrorIs(t, err, bridgeipc.ErrForbidden)
		require.ErrorContains(t, err, "not merged")
		assert.Equal(t, "newer", readString(t, filepath.Join(confd, "a.conf")))
		assert.DirExists(t, item)

		restored, err := RestoreFromTrash(t.Context(), item, home, uid, gid, true, RestoreOptions{KeepBoth: true})
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(system, "conf.d (copy)"), restored)
		assert.Equal(t, "trashed", readString(t, filepath.Join(restored, "a.conf")))
	})
}

// A folder the user chose is reached as a move reaches it: on a root bridge
// a link in a folder the session user can write is refused, not followed.
func TestRestoreIntoAChosenFolderNeverGoesThroughALinkTheUserCouldPlant(t *testing.T) {
	f := newPlantedLinkFixture(t)
	report := createTestFile(t, f.home, "report.txt", []byte("report"))
	item, err := MoveToTrash(t.Context(), report, f.home, os.Getuid(), os.Getgid(), false)
	require.NoError(t, err)

	_, err = RestoreFromTrash(t.Context(), item, f.home, os.Getuid(), os.Getgid(), true, RestoreOptions{Into: f.docs})
	require.ErrorIs(t, err, bridgeipc.ErrForbidden)
	f.requireVictimUntouched(t)
	assert.FileExists(t, item)
}

// Back where it came from, into a folder the session user does not own, an
// item the user could have written is refused whatever the request chose, so
// a replace never takes away what is there. Into a folder the user chose it
// goes, as a move there would: that destination is the user's own choice in
// the request, not a claim read from the trash, and needs no confirmation.
func TestRestoreIntoAChosenFolderIsAMoveTheUserAskedFor(t *testing.T) {
	_, uid, gid := otherSession()
	useTestTrashRecords(t, uid)
	home, system := t.TempDir(), t.TempDir()
	trash := filepath.Join(home, ".local/share/Trash")
	require.NoError(t, os.MkdirAll(filepath.Join(trash, "info"), 0o700))
	item := createTestFile(t, createTestDir(t, trash, "files"), "app.conf", []byte("user's"))
	require.NoError(t, os.Chmod(item, 0o664))
	dest := createTestFile(t, system, "app.conf", []byte("system's"))
	writeTestTrashInfo(t, trash, "app.conf", dest)

	for _, opts := range []RestoreOptions{
		{Confirmed: []string{dest}},
		{Confirmed: []string{dest}, Replace: true},
		{Confirmed: []string{dest}, KeepBoth: true},
	} {
		_, err := RestoreFromTrash(t.Context(), item, home, uid, gid, true, opts)
		require.ErrorIs(t, err, bridgeipc.ErrForbidden)
		assert.Equal(t, "system's", readString(t, dest))
		requireEntries(t, system, "app.conf")
		assert.FileExists(t, item)
	}

	restored, err := RestoreFromTrash(t.Context(), item, home, uid, gid, true, RestoreOptions{Into: system, Name: "app.conf", Replace: true})
	require.NoError(t, err)
	assert.Equal(t, dest, restored)
	assert.Equal(t, "user's", readString(t, dest))
	requireEntries(t, system, "app.conf")
	assert.NoFileExists(t, item)
}

// What a root bridge trashed from a folder the session user does not own
// goes back over what took its name only with replace, staged and checked
// as any restore there, or beside it with keep both; nothing is left behind
// in the folder or the trash.
func TestRestoreChoicesIntoASystemFolder(t *testing.T) {
	_, uid, gid := otherSession()
	useTestTrashRecords(t, uid)
	home, system := t.TempDir(), t.TempDir()
	conf := createTestFile(t, system, "app.conf", []byte("trashed"))
	require.NoError(t, os.Chmod(conf, 0o644))
	files := filepath.Join(home, ".local/share/Trash/files")

	item, err := MoveToTrash(t.Context(), conf, home, uid, gid, true)
	require.NoError(t, err)
	createTestFile(t, system, "app.conf", []byte("newer"))
	_, err = RestoreFromTrash(t.Context(), item, home, uid, gid, true, RestoreOptions{})
	require.ErrorIs(t, err, ErrNameTaken)
	restored, err := RestoreFromTrash(t.Context(), item, home, uid, gid, true, RestoreOptions{Replace: true})
	require.NoError(t, err)
	assert.Equal(t, conf, restored)
	assert.Equal(t, "trashed", readString(t, conf))
	requireEntries(t, system, "app.conf")
	requireEmptyDir(t, files)

	item, err = MoveToTrash(t.Context(), conf, home, uid, gid, true)
	require.NoError(t, err)
	createTestFile(t, system, "app.conf", []byte("newer"))
	restored, err = RestoreFromTrash(t.Context(), item, home, uid, gid, true, RestoreOptions{KeepBoth: true})
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(system, "app (copy).conf"), restored)
	assert.Equal(t, "trashed", readString(t, restored))
	assert.Equal(t, "newer", readString(t, conf))
	requireEmptyDir(t, files)
}

// A restore renames and never copies: a chosen folder on another filesystem
// than the trash is refused and the item stays.
func TestRestoreIntoAFolderOnAnotherFilesystemIsRefused(t *testing.T) {
	shm, err := os.MkdirTemp("/dev/shm", "linuxio-restore-test-")
	if err != nil {
		t.Skip("needs /dev/shm")
	}
	t.Cleanup(func() { _ = os.RemoveAll(shm) })
	home := t.TempDir()
	var shmStat, homeStat unix.Stat_t
	require.NoError(t, unix.Stat(shm, &shmStat))
	require.NoError(t, unix.Stat(home, &homeStat))
	if shmStat.Dev == homeStat.Dev {
		t.Skip("/dev/shm is on the same filesystem as the test home")
	}
	item, err := MoveToTrash(t.Context(), createTestFile(t, home, "notes.txt", []byte("notes")), home, testTrashUID, testTrashUID, false)
	require.NoError(t, err)

	_, err = restoreTestItem(t, home, item, RestoreOptions{Into: shm})
	require.ErrorContains(t, err, "another filesystem")
	assert.FileExists(t, item)
	requireEmptyDir(t, shm)
}

// A filesystem whose trash cannot be made or opened fails the move with
// ErrNoTrash and leaves the item where it is; a protected or missing item
// fails for its own reason.
func TestMoveToTrashWithoutATrashOnTheFilesystem(t *testing.T) {
	home := t.TempDir()
	createTestFile(t, home, ".local", []byte("not a folder"))
	report := createTestFile(t, home, "report.txt", []byte("report"))

	_, err := MoveToTrash(t.Context(), report, home, testTrashUID, testTrashUID, false)
	require.ErrorIs(t, err, ErrNoTrash)
	assert.FileExists(t, report)

	for _, path := range []string{filepath.Join(home, "missing.txt"), "/etc"} {
		_, err = MoveToTrash(t.Context(), path, home, testTrashUID, testTrashUID, false)
		require.Error(t, err, path)
		require.NotErrorIs(t, err, ErrNoTrash, path)
	}

	// A refusal keeps its own meaning, and so does a failure a retry could
	// get past or a permanent delete would meet too.
	for _, err := range []error{bridgeipc.ErrForbidden, unix.EROFS, unix.ENOSPC, unix.EIO, unix.EMFILE} {
		require.NotErrorIs(t, noTrash(err), ErrNoTrash, err)
	}
	require.ErrorIs(t, noTrash(unix.EACCES), ErrNoTrash)
}

func TestCopyName(t *testing.T) {
	assert.Equal(t, "report (copy).txt", CopyName("report.txt", false, 1))
	assert.Equal(t, "report (copy 2).txt", CopyName("report.txt", false, 2))
	assert.Equal(t, "photos.d (copy 3)", CopyName("photos.d", true, 3))
	assert.Equal(t, ".bashrc (copy)", CopyName(".bashrc", false, 1))
}
