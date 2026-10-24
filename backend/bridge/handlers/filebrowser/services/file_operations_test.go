package services

import (
	"context"
	"errors"
	"io/fs"
	"math"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
)

func TestMoveFileWithCallbacksUsesKnownSizeForRename(t *testing.T) {
	tmpDir := t.TempDir()
	srcFile := createTestFile(t, tmpDir, "source-known-size.txt", []byte("content"))
	dstPath := filepath.Join(tmpDir, "destination-known-size.txt")

	var reported []int64
	err := MoveFileWithCallbacks(srcFile, dstPath, false, &OperationCallbacks{
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

var transferKinds = []struct {
	name       string
	transfer   func(src, dst string, overwrite bool, opts *OperationCallbacks) error
	sourceKept bool
}{
	{"move", func(src, dst string, overwrite bool, opts *OperationCallbacks) error {
		return MoveFileWithCallbacks(src, dst, overwrite, opts, MoveFileOptions{})
	}, false},
	{"copy", CopyFileWithCallbacks, true},
}

func TestTransferMergesIntoExistingDirectory(t *testing.T) {
	for _, tc := range transferKinds {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			src := createTestDir(t, tmpDir, "src")
			createTestFile(t, src, "a.txt", []byte("new"))
			createTestFile(t, createTestDir(t, src, "sub"), "c.txt", []byte("c"))
			dst := createTestDir(t, tmpDir, "dst")
			createTestFile(t, dst, "a.txt", []byte("old"))
			createTestFile(t, dst, "b.txt", []byte("b"))
			createTestFile(t, createTestDir(t, dst, "sub"), "d.txt", []byte("d"))

			err := tc.transfer(src, dst, false, nil)
			require.Error(t, err, "an existing folder is never touched without overwrite")
			assert.Equal(t, "old", readString(t, filepath.Join(dst, "a.txt")))

			require.NoError(t, tc.transfer(src, dst, true, nil))

			assert.Equal(t, "new", readString(t, filepath.Join(dst, "a.txt")), "same-named files are replaced")
			assert.Equal(t, "b", readString(t, filepath.Join(dst, "b.txt")), "other files in the folder are kept")
			assert.Equal(t, "c", readString(t, filepath.Join(dst, "sub", "c.txt")), "subfolders merge too")
			assert.Equal(t, "d", readString(t, filepath.Join(dst, "sub", "d.txt")))
			_, err = os.Lstat(src)
			if tc.sourceKept {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, fs.ErrNotExist, "a merged move removes the emptied source")
			}
			entries, err := os.ReadDir(tmpDir)
			require.NoError(t, err)
			assert.Len(t, entries, map[bool]int{true: 2, false: 1}[tc.sourceKept], "no aside is left behind")
		})
	}
}

func TestTransferOntoAncestorMergesIt(t *testing.T) {
	for _, tc := range transferKinds {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			outer := createTestDir(t, tmpDir, "foo")
			createTestFile(t, outer, "old.txt", []byte("old"))
			inner := createTestDir(t, outer, "foo")
			createTestFile(t, inner, "keep.txt", []byte("keep"))

			require.NoError(t, tc.transfer(inner, outer, true, nil))

			assert.Equal(t, "keep", readString(t, filepath.Join(outer, "keep.txt")))
			assert.Equal(t, "old", readString(t, filepath.Join(outer, "old.txt")), "the outer folder keeps its own files")
			_, err := os.Lstat(inner)
			if tc.sourceKept {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, fs.ErrNotExist)
			}
		})
	}
}

func TestCopyRemovesPartialDestinationOnFailure(t *testing.T) {
	tmpDir := t.TempDir()
	src := createTestDir(t, tmpDir, "src")
	createTestFile(t, src, "a.txt", []byte("aaaa"))
	createTestFile(t, src, "b.txt", []byte("bbbb"))
	dest := filepath.Join(tmpDir, "dest")

	progressed := false
	err := CopyFileWithCallbacks(src, dest, false, &OperationCallbacks{
		Progress: func(int64) { progressed = true },
		Cancel:   func() bool { return progressed },
	})
	require.ErrorIs(t, err, ErrAborted)

	_, err = os.Lstat(dest)
	require.ErrorIs(t, err, fs.ErrNotExist, "a cancelled copy leaves nothing under the final name")
	assert.Equal(t, "aaaa", readString(t, filepath.Join(src, "a.txt")))
	assert.Equal(t, "bbbb", readString(t, filepath.Join(src, "b.txt")))
}

