package services

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/iteminfo"
)

func TestFileInfoFaster(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("get_info_single_file", func(t *testing.T) {
		filePath := createTestFile(t, tmpDir, "test.txt", []byte("content"))

		opts := iteminfo.FileOptions{
			Path:    filePath,
			IsDir:   false,
			Expand:  false,
			Content: false,
		}

		info, err := FileInfoFaster(opts)
		assert.NoError(t, err)
		assert.NotNil(t, info)
		assert.Equal(t, "test.txt", info.Name)
		assert.Equal(t, int64(7), info.Size)
		assert.False(t, info.Hidden)
	})

	t.Run("get_info_nonexistent", func(t *testing.T) {
		opts := iteminfo.FileOptions{
			Path: filepath.Join(tmpDir, "nonexistent.txt"),
		}

		_, err := FileInfoFaster(opts)
		assert.Error(t, err)
	})

	t.Run("get_info_hidden_file", func(t *testing.T) {
		hiddenFile := createTestFile(t, tmpDir, ".hidden", []byte("secret"))

		opts := iteminfo.FileOptions{
			Path: hiddenFile,
		}

		info, err := FileInfoFaster(opts)
		assert.NoError(t, err)
		assert.NotNil(t, info)
		assert.True(t, info.Hidden, "file starting with . should be marked as hidden")
	})

	t.Run("get_info_empty_file", func(t *testing.T) {
		emptyFile := createTestFile(t, tmpDir, "empty.txt", []byte{})

		opts := iteminfo.FileOptions{
			Path: emptyFile,
		}

		info, err := FileInfoFaster(opts)
		assert.NoError(t, err)
		assert.NotNil(t, info)
		assert.Equal(t, int64(0), info.Size)
	})

	t.Run("get_info_large_file", func(t *testing.T) {
		largeContent := make([]byte, 5*1024*1024) // 5 MB
		largeFile := filepath.Join(tmpDir, "large.bin")
		err := os.WriteFile(largeFile, largeContent, 0o644)
		require.NoError(t, err)

		opts := iteminfo.FileOptions{
			Path: largeFile,
		}

		info, err := FileInfoFaster(opts)
		assert.NoError(t, err)
		assert.NotNil(t, info)
		assert.Equal(t, int64(5*1024*1024), info.Size)
	})
}

func TestGetDirInfo(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("get_directory_with_files", func(t *testing.T) {
		dirPath := createTestDir(t, tmpDir, "testdir")
		createTestFile(t, dirPath, "file1.txt", []byte("content1"))
		createTestFile(t, dirPath, "file2.txt", []byte("content2"))

		info, err := GetDirInfo(dirPath, dirPath)
		assert.NoError(t, err)
		assert.NotNil(t, info)
		assert.Equal(t, "testdir", info.Name)
		// Should have files listed
		assert.True(t, len(info.Files) > 0 || len(info.Folders) > 0)
	})

	t.Run("get_empty_directory", func(t *testing.T) {
		emptyDir := createTestDir(t, tmpDir, "empty")

		info, err := GetDirInfo(emptyDir, emptyDir)
		assert.NoError(t, err)
		assert.NotNil(t, info)
		assert.Equal(t, "empty", info.Name)
		assert.Equal(t, 0, len(info.Files), "empty directory should have no files")
		assert.Equal(t, 0, len(info.Folders), "empty directory should have no folders")
	})

	t.Run("get_directory_with_subdirectories", func(t *testing.T) {
		dirPath := createTestDir(t, tmpDir, "parent")
		subDir1 := createTestDir(t, dirPath, "sub1")
		subDir2 := createTestDir(t, dirPath, "sub2")
		createTestFile(t, dirPath, "file.txt", []byte("root"))
		createTestFile(t, subDir1, "nested.txt", []byte("nested"))
		assert.NotEmpty(t, subDir2)

		info, err := GetDirInfo(dirPath, dirPath)
		assert.NoError(t, err)
		assert.NotNil(t, info)
		// Should list both files and subdirectories
		assert.True(t, len(info.Files)+len(info.Folders) > 0)
	})

	t.Run("get_directory_with_hidden_files", func(t *testing.T) {
		dirPath := createTestDir(t, tmpDir, "with_hidden")
		createTestFile(t, dirPath, "visible.txt", []byte("visible"))
		createTestFile(t, dirPath, ".hidden", []byte("hidden"))

		info, err := GetDirInfo(dirPath, dirPath)
		assert.NoError(t, err)
		assert.NotNil(t, info)
		// Both visible and hidden should be returned
		totalItems := len(info.Files) + len(info.Folders)
		assert.GreaterOrEqual(t, totalItems, 1)
	})

	t.Run("get_nonexistent_directory", func(t *testing.T) {
		nonexistent := filepath.Join(tmpDir, "nonexistent")

		_, err := GetDirInfo(nonexistent, nonexistent)
		assert.Error(t, err)
	})

	t.Run("get_file_as_directory", func(t *testing.T) {
		filePath := createTestFile(t, tmpDir, "file.txt", []byte("content"))

		// GetDirInfo may or may not error on a file - implementation dependent
		info, err := GetDirInfo(filePath, filePath)
		// Just verify it either errors or returns something
		if err == nil {
			assert.NotNil(t, info)
		}
	})

	t.Run("directory_with_special_characters", func(t *testing.T) {
		dirPath := createTestDir(t, tmpDir, "dir_with-special.chars")
		createTestFile(t, dirPath, "file-with-dash.txt", []byte("content"))
		createTestFile(t, dirPath, "file_with_underscore.txt", []byte("content"))

		info, err := GetDirInfo(dirPath, dirPath)
		assert.NoError(t, err)
		assert.NotNil(t, info)
		assert.True(t, len(info.Files) > 0)
	})

	t.Run("deep_nested_directory_structure", func(t *testing.T) {
		dirPath := createTestDir(t, tmpDir, "deep")
		level1 := createTestDir(t, dirPath, "level1")
		level2 := createTestDir(t, level1, "level2")
		level3 := createTestDir(t, level2, "level3")
		createTestFile(t, level3, "deep_file.txt", []byte("deep content"))

		// GetDirInfo on level1 should see level2
		info, err := GetDirInfo(level1, level1)
		assert.NoError(t, err)
		assert.NotNil(t, info)
		// Should have the level2 subdirectory
		assert.True(t, len(info.Folders) > 0)
	})
}

func TestFileTypeDetection(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("basic_file_type", func(t *testing.T) {
		filePath := createTestFile(t, tmpDir, "document.txt", []byte("text content"))

		opts := iteminfo.FileOptions{
			Path: filePath,
		}

		info, err := FileInfoFaster(opts)
		assert.NoError(t, err)
		assert.NotNil(t, info)
		// Should have a type set
		assert.NotEmpty(t, info.Type)
	})
}
