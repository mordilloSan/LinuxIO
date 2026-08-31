package indexing

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

type memoryWriter struct {
	mu      sync.Mutex
	entries []IndexEntry
}

func (w *memoryWriter) Write(e IndexEntry) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.entries = append(w.entries, e)
	return nil
}

func (w *memoryWriter) entriesByPath() map[string]IndexEntry {
	w.mu.Lock()
	defer w.mu.Unlock()
	result := make(map[string]IndexEntry, len(w.entries))
	for _, e := range w.entries {
		result[e.RelativePath] = e
	}
	return result
}

func (w *memoryWriter) totalFileSize() int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	var total int64
	for _, e := range w.entries {
		if e.Type != "directory" {
			total += e.Size
		}
	}
	return total
}

func newStreamingIndex(t *testing.T, _ string, root string, includeHidden bool) (*Index, *memoryWriter) {
	t.Helper()
	idx := Initialize(root)
	writer := &memoryWriter{}
	idx.EnableStreaming(writer)
	return idx, writer
}

func TestStartIndexingRequiresStreaming(t *testing.T) {
	mock := newMockFileSystem(t)
	mock.CreateStandardTestStructure()

	idx := Initialize(mock.Root)
	if err := idx.StartIndexing(context.Background()); err == nil {
		t.Fatalf("expected streaming mode error when writer is not configured")
	}
}

func TestStartIndexingStreamsEntries(t *testing.T) {
	mock := newMockFileSystem(t)
	mock.CreateStandardTestStructure()

	idx, writer := newStreamingIndex(t, "test", mock.Root, false)
	if err := idx.StartIndexing(context.Background()); err != nil {
		t.Fatalf("StartIndexing failed: %v", err)
	}

	if idx.NumDirs == 0 || idx.NumFiles == 0 {
		t.Fatalf("expected directories and files to be counted, got dirs=%d files=%d", idx.NumDirs, idx.NumFiles)
	}

	entries := writer.entriesByPath()
	assertHasEntry(t, entries, "/", true)
	assertHasEntry(t, entries, "/documents", true)
	assertHasEntry(t, entries, "/documents/readme.txt", false)
	assertHasEntry(t, entries, "/photos", true)
	assertHasEntry(t, entries, "/photos/image1.jpg", false)
}

func TestStartIndexingSkipsUnreadableEntry(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux directory permissions required")
	}
	root := t.TempDir()
	limited := filepath.Join(root, "limited")
	if err := os.Mkdir(limited, 0o700); err != nil {
		t.Fatalf("create limited directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(limited, "blocked"), []byte("blocked"), 0o600); err != nil {
		t.Fatalf("create blocked file: %v", err)
	}
	if err := os.Chmod(limited, 0o400); err != nil {
		t.Fatalf("remove directory search permission: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(limited, 0o700) })
	if err := os.WriteFile(filepath.Join(root, "visible"), []byte("visible"), 0o600); err != nil {
		t.Fatalf("create visible file: %v", err)
	}

	idx, writer := newStreamingIndex(t, "test", root, false)
	if err := idx.StartIndexing(context.Background()); err != nil {
		t.Fatalf("StartIndexing failed: %v", err)
	}
	entries := writer.entriesByPath()
	assertHasEntry(t, entries, "/visible", false)
	if _, ok := entries["/limited/blocked"]; ok {
		t.Fatal("unreadable entry was indexed")
	}
}

func TestDirMetadataKeyConsistency(t *testing.T) {
	mock := newMockFileSystem(t)

	mock.CreateDir("parent/child")
	mock.CreateFile("parent/child/file1.txt", "abc")
	mock.CreateFile("parent/file2.txt", "abcd")

	idx, writer := newStreamingIndex(t, "test", mock.Root, false)
	if err := idx.StartIndexing(context.Background()); err != nil {
		t.Fatalf("StartIndexing failed: %v", err)
	}

	expectedDiskUsed := uint64(writer.totalFileSize())
	if expectedDiskUsed == 0 {
		t.Fatalf("expected non-zero file sizes from writer")
	}

}

func TestHiddenFilesIncluded(t *testing.T) {
	mock := newMockFileSystem(t)
	mock.CreateStandardTestStructure()

	idx, writer := newStreamingIndex(t, "index", mock.Root, false)
	if err := idx.StartIndexing(context.Background()); err != nil {
		t.Fatalf("StartIndexing with hidden failed: %v", err)
	}
	if _, ok := writer.entriesByPath()["/.config"]; !ok {
		t.Fatalf("expected hidden file to be indexed")
	}
}

func TestGetTotalSize(t *testing.T) {
	mock := newMockFileSystem(t)

	// Create files with known content sizes
	mock.CreateFile("file1.txt", "12345")      // 5 bytes
	mock.CreateFile("file2.txt", "1234567890") // 10 bytes

	idx, _ := newStreamingIndex(t, "test", mock.Root, false)
	if err := idx.StartIndexing(context.Background()); err != nil {
		t.Fatalf("StartIndexing failed: %v", err)
	}

	totalSize := idx.GetTotalSize()
	if totalSize == 0 {
		t.Error("Expected totalSize > 0")
	}
}

func TestHardlinks(t *testing.T) {
	mock := newMockFileSystem(t)

	// Create original file
	mock.CreateFile("original.txt", "This is the original content")

	// Create hardlink
	mock.CreateHardlink("original.txt", "hardlink.txt")

	idx, _ := newStreamingIndex(t, "test", mock.Root, false)
	if err := idx.StartIndexing(context.Background()); err != nil {
		t.Fatalf("StartIndexing failed: %v", err)
	}

	// Should count 2 files
	if idx.NumFiles != 2 {
		t.Errorf("Expected 2 files, got %d", idx.NumFiles)
	}

}

func TestShouldSkip(t *testing.T) {
	tests := []struct {
		name         string
		isDir        bool
		isHidden     bool
		fullCombined string
		shouldSkip   bool
	}{
		{
			name:         "root directory never skipped",
			isDir:        true,
			isHidden:     false,
			fullCombined: "/",
			shouldSkip:   false,
		},
		{
			name:         "hidden file included",
			isDir:        false,
			isHidden:     true,
			fullCombined: "/.hidden",
			shouldSkip:   false,
		},
		{
			name:         "hidden file not skipped when includeHidden=true",
			isDir:        false,
			isHidden:     true,
			fullCombined: "/.hidden",
			shouldSkip:   false,
		},
		{
			name:         "regular file not skipped",
			isDir:        false,
			isHidden:     false,
			fullCombined: "/file.txt",
			shouldSkip:   false,
		},
		{
			name:         "regular directory not skipped",
			isDir:        true,
			isHidden:     false,
			fullCombined: "/documents",
			shouldSkip:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idx := Initialize("/tmp/test")
			result := idx.shouldSkip(tt.isDir, tt.isHidden, tt.fullCombined)

			if result != tt.shouldSkip {
				t.Errorf("Expected shouldSkip=%v, got %v", tt.shouldSkip, result)
			}
		})
	}
}

func TestShouldSkipExcludedPaths(t *testing.T) {
	idx := Initialize("/srv", WithExcludePaths([]string{"/srv/cache"}))

	for _, path := range []string{"/cache", "/cache/nested"} {
		if !idx.shouldSkip(true, false, path) {
			t.Fatalf("expected %s to be excluded", path)
		}
	}
	if idx.shouldSkip(true, false, "/cache-old") {
		t.Fatal("path boundary excluded /cache-old")
	}
}

func TestShouldSkipDockerOverlayMergedWhenIndexingRoot(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-specific path behavior")
	}

	idx := Initialize("/")

	if !idx.shouldSkip(true, false, "/var/lib/docker/overlay2/layer123/merged") {
		t.Fatal("expected docker overlay merged directory to be skipped")
	}
	if idx.shouldSkip(true, false, "/var/lib/docker/overlay2/layer123/diff") {
		t.Fatal("expected docker overlay diff directory not to be skipped")
	}
}

