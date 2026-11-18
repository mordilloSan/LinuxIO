package services

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/fsroot"
)

func TestReadEditorFileEligibility(t *testing.T) {
	tmpDir := t.TempDir()
	plainPath := createTestFile(t, tmpDir, "extensionless", []byte("plain text"))
	nulPath := createTestFile(t, tmpDir, "with-nul.txt", []byte("safe\x00text"))
	largePath := createTestFile(t, tmpDir, "large.txt", bytes.Repeat([]byte("x"), int(MaxTextFileBytes)))
	symlinkPath := filepath.Join(tmpDir, "link.txt")
	require.NoError(t, os.Symlink(plainPath, symlinkPath))
	dirLinkPath := filepath.Join(tmpDir, "dirlink")
	require.NoError(t, os.Symlink(tmpDir, dirLinkPath))

	tests := []struct {
		name    string
		path    string
		wantErr error
	}{
		{name: "plain", path: plainPath},
		{name: "NUL", path: nulPath, wantErr: ErrEditorFileContainsNUL},
		{name: "oversized", path: largePath, wantErr: ErrEditorFileNotEligible},
		{name: "symlink", path: symlinkPath},
		{name: "directory symlink", path: dirLinkPath, wantErr: ErrEditorFileNotEligible},
		{name: "directory", path: tmpDir, wantErr: ErrEditorFileNotEligible},
		{name: "device", path: "/dev/null", wantErr: ErrEditorFileNotEligible},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ReadEditorFile(context.Background(), test.path, Owner{})
			if test.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, test.wantErr)
		})
	}
}

func TestReadEditorFileVersionChangesWithContent(t *testing.T) {
	tmpDir := t.TempDir()
	path := createTestFile(t, tmpDir, "note", []byte("before"))
	first, err := ReadEditorFile(context.Background(), path, Owner{})
	require.NoError(t, err)
	require.NotEmpty(t, first.Version)
	require.NoError(t, os.WriteFile(path, []byte("after"), 0o644))

	root, err := fsroot.OpenAt("/")
	require.NoError(t, err)
	t.Cleanup(func() { _ = root.Close() })
	second, err := EditorFileVersion(context.Background(), root, path)
	require.NoError(t, err)
	assert.NotEqual(t, first.Version, second)
}

func TestReadEditorFileValidatesFullContent(t *testing.T) {
	tmpDir := t.TempDir()
	content := append(bytes.Repeat([]byte("a"), 4096), 0)
	path := createTestFile(t, tmpDir, "late-nul", content)
	_, err := ReadEditorFile(context.Background(), path, Owner{})
	require.ErrorIs(t, err, ErrEditorFileContainsNUL)
}

func TestReadEditorFileAcceptsUTF8AcrossFormerSampleBoundary(t *testing.T) {
	tmpDir := t.TempDir()
	content := append(bytes.Repeat([]byte("a"), 4095), []byte("€")...)
	path := createTestFile(t, tmpDir, "utf8", content)

	read, err := ReadEditorFile(context.Background(), path, Owner{})
	require.NoError(t, err)
	assert.Equal(t, content, read.Content)
}

func TestReadEditorFileReportsAtomicSaveCapability(t *testing.T) {
	tmpDir := t.TempDir()
	path := createTestFile(t, tmpDir, "note", []byte("text"))
	read, err := ReadEditorFile(context.Background(), path, Owner{})
	require.NoError(t, err)
	assert.True(t, read.CanSave)

	if os.Geteuid() == 0 {
		t.Skip("root can replace files in read-only directories")
	}
	require.NoError(t, os.Chmod(tmpDir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(tmpDir, 0o755) })
	read, err = ReadEditorFile(context.Background(), path, Owner{})
	require.NoError(t, err)
	assert.False(t, read.CanSave)
}

func TestReadEditorFileCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := ReadEditorFile(ctx, filepath.Join(t.TempDir(), "missing"), Owner{})
	assert.ErrorIs(t, err, context.Canceled)
}

func TestReadEditorFileIgnoresUnrelatedSiblings(t *testing.T) {
	dir := t.TempDir()
	for i := range 300 {
		createTestFile(t, dir, fmt.Sprintf("sibling-%03d", i), []byte("unused"))
	}
	target := createTestFile(t, dir, "target", []byte("requested"))
	read, err := ReadEditorFile(context.Background(), target, Owner{})
	require.NoError(t, err)
	assert.Equal(t, []byte("requested"), read.Content)
}

