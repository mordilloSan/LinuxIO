package services

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

func TestMoveToTrash(t *testing.T) {
	uid, gid := os.Geteuid(), os.Getegid()

	t.Run("file_goes_to_home_trash_with_info", func(t *testing.T) {
		home := t.TempDir()
		src := createTestFile(t, home, "my file.txt", []byte("data"))

		dest, err := MoveToTrash(t.Context(), src, home, uid, gid, false)
		require.NoError(t, err)

		trash := filepath.Join(home, ".local/share/Trash")
		assert.Equal(t, filepath.Join(trash, "files", "my file.txt"), dest)
		assert.NoFileExists(t, src)
		got, err := os.ReadFile(dest)
		require.NoError(t, err)
		assert.Equal(t, "data", string(got))

		info, err := os.ReadFile(filepath.Join(trash, "info", "my file.txt.trashinfo"))
		require.NoError(t, err)
		assert.Regexp(t, `^\[Trash Info\]\nPath=`+regexp.QuoteMeta(home)+`/my%20file\.txt\nDeletionDate=\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d\n$`, string(info))
	})

	t.Run("name_collision_gets_suffix", func(t *testing.T) {
		home := t.TempDir()
		first := createTestFile(t, createTestDir(t, home, "a"), "x.txt", []byte("1"))
		second := createTestFile(t, createTestDir(t, home, "b"), "x.txt", []byte("2"))

		_, err := MoveToTrash(t.Context(), first, home, uid, gid, false)
		require.NoError(t, err)
		dest, err := MoveToTrash(t.Context(), second, home, uid, gid, false)
		require.NoError(t, err)

		trash := filepath.Join(home, ".local/share/Trash")
		assert.Equal(t, filepath.Join(trash, "files", "x.txt.2"), dest)
		assert.FileExists(t, filepath.Join(trash, "info", "x.txt.trashinfo"))
		assert.FileExists(t, filepath.Join(trash, "info", "x.txt.2.trashinfo"))
	})

	t.Run("directory_tree_moves_intact", func(t *testing.T) {
		home := t.TempDir()
		dir := createTestDir(t, home, "tree")
		createTestFile(t, createTestDir(t, dir, "sub"), "leaf.txt", []byte("leaf"))

		dest, err := MoveToTrash(t.Context(), dir, home, uid, gid, false)
		require.NoError(t, err)

		assert.NoDirExists(t, dir)
		assert.FileExists(t, filepath.Join(dest, "sub", "leaf.txt"))
	})

	t.Run("refuses_root_and_empty_home", func(t *testing.T) {
		home := t.TempDir()
		_, err := MoveToTrash(t.Context(), "/", home, uid, gid, false)
		require.Error(t, err)
		_, err = MoveToTrash(t.Context(), createTestFile(t, home, "f", nil), "", uid, gid, false)
		require.Error(t, err)
	})

	t.Run("missing_source_fails_without_info", func(t *testing.T) {
		home := t.TempDir()
		_, err := MoveToTrash(t.Context(), filepath.Join(home, "nope"), home, uid, gid, false)
		require.Error(t, err)
		assert.NoDirExists(t, filepath.Join(home, ".local/share/Trash"))
	})
}

// A uid no real account uses, so per-mount trashes on the host stay out of
// the listing and only the temp home trash is seen.
const testTrashUID = 424242

