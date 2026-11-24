package storage

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/indexing"
)

// refreshFolder reads folder from disk one level deep and refreshes it in the
// index, the way the daemon does when the folder is opened.
func refreshFolder(t *testing.T, ctx context.Context, store *Store, indexID int64, folder string) FolderRefresh {
	t.Helper()
	contents, err := indexing.Initialize("/").ReadFolder(ctx, folder, 10_000)
	if err != nil {
		t.Fatalf("read folder %s: %v", folder, err)
	}
	result, err := store.RefreshFolder(ctx, indexID, folder, contents)
	if err != nil {
		t.Fatalf("refresh folder %s: %v", folder, err)
	}
	return result
}

// assertAgreesWithDisk fails when a rescan of root still finds something to
// write, apart from the folders' own rows: a folder's time changes with its
// entries and the refresh leaves that to the next scan.
func assertAgreesWithDisk(t *testing.T, ctx context.Context, store *Store, db *sql.DB, indexID int64, root string) {
	t.Helper()
	before := indexRows(t, db, root)
	rescan(t, ctx, store, indexID, root)
	if after := indexRows(t, db, root); !slices.Equal(after, before) {
		t.Fatalf("the refreshed index does not agree with a rescan:\nrefreshed %v\nrescanned %v", before, after)
	}
	var dirs, files, wantDirs, wantFiles int64
	if err := db.QueryRow(`SELECT num_dirs, num_files FROM indexes WHERE id = ?`, indexID).Scan(&dirs, &files); err != nil {
		t.Fatalf("read counts: %v", err)
	}
	if err := db.QueryRow(`SELECT COALESCE(SUM(type = 1), 0), COALESCE(SUM(type = 0), 0) FROM entries WHERE index_id = ?`, indexID).Scan(&wantDirs, &wantFiles); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if dirs != wantDirs || files != wantFiles {
		t.Fatalf("index counts %d folders and %d files, its rows are %d and %d", dirs, files, wantDirs, wantFiles)
	}
}

func TestRefreshOfAnUnchangedFolderWritesNothing(t *testing.T) {
	ctx, store, db, indexID, root := newRescanFixture(t)
	rescan(t, ctx, store, indexID, root)
	before := indexRows(t, db, root)
	var indexedBefore int64
	if err := db.QueryRow(`SELECT last_indexed FROM indexes WHERE id = ?`, indexID).Scan(&indexedBefore); err != nil {
		t.Fatalf("read last_indexed: %v", err)
	}
	if _, err := db.Exec(`UPDATE indexes SET last_indexed = 1 WHERE id = ?`, indexID); err != nil {
		t.Fatalf("mark index row: %v", err)
	}

	result := refreshFolder(t, ctx, store, indexID, root)

	if result.Changed != 0 || len(result.NewFolders) != 0 {
		t.Fatalf("refresh of an unchanged folder = %+v, want nothing", result)
	}
	if after := indexRows(t, db, root); !slices.Equal(after, before) {
		t.Fatalf("rows changed without a change on disk:\nbefore %v\nafter  %v", before, after)
	}
	var indexed int64
	if err := db.QueryRow(`SELECT last_indexed FROM indexes WHERE id = ?`, indexID).Scan(&indexed); err != nil {
		t.Fatalf("read last_indexed: %v", err)
	}
	if indexed != 1 {
		t.Fatal("a refresh that changed nothing rewrote the index row")
	}
}

func TestRefreshAppliesWhatChangedInTheFolder(t *testing.T) {
	ctx, store, db, indexID, root := newRescanFixture(t)
	rescan(t, ctx, store, indexID, root)

	// In the folder: one new file, one that grew, one subfolder gone.
	writeTestFile(t, filepath.Join(root, "new.txt"), 8192)
	writeTestFile(t, filepath.Join(root, "a.txt"), 12288)
	if err := os.RemoveAll(filepath.Join(root, "docs")); err != nil {
		t.Fatalf("remove docs: %v", err)
	}

	result := refreshFolder(t, ctx, store, indexID, root)

	// new.txt, a.txt and the removal of docs.
	if result.Changed != 3 || len(result.NewFolders) != 0 {
		t.Fatalf("refresh = %+v, want 3 changes and no new folder", result)
	}
	rows := strings.Join(indexRows(t, db, root), "\n")
	for _, gone := range []string{"/docs ", "/docs/b.txt", "/docs/deep/c.txt"} {
		if strings.Contains(rows, gone) {
			t.Fatalf("%s is still indexed:\n%s", gone, rows)
		}
	}
	assertAgreesWithDisk(t, ctx, store, db, indexID, root)
}

func TestRefreshDoesNotLookBelowTheFolder(t *testing.T) {
	ctx, store, db, indexID, root := newRescanFixture(t)
	rescan(t, ctx, store, indexID, root)
	before := indexRows(t, db, root)
	writeTestFile(t, filepath.Join(root, "docs", "deep", "later.txt"), 4096)

	if result := refreshFolder(t, ctx, store, indexID, root); result.Changed != 0 {
		t.Fatalf("refresh = %+v, want nothing: the change is two folders down", result)
	}
	if after := indexRows(t, db, root); !slices.Equal(after, before) {
		t.Fatalf("rows changed:\nbefore %v\nafter  %v", before, after)
	}
}

