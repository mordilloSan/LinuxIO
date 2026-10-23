package filebrowser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
)

func TestGenerateUniquePathSkipsDanglingSymlink(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "report.txt")
	if err := os.WriteFile(source, []byte("report"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	if err := os.Symlink("missing.txt", filepath.Join(dir, "report (copy).txt")); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	root, err := fsroot.Open()
	if err != nil {
		t.Fatalf("open root: %v", err)
	}
	defer root.Close()

	got := generateUniquePath(source, false, root)
	want := filepath.Join(dir, "report (copy 2).txt")
	if got != want {
		t.Fatalf("generateUniquePath = %q, want %q", got, want)
	}
}