func TestListRestoreAndPurgeTrash(t *testing.T) {
	home := t.TempDir()
	docs := createTestDir(t, home, "docs")
	report := createTestFile(t, docs, "report.txt", []byte("report"))
	photos := createTestDir(t, docs, "photos")
	createTestFile(t, photos, "a.jpg", []byte("jpeg"))

	_, err := MoveToTrash(t.Context(), report, home, testTrashUID, testTrashUID, false)
	require.NoError(t, err)
	_, err = MoveToTrash(t.Context(), photos, home, testTrashUID, testTrashUID, false)
	require.NoError(t, err)

	items, err := ListTrash(t.Context(), home, testTrashUID)
	require.NoError(t, err)
	require.Len(t, items, 2)
	byName := map[string]TrashItem{}
	for _, item := range items {
		byName[item.Name] = item
	}
	assert.Equal(t, report, byName["report.txt"].OriginalPath)
	assert.Equal(t, int64(len("report")), byName["report.txt"].Size)
	assert.False(t, byName["report.txt"].IsDir)
	assert.True(t, byName["photos"].IsDir)
	assert.Equal(t, photos, byName["photos"].OriginalPath)
	assert.WithinDuration(t, time.Now(), byName["photos"].DeletedAt, time.Minute)

	// Restore recreates the missing parent folder and drops the info file.
	require.NoError(t, os.RemoveAll(docs))
	restored, err := RestoreFromTrash(t.Context(), byName["report.txt"].Path, home, testTrashUID, testTrashUID, false)
	require.NoError(t, err)
	assert.Equal(t, report, restored)
	content, err := os.ReadFile(report)
	require.NoError(t, err)
	assert.Equal(t, "report", string(content))
	assert.NoFileExists(t, filepath.Join(home, ".local/share/Trash/info/report.txt.trashinfo"))

	// A second restore of a name whose original now exists is refused.
	_, err = MoveToTrash(t.Context(), createTestFile(t, docs, "other.txt", []byte("x")), home, testTrashUID, testTrashUID, false)
	require.NoError(t, err)
	createTestFile(t, docs, "other.txt", []byte("newer"))
	_, err = RestoreFromTrash(t.Context(), filepath.Join(home, ".local/share/Trash/files/other.txt"), home, testTrashUID, testTrashUID, false)
	require.ErrorContains(t, err, "already exists")

	removed, err := PurgeFromTrash(t.Context(), byName["photos"].Path, home, testTrashUID, nil)
	require.NoError(t, err)
	assert.Equal(t, int64(2), removed, "the folder and its file")
	assert.NoDirExists(t, byName["photos"].Path)
	assert.NoFileExists(t, filepath.Join(home, ".local/share/Trash/info/photos.trashinfo"))

	items, err = ListTrash(t.Context(), home, testTrashUID)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "other.txt", items[0].Name)
}

func TestTrashRefusesPathsOutsideTheTrash(t *testing.T) {
	home := t.TempDir()
	outside := createTestFile(t, home, "keep.txt", []byte("keep"))

	_, err := RestoreFromTrash(t.Context(), outside, home, testTrashUID, testTrashUID, false)
	require.ErrorContains(t, err, "not in the trash")
	_, err = PurgeFromTrash(t.Context(), outside, home, testTrashUID, nil)
	require.ErrorContains(t, err, "not in the trash")
	assert.FileExists(t, outside)
}

func TestReadTrashInfoResolvesRelativePaths(t *testing.T) {
	home := t.TempDir()
	trash := filepath.Join(home, ".local/share/Trash")
	require.NoError(t, os.MkdirAll(filepath.Join(trash, "info"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(trash, "info", "x.trashinfo"), []byte("[Trash Info]\nPath=some%20dir/x\nDeletionDate=2026-01-02T03:04:05\n"), 0o600))
	root, err := fsroot.Open()
	require.NoError(t, err)
	defer root.Close()

	info, err := os.Open(filepath.Join(trash, "info"))
	require.NoError(t, err)
	defer info.Close()

	original, deletedAt := readTrashInfo(root, trash, info, "x")
	dev, err := deviceOf(root, trash)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(topDirOf(root, trash, dev), "some dir/x"), original)
	assert.Equal(t, time.Date(2026, 1, 2, 3, 4, 5, 0, time.Local), deletedAt)
}

// plantTrashLink points link at target relatively, as a process of the
// session user could: os.Root refuses absolute targets as escaping, so a
// relative link is what a path-based call would follow.
func plantTrashLink(t *testing.T, target, link string) {
	t.Helper()
	rel, err := filepath.Rel(filepath.Dir(link), target)
	require.NoError(t, err)
	require.NoError(t, os.Symlink(rel, link))
}

