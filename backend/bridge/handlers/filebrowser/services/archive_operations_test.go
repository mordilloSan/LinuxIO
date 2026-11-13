package services

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
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

// createZipFile writes a zip of filenames to path, created or truncated
// here as the compress task creates its temp file, and closes it.
func createZipFile(t *testing.T, path string, opts *OperationCallbacks, skipPath string, filenames ...string) error {
	t.Helper()
	out, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
	require.NoError(t, err)
	defer out.Close()
	return CreateZip(out, opts, skipPath, ArchiveSources{}, filenames...)
}

// createTarGzFile is createZipFile for a tar.gz.
func createTarGzFile(t *testing.T, path string, opts *OperationCallbacks, skipPath string, workers int, filenames ...string) error {
	t.Helper()
	out, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
	require.NoError(t, err)
	defer out.Close()
	return CreateTarGz(out, opts, skipPath, ArchiveSources{}, workers, filenames...)
}

func TestComputeArchiveSize(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("single_file", func(t *testing.T) {
		filePath := createTestFile(t, tmpDir, "file1.txt", []byte("hello"))
		size, err := ComputeArchiveSize(t.Context(), []string{filePath}, Owner{})
		require.NoError(t, err)
		assert.Equal(t, int64(5), size, "single file size should be 5 bytes")
	})

	t.Run("multiple_files", func(t *testing.T) {
		file1 := createTestFile(t, tmpDir, "file1.txt", []byte("hello"))
		file2 := createTestFile(t, tmpDir, "file2.txt", []byte("world"))
		size, err := ComputeArchiveSize(t.Context(), []string{file1, file2}, Owner{})
		require.NoError(t, err)
		assert.Equal(t, int64(10), size, "two files should total 10 bytes")
	})

	t.Run("directory_with_files", func(t *testing.T) {
		subDir := createTestDir(t, tmpDir, "subdir")
		createTestFile(t, subDir, "file1.txt", []byte("test"))
		createTestFile(t, subDir, "file2.txt", []byte("data"))

		size, err := ComputeArchiveSize(t.Context(), []string{subDir}, Owner{})
		require.NoError(t, err)
		assert.Equal(t, int64(8), size, "directory with two 4-byte files should be 8 bytes")
	})

	t.Run("nested_directories", func(t *testing.T) {
		dir1 := createTestDir(t, tmpDir, "dir1")
		dir2 := createTestDir(t, dir1, "dir2")
		createTestFile(t, dir1, "file1.txt", []byte("a"))
		createTestFile(t, dir2, "file2.txt", []byte("bb"))

		size, err := ComputeArchiveSize(t.Context(), []string{dir1}, Owner{})
		require.NoError(t, err)
		assert.Equal(t, int64(3), size, "nested directories with 1+2 byte files should be 3 bytes")
	})

	t.Run("nonexistent_file", func(t *testing.T) {
		_, err := ComputeArchiveSize(t.Context(), []string{filepath.Join(tmpDir, "nonexistent.txt")}, Owner{})
		require.Error(t, err, "should error on nonexistent file")
	})

	t.Run("empty_directory", func(t *testing.T) {
		emptyDir := createTestDir(t, tmpDir, "empty")
		size, err := ComputeArchiveSize(t.Context(), []string{emptyDir}, Owner{})
		require.NoError(t, err)
		assert.Equal(t, int64(0), size, "empty directory should have 0 size")
	})

	t.Run("mixed_files_and_directories", func(t *testing.T) {
		file1 := createTestFile(t, tmpDir, "file1.txt", []byte("abc"))
		dir1 := createTestDir(t, tmpDir, "dir1")
		createTestFile(t, dir1, "file2.txt", []byte("defgh"))

		size, err := ComputeArchiveSize(t.Context(), []string{file1, dir1}, Owner{})
		require.NoError(t, err)
		// File1 is 3 bytes, file2.txt is 5 bytes = 8 bytes total for content files
		assert.GreaterOrEqual(t, size, int64(8), "should include both files (at least 8 bytes)")
	})
}