func TestRefreshReportsANewSubfolderAndLeavesItToAReindex(t *testing.T) {
	ctx, store, db, indexID, root := newRescanFixture(t)
	rescan(t, ctx, store, indexID, root)
	writeTestFile(t, filepath.Join(root, "fresh", "x.txt"), 4096)

	result := refreshFolder(t, ctx, store, indexID, root)

	if want := []string{filepath.Join(root, "fresh")}; !slices.Equal(result.NewFolders, want) {
		t.Fatalf("new folders = %v, want %v", result.NewFolders, want)
	}
	if rows := strings.Join(indexRows(t, db, root), "\n"); strings.Contains(rows, "/fresh") {
		t.Fatalf("the new folder was indexed by the refresh:\n%s", rows)
	}
}

func TestRefreshOfASubfolderReplacedByAFileRemovesWhatWasBelow(t *testing.T) {
	ctx, store, db, indexID, root := newRescanFixture(t)
	rescan(t, ctx, store, indexID, root)
	if err := os.RemoveAll(filepath.Join(root, "docs")); err != nil {
		t.Fatalf("remove docs: %v", err)
	}
	writeTestFile(t, filepath.Join(root, "docs"), 4096)

	refreshFolder(t, ctx, store, indexID, root)

	rows := strings.Join(indexRows(t, db, root), "\n")
	if strings.Contains(rows, "/docs/") {
		t.Fatalf("rows are left below a path that is a file now:\n%s", rows)
	}
	if !strings.Contains(rows, "/docs 0 4096 4096") {
		t.Fatalf("docs is not indexed as a file:\n%s", rows)
	}
	assertAgreesWithDisk(t, ctx, store, db, indexID, root)
}

func TestRefreshOfAFileReplacedByAFolderReportsTheFolder(t *testing.T) {
	ctx, store, db, indexID, root := newRescanFixture(t)
	rescan(t, ctx, store, indexID, root)
	if err := os.Remove(filepath.Join(root, "a.txt")); err != nil {
		t.Fatalf("remove a.txt: %v", err)
	}
	writeTestFile(t, filepath.Join(root, "a.txt", "inner"), 4096)

	result := refreshFolder(t, ctx, store, indexID, root)

	if want := []string{filepath.Join(root, "a.txt")}; !slices.Equal(result.NewFolders, want) {
		t.Fatalf("new folders = %v, want %v", result.NewFolders, want)
	}
	if rows := strings.Join(indexRows(t, db, root), "\n"); strings.Contains(rows, "/a.txt") {
		t.Fatalf("the file's row was kept:\n%s", rows)
	}
}

func TestRefreshOfAFolderTheIndexDoesNotHoldIsRefused(t *testing.T) {
	ctx, store, _, indexID, root := newRescanFixture(t)
	rescan(t, ctx, store, indexID, filepath.Join(root, "docs"))

	contents, err := indexing.Initialize("/").ReadFolder(ctx, filepath.Join(root, "empty"), 100)
	if err != nil {
		t.Fatalf("read folder: %v", err)
	}
	if _, err := store.RefreshFolder(ctx, indexID, filepath.Join(root, "empty"), contents); !errors.Is(err, ErrFolderNotIndexed) {
		t.Fatalf("refresh error = %v, want ErrFolderNotIndexed", err)
	}
}

func TestRefreshCountsAHardLinkOnce(t *testing.T) {
	ctx, store, db, indexID, root := newRescanFixture(t)
	// a.txt has a second name one folder down, which the index knows of.
	if err := os.Link(filepath.Join(root, "a.txt"), filepath.Join(root, "docs", "a-below.txt")); err != nil {
		t.Fatalf("link: %v", err)
	}
	rescan(t, ctx, store, indexID, root)
	// A third name, in the folder that is refreshed.
	if err := os.Link(filepath.Join(root, "a.txt"), filepath.Join(root, "a-again.txt")); err != nil {
		t.Fatalf("link: %v", err)
	}

	refreshFolder(t, ctx, store, indexID, root)

	var names, counted int64
	if err := db.QueryRow(`SELECT COUNT(*), SUM(size_contribution) FROM entries WHERE index_id = ? AND name IN ('a.txt', 'a-again.txt', 'a-below.txt')`, indexID).Scan(&names, &counted); err != nil {
		t.Fatalf("sum contributions: %v", err)
	}
	if names != 3 || counted != 4096 {
		t.Fatalf("%d names of the file count %d bytes, want 3 names and 4096 bytes", names, counted)
	}
	assertAgreesWithDisk(t, ctx, store, db, indexID, root)
}

func TestUpsertOfAFileWhereAFolderWasIndexedRemovesWhatWasBelow(t *testing.T) {
	ctx, store, db, indexID, root := newRescanFixture(t)
	rescan(t, ctx, store, indexID, root)
	docs := filepath.Join(root, "docs")
	if err := os.RemoveAll(docs); err != nil {
		t.Fatalf("remove docs: %v", err)
	}
	writeTestFile(t, docs, 4096)
	info, err := os.Lstat(docs)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	if err := store.UpsertEntry(ctx, indexID, indexing.EntryFromFileInfo(docs, info)); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	if rows := strings.Join(indexRows(t, db, root), "\n"); strings.Contains(rows, "/docs/") || !strings.Contains(rows, "/docs 0 4096 4096") {
		t.Fatalf("docs is not one file row:\n%s", rows)
	}
	assertAgreesWithDisk(t, ctx, store, db, indexID, root)
}