func writeTestTrashInfo(t *testing.T, trash, name, original string) {
	t.Helper()
	content := "[Trash Info]\nPath=" + (&url.URL{Path: original}).EscapedPath() + "\nDeletionDate=2026-01-02T03:04:05\n"
	require.NoError(t, os.WriteFile(filepath.Join(trash, "info", name+".trashinfo"), []byte(content), 0o600))
}

func requireEmptyDir(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Emptyf(t, entries, "entries of %s", dir)
}

func requireEntries(t *testing.T, dir string, names ...string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	got := make([]string, 0, len(entries))
	for _, entry := range entries {
		got = append(got, entry.Name())
	}
	require.Equalf(t, names, got, "entries of %s", dir)
}

func requireMode(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	info, err := os.Lstat(path)
	require.NoError(t, err)
	require.Equalf(t, mode, info.Mode().Perm(), "mode of %s", path)
}

// A trash folder that already exists (EEXIST) was not made by this request:
// it is only opened, never chowned or chmodded. The ones it makes get 0700.
func TestMoveToTrashLeavesAnExistingTrashFolderAlone(t *testing.T) {
	home := t.TempDir()
	files := filepath.Join(home, ".local/share/Trash/files")
	require.NoError(t, os.MkdirAll(files, 0o700))
	require.NoError(t, os.Chmod(files, 0o750))

	_, err := MoveToTrash(t.Context(), createTestFile(t, home, "notes.txt", nil), home, os.Getuid(), os.Getgid(), true)
	require.NoError(t, err)
	requireMode(t, files, 0o750)
	requireMode(t, filepath.Join(home, ".local/share/Trash/info"), 0o700)
	requireMode(t, filepath.Join(home, ".local/share/Trash/info/notes.txt.trashinfo"), 0o600)
}

// The .trashinfo is created exclusively, so a symlink the user planted at its
// name is skipped like any taken name rather than written through.
func TestMoveToTrashNeverFollowsASymlinkAtTheInfoName(t *testing.T) {
	home := t.TempDir()
	victim := createTestFile(t, t.TempDir(), "victim", []byte("victim"))
	info := filepath.Join(home, ".local/share/Trash/info")
	require.NoError(t, os.MkdirAll(info, 0o700))
	plantTrashLink(t, victim, filepath.Join(info, "notes.txt.trashinfo"))

	dest, err := MoveToTrash(t.Context(), createTestFile(t, home, "notes.txt", []byte("notes")), home, os.Getuid(), os.Getgid(), true)
	require.NoError(t, err)
	assert.Equal(t, "notes.txt.2", filepath.Base(dest))
	content, err := os.ReadFile(victim)
	require.NoError(t, err)
	assert.Equal(t, "victim", string(content))
	requireMode(t, victim, 0o644)
}

// Missing parents of a restore inside a folder the session user owns are made
// through handles and handed to that user, with the modes an upload uses.
func TestRestoreHandsNewParentsToTheSessionUser(t *testing.T) {
	home := t.TempDir()
	notes := filepath.Join(home, "a", "b", "notes.txt")
	trash := filepath.Join(home, ".local/share/Trash")
	require.NoError(t, os.MkdirAll(filepath.Join(trash, "info"), 0o700))
	createTestFile(t, createTestDir(t, trash, "files"), "notes.txt", []byte("notes"))
	writeTestTrashInfo(t, trash, "notes.txt", notes)

	restored, err := RestoreFromTrash(t.Context(), filepath.Join(trash, "files", "notes.txt"), home, os.Getuid(), os.Getgid(), true)
	require.NoError(t, err)
	assert.Equal(t, notes, restored)
	requireMode(t, filepath.Join(home, "a"), 0o755)
	requireMode(t, filepath.Join(home, "a", "b"), 0o755)
	content, err := os.ReadFile(notes)
	require.NoError(t, err)
	assert.Equal(t, "notes", string(content))
	assert.NoFileExists(t, filepath.Join(trash, "info", "notes.txt.trashinfo"))
}