func TestComputeExtractSize(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("zip_archive", func(t *testing.T) {
		srcDir := createTestDir(t, tmpDir, "zip-src-size")
		content := []byte("zip data")
		createTestFile(t, srcDir, "file.txt", content)
		zipPath := filepath.Join(tmpDir, "archive.zip")

		err := createZipFile(t, zipPath, nil, zipPath, srcDir)
		require.NoError(t, err)

		size, err := ComputeExtractSize(zipPath)
		require.NoError(t, err)
		assert.Equal(t, int64(len(content)), size)
	})

	t.Run("tar_gz_archive", func(t *testing.T) {
		srcDir := createTestDir(t, tmpDir, "tar-src-size")
		content := []byte("tar data payload")
		createTestFile(t, srcDir, "nested.txt", content)
		tarPath := filepath.Join(tmpDir, "archive.tar.gz")

		err := createTarGzFile(t, tarPath, nil, tarPath, 0, srcDir)
		require.NoError(t, err)

		size, err := ComputeExtractSize(tarPath)
		require.NoError(t, err)
		assert.Equal(t, int64(len(content)), size)
	})

	t.Run("unsupported_format", func(t *testing.T) {
		_, err := ComputeExtractSize(filepath.Join(tmpDir, "file.bin"))
		require.Error(t, err)
	})
}

func TestCreateZip(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("create_zip_single_file", func(t *testing.T) {
		srcFile := createTestFile(t, tmpDir, "source.txt", []byte("test content"))
		zipPath := filepath.Join(tmpDir, "test.zip")

		err := createZipFile(t, zipPath, nil, zipPath, srcFile)
		require.NoError(t, err, "CreateZip should not error")

		// Verify zip file exists and has content
		stat, err := os.Stat(zipPath)
		require.NoError(t, err, "zip file should exist")
		assert.Positive(t, stat.Size(), "zip file should have content")
	})

	t.Run("create_zip_multiple_files", func(t *testing.T) {
		file1 := createTestFile(t, tmpDir, "file1.txt", []byte("content1"))
		file2 := createTestFile(t, tmpDir, "file2.txt", []byte("content2"))
		zipPath := filepath.Join(tmpDir, "multi.zip")

		err := createZipFile(t, zipPath, nil, zipPath, file1, file2)
		require.NoError(t, err, "CreateZip should handle multiple files")

		stat, err := os.Stat(zipPath)
		require.NoError(t, err, "zip file should exist")
		assert.Positive(t, stat.Size(), "zip file should have content")
	})

	t.Run("create_zip_with_directory", func(t *testing.T) {
		subDir := createTestDir(t, tmpDir, "archive_subdir")
		createTestFile(t, subDir, "file.txt", []byte("data"))
		zipPath := filepath.Join(tmpDir, "dir.zip")

		err := createZipFile(t, zipPath, nil, zipPath, subDir)
		require.NoError(t, err, "CreateZip should handle directories")

		stat, err := os.Stat(zipPath)
		require.NoError(t, err, "zip file should exist")
		assert.Positive(t, stat.Size(), "zip file should have content")
	})

	t.Run("create_zip_overwrites_existing", func(t *testing.T) {
		srcFile := createTestFile(t, tmpDir, "source.txt", []byte("original"))
		zipPath := filepath.Join(tmpDir, "overwrite.zip")

		// Create first zip
		err := createZipFile(t, zipPath, nil, zipPath, srcFile)
		require.NoError(t, err)
		firstStat, err := os.Stat(zipPath)
		require.NoError(t, err)

		// Small delay to ensure file times differ
		time.Sleep(10 * time.Millisecond)

		// Overwrite with different file
		modifiedFile := createTestFile(t, tmpDir, "modified.txt", []byte("much longer content here"))
		err = createZipFile(t, zipPath, nil, zipPath, modifiedFile)
		require.NoError(t, err)
		secondStat, err := os.Stat(zipPath)
		require.NoError(t, err)

		// File should have been overwritten - size should likely be different
		// (though size comparison isn't guaranteed, just check file exists and is valid)
		require.NoError(t, err, "should successfully overwrite zip file")
		assert.Positive(t, secondStat.Size(), "recreated zip should have content")
		assert.Positive(t, firstStat.Size(), "original zip should have content")
	})

	t.Run("create_zip_with_absolute_symlink_chain", func(t *testing.T) {
		fixtureDir := t.TempDir()
		sourceDir := createTestDir(t, fixtureDir, "source")
		target := createTestFile(t, fixtureDir, "python-target", []byte("python binary"))

		require.NoError(t, os.Symlink(target, filepath.Join(sourceDir, "python3")))
		require.NoError(t, os.Symlink("python3", filepath.Join(sourceDir, "python")))

		zipPath := filepath.Join(fixtureDir, "source.zip")
		require.NoError(t, createZipFile(t, zipPath, nil, zipPath, sourceDir))

		extractDir := filepath.Join(fixtureDir, "extracted")
		require.NoError(t, ExtractArchive(t.Context(), zipPath, extractDir, Owner{}, nil, 0))

		content, err := os.ReadFile(filepath.Join(extractDir, "source", "python"))
		require.NoError(t, err)
		assert.Equal(t, "python binary", string(content))
	})
}