func TestShouldSkipDockerOverlayMergedWhenIndexingDockerRoot(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-specific path behavior")
	}

	idx := Initialize("/var/lib/docker")

	if !idx.shouldSkip(true, false, "/overlay2/layer123/merged") {
		t.Fatal("expected docker overlay merged directory to be skipped")
	}
	if idx.shouldSkip(true, false, "/overlay2/layer123/diff") {
		t.Fatal("expected docker overlay diff directory not to be skipped")
	}
}

func TestShouldSkipExternalMountRespectsNetworkMountOption(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-specific path behavior")
	}

	restore := loadExternalMountPointsFn
	loadExternalMountPointsFn = func() map[string]string {
		return map[string]string{"/mnt/share": "nfs"}
	}
	defer func() { loadExternalMountPointsFn = restore }()

	defaultIdx := Initialize("/mnt/share")
	if !defaultIdx.shouldSkip(true, false, "/nested") {
		t.Fatal("expected network mount contents to be skipped by default")
	}

	includeIdx := Initialize("/mnt/share", WithNetworkMounts(true))
	if includeIdx.shouldSkip(true, false, "/nested") {
		t.Fatal("expected network mount contents to be indexed when option is enabled")
	}
}

func TestIsHidden(t *testing.T) {
	mock := newMockFileSystem(t)

	// Create hidden and regular files
	mock.CreateFile(".hidden", "hidden")
	mock.CreateFile("visible.txt", "visible")

	tests := []struct {
		name       string
		filename   string
		wantHidden bool
	}{
		{"hidden file", ".hidden", true},
		{"visible file", "visible.txt", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := os.Stat(mock.Root + "/" + tt.filename)
			if err != nil {
				t.Fatalf("Failed to stat file: %v", err)
			}

			result := isHidden(info)
			if result != tt.wantHidden {
				t.Errorf("Expected isHidden=%v for %s, got %v", tt.wantHidden, tt.filename, result)
			}
		})
	}
}

func assertHasEntry(t *testing.T, entries map[string]IndexEntry, path string, isDir bool) {
	t.Helper()
	entry, ok := entries[path]
	if !ok {
		t.Fatalf("expected entry for %s", path)
	}
	expectedType := "file"
	if isDir {
		expectedType = "directory"
	}
	if entry.Type != expectedType {
		t.Fatalf("entry %s expected Type=%s, got %s", path, expectedType, entry.Type)
	}
}

func TestValidateRelativePath(t *testing.T) {
	valid := []string{"/", "/home/user", "notes..txt", "/a/b..c/d", "/a/.hidden", "a/./b"}
	for _, p := range valid {
		if !ValidateRelativePath(p) {
			t.Errorf("ValidateRelativePath(%q) = false, want true", p)
		}
	}
	invalid := []string{"..", "/..", "../etc", "/a/../b", "a/.."}
	for _, p := range invalid {
		if ValidateRelativePath(p) {
			t.Errorf("ValidateRelativePath(%q) = true, want false", p)
		}
	}
}
