package services

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	ipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
)

func TestMoveFileWithCallbacksUsesKnownSizeForRename(t *testing.T) {
	tmpDir := t.TempDir()
	srcFile := createTestFile(t, tmpDir, "source-known-size.txt", []byte("content"))
	dstPath := filepath.Join(tmpDir, "destination-known-size.txt")

	var reported []int64
	err := MoveFileWithCallbacks(srcFile, dstPath, false, &ipc.OperationCallbacks{
		Progress: func(n int64) {
			reported = append(reported, n)
		},
	}, MoveFileOptions{KnownSize: 12345, HasKnownSize: true})
	require.NoError(t, err)
	assert.Equal(t, []int64{12345}, reported)
}

func TestMoveFileWithCallbacks(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("move_file_success", func(t *testing.T) {
		srcFile := createTestFile(t, tmpDir, "source.txt", []byte("content"))
		dstPath := filepath.Join(tmpDir, "destination.txt")

		err := MoveFileWithCallbacks(srcFile, dstPath, false, nil, MoveFileOptions{})
		require.NoError(t, err)

		_, err = os.Stat(srcFile)
		require.Error(t, err, "source file should be deleted after move")

		content, err := os.ReadFile(dstPath)
		require.NoError(t, err)
		assert.Equal(t, []byte("content"), content, "destination should have source content")
	})

	t.Run("move_file_to_different_directory", func(t *testing.T) {
		srcFile := createTestFile(t, tmpDir, "file.txt", []byte("data"))
		destDir := createTestDir(t, tmpDir, "subdir")
		dstPath := filepath.Join(destDir, "file.txt")

		err := MoveFileWithCallbacks(srcFile, dstPath, false, nil, MoveFileOptions{})
		require.NoError(t, err)

		content, err := os.ReadFile(dstPath)
		require.NoError(t, err)
		assert.Equal(t, []byte("data"), content)
	})

	t.Run("move_nonexistent_file", func(t *testing.T) {
		srcPath := filepath.Join(tmpDir, "nonexistent.txt")
		dstPath := filepath.Join(tmpDir, "dest.txt")

		err := MoveFileWithCallbacks(srcPath, dstPath, false, nil, MoveFileOptions{})
		require.Error(t, err, "should error when source doesn't exist")
	})

	t.Run("move_file_overwrites_existing", func(t *testing.T) {
		srcFile := createTestFile(t, tmpDir, "src.txt", []byte("new"))
		dstFile := createTestFile(t, tmpDir, "dst.txt", []byte("old"))

		err := MoveFileWithCallbacks(srcFile, dstFile, true, nil, MoveFileOptions{})
		require.NoError(t, err)

		content, err := os.ReadFile(dstFile)
		require.NoError(t, err)
		assert.Equal(t, []byte("new"), content, "destination should be overwritten")
	})
}

func TestCopyFileWithCallbacks(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("copy_file_success", func(t *testing.T) {
		srcFile := createTestFile(t, tmpDir, "source.txt", []byte("original"))
		destPath := filepath.Join(tmpDir, "copy.txt")

		err := CopyFileWithCallbacks(srcFile, destPath, false, nil)
		require.NoError(t, err)

		// Source should still exist
		_, err = os.Stat(srcFile)
		require.NoError(t, err, "source file should still exist after copy")

		// Destination should exist with same content
		content, err := os.ReadFile(destPath)
		require.NoError(t, err)
		assert.Equal(t, []byte("original"), content)
	})

	t.Run("copy_file_to_directory", func(t *testing.T) {
		srcFile := createTestFile(t, tmpDir, "file.txt", []byte("content"))
		destDir := createTestDir(t, tmpDir, "subdir")
		destPath := filepath.Join(destDir, "file.txt")

		err := CopyFileWithCallbacks(srcFile, destPath, false, nil)
		require.NoError(t, err)

		content, err := os.ReadFile(destPath)
		require.NoError(t, err)
		assert.Equal(t, []byte("content"), content)
	})

	t.Run("copy_nonexistent_file", func(t *testing.T) {
		srcPath := filepath.Join(tmpDir, "nonexistent.txt")
		destPath := filepath.Join(tmpDir, "dest.txt")

		err := CopyFileWithCallbacks(srcPath, destPath, false, nil)
		require.Error(t, err)
	})

	t.Run("copy_large_file", func(t *testing.T) {
		largContent := make([]byte, 5*1024*1024) // 5 MB
		for i := range largContent {
			largContent[i] = byte(i % 256)
		}
		srcFile := createTestFile(t, tmpDir, "large.bin", largContent)
		destPath := filepath.Join(tmpDir, "large_copy.bin")

		err := CopyFileWithCallbacks(srcFile, destPath, false, nil)
		require.NoError(t, err)

		content, err := os.ReadFile(destPath)
		require.NoError(t, err)
		assert.Equal(t, largContent, content, "large file content should match")
	})

	t.Run("copy_conflict_without_overwrite", func(t *testing.T) {
		srcFile := createTestFile(t, tmpDir, "src.txt", []byte("new"))
		dstFile := createTestFile(t, tmpDir, "dst.txt", []byte("old"))

		err := CopyFileWithCallbacks(srcFile, dstFile, false, nil)
		require.Error(t, err, "should error when destination exists and overwrite is false")
	})
}

