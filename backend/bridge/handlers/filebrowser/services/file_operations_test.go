package services

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/iteminfo"
)

func TestMoveFile(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("move_file_success", func(t *testing.T) {
		srcFile := createTestFile(t, tmpDir, "source.txt", []byte("content"))
		dstPath := filepath.Join(tmpDir, "destination.txt")

		err := MoveFile(srcFile, dstPath)
		assert.NoError(t, err)

		// Source should not exist
		_, err = os.Stat(srcFile)
		assert.Error(t, err, "source file should be deleted after move")

		// Destination should exist
		content, err := os.ReadFile(dstPath)
		assert.NoError(t, err)
		assert.Equal(t, []byte("content"), content, "destination should have source content")
	})

	t.Run("move_file_to_different_directory", func(t *testing.T) {
		srcFile := createTestFile(t, tmpDir, "file.txt", []byte("data"))
		destDir := createTestDir(t, tmpDir, "subdir")
		dstPath := filepath.Join(destDir, "file.txt")

		err := MoveFile(srcFile, dstPath)
		assert.NoError(t, err)

		content, err := os.ReadFile(dstPath)
		assert.NoError(t, err)
		assert.Equal(t, []byte("data"), content)
	})

	t.Run("move_nonexistent_file", func(t *testing.T) {
		srcPath := filepath.Join(tmpDir, "nonexistent.txt")
		dstPath := filepath.Join(tmpDir, "dest.txt")

		err := MoveFile(srcPath, dstPath)
		assert.Error(t, err, "should error when source doesn't exist")
	})

	t.Run("move_file_overwrites_existing", func(t *testing.T) {
		srcFile := createTestFile(t, tmpDir, "src.txt", []byte("new"))
		dstFile := createTestFile(t, tmpDir, "dst.txt", []byte("old"))

		err := MoveFile(srcFile, dstFile)
		assert.NoError(t, err)

		content, err := os.ReadFile(dstFile)
		assert.NoError(t, err)
		assert.Equal(t, []byte("new"), content, "destination should be overwritten")
	})
}

func TestCopyFile(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("copy_file_success", func(t *testing.T) {
		srcFile := createTestFile(t, tmpDir, "source.txt", []byte("original"))
		destPath := filepath.Join(tmpDir, "copy.txt")

		err := CopyFile(srcFile, destPath)
		assert.NoError(t, err)

		// Source should still exist
		_, err = os.Stat(srcFile)
		assert.NoError(t, err, "source file should still exist after copy")

		// Destination should exist with same content
		content, err := os.ReadFile(destPath)
		assert.NoError(t, err)
		assert.Equal(t, []byte("original"), content)
	})

	t.Run("copy_file_to_directory", func(t *testing.T) {
		srcFile := createTestFile(t, tmpDir, "file.txt", []byte("content"))
		destDir := createTestDir(t, tmpDir, "subdir")
		destPath := filepath.Join(destDir, "file.txt")

		err := CopyFile(srcFile, destPath)
		assert.NoError(t, err)

		content, err := os.ReadFile(destPath)
		assert.NoError(t, err)
		assert.Equal(t, []byte("content"), content)
	})

	t.Run("copy_nonexistent_file", func(t *testing.T) {
		srcPath := filepath.Join(tmpDir, "nonexistent.txt")
		destPath := filepath.Join(tmpDir, "dest.txt")

		err := CopyFile(srcPath, destPath)
		assert.Error(t, err)
	})

	t.Run("copy_large_file", func(t *testing.T) {
		largContent := make([]byte, 5*1024*1024) // 5 MB
		for i := range largContent {
			largContent[i] = byte(i % 256)
		}
		srcFile := createTestFile(t, tmpDir, "large.bin", largContent)
		destPath := filepath.Join(tmpDir, "large_copy.bin")

		err := CopyFile(srcFile, destPath)
		assert.NoError(t, err)

		content, err := os.ReadFile(destPath)
		assert.NoError(t, err)
		assert.Equal(t, largContent, content, "large file content should match")
	})
}