func openTestDir(t *testing.T, path string) *os.File {
	t.Helper()
	dir, err := os.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = dir.Close() })
	return dir
}

// Into a system folder the item goes under a temporary name first and is
// checked there, where the session user cannot reach it. Anything but the
// item checked in the trash, or an item whose name is taken by then, goes
// back to the trash and nothing is left in the folder.
func TestRestoreIntoSystemFolderPutsAReplacedItemBack(t *testing.T) {
	trashFiles, system := t.TempDir(), t.TempDir()
	files, dir := openTestDir(t, trashFiles), openTestDir(t, system)
	session := Owner{UID: uint32(os.Getuid()) + 1, GID: uint32(os.Getgid()) + 1, Set: true}
	createTestFile(t, trashFiles, "app.conf", []byte("swapped"))
	var checked unix.Stat_t
	require.NoError(t, unix.Lstat(createTestFile(t, t.TempDir(), "app.conf", []byte("checked")), &checked))

	err := restoreIntoSystemFolder(files, "app.conf", checked, dir, "app.conf", session)
	require.ErrorIs(t, err, errTrashItemReplaced)
	requireEntries(t, trashFiles, "app.conf")
	requireEmptyDir(t, system)

	require.NoError(t, unix.Lstat(filepath.Join(trashFiles, "app.conf"), &checked))
	taken := createTestFile(t, system, "app.conf", []byte("taken"))
	err = restoreIntoSystemFolder(files, "app.conf", checked, dir, "app.conf", session)
	require.ErrorIs(t, err, unix.EEXIST)
	requireEntries(t, trashFiles, "app.conf")
	require.NoError(t, os.Remove(taken))
	requireEmptyDir(t, system)

	require.NoError(t, restoreIntoSystemFolder(files, "app.conf", checked, dir, "app.conf", session))
	content, err := os.ReadFile(filepath.Join(system, "app.conf"))
	require.NoError(t, err)
	assert.Equal(t, "swapped", string(content))
	requireEmptyDir(t, trashFiles)
}

// The session user of the root trash tests, who has no account.
const rootTestUID, rootTestGID = 12345, 12346

// rootSessionHome skips unless root and returns a home the session user owns,
// with the folders down to its trash made as that user would have made them.
func rootSessionHome(t *testing.T) string {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("acting for another user needs root")
	}
	home := filepath.Join(t.TempDir(), "home")
	trash := filepath.Join(home, ".local/share/Trash")
	require.NoError(t, os.MkdirAll(trash, 0o700))
	for dir := trash; dir != filepath.Dir(home); dir = filepath.Dir(dir) {
		require.NoError(t, os.Chown(dir, rootTestUID, rootTestGID))
	}
	return home
}

// On a root bridge the trash's folders belong to the session user, so none
// is followed if it is a symlink: root would otherwise make the trash's
// folders, write the .trashinfo or move the item wherever the link points.
func TestMoveToTrashNeverFollowsASymlinkedTrashFolderAsRoot(t *testing.T) {
	for _, sub := range []string{".local/share/Trash", ".local/share/Trash/files", ".local/share/Trash/info"} {
		t.Run(filepath.Base(sub), func(t *testing.T) {
			home := rootSessionHome(t)
			victim := t.TempDir()
			src := createTestFile(t, home, "notes.txt", []byte("notes"))
			link := filepath.Join(home, sub)
			require.NoError(t, os.RemoveAll(link))
			plantTrashLink(t, victim, link)

			_, err := MoveToTrash(t.Context(), src, home, rootTestUID, rootTestGID, true)
			require.Error(t, err)
			assert.FileExists(t, src)
			requireEmptyDir(t, victim)
		})
	}
}

