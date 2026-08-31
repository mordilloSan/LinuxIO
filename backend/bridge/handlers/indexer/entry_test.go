package indexer

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	indexerapi "github.com/mordilloSan/LinuxIO/backend/indexer/api"
)

func TestEntryFromFileInfo(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".hidden-file")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	entry := EntryFromFileInfo(path+"/../.hidden-file", info, -1)
	if entry.Path != filepath.ToSlash(filepath.Clean(path)) {
		t.Fatalf("path = %q", entry.Path)
	}
	if entry.AbsPath != filepath.Clean(path) || entry.Name != ".hidden-file" || entry.Type != "file" {
		t.Fatalf("entry = %#v", entry)
	}
	if !entry.Hidden || entry.Size != 5 || entry.ModUnix != info.ModTime().Unix() {
		t.Fatalf("entry metadata = %#v", entry)
	}

	dir := filepath.Join(root, "folder")
	if mkdirErr := os.Mkdir(dir, 0o700); mkdirErr != nil {
		t.Fatal(mkdirErr)
	}
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	directory := EntryFromFileInfo(dir, dirInfo, 123)
	if directory.Type != "directory" || directory.Size != 123 || directory.Hidden {
		t.Fatalf("directory entry = %#v", directory)
	}
	if directory.ModUnix == 0 || directory.ModUnix > time.Now().Unix() {
		t.Fatalf("directory mod time = %d", directory.ModUnix)
	}
}

func TestEntryFromFileInfoNil(t *testing.T) {
	if got := EntryFromFileInfo("/missing", nil, 1); got != (indexerapi.EntryRequest{}) {
		t.Fatalf("entry = %#v", got)
	}
}
