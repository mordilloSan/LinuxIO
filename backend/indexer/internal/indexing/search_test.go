package indexing

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func containsReport(name string) bool { return strings.Contains(name, "report") }

func foundPaths(search TreeSearch) []string {
	paths := make([]string, 0, len(search.Found))
	for _, entry := range search.Found {
		paths = append(paths, entry.RelativePath)
	}
	slices.Sort(paths)
	return paths
}

func searchTree(t *testing.T, idx *Index, folder string) TreeSearch {
	t.Helper()
	search, err := idx.SearchTree(context.Background(), folder, containsReport, time.Now().Add(time.Minute), 100)
	if err != nil {
		t.Fatalf("SearchTree: %v", err)
	}
	return search
}

func TestASearchOfATreeFindsMatchesAtEveryDepth(t *testing.T) {
	root := t.TempDir()
	writeFolderFile(t, filepath.Join(root, "report-top.txt"), 4096)
	writeFolderFile(t, filepath.Join(root, "other.txt"), 0)
	writeFolderFile(t, filepath.Join(root, "a", "b", "c", "deep-report.txt"), 0)
	if err := os.MkdirAll(filepath.Join(root, "a", "reports"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	search := searchTree(t, Initialize("/"), root)

	if !search.Complete {
		t.Fatal("a search of a small tree with time to spare was cut short")
	}
	want := []string{
		filepath.Join(root, "a", "b", "c", "deep-report.txt"),
		filepath.Join(root, "a", "reports"),
		filepath.Join(root, "report-top.txt"),
	}
	if got := foundPaths(search); !slices.Equal(got, want) {
		t.Fatalf("found %v, want %v", got, want)
	}
	for _, entry := range search.Found {
		switch filepath.Base(entry.RelativePath) {
		case "reports":
			if entry.Type != "directory" {
				t.Fatalf("a folder was found as %q", entry.Type)
			}
		case "report-top.txt":
			// The size the index gives a file: its allocated blocks.
			if entry.Type != "file" || entry.Size != 4096 || entry.Inode == 0 {
				t.Fatalf("report-top.txt found as %+v", entry)
			}
		}
	}
}

func TestASearchOfATreeDoesNotFollowASymlinkedFolder(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeFolderFile(t, filepath.Join(outside, "secret-report.txt"), 0)
	if err := os.Symlink(outside, filepath.Join(root, "report-link")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	search := searchTree(t, Initialize("/"), root)

	// The link is an entry whose name matches, never the folder it points to.
	if want := []string{filepath.Join(root, "report-link")}; !slices.Equal(foundPaths(search), want) {
		t.Fatalf("found %v, want %v", foundPaths(search), want)
	}
	if search.Found[0].Type != "file" {
		t.Fatalf("the link was found as %q", search.Found[0].Type)
	}
}

func TestASearchOfATreeDoesNotStartThroughASymlinkedFolder(t *testing.T) {
	root := t.TempDir()
	writeFolderFile(t, filepath.Join(root, "real", "report.txt"), 0)
	if err := os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "link")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	_, err := Initialize("/").SearchTree(context.Background(), filepath.Join(root, "link"), containsReport, time.Now().Add(time.Minute), 100)
	if !NotThere(err) {
		t.Fatalf("a search below a symlinked folder answered %v, want a path that is not there", err)
	}
}

func TestASearchOfATreeLeavesOutWhatTheIndexLeavesOut(t *testing.T) {
	root := t.TempDir()
	writeFolderFile(t, filepath.Join(root, "kept", "report-a.txt"), 0)
	writeFolderFile(t, filepath.Join(root, "left-out", "report-b.txt"), 0)
	left := filepath.Join(root, "left-out")
	idx := Initialize("/", WithExcludePaths([]string{left}))

	if want := []string{filepath.Join(root, "kept", "report-a.txt")}; !slices.Equal(foundPaths(searchTree(t, idx, root)), want) {
		t.Fatalf("found %v, want %v", foundPaths(searchTree(t, idx, root)), want)
	}
	if _, err := idx.SearchTree(context.Background(), left, containsReport, time.Now().Add(time.Minute), 100); !errors.Is(err, ErrNotIndexed) {
		t.Fatalf("a search of an excluded folder answered %v, want ErrNotIndexed", err)
	}
}

func TestASearchOfATreeStopsAtItsDeadline(t *testing.T) {
	root := t.TempDir()
	writeFolderFile(t, filepath.Join(root, "report.txt"), 0)
	writeFolderFile(t, filepath.Join(root, "sub", "report.txt"), 0)

	search, err := Initialize("/").SearchTree(context.Background(), root, containsReport, time.Now().Add(-time.Second), 100)
	if err != nil {
		t.Fatalf("SearchTree: %v", err)
	}
	if search.Complete {
		t.Fatal("a search past its deadline says it is complete")
	}
}

func TestASearchOfATreeStopsWhenItsRequestEnds(t *testing.T) {
	root := t.TempDir()
	writeFolderFile(t, filepath.Join(root, "report.txt"), 0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := Initialize("/").SearchTree(ctx, root, containsReport, time.Now().Add(time.Minute), 100); !errors.Is(err, context.Canceled) {
		t.Fatalf("a search whose request ended answered %v, want context.Canceled", err)
	}
}

func TestASearchOfATreeKeepsTheNewestMatches(t *testing.T) {
	old := readDirChunk
	readDirChunk = 2
	t.Cleanup(func() { readDirChunk = old })
	root := t.TempDir()
	base := time.Now().Add(-time.Hour)
	for i, name := range []string{"report-1", "report-2", "report-3", "report-4", "report-5", "report-6"} {
		path := filepath.Join(root, name)
		writeFolderFile(t, path, 0)
		at := base.Add(time.Duration(i) * time.Minute)
		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatalf("chtimes: %v", err)
		}
	}

	search, err := Initialize("/").SearchTree(context.Background(), root, containsReport, time.Now().Add(time.Minute), 2)
	if err != nil {
		t.Fatalf("SearchTree: %v", err)
	}
	if want := []string{filepath.Join(root, "report-5"), filepath.Join(root, "report-6")}; !slices.Equal(foundPaths(search), want) || !search.Complete {
		t.Fatalf("found %v (complete %v), want %v", foundPaths(search), search.Complete, want)
	}
}

// What a scan would drop for now is not taken for gone: the index may hold
// it from when it could be read, and the answer falls back to the index.
func TestASearchOfATreeThatCouldNotReadAFolderIsNotComplete(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a folder whatever its mode")
	}
	root := t.TempDir()
	writeFolderFile(t, filepath.Join(root, "report.txt"), 0)
	closed := filepath.Join(root, "closed")
	writeFolderFile(t, filepath.Join(closed, "report-inside.txt"), 0)
	if err := os.Chmod(closed, 0); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(closed, 0o755) })

	search := searchTree(t, Initialize("/"), root)

	if search.Complete {
		t.Fatal("a search that could not read a folder says it is complete")
	}
	if want := []string{filepath.Join(root, "report.txt")}; !slices.Equal(foundPaths(search), want) {
		t.Fatalf("found %v, want %v", foundPaths(search), want)
	}
}