func TestMoveAcrossFilesystemsRemovesEachSourceAfterItsCopy(t *testing.T) {
	other := otherFilesystemDir(t)
	tmpDir := t.TempDir()
	src := createTestDir(t, tmpDir, "src")
	createTestFile(t, src, "a.txt", []byte("aaaa"))
	createTestFile(t, src, "b.txt", []byte("bbbb"))
	dest := filepath.Join(other, "moved")

	// Cancel as soon as the first file (in directory order) has left the source.
	err := MoveFileWithCallbacks(src, dest, false, &OperationCallbacks{
		Cancel: func() bool { return len(dirNames(t, src)) < 2 },
	}, MoveFileOptions{})
	require.ErrorIs(t, err, ErrAborted)

	contents := map[string]string{"a.txt": "aaaa", "b.txt": "bbbb"}
	movedNames, leftNames := dirNames(t, dest), dirNames(t, src)
	require.Len(t, movedNames, 1, "the moved file stays at the destination")
	require.Len(t, leftNames, 1, "the unmoved file stays at the source")
	assert.NotEqual(t, movedNames[0], leftNames[0], "no file is in both places or in neither")
	assert.Equal(t, contents[movedNames[0]], readString(t, filepath.Join(dest, movedNames[0])))
	assert.Equal(t, contents[leftNames[0]], readString(t, filepath.Join(src, leftNames[0])))

	require.NoError(t, MoveFileWithCallbacks(src, dest, true, nil, MoveFileOptions{}))
	assert.ElementsMatch(t, []string{"a.txt", "b.txt"}, dirNames(t, dest))
	_, err = os.Lstat(src)
	require.ErrorIs(t, err, fs.ErrNotExist, "the emptied source folder is removed")
}

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func TestPrepareDestinationRestoresReplacedEntryOnFailure(t *testing.T) {
	tmpDir := t.TempDir()
	createTestFile(t, tmpDir, "src.txt", []byte("new"))
	dst := createTestFile(t, tmpDir, "dst.txt", []byte("old"))
	root, err := fsroot.Open()
	require.NoError(t, err)
	defer root.Close()

	finish, err := prepareDestination(root, dst, true, false)
	require.NoError(t, err)
	_, err = os.Lstat(dst)
	require.ErrorIs(t, err, fs.ErrNotExist, "destination is moved aside for the transfer")

	finish(errors.New("transfer failed"))
	assert.Equal(t, "old", readString(t, dst), "failed transfer restores the replaced file")

	finish, err = prepareDestination(root, dst, true, false)
	require.NoError(t, err)
	finish(nil)
	entries, err := os.ReadDir(tmpDir)
	require.NoError(t, err)
	assert.Len(t, entries, 1, "successful transfer removes the replaced file")
}

func TestPrepareDestinationRemovesPartialOnlyForCopies(t *testing.T) {
	root, err := fsroot.Open()
	require.NoError(t, err)
	defer root.Close()

	for _, tc := range []struct {
		name          string
		removePartial bool
		wantKept      bool
	}{
		{"copy", true, false},
		{"move", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dst := filepath.Join(t.TempDir(), "dst")
			finish, err := prepareDestination(root, dst, false, tc.removePartial)
			require.NoError(t, err)
			createTestFile(t, filepath.Dir(dst), "dst", []byte("partial"))

			finish(errors.New("transfer failed"))
			_, err = os.Lstat(dst)
			if tc.wantKept {
				require.NoError(t, err, "a move keeps what already crossed")
			} else {
				require.ErrorIs(t, err, fs.ErrNotExist, "a copy drops its partial output")
			}
		})
	}
}

func readString(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(content)
}