// A restore takes the item and its .trashinfo from the trash's own folders,
// never through a symlink the user put there in their place: that would let
// the user pick any file on the system, say one only root can reach, as the
// item, or have root remove a .trashinfo anywhere, such as another user's.
func TestRestoreNeverFollowsASymlinkedTrashFolderAsRoot(t *testing.T) {
	t.Run("files", func(t *testing.T) {
		home := rootSessionHome(t)
		victim := t.TempDir()
		secret := createTestFile(t, victim, "secret", []byte("secret"))
		trash := filepath.Join(home, ".local/share/Trash")
		createTestDir(t, trash, "info")
		plantTrashLink(t, victim, filepath.Join(trash, "files"))
		writeTestTrashInfo(t, trash, "secret", filepath.Join(home, "secret"))

		_, err := RestoreFromTrash(t.Context(), filepath.Join(trash, "files", "secret"), home, rootTestUID, rootTestGID, true)
		require.Error(t, err)
		assert.FileExists(t, secret)
		assert.NoFileExists(t, filepath.Join(home, "secret"))
	})
	t.Run("info", func(t *testing.T) {
		home := rootSessionHome(t)
		victim := t.TempDir()
		trash := filepath.Join(home, ".local/share/Trash")
		createTestFile(t, createTestDir(t, trash, "files"), "notes.txt", []byte("notes"))
		plantTrashLink(t, victim, filepath.Join(trash, "info"))
		writeTestTrashInfo(t, trash, "notes.txt", filepath.Join(home, "notes.txt"))

		_, err := RestoreFromTrash(t.Context(), filepath.Join(trash, "files", "notes.txt"), home, rootTestUID, rootTestGID, true)
		require.Error(t, err)
		assert.FileExists(t, filepath.Join(victim, "notes.txt.trashinfo"))
		assert.FileExists(t, filepath.Join(trash, "files", "notes.txt"))
	})
}

// Delete forever, Empty trash and the Trash view work from the trash's own
// folders too: a symlinked files folder must not make root delete or list
// anything outside the trash.
func TestPurgeAndListNeverFollowASymlinkedTrashFolderAsRoot(t *testing.T) {
	home := rootSessionHome(t)
	victim := t.TempDir()
	secret := createTestFile(t, victim, "secret", []byte("secret"))
	trash := filepath.Join(home, ".local/share/Trash")
	createTestDir(t, trash, "info")
	plantTrashLink(t, victim, filepath.Join(trash, "files"))

	_, err := PurgeFromTrash(t.Context(), filepath.Join(trash, "files", "secret"), home, rootTestUID, nil)
	require.Error(t, err)
	assert.FileExists(t, secret)
	items, err := ListTrash(t.Context(), home, rootTestUID)
	require.NoError(t, err)
	for _, item := range items {
		assert.NotEqual(t, "secret", item.Name)
	}
}

// Purging a folder removes a link inside it, never what the link points at.
func TestPurgeRemovesALinkNotItsTarget(t *testing.T) {
	home := t.TempDir()
	victim := t.TempDir()
	kept := createTestFile(t, victim, "kept.txt", []byte("kept"))
	tree := createTestDir(t, home, "tree")
	plantTrashLink(t, victim, filepath.Join(tree, "link"))
	trashed, err := MoveToTrash(t.Context(), tree, home, testTrashUID, testTrashUID, false)
	require.NoError(t, err)

	removed, err := PurgeFromTrash(t.Context(), trashed, home, testTrashUID, nil)
	require.NoError(t, err)
	assert.Equal(t, int64(2), removed, "the link and the folder")
	assert.NoDirExists(t, trashed)
	assert.FileExists(t, kept)
}

