package daemon

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func postRefresh(d *daemon, path string) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	d.handleRefresh(rr, httptest.NewRequest(http.MethodPost, "/refresh?path="+url.QueryEscape(path), nil))
	return rr
}

// newDaemonWithIndexedFolder returns a daemon whose index holds a folder with
// one file and one subfolder.
func newDaemonWithIndexedFolder(t *testing.T) (*daemon, func() []string, string) {
	t.Helper()
	d, db := newIndexedDaemon(t)
	folder := filepath.Join(t.TempDir(), "folder")
	writeFile(t, filepath.Join(folder, "a.txt"))
	writeFile(t, filepath.Join(folder, "sub", "b.txt"))
	postReindex(d, folder)
	paths := func() []string { return indexedPaths(t, db, folder) }
	waitUntil(t, "the folder is indexed", func() bool { return len(paths()) == 4 && !d.busy() })
	return d, paths, folder
}

// forgetRefresh makes the daemon take folder for one it has not refreshed
// for longer than it remembers.
func forgetRefresh(d *daemon, folder string) {
	d.refreshes.mu.Lock()
	defer d.refreshes.mu.Unlock()
	d.refreshes.at[folder] = time.Now().Add(-2 * refreshWindow)
}

func TestOpeningAFolderBringsItsEntriesUpToDate(t *testing.T) {
	d, paths, folder := newDaemonWithIndexedFolder(t)
	// Changed without LinuxIO: one file added, one removed.
	writeFile(t, filepath.Join(folder, "new.txt"))
	if err := os.Remove(filepath.Join(folder, "a.txt")); err != nil {
		t.Fatalf("remove: %v", err)
	}

	if rr := postRefresh(d, folder); rr.Code != http.StatusAccepted {
		t.Fatalf("refresh answered %d %q, want 202", rr.Code, rr.Body.String())
	}

	want := []string{folder, filepath.Join(folder, "new.txt"), filepath.Join(folder, "sub"), filepath.Join(folder, "sub", "b.txt")}
	waitUntil(t, "the folder is refreshed", func() bool { return slices.Equal(paths(), want) })
}

func TestARefreshReindexesASubfolderItFindsNew(t *testing.T) {
	d, paths, folder := newDaemonWithIndexedFolder(t)
	writeFile(t, filepath.Join(folder, "fresh", "deep", "c.txt"))

	postRefresh(d, folder)

	waitUntil(t, "the new subfolder is indexed", func() bool {
		return slices.Contains(paths(), filepath.Join(folder, "fresh", "deep", "c.txt"))
	})
}

func TestAFolderRefreshedAShortWhileAgoIsNotReadAgain(t *testing.T) {
	d, paths, folder := newDaemonWithIndexedFolder(t)
	postRefresh(d, folder)
	waitUntil(t, "the first refresh has run", func() bool { return d.refreshedRecently(folder) && !d.busy() })
	writeFile(t, filepath.Join(folder, "later.txt"))

	if rr := postRefresh(d, folder); rr.Code != http.StatusOK {
		t.Fatalf("a refresh inside the window answered %d, want 200: nothing was queued", rr.Code)
	}
	if d.busy() {
		t.Fatal("a refresh inside the window queued work")
	}
	if slices.Contains(paths(), filepath.Join(folder, "later.txt")) {
		t.Fatal("the folder was read again inside the window")
	}

	// Once the window has passed, the next visit picks the change up.
	forgetRefresh(d, folder)
	if rr := postRefresh(d, folder); rr.Code != http.StatusAccepted {
		t.Fatalf("a refresh after the window answered %d, want 202", rr.Code)
	}
	waitUntil(t, "the later file is indexed", func() bool { return slices.Contains(paths(), filepath.Join(folder, "later.txt")) })
}

func TestRefreshesOfOneFolderCollapseWhileTheyWait(t *testing.T) {
	d, _, folder := newDaemonWithIndexedFolder(t)
	release := holdQueue(t, d)
	defer release()

	for range 3 {
		if rr := postRefresh(d, folder); rr.Code != http.StatusAccepted {
			t.Fatalf("refresh answered %d, want 202", rr.Code)
		}
	}
	if waiting := d.queued(); waiting != 1 {
		t.Fatalf("%d refreshes wait, want 1", waiting)
	}
}

func TestARefreshOfAFolderTheIndexDoesNotHoldReindexesIt(t *testing.T) {
	d, db := newIndexedDaemon(t)
	folder := filepath.Join(t.TempDir(), "unknown")
	writeFile(t, filepath.Join(folder, "a.txt"))

	postRefresh(d, folder)

	waitUntil(t, "the folder is indexed", func() bool { return len(indexedPaths(t, db, folder)) == 2 })
}

func TestARefreshOfAFolderThatIsGoneRemovesIt(t *testing.T) {
	d, paths, folder := newDaemonWithIndexedFolder(t)
	if err := os.RemoveAll(folder); err != nil {
		t.Fatalf("remove: %v", err)
	}

	postRefresh(d, folder)

	waitUntil(t, "the folder's rows are gone", func() bool { return len(paths()) == 0 })
}

