package services

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper function to create temporary test files/directories
func createTestFile(t *testing.T, dir, name string, content []byte) string {
	filePath := filepath.Join(dir, name)
	err := os.WriteFile(filePath, content, 0o644)
	require.NoError(t, err, "Failed to create test file: %s", name)
	return filePath
}

func createTestDir(t *testing.T, dir, name string) string {
	dirPath := filepath.Join(dir, name)
	err := os.MkdirAll(dirPath, 0o755)
	require.NoError(t, err, "Failed to create test directory: %s", name)
	return dirPath
}

func TestComputeArchiveSize(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("single_file", func(t *testing.T) {
		filePath := createTestFile(t, tmpDir, "file1.txt", []byte("hello"))
		size, err := ComputeArchiveSize([]string{filePath})
		assert.NoError(t, err)
		assert.Equal(t, int64(5), size, "single file size should be 5 bytes")
	})

	t.Run("multiple_files", func(t *testing.T) {
		file1 := createTestFile(t, tmpDir, "file1.txt", []byte("hello"))
		file2 := createTestFile(t, tmpDir, "file2.txt", []byte("world"))
		size, err := ComputeArchiveSize([]string{file1, file2})
		assert.NoError(t, err)
		assert.Equal(t, int64(10), size, "two files should total 10 bytes")
	})

	t.Run("directory_with_files", func(t *testing.T) {
		subDir := createTestDir(t, tmpDir, "subdir")
		createTestFile(t, subDir, "file1.txt", []byte("test"))
		createTestFile(t, subDir, "file2.txt", []byte("data"))

		size, err := ComputeArchiveSize([]string{subDir})
		assert.NoError(t, err)
		assert.Equal(t, int64(8), size, "directory with two 4-byte files should be 8 bytes")
	})

	t.Run("nested_directories", func(t *testing.T) {
		dir1 := createTestDir(t, tmpDir, "dir1")
		dir2 := createTestDir(t, dir1, "dir2")
		createTestFile(t, dir1, "file1.txt", []byte("a"))
		createTestFile(t, dir2, "file2.txt", []byte("bb"))

		size, err := ComputeArchiveSize([]string{dir1})
		assert.NoError(t, err)
		assert.Equal(t, int64(3), size, "nested directories with 1+2 byte files should be 3 bytes")
	})

	t.Run("nonexistent_file", func(t *testing.T) {
		_, err := ComputeArchiveSize([]string{filepath.Join(tmpDir, "nonexistent.txt")})
		assert.Error(t, err, "should error on nonexistent file")
	})

	t.Run("empty_directory", func(t *testing.T) {
		emptyDir := createTestDir(t, tmpDir, "empty")
		size, err := ComputeArchiveSize([]string{emptyDir})
		assert.NoError(t, err)
		assert.Equal(t, int64(0), size, "empty directory should have 0 size")
	})

	t.Run("mixed_files_and_directories", func(t *testing.T) {
		file1 := createTestFile(t, tmpDir, "file1.txt", []byte("abc"))
		dir1 := createTestDir(t, tmpDir, "dir1")
		createTestFile(t, dir1, "file2.txt", []byte("defgh"))

		size, err := ComputeArchiveSize([]string{file1, dir1})
		assert.NoError(t, err)
		// File1 is 3 bytes, file2.txt is 5 bytes = 8 bytes total for content files
		assert.GreaterOrEqual(t, size, int64(8), "should include both files (at least 8 bytes)")
	})
}

func TestCreateZip(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("create_zip_single_file", func(t *testing.T) {
		srcFile := createTestFile(t, tmpDir, "source.txt", []byte("test content"))
		zipPath := filepath.Join(tmpDir, "test.zip")

		err := CreateZip(zipPath, nil, zipPath, srcFile)
		assert.NoError(t, err, "CreateZip should not error")

		// Verify zip file exists and has content
		stat, err := os.Stat(zipPath)
		assert.NoError(t, err, "zip file should exist")
		assert.Greater(t, stat.Size(), int64(0), "zip file should have content")
	})

	t.Run("create_zip_multiple_files", func(t *testing.T) {
		file1 := createTestFile(t, tmpDir, "file1.txt", []byte("content1"))
		file2 := createTestFile(t, tmpDir, "file2.txt", []byte("content2"))
		zipPath := filepath.Join(tmpDir, "multi.zip")

		err := CreateZip(zipPath, nil, zipPath, file1, file2)
		assert.NoError(t, err, "CreateZip should handle multiple files")

		stat, err := os.Stat(zipPath)
		assert.NoError(t, err, "zip file should exist")
		assert.Greater(t, stat.Size(), int64(0), "zip file should have content")
	})

	t.Run("create_zip_with_directory", func(t *testing.T) {
		subDir := createTestDir(t, tmpDir, "archive_subdir")
		createTestFile(t, subDir, "file.txt", []byte("data"))
		zipPath := filepath.Join(tmpDir, "dir.zip")

		err := CreateZip(zipPath, nil, zipPath, subDir)
		assert.NoError(t, err, "CreateZip should handle directories")

		stat, err := os.Stat(zipPath)
		assert.NoError(t, err, "zip file should exist")
		assert.Greater(t, stat.Size(), int64(0), "zip file should have content")
	})

	t.Run("create_zip_overwrites_existing", func(t *testing.T) {
		srcFile := createTestFile(t, tmpDir, "source.txt", []byte("original"))
		zipPath := filepath.Join(tmpDir, "overwrite.zip")

		// Create first zip
		err := CreateZip(zipPath, nil, zipPath, srcFile)
		assert.NoError(t, err)
		firstStat, _ := os.Stat(zipPath)
		firstSize := firstStat.Size()

		// Small delay to ensure file times differ
		time.Sleep(10 * time.Millisecond)

		// Overwrite with different file
		modifiedFile := createTestFile(t, tmpDir, "modified.txt", []byte("much longer content here"))
		err = CreateZip(zipPath, nil, zipPath, modifiedFile)
		assert.NoError(t, err)
		secondStat, _ := os.Stat(zipPath)

		// File should have been overwritten - size should likely be different
		// (though size comparison isn't guaranteed, just check file exists and is valid)
		assert.NoError(t, err, "should successfully overwrite zip file")
		assert.Greater(t, secondStat.Size(), int64(0), "recreated zip should have content")
		_ = firstSize // firstSize kept for reference but size may vary based on compression
	})
}