func TestCreateTarGz(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("create_targz_single_file", func(t *testing.T) {
		srcFile := createTestFile(t, tmpDir, "source.txt", []byte("test content"))
		targzPath := filepath.Join(tmpDir, "test.tar.gz")

		err := createTarGzFile(t, targzPath, nil, targzPath, 0, srcFile)
		require.NoError(t, err, "CreateTarGz should not error")

		// Verify tar.gz file exists and has content
		stat, err := os.Stat(targzPath)
		require.NoError(t, err, "tar.gz file should exist")
		assert.Positive(t, stat.Size(), "tar.gz file should have content")
	})

	t.Run("create_targz_multiple_files", func(t *testing.T) {
		file1 := createTestFile(t, tmpDir, "file1.txt", []byte("content1"))
		file2 := createTestFile(t, tmpDir, "file2.txt", []byte("content2"))
		targzPath := filepath.Join(tmpDir, "multi.tar.gz")

		err := createTarGzFile(t, targzPath, nil, targzPath, 0, file1, file2)
		require.NoError(t, err, "CreateTarGz should handle multiple files")

		stat, err := os.Stat(targzPath)
		require.NoError(t, err, "tar.gz file should exist")
		assert.Positive(t, stat.Size(), "tar.gz file should have content")
	})

	t.Run("create_targz_with_directory", func(t *testing.T) {
		subDir := createTestDir(t, tmpDir, "archive_subdir_gz")
		createTestFile(t, subDir, "file.txt", []byte("data"))
		targzPath := filepath.Join(tmpDir, "dir.tar.gz")

		err := createTarGzFile(t, targzPath, nil, targzPath, 0, subDir)
		require.NoError(t, err, "CreateTarGz should handle directories")

		stat, err := os.Stat(targzPath)
		require.NoError(t, err, "tar.gz file should exist")
		assert.Positive(t, stat.Size(), "tar.gz file should have content")
	})
}

func TestExtractArchive(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("extract_zip_archive", func(t *testing.T) {
		srcDir := createTestDir(t, tmpDir, "zip-src")
		createTestFile(t, srcDir, "file.txt", []byte("zip data"))
		zipPath := filepath.Join(tmpDir, "archive.zip")

		err := createZipFile(t, zipPath, nil, zipPath, srcDir)
		require.NoError(t, err, "CreateZip should succeed before extraction")

		destDir := filepath.Join(tmpDir, "zip-dest")
		err = ExtractArchive(t.Context(), zipPath, destDir, Owner{}, nil, 0)
		require.NoError(t, err, "ExtractArchive should extract zip")

		content, err := os.ReadFile(filepath.Join(destDir, "zip-src", "file.txt"))
		require.NoError(t, err, "extracted file should exist")
		assert.Equal(t, "zip data", string(content), "extracted content should match")
	})

	t.Run("extract_tar_gz_archive", func(t *testing.T) {
		srcDir := createTestDir(t, tmpDir, "tar-src")
		createTestFile(t, srcDir, "nested.txt", []byte("tar data"))
		tarPath := filepath.Join(tmpDir, "archive.tar.gz")

		err := createTarGzFile(t, tarPath, nil, tarPath, 0, srcDir)
		require.NoError(t, err, "CreateTarGz should succeed before extraction")

		destDir := filepath.Join(tmpDir, "tar-dest")
		err = ExtractArchive(t.Context(), tarPath, destDir, Owner{}, nil, 0)
		require.NoError(t, err, "ExtractArchive should extract tar.gz")

		content, err := os.ReadFile(filepath.Join(destDir, "tar-src", "nested.txt"))
		require.NoError(t, err, "extracted file should exist")
		assert.Equal(t, "tar data", string(content), "extracted content should match")
	})
}