func TestCopyFileWithCallbacksPreservesSymlinks(t *testing.T) {
	tmpDir := t.TempDir()
	srcDir := createTestDir(t, tmpDir, "src")
	createTestFile(t, srcDir, "target.txt", []byte("target"))
	subDir := createTestDir(t, srcDir, "subdir")
	createTestFile(t, subDir, "nested.txt", []byte("nested"))

	createSymlinkOrSkip(t, "target.txt", filepath.Join(srcDir, "file-link"))
	createSymlinkOrSkip(t, "subdir", filepath.Join(srcDir, "dir-link"))
	createSymlinkOrSkip(t, "missing.txt", filepath.Join(srcDir, "dangling-link"))
	createSymlinkOrSkip(t, "/etc/hostname", filepath.Join(srcDir, "absolute-link"))

	destDir := filepath.Join(tmpDir, "dest")
	var progressed int64
	err := CopyFileWithCallbacks(srcDir, destDir, false, &ipc.OperationCallbacks{
		Progress: func(n int64) {
			progressed += n
		},
	})
	require.NoError(t, err)

	assertSymlinkTarget(t, filepath.Join(destDir, "file-link"), "target.txt")
	assertSymlinkTarget(t, filepath.Join(destDir, "dir-link"), "subdir")
	assertSymlinkTarget(t, filepath.Join(destDir, "dangling-link"), "missing.txt")
	assertSymlinkTarget(t, filepath.Join(destDir, "absolute-link"), "/etc/hostname")

	content, err := os.ReadFile(filepath.Join(destDir, "target.txt"))
	require.NoError(t, err)
	assert.Equal(t, []byte("target"), content)
	nestedContent, err := os.ReadFile(filepath.Join(destDir, "subdir", "nested.txt"))
	require.NoError(t, err)
	assert.Equal(t, []byte("nested"), nestedContent)
	assert.Equal(t, int64(len("target")+len("nested")), progressed)
}

func TestCopyFileWithCallbacksPreservesTopLevelSymlink(t *testing.T) {
	tmpDir := t.TempDir()
	createTestFile(t, tmpDir, "target.txt", []byte("target"))
	srcLink := filepath.Join(tmpDir, "source-link")
	createSymlinkOrSkip(t, "target.txt", srcLink)
	destLink := filepath.Join(tmpDir, "dest-link")

	err := CopyFileWithCallbacks(srcLink, destLink, false, nil)
	require.NoError(t, err)

	assertSymlinkTarget(t, destLink, "target.txt")
}