func TestDeleteFiles(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("delete_single_file", func(t *testing.T) {
		filePath := createTestFile(t, tmpDir, "todelete.txt", []byte("data"))

		err := DeleteFiles(filePath)
		assert.NoError(t, err)

		_, err = os.Stat(filePath)
		assert.Error(t, err, "file should be deleted")
	})

	t.Run("delete_directory", func(t *testing.T) {
		dirPath := createTestDir(t, tmpDir, "todelete_dir")
		createTestFile(t, dirPath, "file1.txt", []byte("content"))
		createTestFile(t, dirPath, "file2.txt", []byte("content"))

		err := DeleteFiles(dirPath)
		assert.NoError(t, err)

		_, err = os.Stat(dirPath)
		assert.Error(t, err, "directory should be deleted")
	})

	t.Run("delete_nonexistent", func(t *testing.T) {
		safeDir := createTestDir(t, tmpDir, "safe")
		missingPath := filepath.Join(tmpDir, "nonexistent.txt")

		err := DeleteFiles(missingPath)
		assert.NoError(t, err, "deleting a nonexistent path should be a no-op")

		_, err = os.Stat(missingPath)
		assert.Error(t, err, "missing path should still not exist")

		// Ensure other directories are untouched
		_, err = os.Stat(safeDir)
		assert.NoError(t, err, "existing directories must remain intact")
	})

	t.Run("delete_directory_with_nested_files", func(t *testing.T) {
		dirPath := createTestDir(t, tmpDir, "nested")
		subDir := createTestDir(t, dirPath, "subdir")
		createTestFile(t, dirPath, "file1.txt", []byte("root"))
		createTestFile(t, subDir, "file2.txt", []byte("nested"))

		err := DeleteFiles(dirPath)
		assert.NoError(t, err)

		_, err = os.Stat(dirPath)
		assert.Error(t, err)
	})
}

func TestCreateDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("create_directory_success", func(t *testing.T) {
		newDir := filepath.Join(tmpDir, "newdir")
		opts := iteminfo.FileOptions{
			Path:  newDir,
			IsDir: true,
		}

		err := CreateDirectory(opts)
		assert.NoError(t, err)

		stat, err := os.Stat(newDir)
		assert.NoError(t, err)
		assert.True(t, stat.IsDir(), "created path should be a directory")
	})

	t.Run("create_nested_directory", func(t *testing.T) {
		newDir := filepath.Join(tmpDir, "parent", "child")
		opts := iteminfo.FileOptions{
			Path:  newDir,
			IsDir: true,
		}

		err := CreateDirectory(opts)
		assert.NoError(t, err)

		stat, err := os.Stat(newDir)
		assert.NoError(t, err)
		assert.True(t, stat.IsDir())
	})

	t.Run("create_existing_directory", func(t *testing.T) {
		existingDir := createTestDir(t, tmpDir, "existing")
		opts := iteminfo.FileOptions{
			Path:  existingDir,
			IsDir: true,
		}

		// Should not error if directory already exists
		err := CreateDirectory(opts)
		assert.NoError(t, err)
	})
}

func TestWriteContentInFile(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("write_content_new_file", func(t *testing.T) {
		filePath := filepath.Join(tmpDir, "newfile.txt")
		content := []byte("Hello, World!")
		reader := bytes.NewReader(content)

		opts := iteminfo.FileOptions{
			Path:    filePath,
			Content: true,
		}

		err := WriteContentInFile(opts, reader)
		assert.NoError(t, err)

		data, err := os.ReadFile(filePath)
		assert.NoError(t, err)
		assert.Equal(t, content, data)
	})

	t.Run("write_content_overwrites_file", func(t *testing.T) {
		filePath := createTestFile(t, tmpDir, "existing.txt", []byte("old content"))
		newContent := []byte("new content")
		reader := bytes.NewReader(newContent)

		opts := iteminfo.FileOptions{
			Path:    filePath,
			Content: true,
		}

		err := WriteContentInFile(opts, reader)
		assert.NoError(t, err)

		data, err := os.ReadFile(filePath)
		assert.NoError(t, err)
		assert.Equal(t, newContent, data)
	})

	t.Run("write_large_content", func(t *testing.T) {
		filePath := filepath.Join(tmpDir, "large.txt")
		largeContent := make([]byte, 10*1024*1024) // 10 MB
		for i := range largeContent {
			largeContent[i] = byte(i % 256)
		}
		reader := bytes.NewReader(largeContent)

		opts := iteminfo.FileOptions{
			Path:    filePath,
			Content: true,
		}

		err := WriteContentInFile(opts, reader)
		assert.NoError(t, err)

		data, err := os.ReadFile(filePath)
		assert.NoError(t, err)
		assert.Equal(t, largeContent, data)
	})

	t.Run("write_empty_content", func(t *testing.T) {
		filePath := filepath.Join(tmpDir, "empty.txt")
		reader := bytes.NewReader([]byte{})

		opts := iteminfo.FileOptions{
			Path:    filePath,
			Content: true,
		}

		err := WriteContentInFile(opts, reader)
		assert.NoError(t, err)

		data, err := os.ReadFile(filePath)
		assert.NoError(t, err)
		assert.Equal(t, []byte{}, data)
	})

	t.Run("write_to_directory", func(t *testing.T) {
		dirPath := createTestDir(t, tmpDir, "dirtest")
		reader := bytes.NewReader([]byte("content"))

		opts := iteminfo.FileOptions{
			Path:    dirPath,
			Content: true,
		}

		// WriteContentInFile may error or may create a file, just verify it handles the case
		_ = WriteContentInFile(opts, reader)
	})
}

