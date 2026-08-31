package daemon

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/configfile"
	"github.com/mordilloSan/LinuxIO/backend/indexer/storage"
)

// TestFreshScanGenerations verifies the atomic-publish lifecycle: a fresh
// scan's generation row stays invisible to readers until published, the
// previous generation serves reads meanwhile, crashed-scan debris is cleaned
// up, and retention keeps only the newest published generation.
func TestFreshScanGenerations(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "gen.db")
	db, openErr := storage.Open(dbPath, storage.DefaultOpenOptions())
	if openErr != nil {
		t.Fatalf("open db: %v", openErr)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			t.Fatalf("close db: %v", closeErr)
		}
	}()
	store := storage.NewStoreWithDB(db, dbPath)

	prep := func(fresh bool) int64 { return prepGeneration(t, ctx, db, fresh) }
	publish := func(id, ts int64) { publishGeneration(t, db, id, ts) }
	latestID := func() int64 { return mustLatestID(t, ctx, store) }
	countRows := func(query string) int { return countGenerationRows(t, db, query) }

	// A fresh scan's generation is invisible until published.
	id1 := prep(true)
	if _, noneErr := store.LatestIndexID(ctx); !errors.Is(noneErr, sql.ErrNoRows) {
		t.Fatalf("unpublished generation should be invisible, got err=%v", noneErr)
	}
	publish(id1, 1000)
	if got := latestID(); got != id1 {
		t.Fatalf("latest = %d, want %d", got, id1)
	}

	// A second fresh scan gets a new row; readers keep seeing the old one.
	id2 := prep(true)
	if id2 == id1 {
		t.Fatalf("fresh scan must use a new generation row, got same id %d", id1)
	}
	if got := latestID(); got != id1 {
		t.Fatalf("readers should still see %d during scan, got %d", id1, got)
	}
	publish(id2, 2000)
	if got := latestID(); got != id2 {
		t.Fatalf("after publish, latest should be %d, got %d", id2, got)
	}

	// Crashed-scan debris (unpublished rows) is removed by the next fresh scan.
	prep(true)
	prep(true)
	if n := countRows(`SELECT COUNT(*) FROM indexes WHERE last_indexed = 0`); n != 1 {
		t.Fatalf("expected exactly 1 in-progress generation after cleanup, got %d", n)
	}

	// Non-fresh reuses the newest published generation instead of creating one.
	if reused := prep(false); reused != id2 {
		t.Fatalf("non-fresh should reuse published generation %d, got %d", id2, reused)
	}

	// Retention keeps the single newest published generation.
	if _, pruneErr := storage.PruneOldIndexes(ctx, db, 1, 0); pruneErr != nil {
		t.Fatalf("prune: %v", pruneErr)
	}
	if n := countRows(`SELECT COUNT(*) FROM indexes`); n != 1 {
		t.Fatalf("expected 1 generation after prune, got %d", n)
	}
	if got := latestID(); got != id2 {
		t.Fatalf("prune must keep the published generation %d, got %d", id2, got)
	}
}

func TestRunIndexModeAutomaticRetention(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("content"), 0o600); err != nil {
		t.Fatalf("write indexed file: %v", err)
	}
	dbPath := filepath.Join(t.TempDir(), "retention.db")
	opts := storage.DefaultOpenOptions()

	for range 2 {
		if _, err := RunIndexMode("retention", root, nil, true, false, true, dbPath, 0, configfile.DefaultIntegrityCheck, opts, nil); err != nil {
			t.Fatalf("run index without automatic retention: %v", err)
		}
	}
	if got := countPublishedGenerations(t, dbPath); got != 2 {
		t.Fatalf("published generations with keep_indexes=0 = %d, want 2", got)
	}

	if _, err := RunIndexMode("retention", root, nil, true, false, true, dbPath, 1, configfile.DefaultIntegrityCheck, opts, nil); err != nil {
		t.Fatalf("run index with automatic retention: %v", err)
	}
	if got := countPublishedGenerations(t, dbPath); got != 1 {
		t.Fatalf("published generations with keep_indexes=1 = %d, want 1", got)
	}
}

func TestRunIndexModeReportsDeletedEntries(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "deleted.txt")
	if err := os.WriteFile(filePath, []byte("content"), 0o600); err != nil {
		t.Fatalf("write indexed file: %v", err)
	}
	dbPath := filepath.Join(t.TempDir(), "deleted.db")
	opts := storage.DefaultOpenOptions()

	if _, err := RunIndexMode("deleted", root, nil, true, false, true, dbPath, 0, configfile.DefaultIntegrityCheck, opts, nil); err != nil {
		t.Fatalf("initial index: %v", err)
	}
	if err := os.Remove(filePath); err != nil {
		t.Fatalf("remove indexed file: %v", err)
	}
	stats, err := RunIndexMode("deleted", root, nil, true, false, false, dbPath, 0, configfile.DefaultIntegrityCheck, opts, nil)
	if err != nil {
		t.Fatalf("incremental index: %v", err)
	}
	if stats.DeletedEntries != 1 {
		t.Fatalf("DeletedEntries = %d, want 1", stats.DeletedEntries)
	}
}

func countPublishedGenerations(t *testing.T, dbPath string) int {
	t.Helper()
	db, err := storage.Open(dbPath, storage.DefaultOpenOptions())
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			t.Fatalf("close database: %v", closeErr)
		}
	}()

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM indexes WHERE last_indexed > 0`).Scan(&count); err != nil {
		t.Fatalf("count published generations: %v", err)
	}
	return count
}

func prepGeneration(t *testing.T, ctx context.Context, db *sql.DB, fresh bool) int64 {
	t.Helper()
	id, err := prepareIndexRecord(ctx, db, "root", "/", false, fresh)
	if err != nil {
		t.Fatalf("prepare index record (fresh=%v): %v", fresh, err)
	}
	return id
}

func publishGeneration(t *testing.T, db *sql.DB, id, ts int64) {
	t.Helper()
	if _, err := db.Exec(`UPDATE indexes SET last_indexed = ? WHERE id = ?`, ts, id); err != nil {
		t.Fatalf("publish generation %d: %v", id, err)
	}
}

func mustLatestID(t *testing.T, ctx context.Context, store *storage.Store) int64 {
	t.Helper()
	id, err := store.LatestIndexID(ctx)
	if err != nil {
		t.Fatalf("latest index id: %v", err)
	}
	return id
}

func countGenerationRows(t *testing.T, db *sql.DB, query string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query).Scan(&n); err != nil {
		t.Fatalf("count query: %v", err)
	}
	return n
}