func TestArchiveOperationsEdgeCases(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("empty_file_list", func(t *testing.T) {
		size, err := ComputeArchiveSize(t.Context(), []string{}, Owner{})
		require.NoError(t, err)
		assert.Equal(t, int64(0), size, "empty file list should have 0 size")
	})

	t.Run("symlink_handling", func(t *testing.T) {
		srcFile := createTestFile(t, tmpDir, "original.txt", []byte("content"))
		linkPath := filepath.Join(tmpDir, "link.txt")

		err := os.Symlink(srcFile, linkPath)
		if err != nil {
			t.Skip("symlinks not supported on this platform")
		}

		size, err := ComputeArchiveSize(t.Context(), []string{linkPath}, Owner{})
		require.NoError(t, err)
		assert.Equal(t, int64(7), size, "symlink should resolve to target size")
	})

	t.Run("large_file_simulation", func(t *testing.T) {
		// Create a file with known size
		largeContent := make([]byte, 10*1024*1024) // 10 MB
		for i := range largeContent {
			largeContent[i] = byte(i % 256)
		}
		largeFile := createTestFile(t, tmpDir, "large.bin", largeContent)

		size, err := ComputeArchiveSize(t.Context(), []string{largeFile}, Owner{})
		require.NoError(t, err)
		assert.Equal(t, int64(10*1024*1024), size, "should correctly compute large file size")
	})
}

func writeSingleEntryZip(t *testing.T, path, entryName string) {
	f, err := os.Create(path)
	require.NoError(t, err)
	zw := zip.NewWriter(f)
	w, err := zw.Create(entryName)
	require.NoError(t, err)
	_, err = w.Write([]byte("pwned"))
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	require.NoError(t, f.Close())
}

func writeSingleEntryTarGz(t *testing.T, path, entryName string) {
	f, err := os.Create(path)
	require.NoError(t, err)
	gw := gzip.NewWriter(f)
	tw := tar.NewWriter(gw)
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: entryName, Typeflag: tar.TypeReg, Mode: 0o644, Size: 5}))
	_, err = tw.Write([]byte("pwned"))
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())
	require.NoError(t, f.Close())
}

func TestExtractArchiveStaysInsideDestination(t *testing.T) {
	formats := []struct {
		ext   string
		write func(t *testing.T, path, entryName string)
	}{
		{".zip", writeSingleEntryZip},
		{".tar.gz", writeSingleEntryTarGz},
	}

	for _, format := range formats {
		t.Run("dotdot_entry"+format.ext, func(t *testing.T) {
			tmpDir := t.TempDir()
			archivePath := filepath.Join(tmpDir, "archive"+format.ext)
			format.write(t, archivePath, "../escape.txt")

			require.Error(t, ExtractArchive(t.Context(), archivePath, filepath.Join(tmpDir, "dest"), Owner{}, nil, 0))
			assert.NoFileExists(t, filepath.Join(tmpDir, "escape.txt"))
		})

	}
}

// The destination is always created fresh, so a folder that already exists,
// with whatever symlinks it holds, is refused instead of being merged into.
func TestExtractArchiveRefusesExistingDestination(t *testing.T) {
	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "archive.zip")
	writeSingleEntryZip(t, archivePath, "link/pwned.txt")
	destDir := createTestDir(t, tmpDir, "dest")
	outside := createTestDir(t, tmpDir, "outside")
	require.NoError(t, os.Symlink("../outside", filepath.Join(destDir, "link")))

	require.ErrorContains(t, ExtractArchive(t.Context(), archivePath, destDir, Owner{}, nil, 0), "already exists")
	assert.NoFileExists(t, filepath.Join(outside, "pwned.txt"))
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestCopyWithCallbacksReportsProgressAfterWrite(t *testing.T) {
	var progressed int64
	opts := &OperationCallbacks{Progress: func(n int64) { progressed += n }}

	require.Error(t, copyWithCallbacks(failingWriter{}, strings.NewReader("hello"), opts))
	assert.Zero(t, progressed, "bytes that never reached the destination must not count as progress")
}

