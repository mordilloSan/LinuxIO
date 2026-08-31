package atomicfile_test

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/atomicfile"
)

func TestWriteFileCreatesFileWithRequestedMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	data := []byte("new configuration\n")

	if err := atomicfile.WriteFile(path, data, 0o640); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read created file: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("content = %q, want %q", got, data)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat created file: %v", err)
	}
	if gotMode, wantMode := info.Mode().Perm(), os.FileMode(0o640); gotMode != wantMode {
		t.Fatalf("mode = %v, want %v", gotMode, wantMode)
	}
	assertNoTemporaryFiles(t, path)
}

func TestWriteFileAtomicallyReplacesAndPreservesMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "indexer.env")
	oldData := []byte("OLD=value\n")
	newData := []byte("NEW=value\n")
	if err := os.WriteFile(path, oldData, 0o600); err != nil {
		t.Fatalf("write original file: %v", err)
	}

	original, err := os.Open(path)
	if err != nil {
		t.Fatalf("open original file: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := original.Close(); closeErr != nil {
			t.Errorf("close original file: %v", closeErr)
		}
	})
	originalInfo, err := original.Stat()
	if err != nil {
		t.Fatalf("stat original file: %v", err)
	}

	if writeErr := atomicfile.WriteFile(path, newData, 0o644); writeErr != nil {
		t.Fatalf("WriteFile: %v", writeErr)
	}

	currentData, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read replacement: %v", err)
	}
	if string(currentData) != string(newData) {
		t.Fatalf("replacement content = %q, want %q", currentData, newData)
	}

	currentInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat replacement: %v", err)
	}
	if gotMode, wantMode := currentInfo.Mode().Perm(), os.FileMode(0o600); gotMode != wantMode {
		t.Fatalf("replacement mode = %v, want preserved mode %v", gotMode, wantMode)
	}
	if os.SameFile(originalInfo, currentInfo) {
		t.Fatal("replacement reused the original file instead of renaming a new file into place")
	}

	openHandleData, err := io.ReadAll(original)
	if err != nil {
		t.Fatalf("read original open handle: %v", err)
	}
	if string(openHandleData) != string(oldData) {
		t.Fatalf("original open handle reads %q, want %q", openHandleData, oldData)
	}
	assertNoTemporaryFiles(t, path)
}

func TestWriteFileFollowsSymlinkAndPreservesTargetMode(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.env")
	link := filepath.Join(dir, "indexer.env")
	if err := os.WriteFile(target, []byte("OLD=value\n"), 0o600); err != nil {
		t.Fatalf("write symlink target: %v", err)
	}
	if err := os.Symlink(filepath.Base(target), link); err != nil {
		t.Skipf("create symlink: %v", err)
	}

	if err := atomicfile.WriteFile(link, []byte("NEW=value\n"), 0o644); err != nil {
		t.Fatalf("WriteFile through symlink: %v", err)
	}

	linkInfo, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("lstat symlink: %v", err)
	}
	if linkInfo.Mode()&os.ModeSymlink == 0 {
		t.Fatal("WriteFile replaced the symlink instead of its target")
	}
	linkTarget, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("read symlink: %v", err)
	}
	if linkTarget != filepath.Base(target) {
		t.Fatalf("symlink target = %q, want %q", linkTarget, filepath.Base(target))
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read symlink target: %v", err)
	}
	if string(got) != "NEW=value\n" {
		t.Fatalf("target content = %q, want %q", got, "NEW=value\n")
	}
	targetInfo, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat symlink target: %v", err)
	}
	if gotMode, wantMode := targetInfo.Mode().Perm(), os.FileMode(0o600); gotMode != wantMode {
		t.Fatalf("target mode = %v, want preserved mode %v", gotMode, wantMode)
	}
	assertNoTemporaryFiles(t, target)
}

func TestWriteFileLeavesDanglingSymlinkUntouched(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "indexer.env")
	missingTarget := "missing.env"
	if err := os.Symlink(missingTarget, link); err != nil {
		t.Skipf("create symlink: %v", err)
	}

	err := atomicfile.WriteFile(link, []byte("data"), 0o600)
	if err == nil {
		t.Fatal("WriteFile succeeded through a dangling symlink")
	}
	if !strings.Contains(err.Error(), "resolve symlink") {
		t.Fatalf("error = %q, want symlink resolution context", err)
	}

	gotTarget, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("read dangling symlink after failure: %v", err)
	}
	if gotTarget != missingTarget {
		t.Fatalf("symlink target = %q, want %q", gotTarget, missingTarget)
	}
	if _, err := os.Stat(filepath.Join(dir, missingTarget)); !os.IsNotExist(err) {
		t.Fatalf("missing target was unexpectedly created: %v", err)
	}
	assertNoTemporaryFiles(t, link)
}

func TestWriteFileCleansTemporaryFileWhenRenameFails(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "existing-directory")
	if err := os.Mkdir(destination, 0o755); err != nil {
		t.Fatalf("create destination directory: %v", err)
	}

	err := atomicfile.WriteFile(destination, []byte("data"), 0o600)
	if err == nil {
		t.Fatal("WriteFile replaced a directory with a file")
	}
	if !strings.Contains(err.Error(), "rename temp file") {
		t.Fatalf("error = %q, want rename context", err)
	}

	info, statErr := os.Stat(destination)
	if statErr != nil {
		t.Fatalf("stat destination after failure: %v", statErr)
	}
	if !info.IsDir() {
		t.Fatal("destination directory was damaged after failed replacement")
	}
	assertNoTemporaryFiles(t, destination)
}

func assertNoTemporaryFiles(t *testing.T, destination string) {
	t.Helper()
	pattern := filepath.Join(filepath.Dir(destination), "."+filepath.Base(destination)+".tmp-*")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatalf("glob temporary files: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary files left behind: %v", matches)
	}
}
