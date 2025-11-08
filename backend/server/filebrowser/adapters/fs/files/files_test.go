package files

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/server/filebrowser/services"
)

func TestDeleteFilesRemovesFilesAndDirectories(t *testing.T) {
	fs := services.NewFileService()
	t.Run("file", func(t *testing.T) {
		filePath := filepath.Join(t.TempDir(), "to-delete.txt")
		writeFile(t, filePath, "orphan")

		if err := fs.DeleteFiles(filePath); err != nil {
			t.Fatalf("DeleteFiles returned error for file: %v", err)
		}
		if _, err := os.Stat(filePath); !os.IsNotExist(err) {
			t.Fatalf("expected file to be deleted, stat err=%v", err)
		}
	})

	t.Run("directory", func(t *testing.T) {
		root := t.TempDir()
		dirPath := filepath.Join(root, "subdir")
		writeFile(t, filepath.Join(dirPath, "nested", "file.txt"), "nested")

		if err := fs.DeleteFiles(dirPath); err != nil {
			t.Fatalf("DeleteFiles returned error for directory: %v", err)
		}
		if _, err := os.Stat(dirPath); !os.IsNotExist(err) {
			t.Fatalf("expected directory to be deleted, stat err=%v", err)
		}
	})
}

func TestCopyResource(t *testing.T) {
	mcs := services.NewMoveCopyService()
	t.Run("copies file", func(t *testing.T) {
		root := t.TempDir()
		src := filepath.Join(root, "src", "file.txt")
		dstDir := filepath.Join(root, "copies")
		dst := filepath.Join(dstDir, "file-copy.txt")
		writeFile(t, src, "file data")
		must(os.MkdirAll(dstDir, 0o775))

		if err := mcs.CopyResource(false, src, dst); err != nil {
			t.Fatalf("CopyResource file: %v", err)
		}
		assertFileContent(t, dst, "file data")
		assertFileContent(t, src, "file data") // original untouched
	})

	t.Run("copies directory tree", func(t *testing.T) {
		root := t.TempDir()
		srcDir := filepath.Join(root, "srcdir")
		srcNested := filepath.Join(srcDir, "nested", "file.txt")
		writeFile(t, srcNested, "nested data")
		dstDir := filepath.Join(root, "dstdir")

		if err := mcs.CopyResource(true, srcDir, dstDir); err != nil {
			t.Fatalf("CopyResource directory: %v", err)
		}
		assertFileContent(t, filepath.Join(dstDir, "nested", "file.txt"), "nested data")
		assertFileContent(t, filepath.Join(srcDir, "nested", "file.txt"), "nested data")
	})

	t.Run("rejects nested destination for directories", func(t *testing.T) {
		root := t.TempDir()
		srcDir := filepath.Join(root, "tree")
		subDir := filepath.Join(srcDir, "child")
		must(os.MkdirAll(subDir, 0o775))

		err := mcs.CopyResource(true, srcDir, filepath.Join(subDir, "copy"))
		if err == nil || !strings.Contains(err.Error(), "cannot move directory") {
			t.Fatalf("expected nested destination error, got %v", err)
		}
	})
}

func TestMoveResource(t *testing.T) {
	mcs := services.NewMoveCopyService()
	t.Run("renames file", func(t *testing.T) {
		root := t.TempDir()
		src := filepath.Join(root, "file.txt")
		dst := filepath.Join(root, "renamed.txt")
		writeFile(t, src, "rename me")

		if err := mcs.MoveResource(false, src, dst); err != nil {
			t.Fatalf("MoveResource file rename: %v", err)
		}
		if _, err := os.Stat(src); !os.IsNotExist(err) {
			t.Fatalf("expected source file removed, stat err=%v", err)
		}
		assertFileContent(t, dst, "rename me")
	})

	t.Run("moves directory tree", func(t *testing.T) {
		root := t.TempDir()
		srcDir := filepath.Join(root, "srcdir")
		writeFile(t, filepath.Join(srcDir, "nested", "file.txt"), "dir data")
		dstDir := filepath.Join(root, "moved")

		if err := mcs.MoveResource(true, srcDir, dstDir); err != nil {
			t.Fatalf("MoveResource directory: %v", err)
		}
		if _, err := os.Stat(srcDir); !os.IsNotExist(err) {
			t.Fatalf("expected source directory removed, stat err=%v", err)
		}
		assertFileContent(t, filepath.Join(dstDir, "nested", "file.txt"), "dir data")
	})

	t.Run("rejects moving directory into itself", func(t *testing.T) {
		root := t.TempDir()
		srcDir := filepath.Join(root, "tree")
		subDir := filepath.Join(srcDir, "child")
		must(os.MkdirAll(subDir, 0o775))

		err := mcs.MoveResource(true, srcDir, filepath.Join(subDir, "move"))
		if err == nil || !strings.Contains(err.Error(), "cannot move directory") {
			t.Fatalf("expected nested destination error, got %v", err)
		}
	})

	t.Run("fails when destination parent missing", func(t *testing.T) {
		root := t.TempDir()
		src := filepath.Join(root, "file.txt")
		writeFile(t, src, "data")
		dst := filepath.Join(root, "missing", "file.txt")

		err := mcs.MoveResource(false, src, dst)
		if err == nil || !strings.Contains(err.Error(), "destination directory does not exist") {
			t.Fatalf("expected missing parent error, got %v", err)
		}
		// source should still exist
		assertFileContent(t, src, "data")
	})
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	must(os.MkdirAll(filepath.Dir(path), 0o775))
	must(os.WriteFile(path, []byte(content), 0o664))
}

func assertFileContent(t *testing.T, path, expected string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed reading %s: %v", path, err)
	}
	if string(data) != expected {
		t.Fatalf("unexpected content in %s: got %q want %q", path, data, expected)
	}
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
