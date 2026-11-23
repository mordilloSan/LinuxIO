package indexing

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func writeFolderFile(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestReadFolderListsOneLevelOnly(t *testing.T) {
	root := t.TempDir()
	writeFolderFile(t, filepath.Join(root, "a.txt"), 4096)
	writeFolderFile(t, filepath.Join(root, "sub", "deep.txt"), 4096)
	if err := os.Symlink("/etc", filepath.Join(root, "link")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	contents, err := Initialize("/").ReadFolder(context.Background(), root, 100)
	if err != nil {
		t.Fatalf("ReadFolder: %v", err)
	}

	var files []string
	for _, file := range contents.Files {
		if file.Type != "file" {
			t.Fatalf("%s has type %q, want file", file.RelativePath, file.Type)
		}
		files = append(files, file.RelativePath)
	}
	slices.Sort(files)
	// A link is an entry of its own, never the folder it points to.
	if want := []string{filepath.Join(root, "a.txt"), filepath.Join(root, "link")}; !slices.Equal(files, want) {
		t.Fatalf("files = %v, want %v", files, want)
	}
	if want := []string{filepath.Join(root, "sub")}; !slices.Equal(contents.Folders, want) {
		t.Fatalf("folders = %v, want %v", contents.Folders, want)
	}
	for _, file := range contents.Files {
		if file.RelativePath == filepath.Join(root, "a.txt") && (file.Size != 4096 || file.Inode == 0 || file.Nlink != 1) {
			t.Fatalf("a.txt read as %+v", file)
		}
	}
}

func TestReadFolderGivesUpOnAFolderWithMoreEntriesThanTheLimit(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a", "b", "c", "d"} {
		writeFolderFile(t, filepath.Join(root, name), 0)
	}
	previous := readDirChunk
	readDirChunk = 2
	t.Cleanup(func() { readDirChunk = previous })

	if _, err := Initialize("/").ReadFolder(context.Background(), root, 3); !errors.Is(err, ErrFolderTooLarge) {
		t.Fatalf("ReadFolder error = %v, want ErrFolderTooLarge", err)
	}
	contents, err := Initialize("/").ReadFolder(context.Background(), root, 4)
	if err != nil || len(contents.Files) != 4 {
		t.Fatalf("ReadFolder at the limit = %d files, %v; want 4", len(contents.Files), err)
	}
}

func TestReadFolderLeavesOutWhatTheIndexLeavesOut(t *testing.T) {
	root := t.TempDir()
	writeFolderFile(t, filepath.Join(root, "kept", "a.txt"), 0)
	writeFolderFile(t, filepath.Join(root, "left-out", "b.txt"), 0)
	index := Initialize("/", WithExcludePaths([]string{filepath.Join(root, "left-out")}))

	contents, err := index.ReadFolder(context.Background(), root, 100)
	if err != nil {
		t.Fatalf("ReadFolder: %v", err)
	}
	if want := []string{filepath.Join(root, "kept")}; !slices.Equal(contents.Folders, want) {
		t.Fatalf("folders = %v, want %v", contents.Folders, want)
	}
	if _, err := index.ReadFolder(context.Background(), filepath.Join(root, "left-out"), 100); !errors.Is(err, ErrNotIndexed) {
		t.Fatalf("ReadFolder of an excluded folder = %v, want ErrNotIndexed", err)
	}
}

func TestReadFolderDoesNotGoThroughASymlink(t *testing.T) {
	root := t.TempDir()
	writeFolderFile(t, filepath.Join(root, "real", "a.txt"), 0)
	if err := os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "link")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if _, err := Initialize("/").ReadFolder(context.Background(), filepath.Join(root, "link"), 100); err == nil {
		t.Fatal("ReadFolder read a folder through a symlink")
	}
}

func TestAFolderEntryCarriesItsDeviceAndInode(t *testing.T) {
	root := t.TempDir()
	writeFolderFile(t, filepath.Join(root, "sub", "a.txt"), 0)
	info, err := os.Lstat(filepath.Join(root, "sub"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	want := EntryFromFileInfo(filepath.Join(root, "sub"), info)

	idx := Initialize("/")
	writer := &memoryWriter{}
	idx.EnableStreaming(writer)
	if err := idx.StartIndexingFromPath(context.Background(), root); err != nil {
		t.Fatalf("scan: %v", err)
	}

	got, ok := writer.entriesByPath()[filepath.Join(root, "sub")]
	if !ok {
		t.Fatal("the subfolder was not scanned")
	}
	// A move is recognised by them: the folder at the new path is the one
	// the index holds at the old path.
	if got.Device == 0 || got.Device != want.Device || got.Inode != want.Inode {
		t.Fatalf("folder scanned with device %d inode %d, want %d and %d", got.Device, got.Inode, want.Device, want.Inode)
	}
}

func TestLstatDoesNotGoThroughASymlinkedFolder(t *testing.T) {
	root := t.TempDir()
	writeFolderFile(t, filepath.Join(root, "real", "a.txt"), 0)
	if err := os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "link")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if info, err := Lstat(filepath.Join(root, "real", "a.txt")); err != nil || info.IsDir() || info.Name() != "a.txt" {
		t.Fatalf("Lstat of a file = %v, %v", info, err)
	}
	// The link itself is an entry; what is behind it is not reached.
	if info, err := Lstat(filepath.Join(root, "link")); err != nil || info.IsDir() {
		t.Fatalf("Lstat of a link = %v, %v; want the link, not the folder", info, err)
	}
	if _, err := Lstat(filepath.Join(root, "link", "a.txt")); err == nil {
		t.Fatal("Lstat looked at a file through a symlinked folder")
	}
	if _, err := Lstat(filepath.Join(root, "missing")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Lstat of a missing path = %v, want not exist", err)
	}
	if info, err := Lstat("/"); err != nil || !info.IsDir() {
		t.Fatalf("Lstat of the root = %v, %v", info, err)
	}
}