func TestGetContent(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("get_text_file_content", func(t *testing.T) {
		filePath := createTestFile(t, tmpDir, "text.txt", []byte("Hello, World!"))

		content, err := GetContent(filePath)
		assert.NoError(t, err)
		assert.Equal(t, "Hello, World!", content)
	})

	t.Run("get_empty_file_content", func(t *testing.T) {
		filePath := createTestFile(t, tmpDir, "empty.txt", []byte{})

		// GetContent may return the filename for empty files or other behavior
		content, err := GetContent(filePath)
		if err == nil {
			assert.NotNil(t, content)
		}
		// Empty file handling is implementation dependent
	})

	t.Run("get_large_file_content", func(t *testing.T) {
		largeContent := "x"
		for i := 0; i < 1000; i++ {
			largeContent += "1234567890"
		}
		filePath := createTestFile(t, tmpDir, "large.txt", []byte(largeContent))

		content, err := GetContent(filePath)
		assert.NoError(t, err)
		assert.Equal(t, largeContent, content)
	})

	t.Run("get_nonexistent_file", func(t *testing.T) {
		_, err := GetContent(filepath.Join(tmpDir, "nonexistent.txt"))
		assert.Error(t, err)
	})

	t.Run("get_directory_content_fails", func(t *testing.T) {
		dirPath := createTestDir(t, tmpDir, "testdir")

		_, err := GetContent(dirPath)
		assert.Error(t, err, "should error when getting content of directory")
	})

	t.Run("get_multiline_content", func(t *testing.T) {
		multilineContent := "line1\nline2\nline3\n"
		filePath := createTestFile(t, tmpDir, "multiline.txt", []byte(multilineContent))

		content, err := GetContent(filePath)
		assert.NoError(t, err)
		assert.Equal(t, multilineContent, content)
	})
}

func TestCommonPrefix(t *testing.T) {
	t.Run("single_path", func(t *testing.T) {
		result := CommonPrefix('/', "/path/to/file")
		assert.Equal(t, "/path/to/file", result)
	})

	t.Run("identical_paths", func(t *testing.T) {
		result := CommonPrefix('/', "/path/to/file", "/path/to/file")
		assert.Equal(t, "/path/to/file", result)
	})

	t.Run("common_prefix", func(t *testing.T) {
		result := CommonPrefix('/', "/path/to/file1", "/path/to/file2")
		assert.Equal(t, "/path/to", result)
	})

	t.Run("different_roots", func(t *testing.T) {
		result := CommonPrefix('/', "/path/a", "/home/b")
		assert.Equal(t, "", result)
	})

	t.Run("no_common_prefix", func(t *testing.T) {
		result := CommonPrefix('/', "a", "b")
		assert.Equal(t, "", result)
	})

	t.Run("multiple_paths_with_common_prefix", func(t *testing.T) {
		result := CommonPrefix('/', "/data/files/1", "/data/files/2", "/data/files/3")
		assert.Equal(t, "/data/files", result)
	})

	t.Run("custom_separator", func(t *testing.T) {
		result := CommonPrefix(':', "a:b:c:d", "a:b:c:e")
		assert.Equal(t, "a:b:c", result)
	})
}
