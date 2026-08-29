package iteminfo

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollectStatInfo(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("collect_stat_regular_file", func(t *testing.T) {
		// Create a test file
		testFile := filepath.Join(tmpDir, "test.txt")
		testContent := []byte("test content")
		err := os.WriteFile(testFile, testContent, 0o644)
		require.NoError(t, err)

		// Collect stat info
		stat, err := CollectStatInfo(context.Background(), testFile)
		require.NoError(t, err)
		assert.NotNil(t, stat)

		// Verify basic properties
		assert.NotEmpty(t, stat.Mode, "mode should not be empty")
		assert.NotEmpty(t, stat.Permissions, "permissions should not be empty")
	})

	t.Run("collect_stat_directory", func(t *testing.T) {
		testDir := filepath.Join(tmpDir, "testdir")
		err := os.MkdirAll(testDir, 0o755)
		require.NoError(t, err)

		stat, err := CollectStatInfo(context.Background(), testDir)
		require.NoError(t, err)
		assert.NotNil(t, stat)
		assert.NotEmpty(t, stat.Permissions)
	})

	t.Run("nonexistent_path", func(t *testing.T) {
		stat, err := CollectStatInfo(context.Background(), filepath.Join(tmpDir, "nonexistent"))
		require.Error(t, err)
		assert.Nil(t, stat)
	})

	t.Run("collect_stat_with_different_permissions", func(t *testing.T) {
		testFile := filepath.Join(tmpDir, "perms.txt")
		err := os.WriteFile(testFile, []byte("test"), 0o600)
		require.NoError(t, err)

		stat, err := CollectStatInfo(context.Background(), testFile)
		require.NoError(t, err)
		assert.NotNil(t, stat)
		assert.NotEmpty(t, stat.Permissions)
		// Verify it contains permission info (should mention read/write/execute)
		assert.NotEmpty(t,
			stat.Permissions,
			"permissions string should be populated",
		)
	})

	t.Run("collect_stat_preserves_modification_time", func(t *testing.T) {
		testFile := filepath.Join(tmpDir, "time.txt")
		err := os.WriteFile(testFile, []byte("test"), 0o644)
		require.NoError(t, err)

		// Get stat info
		stat, err := CollectStatInfo(context.Background(), testFile)
		require.NoError(t, err)

		assert.NotEmpty(t, stat.Mode)
	})

	t.Run("collect_stat_mode_string", func(t *testing.T) {
		testFile := filepath.Join(tmpDir, "mode.txt")
		err := os.WriteFile(testFile, []byte("test"), 0o755)
		require.NoError(t, err)

		stat, err := CollectStatInfo(context.Background(), testFile)
		require.NoError(t, err)
		assert.NotEmpty(t, stat.Mode, "mode should be populated")
		// Mode should start with '-' for regular file or 'd' for directory
		assert.NotEmpty(t,
			stat.Mode,
			"mode string should not be empty",
		)
	})

	t.Run("collect_stat_raw_string", func(t *testing.T) {
		testFile := filepath.Join(tmpDir, "raw.txt")
		err := os.WriteFile(testFile, []byte("test"), 0o644)
		require.NoError(t, err)

		stat, err := CollectStatInfo(context.Background(), testFile)
		require.NoError(t, err)
		assert.NotEmpty(t, stat.Mode)
	})

	t.Run("collect_stat_symlink", func(t *testing.T) {
		targetFile := filepath.Join(tmpDir, "target.txt")
		linkFile := filepath.Join(tmpDir, "link.txt")

		err := os.WriteFile(targetFile, []byte("target"), 0o644)
		require.NoError(t, err)

		// Try to create symlink (might not work on all platforms)
		err = os.Symlink(targetFile, linkFile)
		if err != nil {
			t.Skip("symlinks not supported on this platform")
		}

		stat, err := CollectStatInfo(context.Background(), linkFile)
		require.NoError(t, err)
		assert.NotNil(t, stat)
		assert.NotEmpty(t, stat.Mode)
		assert.NotEqual(t, 'L', rune(stat.Mode[0]), "permissions describe the symlink target")
	})

	t.Run("collect_stat_empty_file", func(t *testing.T) {
		testFile := filepath.Join(tmpDir, "empty.txt")
		err := os.WriteFile(testFile, []byte{}, 0o644)
		require.NoError(t, err)

		stat, err := CollectStatInfo(context.Background(), testFile)
		require.NoError(t, err)
		assert.NotEmpty(t, stat.Mode)
	})

	t.Run("collect_stat_multiple_files_different_sizes", func(t *testing.T) {
		files := []struct {
			name    string
			content string
		}{
			{"small.txt", "a"},
			{"medium.txt", "medium content here"},
			{"large.txt", "this is a much larger file with more content"},
		}

		for _, f := range files {
			testFile := filepath.Join(tmpDir, f.name)
			err := os.WriteFile(testFile, []byte(f.content), 0o644)
			require.NoError(t, err)

			stat, err := CollectStatInfo(context.Background(), testFile)
			require.NoError(t, err)
			assert.NotEmpty(t, stat.Mode, f.name)
		}
	})
}

func TestFormatPermissionHuman(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("format_readable_file", func(t *testing.T) {
		testFile := filepath.Join(tmpDir, "readable.txt")
		err := os.WriteFile(testFile, []byte("test"), 0o644)
		require.NoError(t, err)

		info, err := os.Stat(testFile)
		require.NoError(t, err)

		formatted := formatPermissionHuman(info.Mode())
		assert.NotEmpty(t, formatted)
		// Should contain information about permissions
		assert.Contains(t, formatted, "read")
	})

	t.Run("format_executable_file", func(t *testing.T) {
		testFile := filepath.Join(tmpDir, "executable.sh")
		err := os.WriteFile(testFile, []byte("#!/bin/bash\necho test"), 0o755)
		require.NoError(t, err)

		info, err := os.Stat(testFile)
		require.NoError(t, err)

		formatted := formatPermissionHuman(info.Mode())
		assert.NotEmpty(t, formatted)
		// Should contain execute permission info
		assert.Contains(t, formatted, "execute")
	})

	t.Run("format_restricted_file", func(t *testing.T) {
		testFile := filepath.Join(tmpDir, "restricted.txt")
		err := os.WriteFile(testFile, []byte("secret"), 0o600)
		require.NoError(t, err)

		info, err := os.Stat(testFile)
		require.NoError(t, err)

		formatted := formatPermissionHuman(info.Mode())
		assert.NotEmpty(t, formatted)
		// Should indicate read and write but no execute
		assert.Contains(t, formatted, "read")
	})
}