func TestAFolderWithTooManyEntriesIsNotRefreshed(t *testing.T) {
	d, paths, folder := newDaemonWithIndexedFolder(t)
	previous := refreshEntryLimit
	refreshEntryLimit = 2
	t.Cleanup(func() { refreshEntryLimit = previous })
	writeFile(t, filepath.Join(folder, "new.txt"))

	postRefresh(d, folder)

	// Remembered, so that the next visits do not read it again either.
	waitUntil(t, "the refresh has run", func() bool { return d.refreshedRecently(folder) && !d.busy() })
	if slices.Contains(paths(), filepath.Join(folder, "new.txt")) {
		t.Fatal("a folder over the entry limit was refreshed")
	}
}

func TestARefreshOfAFolderLeftOutOfTheIndexReadsNothing(t *testing.T) {
	d, db := newIndexedDaemon(t)
	folder := filepath.Join(t.TempDir(), "left-out")
	writeFile(t, filepath.Join(folder, "a.txt"))
	d.settings.cfg.ExcludePaths = []string{folder}

	postRefresh(d, folder)

	waitUntil(t, "the refresh has run", func() bool { return d.refreshedRecently(folder) && !d.busy() })
	if got := indexedPaths(t, db, folder); len(got) != 0 {
		t.Fatalf("an excluded folder was indexed: %v", got)
	}
}

func TestARefreshRejectsAPathThatClimbs(t *testing.T) {
	d, _ := newIndexedDaemon(t)
	if rr := postRefresh(d, "/srv/../etc"); rr.Code != http.StatusBadRequest {
		t.Fatalf("refresh answered %d, want 400", rr.Code)
	}
}

func TestAFolderTheIndexDoesNotHoldIsReindexedOnceInTheWindow(t *testing.T) {
	d, db := newIndexedDaemon(t)
	folder := filepath.Join(t.TempDir(), "unknown")
	writeFile(t, filepath.Join(folder, "a.txt"))
	postRefresh(d, folder)
	waitUntil(t, "the folder is indexed", func() bool { return len(indexedPaths(t, db, folder)) == 2 && !d.busy() })

	if rr := postRefresh(d, folder); rr.Code != http.StatusOK {
		t.Fatalf("a second refresh inside the window answered %d, want 200", rr.Code)
	}
}

func TestAnUpdateDoesNotLookThroughASymlinkedFolder(t *testing.T) {
	d, paths, folder := newDaemonWithIndexedFolder(t)
	outside := filepath.Join(t.TempDir(), "outside")
	writeFile(t, filepath.Join(outside, "b.txt"))
	// sub is swapped for a link to a folder elsewhere: sub/b.txt now names a
	// file outside the tree, which the index must not take for its own.
	sub := filepath.Join(folder, "sub")
	if err := os.RemoveAll(sub); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := os.Symlink(outside, sub); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	postAdd(d, filepath.Join(sub, "b.txt"))

	waitUntil(t, "the update is applied", func() bool { return !d.busy() })
	if slices.Contains(paths(), filepath.Join(sub, "b.txt")) {
		t.Fatal("the index holds a row for a path that leads through a symlink")
	}
}

// A folder is opened by its path while the folder it is in is unknown to the
// index too. Reindexed by itself it adds its size to the folders above, and
// the reindex of the folder it is in, when that one is found, adds it again.
func TestOpeningAFolderBelowAnotherTheIndexDoesNotHoldCountsItOnce(t *testing.T) {
	d, db := newIndexedDaemon(t)
	folder := filepath.Join(t.TempDir(), "folder")
	writeFile(t, filepath.Join(folder, "a.txt"))
	postReindex(d, folder)
	waitUntil(t, "the folder is indexed", func() bool { return len(indexedPaths(t, db, folder)) == 2 && !d.busy() })
	writeFile(t, filepath.Join(folder, "new", "sub", "big.txt"))

	postRefresh(d, filepath.Join(folder, "new", "sub"))
	waitUntil(t, "the opened folder is indexed", func() bool {
		return slices.Contains(indexedPaths(t, db, folder), filepath.Join(folder, "new", "sub", "big.txt")) && !d.busy()
	})
	postRefresh(d, folder)
	waitUntil(t, "the folder above is refreshed", func() bool { return !d.busy() })
	got := folderSize(t, db, folder)

	postReindex(d, folder)
	waitUntil(t, "the folder is reindexed", func() bool { return !d.busy() })
	if want := folderSize(t, db, folder); got != want {
		t.Fatalf("the folder counted %d bytes after the refreshes, a reindex counts %d", got, want)
	}
	if paths := indexedPaths(t, db, folder); !slices.Contains(paths, filepath.Join(folder, "new")) {
		t.Fatalf("the folder in between has no row: %v", paths)
	}
}