func TestCopyFileWithCallbacksRejectsSymlinkOntoOwnTarget(t *testing.T) {
	tmpDir := t.TempDir()
	targetPath := createTestFile(t, tmpDir, "target.txt", []byte("target"))
	srcLink := filepath.Join(tmpDir, "source-link")
	createSymlinkOrSkip(t, "target.txt", srcLink)

	err := CopyFileWithCallbacks(srcLink, targetPath, true, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot copy symlink")

	content, readErr := os.ReadFile(targetPath)
	require.NoError(t, readErr)
	assert.Equal(t, []byte("target"), content)
	assertSymlinkTarget(t, srcLink, "target.txt")
}

func TestCopyFileWithCallbacksRejectsFifo(t *testing.T) {
	tmpDir := t.TempDir()
	srcFIFO := filepath.Join(tmpDir, "source-fifo")
	if err := syscall.Mkfifo(srcFIFO, 0o644); err != nil {
		t.Skipf("mkfifo not supported: %v", err)
	}

	err := CopyFileWithCallbacks(srcFIFO, filepath.Join(tmpDir, "dest-fifo"), false, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot copy non-regular file")
}

func TestCopyFileWithCallbacksSkipsNestedFifo(t *testing.T) {
	tmpDir := t.TempDir()
	srcDir := createTestDir(t, tmpDir, "src")
	createTestFile(t, srcDir, "data.txt", []byte("data"))
	srcFIFO := filepath.Join(srcDir, "source-fifo")
	if err := syscall.Mkfifo(srcFIFO, 0o644); err != nil {
		t.Skipf("mkfifo not supported: %v", err)
	}

	destDir := filepath.Join(tmpDir, "dest")
	// Must not block opening the FIFO and must not fail the rest of the copy.
	require.NoError(t, CopyFileWithCallbacks(srcDir, destDir, false, nil))

	_, err := os.Lstat(filepath.Join(destDir, "source-fifo"))
	assert.True(t, os.IsNotExist(err), "fifo should be skipped, not recreated")
	content, err := os.ReadFile(filepath.Join(destDir, "data.txt"))
	require.NoError(t, err)
	assert.Equal(t, []byte("data"), content)
}

func TestCopyFileWithCallbacksRejectsTopLevelFifo(t *testing.T) {
	tmpDir := t.TempDir()
	srcFIFO := filepath.Join(tmpDir, "source-fifo")
	if err := syscall.Mkfifo(srcFIFO, 0o644); err != nil {
		t.Skipf("mkfifo not supported: %v", err)
	}

	err := CopyFileWithCallbacks(srcFIFO, filepath.Join(tmpDir, "dest-fifo"), false, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot copy non-regular file")
}

func TestComputeCopySizeTreatsSymlinksAsZero(t *testing.T) {
	tmpDir := t.TempDir()
	srcDir := createTestDir(t, tmpDir, "src")
	createTestFile(t, srcDir, "target.txt", []byte("target"))
	createSymlinkOrSkip(t, "target.txt", filepath.Join(srcDir, "file-link"))
	createSymlinkOrSkip(t, "missing.txt", filepath.Join(srcDir, "dangling-link"))

	size, err := ComputeCopySize(srcDir)
	require.NoError(t, err)
	assert.Equal(t, int64(len("target")), size)

	linkSize, err := ComputeCopySize(filepath.Join(srcDir, "file-link"))
	require.NoError(t, err)
	assert.Zero(t, linkSize)
}

func createSymlinkOrSkip(t *testing.T, target, linkPath string) {
	t.Helper()
	if err := os.Symlink(target, linkPath); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
}

func assertSymlinkTarget(t *testing.T, linkPath, expectedTarget string) {
	t.Helper()
	info, err := os.Lstat(linkPath)
	require.NoError(t, err)
	require.NotZero(t, info.Mode()&os.ModeSymlink, "%s should be a symlink", linkPath)
	target, err := os.Readlink(linkPath)
	require.NoError(t, err)
	assert.Equal(t, expectedTarget, target)
}

func TestDeleteFilesWithProgress(t *testing.T) {
	t.Run("single_file_reports_one_item", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := createTestFile(t, tmpDir, "large.bin", []byte("data"))
		var progress []int64

		processed, err := DeleteFilesWithProgress(context.Background(), filePath, DeleteOptions{
			Progress: func(processed int64) {
				progress = append(progress, processed)
			},
		})
		require.NoError(t, err)
		assert.Equal(t, int64(1), processed)
		assert.Equal(t, []int64{1}, progress)
		_, err = os.Lstat(filePath)
		require.Error(t, err)
	})

	t.Run("directory_reports_running_entry_count", func(t *testing.T) {
		tmpDir := t.TempDir()
		dirPath := createTestDir(t, tmpDir, "tree")
		subDir := createTestDir(t, dirPath, "subdir")
		createTestFile(t, dirPath, "file1.txt", []byte("root"))
		createTestFile(t, subDir, "file2.txt", []byte("nested"))
		var progress []int64

		processed, err := DeleteFilesWithProgress(context.Background(), dirPath, DeleteOptions{
			Progress: func(processed int64) {
				progress = append(progress, processed)
			},
		})
		require.NoError(t, err)
		assert.Equal(t, int64(4), processed)
		assert.Equal(t, []int64{1, 2, 3, 4}, progress)
		_, err = os.Lstat(dirPath)
		require.Error(t, err)
	})

	t.Run("symlink_delete_does_not_follow_target", func(t *testing.T) {
		tmpDir := t.TempDir()
		targetDir := createTestDir(t, tmpDir, "target")
		targetFile := createTestFile(t, targetDir, "kept.txt", []byte("keep"))
		linkPath := filepath.Join(tmpDir, "target-link")
		if err := os.Symlink(targetDir, linkPath); err != nil {
			t.Skipf("symlink not supported: %v", err)
		}

		processed, err := DeleteFilesWithProgress(context.Background(), linkPath, DeleteOptions{})
		require.NoError(t, err)
		assert.Equal(t, int64(1), processed)
		_, err = os.Lstat(linkPath)
		require.Error(t, err)
		_, err = os.Lstat(targetFile)
		require.NoError(t, err)
	})
}

func TestCountEntries(t *testing.T) {
	t.Run("empty_directory_counts_itself", func(t *testing.T) {
		tmpDir := t.TempDir()
		dirPath := createTestDir(t, tmpDir, "empty")

		total, err := CountEntries(context.Background(), dirPath, true)
		require.NoError(t, err)
		assert.Equal(t, int64(1), total)
	})

	t.Run("tree_counts_files_and_directories", func(t *testing.T) {
		tmpDir := t.TempDir()
		dirPath := createTestDir(t, tmpDir, "tree")
		subDir := createTestDir(t, dirPath, "subdir")
		createTestFile(t, dirPath, "file1.txt", []byte("root"))
		createTestFile(t, subDir, "file2.txt", []byte("nested"))

		total, err := CountEntries(context.Background(), dirPath, true)
		require.NoError(t, err)
		assert.Equal(t, int64(4), total)
	})

	t.Run("non_recursive_counts_only_the_target", func(t *testing.T) {
		tmpDir := t.TempDir()
		dirPath := createTestDir(t, tmpDir, "tree")
		createTestFile(t, dirPath, "file1.txt", []byte("root"))

		total, err := CountEntries(context.Background(), dirPath, false)
		require.NoError(t, err)
		assert.Equal(t, int64(1), total)
	})
}

func TestCopyFileWithCallbacksPreservesModeAndTimes(t *testing.T) {
	tmpDir := t.TempDir()
	srcDir := createTestDir(t, tmpDir, "src")
	script := createTestFile(t, srcDir, "run.sh", []byte("#!/bin/sh\n"))
	secret := createTestFile(t, srcDir, "key", []byte("secret"))
	roDir := createTestDir(t, srcDir, "readonly")
	createTestFile(t, roDir, "inside.txt", []byte("inside"))
	sgidDir := createTestDir(t, srcDir, "shared")

	when := time.Date(2020, 3, 4, 5, 6, 7, 0, time.UTC)
	require.NoError(t, os.Chmod(script, 0o750))
	require.NoError(t, os.Chmod(secret, 0o600))
	require.NoError(t, os.Chmod(sgidDir, os.ModeSetgid|0o770))
	require.NoError(t, os.Chtimes(script, when, when))
	require.NoError(t, os.Chtimes(roDir, when, when))
	require.NoError(t, os.Chmod(roDir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(roDir, 0o755) })

	destDir := filepath.Join(tmpDir, "dest")
	require.NoError(t, CopyFileWithCallbacks(srcDir, destDir, false, nil))
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(destDir, "readonly"), 0o755) })

	assert.Equal(t, os.FileMode(0o750), statMode(t, filepath.Join(destDir, "run.sh")))
	assert.Equal(t, os.FileMode(0o600), statMode(t, filepath.Join(destDir, "key")))
	assert.Equal(t, os.FileMode(0o555), statMode(t, filepath.Join(destDir, "readonly")).Perm())
	assert.Equal(t, os.ModeDir|os.ModeSetgid|0o770, statMode(t, filepath.Join(destDir, "shared")))

	content, err := os.ReadFile(filepath.Join(destDir, "readonly", "inside.txt"))
	require.NoError(t, err)
	assert.Equal(t, []byte("inside"), content)

	assert.Equal(t, when.UnixNano(), statModTime(t, filepath.Join(destDir, "run.sh")).UnixNano())
	assert.Equal(t, when.UnixNano(), statModTime(t, filepath.Join(destDir, "readonly")).UnixNano())
}