// The editor opens only a regular file. Handed a name that leads to a FIFO,
// as one swapped in after the path was resolved does, it refuses it without
// opening it, where an open would wait for a writer.
func TestTheEditorNeverOpensWhatIsNotARegularFile(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "fifo")
	require.NoError(t, unix.Mkfifo(fifo, 0o644))
	t.Cleanup(func() {
		// Lets an open that waits for a writer return, should one hang.
		if f, err := os.OpenFile(fifo, os.O_WRONLY|unix.O_NONBLOCK, 0); err == nil {
			_ = f.Close()
		}
	})
	link := filepath.Join(dir, "notes.txt")
	require.NoError(t, os.Symlink("fifo", link)) // os.Root follows a relative one, O_NOFOLLOW or not

	var err error
	done := make(chan error, 1)
	go func() {
		file, _, openErr := openEditorTarget(t.Context(), link)
		if openErr == nil {
			_ = file.Close()
		}
		done <- openErr
	}()
	select {
	case err = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the editor waits on the FIFO")
	}
	require.ErrorIs(t, err, ErrEditorFileNotEligible)
}

// openRegularAt decides on the handle of what is at the name, opened without
// following a link: a FIFO, a socket, a folder, a device and a link are all
// refused, with what fstat said of them, and only a regular file is opened.
func TestOpenRegularAtOpensOnlyARegularFile(t *testing.T) {
	dir := t.TempDir()
	createTestFile(t, dir, "notes.txt", []byte("notes"))
	require.NoError(t, unix.Mkfifo(filepath.Join(dir, "fifo"), 0o644))
	socket, err := net.Listen("unix", filepath.Join(dir, "socket"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = socket.Close() })
	require.NoError(t, os.Mkdir(filepath.Join(dir, "folder"), 0o755))
	require.NoError(t, os.Symlink("notes.txt", filepath.Join(dir, "link")))
	folder := openTestDir(t, dir)
	devices := openTestDir(t, "/dev")

	for _, at := range []entry{
		{dir: folder, name: "fifo"}, {dir: folder, name: "socket"}, {dir: folder, name: "folder"},
		{dir: folder, name: "link"}, {dir: devices, name: "null"},
	} {
		file, info, openErr := openRegularAt(at)
		require.ErrorIs(t, openErr, ErrNotRegularFile, at.name)
		require.Nil(t, file, at.name)
		require.NotNil(t, info, at.name)
	}

	file, info, err := openRegularAt(entry{dir: folder, name: "notes.txt"})
	require.NoError(t, err)
	defer file.Close()
	require.True(t, info.Mode().IsRegular())
	content, err := io.ReadAll(file)
	require.NoError(t, err)
	require.Equal(t, "notes", string(content))
}

// The type is decided on the handle of what is at the name when it is
// opened: a file swapped for a link to a FIFO after its folder was reached is
// refused, never followed.
func TestOpenRegularAtDecidesOnWhatItOpened(t *testing.T) {
	dir := t.TempDir()
	createTestFile(t, dir, "notes.txt", []byte("notes"))
	require.NoError(t, unix.Mkfifo(filepath.Join(dir, "fifo"), 0o644))
	folder := openTestDir(t, dir)

	swap := filepath.Join(dir, "swap")
	require.NoError(t, os.Symlink("fifo", swap))
	require.NoError(t, os.Rename(swap, filepath.Join(dir, "notes.txt")))
	_, info, err := openRegularAt(entry{dir: folder, name: "notes.txt"})
	require.ErrorIs(t, err, ErrNotRegularFile)
	require.NotZero(t, info.Mode()&os.ModeSymlink, "the link itself was judged")
}

// The download, the editor and an archive still read a regular file reached
// through links, at the last name and on the way.
func TestOpenRegularForReadFollowsLinksToARegularFile(t *testing.T) {
	dir := t.TempDir()
	createTestFile(t, createTestDir(t, dir, "real"), "notes.txt", []byte("notes"))
	require.NoError(t, os.Symlink(filepath.Join(dir, "real"), filepath.Join(dir, "folder")))
	require.NoError(t, os.Symlink("folder/notes.txt", filepath.Join(dir, "alias")))

	file, info, err := OpenRegularForRead(t.Context(), filepath.Join(dir, "alias"))
	require.NoError(t, err)
	defer file.Close()
	require.Equal(t, int64(5), info.Size())
	content, err := io.ReadAll(file)
	require.NoError(t, err)
	require.Equal(t, "notes", string(content))
}