func TestASearchOfATreeLeavesTheFolderItselfOut(t *testing.T) {
	root := filepath.Join(t.TempDir(), "report-folder")
	writeFolderFile(t, filepath.Join(root, "report.txt"), 0)

	search := searchTree(t, Initialize("/"), root)

	if want := []string{filepath.Join(root, "report.txt")}; !slices.Equal(foundPaths(search), want) {
		t.Fatalf("found %v, want %v", foundPaths(search), want)
	}
}

// An entry root may not look at, such as a user's FUSE mount without
// allow_other, makes the search incomplete only where a scan would have
// indexed it.
func TestAnEntryThatCannotBeLookedAtCountsOnlyWhereTheIndexWouldHoldIt(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root looks at an entry whatever its folder's mode")
	}
	root := t.TempDir()
	locked := filepath.Join(root, "locked")
	writeFolderFile(t, filepath.Join(locked, "mount"), 0)
	// Readable, not searchable: its names are listed, none can be looked at.
	if err := os.Chmod(locked, 0o444); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	if searchTree(t, Initialize("/"), root).Complete {
		t.Fatal("a search that could not look at an indexed entry says it is complete")
	}
	left := Initialize("/", WithExcludePaths([]string{filepath.Join(locked, "mount")}))
	if !searchTree(t, left, root).Complete {
		t.Fatal("an entry the index leaves out made the search incomplete")
	}
}

func TestASearchOfATreeStopsBelowItsDepthLimit(t *testing.T) {
	old := maxSearchDepth
	maxSearchDepth = 2
	t.Cleanup(func() { maxSearchDepth = old })
	root := t.TempDir()
	writeFolderFile(t, filepath.Join(root, "a", "b", "report-b.txt"), 0)
	writeFolderFile(t, filepath.Join(root, "a", "b", "c", "report-c.txt"), 0)

	search := searchTree(t, Initialize("/"), root)

	if search.Complete {
		t.Fatal("a search that stopped at its depth limit says it is complete")
	}
	if want := []string{filepath.Join(root, "a", "b", "report-b.txt")}; !slices.Equal(foundPaths(search), want) {
		t.Fatalf("found %v, want %v", foundPaths(search), want)
	}

	maxSearchDepth = 3
	if search := searchTree(t, Initialize("/"), root); !search.Complete || len(search.Found) != 2 {
		t.Fatalf("a search within the limit found %v (complete %v)", foundPaths(search), search.Complete)
	}
}

func TestLeavesOutIsWhatTheIndexLeavesOut(t *testing.T) {
	idx := Initialize("/", WithExcludePaths([]string{"/srv/cache"}))
	for path, want := range map[string]bool{"/srv/cache": true, "/srv/cache/a.txt": true, "/srv/cached": false, "/srv": false, "/": false} {
		if got := idx.LeavesOut(path); got != want {
			t.Errorf("LeavesOut(%q) = %v, want %v", path, got, want)
		}
	}
}