// On a root bridge the item to trash is reached without following a link in
// a folder the session user can write, where a process of theirs could have
// swapped a folder for a link after the protected-folder check, and that
// check runs on the folder the item is then moved from.
func TestMoveToTrashNeverTrashesThroughALinkTheUserCouldPlant(t *testing.T) {
	home := t.TempDir()
	victim := t.TempDir()
	secret := createTestFile(t, victim, "notes.txt", []byte("secret"))
	plantTrashLink(t, victim, filepath.Join(home, "docs"))

	_, err := MoveToTrash(t.Context(), filepath.Join(home, "docs", "notes.txt"), home, os.Getuid(), os.Getgid(), true)
	require.ErrorIs(t, err, bridgeipc.ErrForbidden)
	assert.FileExists(t, secret)
	assert.NoDirExists(t, filepath.Join(home, ".local/share/Trash"))

	_, err = MoveToTrash(t.Context(), "/etc", home, testTrashUID, testTrashUID, false)
	require.ErrorContains(t, err, "system folder")
}

// A link the session user cannot have planted, one in a folder only root can
// write such as /home -> /var/home, is still followed on a root bridge.
func TestMoveToTrashFollowsALinkInARootFolderAsRoot(t *testing.T) {
	home := rootSessionHome(t)
	base := filepath.Dir(home)
	actual := createTestDir(t, base, "real")
	notes := createTestFile(t, actual, "notes.txt", []byte("notes"))
	plantTrashLink(t, actual, filepath.Join(base, "alias"))

	trashed, err := MoveToTrash(t.Context(), filepath.Join(base, "alias", "notes.txt"), home, rootTestUID, rootTestGID, true)
	require.NoError(t, err)
	assert.NoFileExists(t, notes)
	assert.FileExists(t, trashed)
}

// An unprivileged bridge acts as the user and follows their own links, so a
// symlinked ~/.local/share keeps working there.
func TestUnprivilegedTrashFollowsTheUsersOwnLinks(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("a root bridge does not follow links in the user's trash path")
	}
	home := t.TempDir()
	data := createTestDir(t, home, "data")
	createTestDir(t, home, ".local")
	plantTrashLink(t, data, filepath.Join(home, ".local", "share"))
	notes := createTestFile(t, home, "notes.txt", []byte("notes"))

	_, err := MoveToTrash(t.Context(), notes, home, testTrashUID, testTrashUID, false)
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(data, "Trash", "files", "notes.txt"))
	items, err := ListTrash(t.Context(), home, testTrashUID)
	require.NoError(t, err)
	require.Len(t, items, 1)
	restored, err := RestoreFromTrash(t.Context(), items[0].Path, home, testTrashUID, testTrashUID, false)
	require.NoError(t, err)
	assert.Equal(t, notes, restored)
}

// A FIFO the user put at a .trashinfo name, with a writer holding it open,
// must not hang a list or a restore.
func TestReadTrashInfoDoesNotWaitOnAFIFO(t *testing.T) {
	info := t.TempDir()
	fifo := filepath.Join(info, "x.trashinfo")
	require.NoError(t, unix.Mkfifo(fifo, 0o600))
	writer, err := os.OpenFile(fifo, os.O_RDWR, 0)
	require.NoError(t, err)
	defer writer.Close()
	dir := openTestDir(t, info)
	root, err := fsroot.Open()
	require.NoError(t, err)
	defer root.Close()

	done := make(chan string, 1)
	go func() {
		original, _ := readTrashInfo(root, filepath.Dir(info), dir, "x")
		done <- original
	}()
	select {
	case original := <-done:
		assert.Empty(t, original)
	case <-time.After(5 * time.Second):
		t.Fatal("reading a FIFO .trashinfo hung")
	}
}