func TestCopyFileWithCallbacksPreservesHardlinks(t *testing.T) {
	tmpDir := t.TempDir()
	srcDir := createTestDir(t, tmpDir, "src")
	first := createTestFile(t, srcDir, "first.bin", []byte("linked-data"))
	require.NoError(t, os.Link(first, filepath.Join(srcDir, "second.bin")))
	createTestFile(t, srcDir, "plain.bin", []byte("plain"))

	destDir := filepath.Join(tmpDir, "dest")
	var progressed int64
	require.NoError(t, CopyFileWithCallbacks(srcDir, destDir, false, &ipc.OperationCallbacks{
		Progress: func(n int64) { progressed += n },
	}))

	firstStat := statSys(t, filepath.Join(destDir, "first.bin"))
	secondStat := statSys(t, filepath.Join(destDir, "second.bin"))
	plainStat := statSys(t, filepath.Join(destDir, "plain.bin"))
	assert.Equal(t, firstStat.Ino, secondStat.Ino, "hardlinked files should share an inode after copy")
	assert.EqualValues(t, 2, firstStat.Nlink)
	assert.NotEqual(t, firstStat.Ino, plainStat.Ino)

	expected, err := ComputeCopySize(srcDir)
	require.NoError(t, err)
	assert.Equal(t, expected, progressed, "progress should still account for every file")
}

