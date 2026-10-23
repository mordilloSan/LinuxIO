package services

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