func TestAddArchiveSizeSaturates(t *testing.T) {
	assert.Equal(t, int64(30), addArchiveSize(10, 20))
	assert.Equal(t, int64(math.MaxInt64), addArchiveSize(10, math.MaxUint64))
	assert.Equal(t, int64(math.MaxInt64), addArchiveSize(math.MaxInt64-5, 6))
	assert.Equal(t, int64(math.MaxInt64), addArchiveSize(math.MaxInt64, 1))
}

func TestCreateArchiveSkipsDanglingSymlinkInDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	srcDir := createTestDir(t, tmpDir, "src")
	createTestFile(t, srcDir, "keep.txt", []byte("kept"))
	require.NoError(t, os.Symlink(filepath.Join(tmpDir, "missing"), filepath.Join(srcDir, "dangling")))

	zipPath := filepath.Join(tmpDir, "out.zip")
	require.NoError(t, createZipFile(t, zipPath, nil, zipPath, srcDir))
	zr, err := zip.OpenReader(zipPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = zr.Close() })
	var zipNames []string
	for _, f := range zr.File {
		zipNames = append(zipNames, f.Name)
	}
	assert.Equal(t, []string{"src/keep.txt"}, zipNames)

	tarPath := filepath.Join(tmpDir, "out.tar.gz")
	require.NoError(t, createTarGzFile(t, tarPath, nil, tarPath, 0, srcDir))
	tarFile, err := os.Open(tarPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tarFile.Close() })
	gr, err := gzip.NewReader(tarFile)
	require.NoError(t, err)
	tr := tar.NewReader(gr)
	var tarNames []string
	for {
		header, nextErr := tr.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		require.NoError(t, nextErr)
		tarNames = append(tarNames, header.Name)
	}
	assert.Equal(t, []string{"src/keep.txt"}, tarNames)
}

func TestCreateTarGzKeepsHeaderSizeWhenFileGrows(t *testing.T) {
	tmpDir := t.TempDir()
	srcDir := createTestDir(t, tmpDir, "src")
	logPath := createTestFile(t, srcDir, "app.log", []byte("0123456789"))
	grown := false
	opts := &OperationCallbacks{Progress: func(int64) {
		if grown {
			return
		}
		grown = true
		logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0)
		require.NoError(t, err)
		_, err = logFile.WriteString("appended while archiving")
		require.NoError(t, err)
		require.NoError(t, logFile.Close())
	}}

	tarPath := filepath.Join(tmpDir, "out.tar.gz")
	require.NoError(t, createTarGzFile(t, tarPath, opts, tarPath, 0, srcDir))
	require.True(t, grown)

	destDir := filepath.Join(tmpDir, "dest")
	require.NoError(t, ExtractArchive(t.Context(), tarPath, destDir, Owner{}, nil, 0))
	content, err := os.ReadFile(filepath.Join(destDir, "src", "app.log"))
	require.NoError(t, err)
	assert.Equal(t, "0123456789", string(content))
}

func TestExtractArchiveKeepsExecutableBit(t *testing.T) {
	tmpDir := t.TempDir()
	srcDir := createTestDir(t, tmpDir, "src")
	script := createTestFile(t, srcDir, "run.sh", []byte("#!/bin/sh\n"))
	require.NoError(t, os.Chmod(script, 0o755))

	zipPath := filepath.Join(tmpDir, "out.zip")
	require.NoError(t, createZipFile(t, zipPath, nil, zipPath, srcDir))
	tarPath := filepath.Join(tmpDir, "out.tar.gz")
	require.NoError(t, createTarGzFile(t, tarPath, nil, tarPath, 0, srcDir))

	for _, archivePath := range []string{zipPath, tarPath} {
		destDir := filepath.Join(tmpDir, "dest-"+filepath.Base(archivePath))
		require.NoError(t, ExtractArchive(t.Context(), archivePath, destDir, Owner{}, nil, 0))
		info, err := os.Stat(filepath.Join(destDir, "src", "run.sh"))
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o755), info.Mode().Perm(), archivePath)
	}
}