// A list or a purge on a root bridge reaches the trash the way a move does,
// so a link the user planted on the way to it is refused there too.
func TestListAndPurgeRefuseALinkTheUserCouldPlantAsRoot(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("acting for another user needs root")
	}
	user := filepath.Join(t.TempDir(), "user")
	files := filepath.Join(user, "real", ".local/share/Trash/files")
	require.NoError(t, os.MkdirAll(files, 0o700))
	for dir := files; dir != filepath.Dir(user); dir = filepath.Dir(dir) {
		require.NoError(t, os.Chown(dir, rootTestUID, rootTestGID))
	}
	item := createTestFile(t, files, "notes.txt", []byte("notes"))
	plantTrashLink(t, filepath.Join(user, "real"), filepath.Join(user, "home"))
	home := filepath.Join(user, "home")

	items, err := ListTrash(t.Context(), home, rootTestUID)
	require.NoError(t, err)
	assert.Empty(t, items)
	_, err = PurgeFromTrash(t.Context(), filepath.Join(home, ".local/share/Trash/files/notes.txt"), home, rootTestUID, nil)
	require.ErrorIs(t, err, bridgeipc.ErrForbidden)
	assert.FileExists(t, item)
}

// The put-back after a failed check can be blocked by the user taking the
// item's name in their trash again. The item is checked while it sits in a
// root-only folder inside the trash, so even then nothing reaches the system
// folder, and what is left over is handed to the user in that folder rather
// than vouched for by root.
func TestRestoreIntoSystemFolderNeverStrandsAUserFileAsRoot(t *testing.T) {
	home := rootSessionHome(t)
	filesPath := createTestDir(t, filepath.Join(home, ".local/share/Trash"), "files")
	require.NoError(t, os.Chown(filesPath, rootTestUID, rootTestGID))
	system := t.TempDir()
	var checked unix.Stat_t // root's item, as the restore saw it in the trash
	require.NoError(t, unix.Lstat(createTestFile(t, t.TempDir(), "app.conf", []byte("root")), &checked))
	swapped := createTestFile(t, filesPath, "app.conf", []byte("user"))
	require.NoError(t, os.Chown(swapped, rootTestUID, rootTestGID))
	files, dir := openTestDir(t, filesPath), openTestDir(t, system)
	session := Owner{UID: rootTestUID, GID: rootTestGID, Set: true}

	stage, err := stageTrashItem(files, "app.conf")
	require.NoError(t, err)
	defer stage.Close()
	createTestFile(t, filesPath, "app.conf", []byte("blocks the put-back"))
	err = placeStaged(stage, files, "app.conf", checked, dir, "app.conf", session)

	require.ErrorIs(t, err, errTrashItemReplaced)
	requireEmptyDir(t, system)
	content, err := os.ReadFile(filepath.Join(stage.Name(), "app.conf"))
	require.NoError(t, err)
	assert.Equal(t, "user", string(content))
	var st unix.Stat_t
	require.NoError(t, unix.Fstat(int(stage.Fd()), &st))
	assert.Equal(t, uint32(rootTestUID), st.Uid, "the staging folder goes to the user")
}

// Between mkdirat and openat the user, who owns the trash's files folder,
// could swap another folder root owns in under the staging name, such as one
// root trashed earlier, content and all. Only a fresh one is taken: the
// bridge's, empty, and not writable by a group or everyone.
func TestOpenFreshStageRefusesASwappedFolder(t *testing.T) {
	files := t.TempDir()
	dir := openTestDir(t, files)
	for name, mode := range map[string]os.FileMode{"fresh": 0o700, "dmask": 0o755, "used": 0o700, "open": 0o770} {
		require.NoError(t, os.Mkdir(filepath.Join(files, name), mode))
		require.NoError(t, os.Chmod(filepath.Join(files, name), mode))
	}
	kept := createTestFile(t, filepath.Join(files, "used"), "root.conf", []byte("root"))

	for name, ok := range map[string]bool{"fresh": true, "dmask": true, "used": false, "open": false} {
		stage, err := openFreshStage(dir, name)
		if ok {
			require.NoErrorf(t, err, "stage %s", name)
			require.NoError(t, stage.Close())
		} else {
			require.Errorf(t, err, "stage %s", name)
		}
	}
	content, err := os.ReadFile(kept)
	require.NoError(t, err)
	assert.Equal(t, "root", string(content))
}
