package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetDataDir(t *testing.T) {
	// Test with explicit dataDir parameter
	t.Run("explicit data dir", func(t *testing.T) {
		tempDir := t.TempDir()
		result, err := GetDataDir(tempDir)
		require.NoError(t, err)
		assert.Equal(t, tempDir, result)
	})

	// Test with explicit non-existent dataDir that can be created
	t.Run("explicit data dir - create new", func(t *testing.T) {
		tempDir := t.TempDir()
		newDir := filepath.Join(tempDir, "new-data-dir")
		result, err := GetDataDir(newDir)
		require.NoError(t, err)
		assert.Equal(t, newDir, result)

		// Verify directory was created
		stat, err := os.Stat(newDir)
		require.NoError(t, err)
		assert.True(t, stat.IsDir())
	})

	// Test with DATA_DIR environment variable
	t.Run("DATA_DIR environment variable", func(t *testing.T) {
		tempDir := t.TempDir()

		t.Setenv("DATA_DIR", tempDir)

		result, err := GetDataDir()
		require.NoError(t, err)
		assert.Equal(t, tempDir, result)
	})

	// Test with invalid explicit dataDir
	t.Run("invalid explicit data dir", func(t *testing.T) {
		invalidPath := "/invalid/path/that/cannot/be/created"
		_, err := GetDataDir(invalidPath)
		assert.Error(t, err)
	})

	// Test fallback behavior (empty dataDir, no env var)
	t.Run("fallback to default directories", func(t *testing.T) {
		// This will try platform-specific defaults, which may or may not work
		// We're mainly testing that it doesn't panic and returns some result
		result, err := GetDataDir()
		// We don't assert success/failure here since it depends on system permissions
		// Just verify we get a string result if no error
		if err == nil {
			assert.NotEmpty(t, result)
		}
	})
}

func TestTestDataDirs(t *testing.T) {
	// Test with existing valid directory
	t.Run("existing valid directory", func(t *testing.T) {
		tempDir := t.TempDir()
		result, err := testDataDirs([]string{tempDir})
		require.NoError(t, err)
		assert.Equal(t, tempDir, result)
	})

	// Test with multiple directories, first one valid
	t.Run("multiple dirs - first valid", func(t *testing.T) {
		tempDir := t.TempDir()
		invalidDir := "/invalid/path"
		result, err := testDataDirs([]string{tempDir, invalidDir})
		require.NoError(t, err)
		assert.Equal(t, tempDir, result)
	})

	// Test with multiple directories, second one valid
	t.Run("multiple dirs - second valid", func(t *testing.T) {
		tempDir := t.TempDir()
		invalidDir := "/invalid/path"
		result, err := testDataDirs([]string{invalidDir, tempDir})
		require.NoError(t, err)
		assert.Equal(t, tempDir, result)
	})

	// Test with non-existing directory that can be created
	t.Run("create new directory", func(t *testing.T) {
		tempDir := t.TempDir()
		newDir := filepath.Join(tempDir, "new-dir")
		result, err := testDataDirs([]string{newDir})
		require.NoError(t, err)
		assert.Equal(t, newDir, result)

		// Verify directory was created
		stat, err := os.Stat(newDir)
		require.NoError(t, err)
		assert.True(t, stat.IsDir())
	})

	// Test with no valid directories
	t.Run("no valid directories", func(t *testing.T) {
		invalidPaths := []string{"/invalid/path1", "/invalid/path2"}
		_, err := testDataDirs(invalidPaths)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "data directory not found")
	})
}

func TestDataDirIssue(t *testing.T) {
	// Test with existing directory
	t.Run("existing writable directory", func(t *testing.T) {
		tempDir := t.TempDir()
		require.NoError(t, dataDirIssue(tempDir, true))
		require.NoError(t, dataDirIssue(tempDir, false))
	})

	// Test with non-existing directory
	t.Run("non-existing dir", func(t *testing.T) {
		tempDir := t.TempDir()
		nonExistentDir := filepath.Join(tempDir, "does-not-exist")
		err := dataDirIssue(nonExistentDir, true)
		require.Error(t, err)
		assert.ErrorIs(t, err, errDataDirMissing)
	})

	// Test with file instead of directory
	t.Run("file instead of directory", func(t *testing.T) {
		tempDir := t.TempDir()
		tempFile := filepath.Join(tempDir, "testfile")
		require.NoError(t, os.WriteFile(tempFile, []byte("test"), 0644))

		err := dataDirIssue(tempFile, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "is not a directory")
	})

	// A read-only directory is rejected for writable callers but accepted for
	// read-only inspection.
	t.Run("read-only directory", func(t *testing.T) {
		if os.Getuid() == 0 {
			t.Skip("root bypasses directory permissions")
		}
		readOnlyDir := makeReadOnlyDir(t)

		err := dataDirIssue(readOnlyDir, true)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not writable by uid")
		assert.NoError(t, dataDirIssue(readOnlyDir, false))
	})
}

func TestGetDataDirExplicitNotWritable(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	readOnlyDir := makeReadOnlyDir(t)

	// An explicit path must fail loudly rather than silently falling back to
	// one of the default candidates.
	result, err := GetDataDir(readOnlyDir)
	require.Error(t, err)
	assert.Empty(t, result)
	assert.Contains(t, err.Error(), readOnlyDir)
	assert.Contains(t, err.Error(), "not writable by uid")
}

