package daemon

import (
	"database/sql"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/indexer/api"
)

func postMove(d *daemon, from, to string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(api.MoveRequest{From: from, To: to})
	rr := httptest.NewRecorder()
	d.handleMove(rr, httptest.NewRequest(http.MethodPost, "/move", strings.NewReader(string(body))))
	return rr
}

func TestAMoveRenamesTheRowsWithoutWalkingTheFolder(t *testing.T) {
	d, paths, folder := newDaemonWithIndexedFolder(t)
	from, to := filepath.Join(folder, "sub"), filepath.Join(folder, "renamed")
	if err := os.Rename(from, to); err != nil {
		t.Fatalf("rename: %v", err)
	}
	// Not in the index: a walk of the moved folder would add it.
	writeFile(t, filepath.Join(to, "unseen.txt"))

	// At once, on a free daemon too: the operation does not wait for the index.
	if rr := postMove(d, from, to); rr.Code != http.StatusAccepted {
		t.Fatalf("move answered %d %q, want 202", rr.Code, rr.Body.String())
	}

	want := []string{folder, filepath.Join(folder, "a.txt"), to, filepath.Join(to, "b.txt")}
	waitUntil(t, "the rows are renamed", func() bool { return slices.Equal(paths(), want) })
	waitUntil(t, "the daemon is free", func() bool { return !d.busy() })
	if got := paths(); !slices.Equal(got, want) {
		t.Fatalf("indexed %v after the move, want %v: the folder was walked", got, want)
	}
}

func TestAMoveThatWasACopyAndADeleteWalksTheNewFolder(t *testing.T) {
	d, paths, folder := newDaemonWithIndexedFolder(t)
	from, to := filepath.Join(folder, "sub"), filepath.Join(folder, "copied")
	// What a move to another filesystem leaves: new files, the old ones gone.
	writeFile(t, filepath.Join(to, "b.txt"))
	writeFile(t, filepath.Join(to, "extra.txt"))
	if err := os.RemoveAll(from); err != nil {
		t.Fatalf("remove: %v", err)
	}

	postMove(d, from, to)

	want := []string{folder, filepath.Join(folder, "a.txt"), to, filepath.Join(to, "b.txt"), filepath.Join(to, "extra.txt")}
	waitUntil(t, "the new folder is indexed and the old one gone", func() bool { return slices.Equal(paths(), want) })
}

func TestAMoveWalksTheNewFolderWhenAnUpdateInsideTheOldOneStillWaits(t *testing.T) {
	d, paths, folder := newDaemonWithIndexedFolder(t)
	from, to := filepath.Join(folder, "sub"), filepath.Join(folder, "renamed")
	release := holdQueue(t, d)
	defer release()
	// A file is created in the folder and the folder is moved before the
	// index heard of the file: its update names a path that is gone by the
	// time it runs, so only a walk of the new folder finds the file.
	writeFile(t, filepath.Join(from, "late.txt"))
	postAdd(d, filepath.Join(from, "late.txt"))
	if err := os.Rename(from, to); err != nil {
		t.Fatalf("rename: %v", err)
	}

	if rr := postMove(d, from, to); rr.Code != http.StatusAccepted {
		t.Fatalf("a move behind other work answered %d, want 202", rr.Code)
	}
	release()

	want := []string{folder, filepath.Join(folder, "a.txt"), to, filepath.Join(to, "b.txt"), filepath.Join(to, "late.txt")}
	waitUntil(t, "the moved folder is indexed with the late file", func() bool { return slices.Equal(paths(), want) })
}

func TestAMoveToAPlaceTheIndexLeavesOutRemovesTheRows(t *testing.T) {
	d, paths, folder := newDaemonWithIndexedFolder(t)
	from, to := filepath.Join(folder, "sub"), filepath.Join(t.TempDir(), "left-out", "sub")
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Rename(from, to); err != nil {
		t.Fatalf("rename: %v", err)
	}
	d.settings.cfg.ExcludePaths = []string{filepath.Dir(to)}

	postMove(d, from, to)

	want := []string{folder, filepath.Join(folder, "a.txt")}
	waitUntil(t, "the moved rows are gone", func() bool { return slices.Equal(paths(), want) && !d.busy() })
}

func TestAMoveWhoseSourceIsStillThereFollowsTheDisk(t *testing.T) {
	d, paths, folder := newDaemonWithIndexedFolder(t)
	from, to := filepath.Join(folder, "sub"), filepath.Join(folder, "partial")
	// A move that stopped part-way: both folders exist.
	writeFile(t, filepath.Join(to, "moved.txt"))

	postMove(d, from, to)

	want := []string{folder, filepath.Join(folder, "a.txt"), to, filepath.Join(to, "moved.txt"), from, filepath.Join(from, "b.txt")}
	slices.Sort(want)
	waitUntil(t, "both folders are indexed", func() bool { return slices.Equal(paths(), want) })
}

func TestAMoveRejectsWhatIsNotAMove(t *testing.T) {
	d, _ := newIndexedDaemon(t)
	for name, request := range map[string][2]string{
		"a path that climbs": {"/srv/../etc", "/srv/x"},
		"the root":           {"/", "/srv/x"},
		"onto the root":      {"/srv/x", "/"},
		"onto itself":        {"/srv/x", "/srv/x"},
		"no destination":     {"/srv/x", ""},
	} {
		if rr := postMove(d, request[0], request[1]); rr.Code != http.StatusBadRequest {
			t.Errorf("%s: move answered %d, want 400", name, rr.Code)
		}
	}
}

func folderSize(t *testing.T, db *sql.DB, folder string) int64 {
	t.Helper()
	var size int64
	if err := db.QueryRow(`SELECT size FROM entries WHERE relative_path = ?`, folder).Scan(&size); err != nil {
		t.Fatalf("read size of %s: %v", folder, err)
	}
	return size
}

// The folder a file was moved into is opened, and refreshed, before the
// index hears of the move.
func TestAMoveBehindARefreshOfItsDestinationLeavesTheSizesRight(t *testing.T) {
	d, db := newIndexedDaemon(t)
	folder := filepath.Join(t.TempDir(), "folder")
	writeFile(t, filepath.Join(folder, "a.txt"))
	writeFile(t, filepath.Join(folder, "sub", "b.txt"))
	postReindex(d, folder)
	waitUntil(t, "the folder is indexed", func() bool { return len(indexedPaths(t, db, folder)) == 4 && !d.busy() })
	want := folderSize(t, db, folder)

	from, to := filepath.Join(folder, "sub", "b.txt"), filepath.Join(folder, "b.txt")
	if err := os.Rename(from, to); err != nil {
		t.Fatalf("rename: %v", err)
	}
	postRefresh(d, folder)
	waitUntil(t, "the refresh has run", func() bool { return !d.busy() })
	postMove(d, from, to)
	waitUntil(t, "the move has run", func() bool { return !d.busy() })

	// The file is in the same folder tree as before, so the folder counts
	// what it counted.
	if got := folderSize(t, db, folder); got != want {
		t.Fatalf("the folder counts %d bytes after the move, want %d as before", got, want)
	}
}

func TestAMoveWhoseSourceCannotBeLookedAtStillIndexesTheDestination(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads every folder")
	}
	d, paths, folder := newDaemonWithIndexedFolder(t)
	locked := filepath.Join(folder, "locked")
	if err := os.Mkdir(locked, 0o000); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	to := filepath.Join(folder, "arrived.txt")
	writeFile(t, to)

	postMove(d, filepath.Join(locked, "left.txt"), to)

	waitUntil(t, "the destination is indexed", func() bool { return slices.Contains(paths(), to) })
}
