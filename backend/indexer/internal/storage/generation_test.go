package storage

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/indexing"
)

// scanBelowRoot reindexes path with root standing for the filesystem root,
// so its rows are "/", "/a.txt" and so on. A path of "/" is a full index.
func scanBelowRoot(t *testing.T, ctx context.Context, store *Store, indexID int64, root, path string) {
	t.Helper()
	reindex, err := store.BeginReindex(ctx, indexID, path)
	if err != nil {
		t.Fatalf("begin reindex of %s: %v", path, err)
	}
	defer reindex.Rollback()
	index := indexing.Initialize(root, indexing.WithHardlinkContributors(reindex.HardlinkContributors()))
	reconciler := reindex.NewReconciler(ctx, nil)
	index.EnableStreaming(reconciler)
	if err := index.StartIndexingFromPath(ctx, path); err != nil {
		t.Fatalf("scan %s: %v", path, err)
	}
	if err := reconciler.Close(); err != nil {
		t.Fatalf("close reconciler: %v", err)
	}
	if _, err := reindex.Commit(ctx, reconciler.ScanTime()); err != nil {
		t.Fatalf("commit reindex of %s: %v", path, err)
	}
}

func TestTheFirstScanPublishesItsGeneration(t *testing.T) {
	ctx, db, dbPath := setupTestDB(t)
	store := NewStoreWithDB(db, dbPath, indexing.SameFile)
	root := filepath.Join(t.TempDir(), "tree")
	writeTestFile(t, filepath.Join(root, "a.txt"), 4096)
	writeTestFile(t, filepath.Join(root, "docs", "b.txt"), 4096)

	indexID, err := store.EnsureGeneration(ctx)
	if err != nil {
		t.Fatalf("ensure generation: %v", err)
	}
	// Nothing is published until a scan has completed: readers see no index.
	if _, latestErr := store.LatestIndexID(ctx); !errors.Is(latestErr, ErrNoGeneration) {
		t.Fatalf("latest generation before the first scan: %v, want none", latestErr)
	}
	if again, againErr := store.EnsureGeneration(ctx); againErr != nil || again != indexID {
		t.Fatalf("a second call gave generation %d (%v), want the same %d", again, againErr, indexID)
	}

	// A scan of part of the tree publishes nothing: readers must not take a
	// folder's rows for an index of everything.
	scanBelowRoot(t, ctx, store, indexID, root, "/docs")
	if _, latestErr := store.LatestIndexID(ctx); !errors.Is(latestErr, ErrNoGeneration) {
		t.Fatalf("latest generation after a scan of one folder: %v, want none", latestErr)
	}

	scanBelowRoot(t, ctx, store, indexID, root, "/")
	published, err := store.LatestIndexID(ctx)
	if err != nil || published != indexID {
		t.Fatalf("latest generation after the first scan = %d (%v), want %d", published, err, indexID)
	}
	latest, err := store.LatestGeneration(ctx)
	if err != nil || latest.NumDirs != 2 || latest.NumFiles != 2 {
		t.Fatalf("published counts = %d folders, %d files (%v), want 2 and 2", latest.NumDirs, latest.NumFiles, err)
	}
	// Later scans keep to that one generation.
	if again, againErr := store.EnsureGeneration(ctx); againErr != nil || again != indexID {
		t.Fatalf("after publication the generation is %d (%v), want %d", again, againErr, indexID)
	}
	var generations int
	if countErr := db.QueryRow(`SELECT COUNT(*) FROM indexes`).Scan(&generations); countErr != nil || generations != 1 {
		t.Fatalf("%d generations (%v), want 1", generations, countErr)
	}
}

func TestAnInterruptedFirstScanIsCarriedOn(t *testing.T) {
	ctx, db, dbPath := setupTestDB(t)
	store := NewStoreWithDB(db, dbPath, indexing.SameFile)
	root := filepath.Join(t.TempDir(), "tree")
	writeTestFile(t, filepath.Join(root, "a.txt"), 4096)
	writeTestFile(t, filepath.Join(root, "docs", "b.txt"), 4096)

	indexID, err := store.EnsureGeneration(ctx)
	if err != nil {
		t.Fatalf("ensure generation: %v", err)
	}
	// A scan that committed part of its rows and then stopped.
	reindex, err := store.BeginReindex(ctx, indexID, root)
	if err != nil {
		t.Fatalf("begin reindex: %v", err)
	}
	reconciler := reindex.NewReconciler(ctx, nil)
	info, err := os.Lstat(filepath.Join(root, "a.txt"))
	if err != nil {
		t.Fatalf("lstat: %v", err)
	}
	if writeErr := reconciler.Write(indexing.EntryFromFileInfo(filepath.Join(root, "a.txt"), info)); writeErr != nil {
		t.Fatalf("write entry: %v", writeErr)
	}
	if compareErr := reconciler.compareByLookup(reconciler.folder(root)); compareErr != nil {
		t.Fatalf("compare: %v", compareErr)
	}
	if closeErr := reconciler.Close(); closeErr != nil {
		t.Fatalf("close: %v", closeErr)
	}
	if commitErr := reindex.commitChunk(ctx); commitErr != nil {
		t.Fatalf("commit part: %v", commitErr)
	}
	reindex.Rollback()

	if _, latestErr := store.LatestIndexID(ctx); !errors.Is(latestErr, ErrNoGeneration) {
		t.Fatalf("latest generation after an interrupted first scan: %v, want none", latestErr)
	}
	again, err := store.EnsureGeneration(ctx)
	if err != nil || again != indexID {
		t.Fatalf("the next scan got generation %d (%v), want to carry on in %d", again, err, indexID)
	}
	rescan(t, ctx, store, again, root)
	rebuilt := rebuildFromNothing(t, root)
	if now := indexRows(t, db, root); strings.Join(now, "\n") != strings.Join(rebuilt, "\n") {
		t.Fatalf("the carried-on index differs from a fresh one:\ncarried on %v\nfresh      %v", now, rebuilt)
	}
}

