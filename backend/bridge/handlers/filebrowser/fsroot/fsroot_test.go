package fsroot

import (
	"io/fs"
	"path/filepath"
	"slices"
	"testing"
)

func TestToRel(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "/", want: "."},
		{in: "", want: "."},
		{in: "/tmp/a", want: "tmp/a"},
		{in: "tmp/a", want: "tmp/a"},
		{in: "/tmp/../etc/passwd", want: "etc/passwd"},
	}

	for _, tc := range tests {
		if got := ToRel(tc.in); got != tc.want {
			t.Fatalf("ToRel(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestWalkDirAndCreateTemp(t *testing.T) {
	tmp := t.TempDir()

	root, err := OpenAt(tmp)
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	defer root.Close()

	if mkdirErr := root.Root.MkdirAll("a/b", 0o755); mkdirErr != nil {
		t.Fatalf("MkdirAll: %v", mkdirErr)
	}
	if writeErr := root.Root.WriteFile("a/b/file.txt", []byte("ok"), 0o644); writeErr != nil {
		t.Fatalf("WriteFile: %v", writeErr)
	}

	var paths []string
	err = root.WalkDir("a", func(path string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Use filepath.ToSlash to keep deterministic assertions on Windows.
		paths = append(paths, filepath.ToSlash(path))
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir: %v", err)
	}

	expected := []string{"a", "a/b", "a/b/file.txt"}
	for _, p := range expected {
		if !slices.Contains(paths, p) {
			t.Fatalf("expected walked path %q not found in %v", p, paths)
		}
	}

	f, relPath, err := root.CreateTemp("a", "tmp-*.txt")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	_ = f.Close()

	if filepath.Dir(filepath.ToSlash(relPath)) != "a" {
		t.Fatalf("CreateTemp dir = %q, want %q", filepath.Dir(filepath.ToSlash(relPath)), "a")
	}
	if _, err := root.Root.Stat(relPath); err != nil {
		t.Fatalf("CreateTemp stat: %v", err)
	}
}