func TestGetReadOnlyDataDir(t *testing.T) {
	t.Run("explicit read-only dir is accepted", func(t *testing.T) {
		if os.Getuid() == 0 {
			t.Skip("root bypasses directory permissions")
		}
		readOnlyDir := makeReadOnlyDir(t)

		result, err := GetReadOnlyDataDir(readOnlyDir)
		require.NoError(t, err)
		assert.Equal(t, readOnlyDir, result)
	})

	t.Run("explicit missing dir errors without creating it", func(t *testing.T) {
		tempDir := t.TempDir()
		missing := filepath.Join(tempDir, "absent")

		_, err := GetReadOnlyDataDir(missing)
		require.Error(t, err)
		assert.Contains(t, err.Error(), missing)
		assert.Contains(t, err.Error(), "does not exist")
		assert.NoDirExists(t, missing)
	})

	t.Run("DATA_DIR environment variable wins over system dirs", func(t *testing.T) {
		tempDir := t.TempDir()
		// A database here must outrank one in /var/lib or the home directory.
		require.NoError(t, os.WriteFile(DatabasePath(tempDir), []byte("db"), 0600))
		t.Setenv("DATA_DIR", tempDir)

		result, err := GetReadOnlyDataDir()
		require.NoError(t, err)
		assert.Equal(t, tempDir, result)
	})
}

func TestReadOnlyDataDir(t *testing.T) {
	t.Run("prefers a candidate holding a database", func(t *testing.T) {
		empty := t.TempDir()
		withDB := t.TempDir()
		require.NoError(t, os.WriteFile(DatabasePath(withDB), []byte("db"), 0600))

		assert.Equal(t, withDB, readOnlyDataDir([]string{empty, withDB}))
	})

	t.Run("falls back to the first readable candidate", func(t *testing.T) {
		tempDir := t.TempDir()
		missing := filepath.Join(tempDir, "absent")

		assert.Equal(t, tempDir, readOnlyDataDir([]string{missing, tempDir}))
	})

	t.Run("falls back to the first candidate when nothing exists", func(t *testing.T) {
		tempDir := t.TempDir()
		first := filepath.Join(tempDir, "first")
		second := filepath.Join(tempDir, "second")

		assert.Equal(t, first, readOnlyDataDir([]string{first, second}))
		assert.NoDirExists(t, first)
	})
}

// makeReadOnlyDir returns a directory the current user cannot write to.
func makeReadOnlyDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "readonly")
	require.NoError(t, os.Mkdir(dir, 0555))
	// Restore write permission so t.TempDir cleanup can remove the tree.
	t.Cleanup(func() {
		_ = os.Chmod(dir, 0755)
	})
	return dir
}

func TestDirectoryExists(t *testing.T) {
	// Test with existing directory
	t.Run("existing directory", func(t *testing.T) {
		tempDir := t.TempDir()
		exists, err := directoryExists(tempDir)
		require.NoError(t, err)
		assert.True(t, exists)
	})

	// Test with non-existing directory
	t.Run("non-existing directory", func(t *testing.T) {
		tempDir := t.TempDir()
		nonExistentDir := filepath.Join(tempDir, "does-not-exist")
		exists, err := directoryExists(nonExistentDir)
		require.NoError(t, err)
		assert.False(t, exists)
	})

	// Test with file instead of directory
	t.Run("file instead of directory", func(t *testing.T) {
		tempDir := t.TempDir()
		tempFile := filepath.Join(tempDir, "testfile")
		err := os.WriteFile(tempFile, []byte("test"), 0644)
		require.NoError(t, err)

		exists, err := directoryExists(tempFile)
		require.Error(t, err)
		assert.False(t, exists)
		assert.Contains(t, err.Error(), "is not a directory")
	})
}

func TestDirectoryIsWritable(t *testing.T) {
	// Test with writable directory
	t.Run("writable directory", func(t *testing.T) {
		tempDir := t.TempDir()
		writable, err := directoryIsWritable(tempDir)
		require.NoError(t, err)
		assert.True(t, writable)
	})

	// Test with non-existing directory
	t.Run("non-existing directory", func(t *testing.T) {
		tempDir := t.TempDir()
		nonExistentDir := filepath.Join(tempDir, "does-not-exist")
		writable, err := directoryIsWritable(nonExistentDir)
		require.Error(t, err)
		assert.False(t, writable)
	})

	// Test with non-writable directory (Unix-like systems only)
	t.Run("non-writable directory", func(t *testing.T) {
		tempDir := t.TempDir()
		readOnlyDir := filepath.Join(tempDir, "readonly")

		// Create the directory
		err := os.Mkdir(readOnlyDir, 0755)
		require.NoError(t, err)

		// Make it read-only
		err = os.Chmod(readOnlyDir, 0444)
		require.NoError(t, err)

		// Restore permissions after test for cleanup
		defer func() {
			_ = os.Chmod(readOnlyDir, 0755)
		}()

		writable, err := directoryIsWritable(readOnlyDir)
		require.Error(t, err)
		assert.False(t, writable)
	})
}
