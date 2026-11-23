package indexing

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestEntryFromFileInfoUsesAllocatedSize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("lstat file: %v", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatal("file stat is not syscall.Stat_t")
	}

	entry := EntryFromFileInfo(path, info)
	wantSize := stat.Blocks * 512
	if entry.Size != wantSize || entry.SizeContribution != 0 {
		t.Fatalf("entry size = %d, contribution = %d; want size %d and contribution 0", entry.Size, entry.SizeContribution, wantSize)
	}
	if entry.Device != stat.Dev || entry.Inode != stat.Ino {
		t.Fatalf("entry identity = (%d,%d), want (%d,%d)", entry.Device, entry.Inode, stat.Dev, stat.Ino)
	}
}

func TestSameFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "file")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("lstat file: %v", err)
	}
	entry := EntryFromFileInfo(path, info)
	device, inode := int64(entry.Device), int64(entry.Inode)

	if !SameFile(path, device, inode) {
		t.Fatal("SameFile = false for the file's own identity")
	}
	if SameFile(path, device, inode+1) {
		t.Fatal("SameFile = true for a different inode")
	}
	if SameFile(root, device, inode) {
		t.Fatal("SameFile = true for a directory")
	}
	if SameFile(filepath.Join(root, "missing"), device, inode) {
		t.Fatal("SameFile = true for a missing path")
	}
}