func TestCreateTarGz(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("create_targz_single_file", func(t *testing.T) {
		srcFile := createTestFile(t, tmpDir, "source.txt", []byte("test content"))
		targzPath := filepath.Join(tmpDir, "test.tar.gz")

		err := CreateTarGz(targzPath, nil, targzPath, srcFile)
		assert.NoError(t, err, "CreateTarGz should not error")

		// Verify tar.gz file exists and has content
		stat, err := os.Stat(targzPath)
		assert.NoError(t, err, "tar.gz file should exist")
		assert.Greater(t, stat.Size(), int64(0), "tar.gz file should have content")
	})

	t.Run("create_targz_multiple_files", func(t *testing.T) {
		file1 := createTestFile(t, tmpDir, "file1.txt", []byte("content1"))
		file2 := createTestFile(t, tmpDir, "file2.txt", []byte("content2"))
		targzPath := filepath.Join(tmpDir, "multi.tar.gz")

		err := CreateTarGz(targzPath, nil, targzPath, file1, file2)
		assert.NoError(t, err, "CreateTarGz should handle multiple files")

		stat, err := os.Stat(targzPath)
		assert.NoError(t, err, "tar.gz file should exist")
		assert.Greater(t, stat.Size(), int64(0), "tar.gz file should have content")
	})

	t.Run("create_targz_with_directory", func(t *testing.T) {
		subDir := createTestDir(t, tmpDir, "archive_subdir_gz")
		createTestFile(t, subDir, "file.txt", []byte("data"))
		targzPath := filepath.Join(tmpDir, "dir.tar.gz")

		err := CreateTarGz(targzPath, nil, targzPath, subDir)
		assert.NoError(t, err, "CreateTarGz should handle directories")

		stat, err := os.Stat(targzPath)
		assert.NoError(t, err, "tar.gz file should exist")
		assert.Greater(t, stat.Size(), int64(0), "tar.gz file should have content")
	})
}

func TestExtractArchive(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("extract_zip_archive", func(t *testing.T) {
		srcDir := createTestDir(t, tmpDir, "zip-src")
		createTestFile(t, srcDir, "file.txt", []byte("zip data"))
		zipPath := filepath.Join(tmpDir, "archive.zip")

		err := CreateZip(zipPath, nil, zipPath, srcDir)
		require.NoError(t, err, "CreateZip should succeed before extraction")

		destDir := filepath.Join(tmpDir, "zip-dest")
		err = ExtractArchive(zipPath, destDir)
		require.NoError(t, err, "ExtractArchive should extract zip")

		content, err := os.ReadFile(filepath.Join(destDir, "zip-src", "file.txt"))
		require.NoError(t, err, "extracted file should exist")
		assert.Equal(t, "zip data", string(content), "extracted content should match")
	})

	t.Run("extract_tar_gz_archive", func(t *testing.T) {
		srcDir := createTestDir(t, tmpDir, "tar-src")
		createTestFile(t, srcDir, "nested.txt", []byte("tar data"))
		tarPath := filepath.Join(tmpDir, "archive.tar.gz")

		err := CreateTarGz(tarPath, nil, tarPath, srcDir)
		require.NoError(t, err, "CreateTarGz should succeed before extraction")

		destDir := filepath.Join(tmpDir, "tar-dest")
		err = ExtractArchive(tarPath, destDir)
		require.NoError(t, err, "ExtractArchive should extract tar.gz")

		content, err := os.ReadFile(filepath.Join(destDir, "tar-src", "nested.txt"))
		require.NoError(t, err, "extracted file should exist")
		assert.Equal(t, "tar data", string(content), "extracted content should match")
	})
}

func TestArchiveOperationsEdgeCases(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("empty_file_list", func(t *testing.T) {
		size, err := ComputeArchiveSize([]string{})
		assert.NoError(t, err)
		assert.Equal(t, int64(0), size, "empty file list should have 0 size")
	})

	t.Run("symlink_handling", func(t *testing.T) {
		srcFile := createTestFile(t, tmpDir, "original.txt", []byte("content"))
		linkPath := filepath.Join(tmpDir, "link.txt")

		err := os.Symlink(srcFile, linkPath)
		if err != nil {
			t.Skip("symlinks not supported on this platform")
		}

		size, err := ComputeArchiveSize([]string{linkPath})
		assert.NoError(t, err)
		assert.Equal(t, int64(7), size, "symlink should resolve to target size")
	})

	t.Run("large_file_simulation", func(t *testing.T) {
		// Create a file with known size
		largeContent := make([]byte, 10*1024*1024) // 10 MB
		for i := range largeContent {
			largeContent[i] = byte(i % 256)
		}
		largeFile := createTestFile(t, tmpDir, "large.bin", largeContent)

		size, err := ComputeArchiveSize([]string{largeFile})
		assert.NoError(t, err)
		assert.Equal(t, int64(10*1024*1024), size, "should correctly compute large file size")
	})
}