// The counts on the status page are those of the rows, also when an earlier
// scan of everything committed part of its rows and then stopped.
func TestAFullScanCountsWhatIsThereAfterAnInterruptedOne(t *testing.T) {
	ctx, db, dbPath := setupTestDB(t)
	store := NewStoreWithDB(db, dbPath, indexing.SameFile)
	indexID, err := store.EnsureGeneration(ctx)
	if err != nil {
		t.Fatalf("ensure generation: %v", err)
	}
	seed := func(entry indexing.IndexEntry) {
		t.Helper()
		if _, seedErr := UpdateEntry(ctx, db, indexID, entry); seedErr != nil {
			t.Fatalf("seed %s: %v", entry.RelativePath, seedErr)
		}
	}
	// What a stopped first scan left: rows, and counts still at zero.
	seed(indexing.IndexEntry{RelativePath: "/", Name: "/", Type: "directory"})
	seed(indexing.IndexEntry{RelativePath: "/kept", Name: "kept", Type: "directory"})
	seed(indexing.IndexEntry{RelativePath: "/kept/a.txt", Name: "a.txt", Type: "file", Size: 1})

	reindex, err := store.BeginReindex(ctx, indexID, "/")
	if err != nil {
		t.Fatalf("begin reindex: %v", err)
	}
	defer reindex.Rollback()
	// The scan of everything finds the same three entries and one more file.
	reconciler := reindex.NewReconciler(ctx, nil)
	for _, entry := range []indexing.IndexEntry{
		{RelativePath: "/kept/a.txt", Name: "a.txt", Type: "file", Size: 1},
		{RelativePath: "/kept/b.txt", Name: "b.txt", Type: "file", Size: 1},
		{RelativePath: "/kept", Name: "kept", Type: "directory", Size: 2},
		{RelativePath: "/", Name: "/", Type: "directory", Size: 2},
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

	latest, err := store.LatestGeneration(ctx)
	if err != nil || latest.NumDirs != 2 || latest.NumFiles != 2 {
		t.Fatalf("counts = %d folders, %d files (%v), want 2 and 2", latest.NumDirs, latest.NumFiles, err)
	}
}

// markLastFullIndex sets the time of the last full index to long ago, so a
// write of it shows.
func markLastFullIndex(t *testing.T, db *sql.DB, indexID int64) {
	t.Helper()
	if _, err := db.Exec(`UPDATE indexes SET last_indexed = 1 WHERE id = ?`, indexID); err != nil {
		t.Fatalf("mark the index row: %v", err)
	}
}

func lastFullIndex(t *testing.T, db *sql.DB, indexID int64) int64 {
	t.Helper()
	var at int64
	if err := db.QueryRow(`SELECT last_indexed FROM indexes WHERE id = ?`, indexID).Scan(&at); err != nil {
		t.Fatalf("read last_indexed: %v", err)
	}
	return at
}

// "Last indexed" on the settings page is when everything was last scanned.
// The updates that follow file operations and opened folders keep the index
// current in between and are not that.
func TestAnUpdateOfPartOfTheIndexKeepsTheTimeOfTheLastFullIndex(t *testing.T) {
	ctx, store, db, indexID, root := newRescanFixture(t)
	rescan(t, ctx, store, indexID, root)
	markLastFullIndex(t, db, indexID)
	check := func(what string) {
		t.Helper()
		if at := lastFullIndex(t, db, indexID); at != 1 {
			t.Fatalf("%s moved the time of the last full index to %d", what, at)
		}
	}

	writeTestFile(t, filepath.Join(root, "docs", "new.txt"), 4096)
	if changed := rescan(t, ctx, store, indexID, filepath.Join(root, "docs")); changed == 0 {
		t.Fatal("the reindex of the folder wrote nothing")
	}
	check("the reindex of a folder")

	writeTestFile(t, filepath.Join(root, "opened.txt"), 4096)
	if result := refreshFolder(t, ctx, store, indexID, root); result.Changed == 0 {
		t.Fatal("the refresh wrote nothing")
	}
	check("the refresh of an opened folder")

	added := filepath.Join(root, "added.txt")
	writeTestFile(t, added, 4096)
	if err := store.UpsertEntry(ctx, indexID, movedTo(t, added)); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	check("the update of one entry")

	if err := store.DeletePath(ctx, indexID, added); err != nil {
		t.Fatalf("delete: %v", err)
	}
	check("the removal of one entry")
}

func TestAFullIndexSetsTheTimeOfTheIndex(t *testing.T) {
	ctx, db, dbPath := setupTestDB(t)
	store := NewStoreWithDB(db, dbPath, indexing.SameFile)
	indexID := insertTestIndex(t, db)
	root := filepath.Join(t.TempDir(), "tree")
	writeTestFile(t, filepath.Join(root, "a.txt"), 4096)
	scanBelowRoot(t, ctx, store, indexID, root, "/")
	markLastFullIndex(t, db, indexID)

	// It finds nothing changed and writes no row, and is a full index still.
	started := time.Now().Unix()
	scanBelowRoot(t, ctx, store, indexID, root, "/")

	if at := lastFullIndex(t, db, indexID); at < started {
		t.Fatalf("the time of the last full index is %d after a full index that committed at %d", at, started)
	}
}