func TestExtractZipStopsWorkersAfterFirstError(t *testing.T) {
	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "archive.zip")
	f, err := os.Create(archivePath)
	require.NoError(t, err)
	zw := zip.NewWriter(f)
	for _, name := range []string{"../escape.txt", "later.txt"} {
		w, createErr := zw.Create(name)
		require.NoError(t, createErr)
		_, err = w.Write([]byte("data"))
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	require.NoError(t, f.Close())

	destDir := filepath.Join(tmpDir, "dest")
	require.Error(t, ExtractArchive(t.Context(), archivePath, destDir, Owner{}, nil, 1))
	assert.NoFileExists(t, filepath.Join(destDir, "later.txt"))
}

// On a root bridge an extract makes its destination, and any missing folder
// above it, without following a link in a folder the session user can write:
// a process of theirs could have swapped a folder on the path for one.
func TestExtractNeverGoesThroughALinkTheUserCouldPlant(t *testing.T) {
	f := newPlantedLinkFixture(t)
	src := createTestFile(t, t.TempDir(), "payload.txt", []byte("payload"))
	archive := filepath.Join(t.TempDir(), "payload.zip")
	require.NoError(t, createZipFile(t, archive, nil, archive, src))

	for _, dest := range []string{filepath.Join(f.docs, "out"), filepath.Join(f.docs, "missing", "out")} {
		require.ErrorIs(t, ExtractArchive(t.Context(), archive, dest, f.session, nil, 0), bridgeipc.ErrForbidden, dest)
		f.requireVictimUntouched(t)
	}

	// A bridge that is not root acts as the user and follows their links.
	require.NoError(t, ExtractArchive(t.Context(), archive, filepath.Join(f.docs, "out"), Owner{}, nil, 0))
	assert.FileExists(t, filepath.Join(f.victim, "out", "payload.txt"))
}

// An aborted extract removes the destination it made, through the handle of
// the folder it made it in.
func TestExtractAbortRemovesItsDestination(t *testing.T) {
	src := createTestFile(t, t.TempDir(), "payload.txt", []byte("payload"))
	archive := filepath.Join(t.TempDir(), "payload.tar.gz")
	require.NoError(t, createTarGzFile(t, archive, nil, archive, 0, src))
	dest := filepath.Join(t.TempDir(), "out")

	err := ExtractArchive(t.Context(), archive, dest, Owner{}, &OperationCallbacks{Cancel: func() bool { return true }}, 0)
	require.ErrorIs(t, err, ErrAborted)
	assert.NoDirExists(t, dest)
}

// archiveContents returns the entries of the zip or tar.gz at path, a folder
// with an empty content and a trailing slash.
func archiveContents(t *testing.T, path string) map[string]string {
	t.Helper()
	contents := map[string]string{}
	if strings.HasSuffix(path, ".zip") {
		zr, err := zip.OpenReader(path)
		require.NoError(t, err)
		defer zr.Close()
		for _, f := range zr.File {
			r, err := f.Open()
			require.NoError(t, err)
			data, err := io.ReadAll(r)
			require.NoError(t, err)
			_ = r.Close()
			contents[f.Name] = string(data)
		}
		return contents
	}
	file, err := os.Open(path)
	require.NoError(t, err)
	defer file.Close()
	gr, err := gzip.NewReader(file)
	require.NoError(t, err)
	tr := tar.NewReader(gr)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return contents
		}
		require.NoError(t, err)
		data, err := io.ReadAll(tr)
		require.NoError(t, err)
		contents[header.Name] = string(data)
	}
}

