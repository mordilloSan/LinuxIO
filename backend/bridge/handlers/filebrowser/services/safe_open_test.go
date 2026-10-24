package services

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
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
			_, err := ReadEditorFile(context.Background(), test.path)
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
	first, err := ReadEditorFile(context.Background(), path)
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
	_, err := ReadEditorFile(context.Background(), path)
	require.ErrorIs(t, err, ErrEditorFileContainsNUL)
}

func TestReadEditorFileAcceptsUTF8AcrossFormerSampleBoundary(t *testing.T) {
	tmpDir := t.TempDir()
	content := append(bytes.Repeat([]byte("a"), 4095), []byte("€")...)
	path := createTestFile(t, tmpDir, "utf8", content)

	read, err := ReadEditorFile(context.Background(), path)
	require.NoError(t, err)
	assert.Equal(t, content, read.Content)
}

func TestReadEditorFileReportsAtomicSaveCapability(t *testing.T) {
	tmpDir := t.TempDir()
	path := createTestFile(t, tmpDir, "note", []byte("text"))
	read, err := ReadEditorFile(context.Background(), path)
	require.NoError(t, err)
	assert.True(t, read.CanSave)

	if os.Geteuid() == 0 {
		t.Skip("root can replace files in read-only directories")
	}
	require.NoError(t, os.Chmod(tmpDir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(tmpDir, 0o755) })
	read, err = ReadEditorFile(context.Background(), path)
	require.NoError(t, err)
	assert.False(t, read.CanSave)
}

func TestReadEditorFileCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := ReadEditorFile(ctx, filepath.Join(t.TempDir(), "missing"))
	assert.ErrorIs(t, err, context.Canceled)
}

func TestReadEditorFileIgnoresUnrelatedSiblings(t *testing.T) {
	dir := t.TempDir()
	for i := range 300 {
		createTestFile(t, dir, fmt.Sprintf("sibling-%03d", i), []byte("unused"))
	}
	target := createTestFile(t, dir, "target", []byte("requested"))
	read, err := ReadEditorFile(context.Background(), target)
	require.NoError(t, err)
	assert.Equal(t, []byte("requested"), read.Content)
}
