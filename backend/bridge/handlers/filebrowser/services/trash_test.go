package services

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
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

	original, deletedAt := readTrashInfo(root, trash, "x")
	dev, err := deviceOf(root, trash)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(topDirOf(root, trash, dev), "some dir/x"), original)
	assert.Equal(t, time.Date(2026, 1, 2, 3, 4, 5, 0, time.Local), deletedAt)
}