// otherFilesystemDir returns a fresh directory on a different filesystem than
// t.TempDir(), or skips the test when the host offers none.
func otherFilesystemDir(t *testing.T) string {
	t.Helper()
	base := statSys(t, t.TempDir())
	for _, candidate := range []string{"/dev/shm", "/run/user/" + strconv.Itoa(os.Getuid()), "/tmp"} {
		info, err := os.Stat(candidate)
		if err != nil || !info.IsDir() {
			continue
		}
		if st, ok := info.Sys().(*syscall.Stat_t); !ok || st.Dev == base.Dev {
			continue
		}
		dir, err := os.MkdirTemp(candidate, "linuxio-xdev-*")
		if err != nil {
			continue
		}
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
		return dir
	}
	t.Skip("no second filesystem available for a cross-device move")
	return ""
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
	err := CopyFileWithCallbacks(srcDir, destDir, false, &OperationCallbacks{
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

	size, err := ComputeCopySize(context.Background(), srcDir)
	require.NoError(t, err)
	assert.Equal(t, int64(len("target")), size)

	linkSize, err := ComputeCopySize(context.Background(), filepath.Join(srcDir, "file-link"))
	require.NoError(t, err)
	assert.Zero(t, linkSize)
}

func TestComputeCopySizeStopsOnCancelledContext(t *testing.T) {
	tmpDir := t.TempDir()
	srcDir := createTestDir(t, tmpDir, "src")
	createTestFile(t, srcDir, "a.txt", []byte("a"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := ComputeCopySize(ctx, srcDir)
	require.ErrorIs(t, err, context.Canceled)
}

func TestMoveFileWithCallbacksSkipsSizeWalkWithoutKnownSize(t *testing.T) {
	tmpDir := t.TempDir()
	srcDir := createTestDir(t, tmpDir, "src")
	createTestFile(t, srcDir, "a.txt", []byte("content"))

	var reported []int64
	err := MoveFileWithCallbacks(srcDir, filepath.Join(tmpDir, "dst"), false, &OperationCallbacks{
		Progress: func(n int64) { reported = append(reported, n) },
	}, MoveFileOptions{})
	require.NoError(t, err)
	assert.Empty(t, reported, "a rename without a known size must not walk the tree to report one")
}

func TestChangePermissionsCtxRecursiveDoesNotFollowSymlinks(t *testing.T) {
	tmpDir := t.TempDir()
	outside := createTestFile(t, tmpDir, "outside.txt", []byte("secret"))
	require.NoError(t, os.Chmod(outside, 0o600))
	treeDir := createTestDir(t, tmpDir, "tree")
	inside := createTestFile(t, treeDir, "inside.txt", []byte("x"))
	createSymlinkOrSkip(t, outside, filepath.Join(treeDir, "link"))

	require.NoError(t, ChangePermissionsCtx(context.Background(), treeDir, 0o700, true, nil))

	assert.Equal(t, os.FileMode(0o600), statMode(t, outside), "link target must keep its mode")
	assert.Equal(t, os.FileMode(0o700), statMode(t, inside))
}

func TestRecursivePermissionAndOwnershipReportFailedEntries(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read a mode 000 directory")
	}
	tmpDir := t.TempDir()
	treeDir := createTestDir(t, tmpDir, "tree")
	locked := createTestDir(t, treeDir, "locked")
	createTestFile(t, locked, "a.txt", []byte("a"))
	require.NoError(t, os.Chmod(locked, 0o000))
	t.Cleanup(func() {
		_ = os.Chmod(treeDir, 0o755)
		_ = os.Chmod(locked, 0o755)
	})

	err := ChangeOwnershipCtx(context.Background(), treeDir, os.Getuid(), os.Getgid(), true, nil)
	require.ErrorContains(t, err, "1 entries failed")

	err = ChangePermissionsCtx(context.Background(), treeDir, 0o000, true, nil)
	require.ErrorContains(t, err, "1 entries failed")
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
	require.NoError(t, CopyFileWithCallbacks(srcDir, destDir, false, &OperationCallbacks{
		Progress: func(n int64) { progressed += n },
	}))

	firstStat := statSys(t, filepath.Join(destDir, "first.bin"))
	secondStat := statSys(t, filepath.Join(destDir, "second.bin"))
	plainStat := statSys(t, filepath.Join(destDir, "plain.bin"))
	assert.Equal(t, firstStat.Ino, secondStat.Ino, "hardlinked files should share an inode after copy")
	assert.EqualValues(t, 2, firstStat.Nlink)
	assert.NotEqual(t, firstStat.Ino, plainStat.Ino)

	expected, err := ComputeCopySize(context.Background(), srcDir)
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

func TestCheckNotProtected(t *testing.T) {
	root, err := fsroot.Open()
	require.NoError(t, err)
	defer root.Close()

	require.ErrorContains(t, CheckNotProtected(root, "/"), "root")
	require.ErrorContains(t, CheckNotProtected(root, "/etc"), "system folder")
	require.ErrorContains(t, CheckNotProtected(root, "/proc/"), "system folder")
	require.NoError(t, CheckNotProtected(root, createTestDir(t, t.TempDir(), "mine")))
	require.NoError(t, CheckNotProtected(root, "/etc/hostname"), "files inside system folders are the user's business")

	other := otherFilesystemDir(t)
	mount := filepath.Dir(other) // the mount point the temp dir was made under
	if filepath.Dir(mount) != "/" {
		t.Skipf("%s is not a top-level mount point", mount)
	}
	// A mount point that is not in the fixed list, when the host has one.
	if !protectedTopLevel[filepath.Base(mount)] {
		require.ErrorContains(t, CheckNotProtected(root, mount), "mount point")
	}
}

func TestEnsureFreeSpace(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, EnsureFreeSpace(dir, 1))
	require.NoError(t, EnsureFreeSpace(dir, 0))
	require.NoError(t, EnsureFreeSpace(filepath.Join(dir, "missing"), 1), "an unreadable destination does not block the transfer")
	err := EnsureFreeSpace(dir, math.MaxInt64)
	require.ErrorContains(t, err, "not enough free space")
	assert.Contains(t, err.Error(), "needed")
}

func TestMoveWithoutReplaceRefusesAnAppearedDestination(t *testing.T) {
	tmpDir := t.TempDir()
	src := createTestFile(t, tmpDir, "src.txt", []byte("src"))
	dst := createTestFile(t, tmpDir, "dst.txt", []byte("dst"))
	root, err := fsroot.Open()
	require.NoError(t, err)
	defer root.Close()
	info, err := os.Lstat(src)
	require.NoError(t, err)

	require.ErrorIs(t, moveWithoutReplace(root, src, dst, info), fs.ErrExist)
	assert.Equal(t, "src", readString(t, src))
	assert.Equal(t, "dst", readString(t, dst), "rename(2) would have replaced this")

	fresh := filepath.Join(tmpDir, "fresh.txt")
	require.NoError(t, moveWithoutReplace(root, src, fresh, info))
	assert.Equal(t, "src", readString(t, fresh))
	_, err = os.Lstat(src)
	require.ErrorIs(t, err, fs.ErrNotExist)
}