// A compress for the session user on a root bridge reads its sources through
// handles: a link in a folder that user can write is left out of the archive
// and reported, never followed, so root does not copy what it points at into
// an archive the user can read. A source named through such a link is
// refused. A bridge that is not root acts as the user and follows their links.
func TestCompressNeverReadsThroughALinkTheUserCouldPlant(t *testing.T) {
	f := newPlantedLinkFixture(t)
	createTestFile(t, createTestDir(t, f.home, "sub"), "mine.txt", []byte("mine"))
	plantTrashLink(t, f.notes, filepath.Join(f.home, "notes-link"))
	out := t.TempDir()

	for _, name := range []string{"home.zip", "home.tar.gz"} {
		t.Run(name, func(t *testing.T) {
			var skipped []string
			sources := ArchiveSources{Owner: f.session, Skipped: func(path string, err error) {
				require.ErrorContains(t, err, "left out: a link to "+f.victim)
				skipped = append(skipped, path)
			}}
			archive := filepath.Join(out, name)
			create := func(sources ArchiveSources, paths ...string) error {
				file, err := os.OpenFile(archive, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
				require.NoError(t, err)
				defer file.Close()
				if strings.HasSuffix(name, ".zip") {
					return CreateZip(file, nil, "", sources, paths...)
				}
				return CreateTarGz(file, nil, "", sources, 1, paths...)
			}
			base := filepath.Base(f.home)

			require.NoError(t, create(sources, f.home))
			assert.Equal(t, map[string]string{base + "/sub/": "", base + "/sub/mine.txt": "mine"}, archiveContents(t, archive))
			assert.Equal(t, []string{f.docs, filepath.Join(f.home, "notes-link")}, skipped)

			err := create(sources, filepath.Join(f.docs, "notes.txt"))
			require.ErrorIs(t, err, bridgeipc.ErrForbidden)
			require.ErrorContains(t, err, f.docs+" is a link to "+f.victim)

			require.NoError(t, create(ArchiveSources{}, f.home))
			assert.Equal(t, "secret", archiveContents(t, archive)[base+"/notes-link"])
			f.requireVictimUntouched(t)
		})
	}
}

// The size estimate of a compress for the session user reads as the archive
// does: a link in a folder that user can write is not measured, and a source
// named through one is refused, so a planted link to / does not send root to
// walk the host. A canceled walk is an error for an owner too.
func TestComputeArchiveSizeNeverMeasuresThroughALinkTheUserCouldPlant(t *testing.T) {
	f := newPlantedLinkFixture(t)
	createTestFile(t, createTestDir(t, f.home, "sub"), "mine.txt", []byte("mine"))

	size, err := ComputeArchiveSize(t.Context(), []string{f.home}, f.session)
	require.NoError(t, err)
	assert.Equal(t, int64(len("mine")), size)

	_, err = ComputeArchiveSize(t.Context(), []string{filepath.Join(f.docs, "notes.txt")}, f.session)
	require.ErrorIs(t, err, bridgeipc.ErrForbidden)

	size, err = ComputeArchiveSize(t.Context(), []string{filepath.Join(f.docs, "notes.txt")}, Owner{})
	require.NoError(t, err)
	assert.Equal(t, int64(len("secret")), size)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = ComputeArchiveSize(ctx, []string{f.home}, f.session)
	require.ErrorIs(t, err, context.Canceled)
}

// A link the session user cannot have planted, in a folder only root can
// write, is still followed into the archive on a root bridge.
func TestCompressFollowsALinkInARootFolderAsRoot(t *testing.T) {
	home := rootSessionHome(t)
	system := createTestDir(t, filepath.Dir(home), "system")
	createTestFile(t, system, "app.conf", []byte("conf"))
	plantTrashLink(t, filepath.Join(system, "app.conf"), filepath.Join(system, "current.conf"))
	archive := filepath.Join(t.TempDir(), "system.zip")
	file, err := os.OpenFile(archive, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
	require.NoError(t, err)
	defer file.Close()
	var skipped []string
	sources := ArchiveSources{Owner: Owner{UID: rootTestUID, GID: rootTestGID, Set: true}, Skipped: func(path string, _ error) {
		skipped = append(skipped, path)
	}}

	require.NoError(t, CreateZip(file, nil, "", sources, system))
	assert.Equal(t, map[string]string{"system/app.conf": "conf", "system/current.conf": "conf"}, archiveContents(t, archive))
	assert.Empty(t, skipped)
}