func TestCopyFileWithCallbacksPreservesXattrs(t *testing.T) {
	tmpDir := t.TempDir()
	srcDir := createTestDir(t, tmpDir, "src")
	file := createTestFile(t, srcDir, "tagged.txt", []byte("tagged"))
	if err := unix.Setxattr(file, "user.linuxio", []byte("file-value"), 0); err != nil {
		t.Skipf("xattrs not supported here: %v", err)
	}
	require.NoError(t, unix.Setxattr(srcDir, "user.linuxio", []byte("dir-value"), 0))

	destDir := filepath.Join(tmpDir, "dest")
	require.NoError(t, CopyFileWithCallbacks(srcDir, destDir, false, nil))

	assert.Equal(t, "file-value", readXattr(t, filepath.Join(destDir, "tagged.txt"), "user.linuxio"))
	assert.Equal(t, "dir-value", readXattr(t, destDir, "user.linuxio"))
}

func TestCopyFileWithCallbacksSkipsSpecialFiles(t *testing.T) {
	tmpDir := t.TempDir()
	srcDir := createTestDir(t, tmpDir, "src")
	createTestFile(t, srcDir, "data.txt", []byte("data"))
	listener, err := net.Listen("unix", filepath.Join(srcDir, "app.sock"))
	if err != nil {
		t.Skipf("unix sockets not supported here: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	destDir := filepath.Join(tmpDir, "dest")
	require.NoError(t, CopyFileWithCallbacks(srcDir, destDir, false, nil))

	content, err := os.ReadFile(filepath.Join(destDir, "data.txt"))
	require.NoError(t, err)
	assert.Equal(t, []byte("data"), content)
	_, err = os.Lstat(filepath.Join(destDir, "app.sock"))
	assert.True(t, os.IsNotExist(err), "socket should be skipped, not recreated")
}

func TestCopyFileWithCallbacksPreservesOwnershipAsRoot(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("ownership can only be preserved as root")
	}
	tmpDir := t.TempDir()
	srcDir := createTestDir(t, tmpDir, "src")
	file := createTestFile(t, srcDir, "owned.txt", []byte("owned"))
	createSymlinkOrSkip(t, "owned.txt", filepath.Join(srcDir, "owned-link"))
	require.NoError(t, os.Lchown(file, 12345, 12346))
	require.NoError(t, os.Lchown(filepath.Join(srcDir, "owned-link"), 12345, 12346))
	require.NoError(t, os.Lchown(srcDir, 12345, 12346))

	destDir := filepath.Join(tmpDir, "dest")
	require.NoError(t, CopyFileWithCallbacks(srcDir, destDir, false, nil))

	for _, name := range []string{"", "owned.txt", "owned-link"} {
		st := statSys(t, filepath.Join(destDir, name))
		assert.EqualValues(t, 12345, st.Uid, name)
		assert.EqualValues(t, 12346, st.Gid, name)
	}
}

func statMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Lstat(path)
	require.NoError(t, err)
	return info.Mode()
}

func statModTime(t *testing.T, path string) time.Time {
	t.Helper()
	info, err := os.Lstat(path)
	require.NoError(t, err)
	return info.ModTime()
}

func statSys(t *testing.T, path string) *syscall.Stat_t {
	t.Helper()
	info, err := os.Lstat(path)
	require.NoError(t, err)
	st, ok := info.Sys().(*syscall.Stat_t)
	require.True(t, ok)
	return st
}

func readXattr(t *testing.T, path, name string) string {
	t.Helper()
	buf := make([]byte, 256)
	n, err := unix.Getxattr(path, name, buf)
	require.NoError(t, err)
	return string(buf[:n])
}
