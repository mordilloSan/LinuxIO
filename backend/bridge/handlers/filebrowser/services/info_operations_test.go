package services

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/iteminfo"
)

func TestListDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	createTestDir(t, tmpDir, "folder")
	createTestFile(t, tmpDir, "plain", []byte("text"))
	createTestFile(t, tmpDir, "large", make([]byte, MaxTextFileBytes))
	require.NoError(t, os.Symlink(filepath.Join(tmpDir, "plain"), filepath.Join(tmpDir, "link")))
	require.NoError(t, os.Symlink(filepath.Join(tmpDir, "folder"), filepath.Join(tmpDir, "folder-link")))

	listing, err := ListDirectory(context.Background(), tmpDir)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"folder", "folder-link"}, testItemNames(listing.Folders))
	assert.ElementsMatch(t, []string{"plain", "large", "link"}, testItemNames(listing.Files))

	items := make(map[string]iteminfo.ItemInfo, len(listing.Files))
	for _, item := range listing.Files {
		items[item.Name] = item
	}
	assert.Equal(t, int64(4), items["plain"].Size)
	assert.True(t, items["plain"].IsRegularFile)
	assert.True(t, items["plain"].CanOpenAsText)
	assert.False(t, items["large"].CanOpenAsText)
	assert.True(t, items["link"].Symlink)
	assert.True(t, items["link"].IsRegularFile)
	assert.True(t, items["link"].CanOpenAsText)
}

func TestListDirectoryTreatsBundleNamedDirectoriesAsFolders(t *testing.T) {
	tmpDir := t.TempDir()
	createTestDir(t, tmpDir, ".bundle")
	createTestDir(t, tmpDir, "Demo.app")

	listing, err := ListDirectory(context.Background(), tmpDir)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{".bundle", "Demo.app"}, testItemNames(listing.Folders))
	assert.Empty(t, listing.Files)

	children, err := ListDirectoryChildren(context.Background(), tmpDir, true)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{".bundle", "Demo.app"}, children.Folders)
	assert.Empty(t, children.Files)
}

func TestListDirectoryDoesNotReadContent(t *testing.T) {
	tmpDir := t.TempDir()
	createTestFile(t, tmpDir, "nul", []byte("text\x00binary"))

	listing, err := ListDirectory(context.Background(), tmpDir)
	require.NoError(t, err)
	require.Len(t, listing.Files, 1)
	assert.True(t, listing.Files[0].CanOpenAsText, "content validation belongs to editor open")
}

func TestListDirectoryChildren(t *testing.T) {
	tmpDir := t.TempDir()
	createTestDir(t, tmpDir, "folder")
	createTestFile(t, tmpDir, "plain", []byte("text"))
	require.NoError(t, os.Symlink(filepath.Join(tmpDir, "folder"), filepath.Join(tmpDir, "folder-link")))
	require.NoError(t, os.Symlink(filepath.Join(tmpDir, "plain"), filepath.Join(tmpDir, "file-link")))

	folders, err := ListDirectoryChildren(context.Background(), tmpDir, false)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"folder", "folder-link"}, folders.Folders)
	assert.Empty(t, folders.Files)

	all, err := ListDirectoryChildren(context.Background(), tmpDir, true)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"folder", "folder-link"}, all.Folders)
	assert.ElementsMatch(t, []string{"plain", "file-link"}, all.Files)
}

func TestListDirectoryCancellation(t *testing.T) {
	tmpDir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := ListDirectory(ctx, tmpDir)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestListDirectoryCancellationAfterFirstBatch(t *testing.T) {
	dir := t.TempDir()
	for i := range directoryReadBatchSize * 2 {
		createTestFile(t, dir, fmt.Sprintf("file-%03d", i), []byte("x"))
	}
	ctx := &cancelAfterErrChecks{Context: context.Background(), cancelAt: 133}
	_, err := ListDirectory(ctx, dir)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestListDirectoryRejectsFile(t *testing.T) {
	path := createTestFile(t, t.TempDir(), "file", []byte("x"))
	_, err := ListDirectory(context.Background(), path)
	assert.ErrorIs(t, err, os.ErrInvalid)
}

func testItemNames(items []iteminfo.ItemInfo) []string {
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.Name)
	}
	return names
}

type cancelAfterErrChecks struct {
	context.Context
	calls    int
	cancelAt int
}

func (c *cancelAfterErrChecks) Err() error {
	c.calls++
	if c.calls >= c.cancelAt {
		return context.Canceled
	}
	return c.Context.Err()
}
