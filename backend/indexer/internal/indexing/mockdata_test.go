package indexing

import (
	"os"
	"path/filepath"
	"testing"
)

type mockFileSystem struct {
	Root string
	t    *testing.T
}

func newMockFileSystem(t *testing.T) *mockFileSystem {
	t.Helper()
	return &mockFileSystem{Root: t.TempDir(), t: t}
}

func (m *mockFileSystem) CreateDir(path string) {
	if err := os.MkdirAll(filepath.Join(m.Root, path), 0o755); err != nil {
		m.t.Fatalf("create directory %s: %v", path, err)
	}
}

func (m *mockFileSystem) CreateFile(path, content string) {
	fullPath := filepath.Join(m.Root, path)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		m.t.Fatalf("create parent directory for %s: %v", path, err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		m.t.Fatalf("create file %s: %v", path, err)
	}
}

func (m *mockFileSystem) CreateHardlink(target, linkPath string) {
	fullLink := filepath.Join(m.Root, linkPath)
	if err := os.MkdirAll(filepath.Dir(fullLink), 0o755); err != nil {
		m.t.Fatalf("create parent directory for hardlink %s: %v", linkPath, err)
	}
	if err := os.Link(filepath.Join(m.Root, target), fullLink); err != nil {
		m.t.Fatalf("create hardlink %s -> %s: %v", linkPath, target, err)
	}
}

func (m *mockFileSystem) CreateStandardTestStructure() {
	m.CreateDir("documents")
	m.CreateFile("documents/readme.txt", "This is a readme file")
	m.CreateFile("documents/notes.txt", "These are notes")
	m.CreateFile("documents/Report.pdf", "PDF content")

	m.CreateDir("photos")
	m.CreateFile("photos/image1.jpg", "JPEG data")
	m.CreateFile("photos/image2.png", "PNG data")

	m.CreateDir("code")
	m.CreateFile("code/main.go", "package main\n")
	m.CreateFile("code/utils.go", "package main\n")

	m.CreateDir("documents/archive")
	m.CreateFile("documents/archive/old.txt", "Old document")
	m.CreateDir("documents/archive/2023")
	m.CreateFile("documents/archive/2023/jan.txt", "January data")
	m.CreateFile("documents/archive/2023/feb.txt", "February data")

	m.CreateFile(".config", "config data")
	m.CreateFile(".hidden_file", "secret content")
	m.CreateFile("documents/.git", "git data")

	m.CreateDir("numbered")
	m.CreateFile("numbered/1.txt", "one")
	m.CreateFile("numbered/2.txt", "two")
	m.CreateFile("numbered/10.txt", "ten")
	m.CreateFile("numbered/20.txt", "twenty")
	m.CreateFile("numbered/100.txt", "hundred")
}
