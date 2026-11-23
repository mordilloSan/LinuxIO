package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/indexing"
)

// rescan reindexes folder the way the daemon and the scanner do, and returns
// how many rows that wrote or deleted.
func rescan(t *testing.T, ctx context.Context, store *Store, indexID int64, folder string) int64 {
	t.Helper()
	reindex, err := store.BeginReindex(ctx, indexID, folder)
	if err != nil {
		t.Fatalf("begin reindex: %v", err)
	}
	defer reindex.Rollback()
	index := indexing.Initialize("/", indexing.WithHardlinkContributors(reindex.HardlinkContributors()))
	reconciler := reindex.NewReconciler(ctx, nil)
	index.EnableStreaming(reconciler)
	if err := index.StartIndexingFromPath(ctx, folder); err != nil {
		t.Fatalf("scan %s: %v", folder, err)
	}
	if err := reconciler.Close(); err != nil {
		t.Fatalf("close reconciler: %v", err)
	}
	if _, err := reindex.Commit(ctx, reconciler.ScanTime()); err != nil {
		t.Fatalf("commit reindex: %v", err)
	}
	return reconciler.Changed()
}

// indexRows is every row below folder as "path type size contribution", in
// path order.
func indexRows(t *testing.T, db *sql.DB, folder string) []string {
	t.Helper()
	rows, err := db.Query(`SELECT relative_path, type, size, size_contribution FROM entries WHERE relative_path = ? OR relative_path LIKE ? ORDER BY relative_path`, folder, folder+"/%")
	if err != nil {
		t.Fatalf("query rows: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var path string
		var typ int
		var size, contribution int64
		if err := rows.Scan(&path, &typ, &size, &contribution); err != nil {
			t.Fatalf("scan row: %v", err)
		}
		out = append(out, fmt.Sprintf("%s %d %d %d", strings.TrimPrefix(path, folder), typ, size, contribution))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read rows: %v", err)
	}
	return out
}

func writeTestFile(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func newRescanFixture(t *testing.T) (context.Context, *Store, *sql.DB, int64, string) {
	t.Helper()
	ctx, db, dbPath := setupTestDB(t)
	indexID := insertTestIndex(t, db)
	root := filepath.Join(t.TempDir(), "tree")
	writeTestFile(t, filepath.Join(root, "a.txt"), 4096)
	writeTestFile(t, filepath.Join(root, "docs", "b.txt"), 8192)
	writeTestFile(t, filepath.Join(root, "docs", "deep", "c.txt"), 4096)
	writeTestFile(t, filepath.Join(root, "empty", ".keep"), 0)
	return ctx, NewStoreWithDB(db, dbPath, indexing.SameFile), db, indexID, root
}

func TestARescanOfAnUnchangedTreeWritesNothing(t *testing.T) {
	ctx, store, db, indexID, root := newRescanFixture(t)
	if changed := rescan(t, ctx, store, indexID, root); changed == 0 {
		t.Fatal("the first scan wrote nothing")
	}
	before := indexRows(t, db, root)
	if len(before) != 8 {
		t.Fatalf("first scan indexed %d rows, want 8: %v", len(before), before)
	}

	if changed := rescan(t, ctx, store, indexID, root); changed != 0 {
		t.Fatalf("a rescan of an unchanged tree wrote or deleted %d rows, want 0", changed)
	}
	if after := indexRows(t, db, root); strings.Join(after, "\n") != strings.Join(before, "\n") {
		t.Fatalf("rows changed without a change on disk:\nbefore %v\nafter  %v", before, after)
	}
}

func TestARescanWritesOnlyWhatChanged(t *testing.T) {
	ctx, store, db, indexID, root := newRescanFixture(t)
	rescan(t, ctx, store, indexID, root)

	// One new file, one file that grew, one file and one folder gone.
	writeTestFile(t, filepath.Join(root, "docs", "new.txt"), 4096)
	writeTestFile(t, filepath.Join(root, "a.txt"), 12288)
	if err := os.Remove(filepath.Join(root, "docs", "b.txt")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(root, "empty")); err != nil {
		t.Fatalf("remove folder: %v", err)
	}
	changed := rescan(t, ctx, store, indexID, root)

	// Written: new.txt, a.txt, and the two folders whose size moved (docs,
	// the root). Deleted: b.txt, and empty with its file. deep and c.txt are
	// untouched.
	if changed != 7 {
		t.Fatalf("the rescan wrote or deleted %d rows, want 7", changed)
	}
	// What the index says must be what a scan from nothing would say.
	rebuilt := rebuildFromNothing(t, root)
	if now := indexRows(t, db, root); strings.Join(now, "\n") != strings.Join(rebuilt, "\n") {
		t.Fatalf("the updated index differs from a fresh one:\nupdated %v\nfresh   %v", now, rebuilt)
	}
}

func TestARescanOfAFolderWithManyFilesFindsWhatIsGone(t *testing.T) {
	old := reconcileChunk
	reconcileChunk = 4
	t.Cleanup(func() { reconcileChunk = old })

	ctx, store, db, indexID, root := newRescanFixture(t)
	for i := range 25 {
		writeTestFile(t, filepath.Join(root, "many", fmt.Sprintf("f%02d", i)), 4096)
	}
	rescan(t, ctx, store, indexID, root)
	if changed := rescan(t, ctx, store, indexID, root); changed != 0 {
		t.Fatalf("a rescan of an unchanged large folder wrote or deleted %d rows, want 0", changed)
	}

	for _, i := range []int{0, 7, 24} {
		if err := os.Remove(filepath.Join(root, "many", fmt.Sprintf("f%02d", i))); err != nil {
			t.Fatalf("remove: %v", err)
		}
	}
	writeTestFile(t, filepath.Join(root, "many", "added"), 4096)
	rescan(t, ctx, store, indexID, root)
	rebuilt := rebuildFromNothing(t, root)
	if now := indexRows(t, db, root); strings.Join(now, "\n") != strings.Join(rebuilt, "\n") {
		t.Fatalf("the updated index differs from a fresh one:\nupdated %v\nfresh   %v", now, rebuilt)
	}
}

// rebuildFromNothing indexes root into a new database and returns its rows.
func rebuildFromNothing(t *testing.T, root string) []string {
	t.Helper()
	ctx, db, dbPath := setupTestDB(t)
	indexID := insertTestIndex(t, db)
	rescan(t, ctx, NewStoreWithDB(db, dbPath, indexing.SameFile), indexID, root)
	return indexRows(t, db, root)
}

func TestAReindexThatIsRolledBackChangesNothing(t *testing.T) {
	ctx, store, db, indexID, root := newRescanFixture(t)
	rescan(t, ctx, store, indexID, root)
	before := indexRows(t, db, root)

	writeTestFile(t, filepath.Join(root, "docs", "new.txt"), 4096)
	writeTestFile(t, filepath.Join(root, "a.txt"), 12288)
	reindex, err := store.BeginReindex(ctx, indexID, root)
	if err != nil {
		t.Fatalf("begin reindex: %v", err)
	}
	index := indexing.Initialize("/")
	reconciler := reindex.NewReconciler(ctx, nil)
	index.EnableStreaming(reconciler)
	if err := index.StartIndexingFromPath(ctx, root); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if err := reconciler.Close(); err != nil {
		t.Fatalf("close reconciler: %v", err)
	}
	reindex.Rollback()

	if after := indexRows(t, db, root); strings.Join(after, "\n") != strings.Join(before, "\n") {
		t.Fatalf("a rolled back reindex changed rows:\nbefore %v\nafter  %v", before, after)
	}
}

func TestAFolderReplacedByAFileLosesWhatWasBelowIt(t *testing.T) {
	ctx, store, db, indexID, root := newRescanFixture(t)
	rescan(t, ctx, store, indexID, root)

	// "docs" held b.txt and deep/c.txt; now a file has that name.
	if err := os.RemoveAll(filepath.Join(root, "docs")); err != nil {
		t.Fatalf("remove folder: %v", err)
	}
	writeTestFile(t, filepath.Join(root, "docs"), 4096)
	rescan(t, ctx, store, indexID, root)

	rebuilt := rebuildFromNothing(t, root)
	if now := indexRows(t, db, root); strings.Join(now, "\n") != strings.Join(rebuilt, "\n") {
		t.Fatalf("the updated index differs from a fresh one:\nupdated %v\nfresh   %v", now, rebuilt)
	}
}

func TestAFolderWithManyFilesReplacedByAFileLosesWhatWasBelowIt(t *testing.T) {
	old := reconcileChunk
	reconcileChunk = 2
	t.Cleanup(func() { reconcileChunk = old })

	ctx, store, db, indexID, root := newRescanFixture(t)
	writeTestFile(t, filepath.Join(root, "x.txt"), 4096)
	writeTestFile(t, filepath.Join(root, "y.txt"), 4096)
	rescan(t, ctx, store, indexID, root)

	if err := os.RemoveAll(filepath.Join(root, "docs")); err != nil {
		t.Fatalf("remove folder: %v", err)
	}
	writeTestFile(t, filepath.Join(root, "docs"), 4096)
	rescan(t, ctx, store, indexID, root)

	rebuilt := rebuildFromNothing(t, root)
	if now := indexRows(t, db, root); strings.Join(now, "\n") != strings.Join(rebuilt, "\n") {
		t.Fatalf("the updated index differs from a fresh one:\nupdated %v\nfresh   %v", now, rebuilt)
	}
}

// A folder the scan gave up on part-way must leave no rows behind: not the
// stored ones, and not those its subfolders had already sent.
func TestAFolderTheScanGaveUpOnLeavesNoRows(t *testing.T) {
	ctx, store, db, indexID, root := newRescanFixture(t)
	reindex, err := store.BeginReindex(ctx, indexID, root)
	if err != nil {
		t.Fatalf("begin reindex: %v", err)
	}
	defer reindex.Rollback()
	reconciler := reindex.NewReconciler(ctx, nil)
	send := func(path string) {
		t.Helper()
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatalf("lstat %s: %v", path, err)
		}
		if err := reconciler.Write(indexing.EntryFromFileInfo(path, info)); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	// docs/deep is sent whole; then docs cannot be read to its end.
	send(filepath.Join(root, "docs", "deep", "c.txt"))
	send(filepath.Join(root, "docs", "deep"))
	send(filepath.Join(root, "docs", "b.txt"))
	if err := reconciler.Drop(filepath.Join(root, "docs")); err != nil {
		t.Fatalf("drop: %v", err)
	}
	send(filepath.Join(root, "a.txt"))
	send(root)
	if err := reconciler.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := reindex.Commit(ctx, reconciler.ScanTime()); err != nil {
		t.Fatalf("commit: %v", err)
	}

	for _, row := range indexRows(t, db, root) {
		if strings.HasPrefix(row, "/docs") {
			t.Fatalf("a row of the folder the scan gave up on is left: %q", row)
		}
	}
}

// A file indexed while it had one link, which then got a second: when the
// first is deleted, the folder scan that removes its row must hand the size
// to the link that is left.
func TestALinkAddedLaterTakesOverWhenTheFirstIsDeleted(t *testing.T) {
	ctx, db, dbPath := setupTestDB(t)
	indexID := insertTestIndex(t, db)
	store := NewStoreWithDB(db, dbPath, indexing.SameFile)
	root := filepath.Join(t.TempDir(), "tree")
	first := filepath.Join(root, "data", "a.iso")
	second := filepath.Join(root, "backup", "a.iso")
	writeTestFile(t, first, 8192)
	if err := os.MkdirAll(filepath.Dir(second), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	rescan(t, ctx, store, indexID, root)

	// The second link is made and indexed on its own, as a file operation does.
	if err := os.Link(first, second); err != nil {
		t.Fatalf("link: %v", err)
	}
	info, err := os.Lstat(second)
	if err != nil {
		t.Fatalf("lstat: %v", err)
	}
	if err := store.UpsertEntry(ctx, indexID, indexing.EntryFromFileInfo(second, info)); err != nil {
		t.Fatalf("add the second link: %v", err)
	}

	// The first link is deleted outside LinuxIO, and only its folder rescanned.
	if err := os.Remove(first); err != nil {
		t.Fatalf("remove: %v", err)
	}
	rescan(t, ctx, store, indexID, filepath.Join(root, "data"))

	rebuilt := rebuildFromNothing(t, root)
	if now := indexRows(t, db, root); strings.Join(now, "\n") != strings.Join(rebuilt, "\n") {
		t.Fatalf("the updated index differs from a fresh one:\nupdated %v\nfresh   %v", now, rebuilt)
	}
}

// A file moved outside LinuxIO, of which only the new folder is reindexed:
// the row left at the old path must stop counting, or the size is in two
// folders.
func TestAFileMovedOutsideLinuxIOIsNotCountedTwice(t *testing.T) {
	ctx, db, dbPath := setupTestDB(t)
	indexID := insertTestIndex(t, db)
	store := NewStoreWithDB(db, dbPath, indexing.SameFile)
	root := filepath.Join(t.TempDir(), "tree")
	writeTestFile(t, filepath.Join(root, "x", "a.iso"), 8192)
	if err := os.MkdirAll(filepath.Join(root, "y"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	rescan(t, ctx, store, indexID, root)

	if err := os.Rename(filepath.Join(root, "x", "a.iso"), filepath.Join(root, "y", "a.iso")); err != nil {
		t.Fatalf("move: %v", err)
	}
	rescan(t, ctx, store, indexID, filepath.Join(root, "y"))

	var counted int64
	if err := db.QueryRow(`SELECT COALESCE(SUM(size_contribution), 0) FROM entries WHERE type = 0 AND relative_path LIKE ?`, root+"/%").Scan(&counted); err != nil {
		t.Fatalf("sum contributions: %v", err)
	}
	if counted != 8192 {
		t.Fatalf("the moved file is counted for %d bytes, want 8192 once", counted)
	}
}

// A hard link whose other links are not in the index must count: it is the
// only place the file's size can be.
func TestALoneHardLinkThatCountsNothingIsCounted(t *testing.T) {
	ctx, db, dbPath := setupTestDB(t)
	indexID := insertTestIndex(t, db)
	store := NewStoreWithDB(db, dbPath, nil)
	reindex, err := store.BeginReindex(ctx, indexID, "/srv")
	if err != nil {
		t.Fatalf("begin reindex: %v", err)
	}
	defer reindex.Rollback()
	reconciler := reindex.NewReconciler(ctx, nil)
	// As a scan sends it when the link it counted was in a folder it then
	// gave up on.
	for _, entry := range []indexing.IndexEntry{
		{RelativePath: "/srv/link", Name: "link", Type: "file", Size: 4096, Device: 7, Inode: 99, Nlink: 2, SizeContribution: 0},
		{RelativePath: "/srv", Name: "srv", Type: "directory", Size: 0},
	} {
		if writeErr := reconciler.Write(entry); writeErr != nil {
			t.Fatalf("write %s: %v", entry.RelativePath, writeErr)
		}
	}
	if closeErr := reconciler.Close(); closeErr != nil {
		t.Fatalf("close: %v", closeErr)
	}
	if _, commitErr := reindex.Commit(ctx, reconciler.ScanTime()); commitErr != nil {
		t.Fatalf("commit: %v", commitErr)
	}

	var contribution, folder int64
	if err := db.QueryRow(`SELECT size_contribution FROM entries WHERE relative_path = '/srv/link'`).Scan(&contribution); err != nil {
		t.Fatalf("read link: %v", err)
	}
	if err := db.QueryRow(`SELECT size FROM entries WHERE relative_path = '/srv'`).Scan(&folder); err != nil {
		t.Fatalf("read folder: %v", err)
	}
	if contribution != 4096 || folder != 4096 {
		t.Fatalf("the lone link counts %d and its folder holds %d, want 4096 and 4096", contribution, folder)
	}
}

// The compare reads a folder's children with one range read of the index
// that keeps them together, not by walking the rows of the whole subtree.
func TestStoredChildrenAreReadFromTheChildrenIndex(t *testing.T) {
	ctx, db, _ := setupTestDB(t)
	indexID := insertTestIndex(t, db)
	for _, folder := range []string{"/", "/srv/data/deep"} {
		depth := 1
		if folder != "/" {
			depth = strings.Count(folder, "/") + 1
		}
		_, childLo, hi := subtreeBounds(folder)
		plan := queryPlan(t, ctx, db, storedChildrenQuery, indexID, depth, childLo, hi)
		if !strings.Contains(plan, "idx_entries_subfolders") || strings.Contains(plan, "SCAN") {
			t.Fatalf("children of %s are read by %q, want a search of idx_entries_subfolders", folder, plan)
		}
	}
}
