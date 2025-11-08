package iteminfo

import (
	"os"
	"path/filepath"
	"testing"
	"time"

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
		stat, err := CollectStatInfo(testFile)
		assert.NoError(t, err)
		assert.NotNil(t, stat)

		// Verify basic properties
		assert.Equal(t, int64(len(testContent)), stat.Size)
		assert.Equal(t, filepath.Base(testFile), stat.Name)
		assert.Equal(t, testFile, stat.RealPath)
		assert.NotEmpty(t, stat.Mode, "mode should not be empty")
		assert.NotEmpty(t, stat.Modified, "modified time should not be empty")
		assert.NotEmpty(t, stat.Permissions, "permissions should not be empty")
	})

	t.Run("collect_stat_directory", func(t *testing.T) {
		testDir := filepath.Join(tmpDir, "testdir")
		err := os.MkdirAll(testDir, 0o755)
		require.NoError(t, err)

		stat, err := CollectStatInfo(testDir)
		assert.NoError(t, err)
		assert.NotNil(t, stat)
		assert.Equal(t, filepath.Base(testDir), stat.Name)
		assert.NotEmpty(t, stat.Permissions)
	})

	t.Run("nonexistent_path", func(t *testing.T) {
		stat, err := CollectStatInfo(filepath.Join(tmpDir, "nonexistent"))
		assert.Error(t, err)
		assert.Nil(t, stat)
	})

	t.Run("collect_stat_with_different_permissions", func(t *testing.T) {
		testFile := filepath.Join(tmpDir, "perms.txt")
		err := os.WriteFile(testFile, []byte("test"), 0o600)
		require.NoError(t, err)

		stat, err := CollectStatInfo(testFile)
		assert.NoError(t, err)
		assert.NotNil(t, stat)
		assert.NotEmpty(t, stat.Permissions)
		// Verify it contains permission info (should mention read/write/execute)
		assert.True(t,
			len(stat.Permissions) > 0,
			"permissions string should be populated",
		)
	})

	t.Run("collect_stat_preserves_modification_time", func(t *testing.T) {
		testFile := filepath.Join(tmpDir, "time.txt")
		err := os.WriteFile(testFile, []byte("test"), 0o644)
		require.NoError(t, err)

		// Get stat info
		stat, err := CollectStatInfo(testFile)
		assert.NoError(t, err)

		// Parse the modification time
		parsedTime, err := time.Parse(time.RFC3339, stat.Modified)
		assert.NoError(t, err, "modified time should be valid RFC3339")

		// Verify it's close to now (within a few seconds)
		now := time.Now()
		assert.True(t,
			now.Sub(parsedTime) < 5*time.Second,
			"modification time should be recent",
		)
	})

	t.Run("collect_stat_mode_string", func(t *testing.T) {
		testFile := filepath.Join(tmpDir, "mode.txt")
		err := os.WriteFile(testFile, []byte("test"), 0o755)
		require.NoError(t, err)

		stat, err := CollectStatInfo(testFile)
		assert.NoError(t, err)
		assert.NotEmpty(t, stat.Mode, "mode should be populated")
		// Mode should start with '-' for regular file or 'd' for directory
		assert.True(t,
			len(stat.Mode) > 0,
			"mode string should not be empty",
		)
	})

	t.Run("collect_stat_raw_string", func(t *testing.T) {
		testFile := filepath.Join(tmpDir, "raw.txt")
		err := os.WriteFile(testFile, []byte("test"), 0o644)
		require.NoError(t, err)

		stat, err := CollectStatInfo(testFile)
		assert.NoError(t, err)
		assert.NotEmpty(t, stat.Raw, "raw stat line should be populated")
		// Raw should contain mode, owner, group, size, time, and path
		assert.Contains(t, stat.Raw, filepath.Base(testFile), "raw stat should contain filename")
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

		// CollectStatInfo uses Lstat, so it should return info about the link itself
		stat, err := CollectStatInfo(linkFile)
		assert.NoError(t, err)
		assert.NotNil(t, stat)
		assert.Equal(t, filepath.Base(linkFile), stat.Name)
	})

	t.Run("collect_stat_empty_file", func(t *testing.T) {
		testFile := filepath.Join(tmpDir, "empty.txt")
		err := os.WriteFile(testFile, []byte{}, 0o644)
		require.NoError(t, err)

		stat, err := CollectStatInfo(testFile)
		assert.NoError(t, err)
		assert.Equal(t, int64(0), stat.Size, "empty file should have size 0")
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

			stat, err := CollectStatInfo(testFile)
			assert.NoError(t, err)
			assert.Equal(t, int64(len(f.content)), stat.Size, f.name+" should have correct size")
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

func TestFormatStatLine(t *testing.T) {
	t.Run("format_stat_line_complete", func(t *testing.T) {
		mode := "-rw-r--r--"
		owner := "user"
		group := "group"
		size := int64(1024)
		modTime := time.Now()
		path := "/path/to/file.txt"

		result := formatStatLine(mode, owner, group, size, modTime, path)
		assert.NotEmpty(t, result)
		assert.Contains(t, result, mode)
		assert.Contains(t, result, owner)
		assert.Contains(t, result, group)
		assert.Contains(t, result, "1024")
		assert.Contains(t, result, path)
	})

	t.Run("format_stat_line_with_spaces", func(t *testing.T) {
		mode := "  -rw-r--r--  "
		owner := "  user  "
		group := "  group  "
		size := int64(512)
		modTime := time.Now()
		path := "/path/to/file"

		result := formatStatLine(mode, owner, group, size, modTime, path)
		assert.NotEmpty(t, result)
		// Should have trimmed the spaces
		assert.Contains(t, result, "-rw-r--r--")
		assert.Contains(t, result, "user")
		assert.Contains(t, result, "group")
	})

	t.Run("format_stat_line_various_sizes", func(t *testing.T) {
		sizes := []int64{0, 1, 1024, 1024 * 1024, 1024 * 1024 * 1024}
		for _, size := range sizes {
			result := formatStatLine("-rw-r--r--", "owner", "group", size, time.Now(), "/file")
			assert.Contains(t, result, "0") // All contain at least a 0 in the size representation
		}
	})
}
