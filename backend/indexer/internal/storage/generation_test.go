package storage

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func TestFullScanGenerationPublication(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "gen.db")
	db, err := OpenContext(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store := NewStoreWithDB(db, dbPath, nil)

	id1, err := store.BeginGeneration(ctx)
	if err != nil {
		t.Fatalf("begin first generation: %v", err)
	}
	if latest, latestErr := store.LatestIndexID(ctx); !errors.Is(latestErr, sql.ErrNoRows) {
		t.Fatalf("unpublished generation visible: %d, %v", latest, latestErr)
	}
	if _, latestErr := store.LatestGeneration(ctx); !errors.Is(latestErr, ErrNoGeneration) {
		t.Fatalf("unpublished generation metadata visible: %v", latestErr)
	}
	if publishErr := store.PublishGeneration(ctx, id1, 2, 3, 4096); publishErr != nil {
		t.Fatalf("publish first generation: %v", publishErr)
	}
	latest, err := store.LatestGeneration(ctx)
	if err != nil || latest.NumDirs != 2 || latest.NumFiles != 3 || latest.TotalSize != 4096 || latest.LastIndexed.IsZero() {
		t.Fatalf("latest generation = %+v, %v; want 2 dirs, 3 files, 4096 bytes", latest, err)
	}
	id2, err := store.BeginGeneration(ctx)
	if err != nil {
		t.Fatalf("begin second generation: %v", err)
	}
	if latestID, err := store.LatestIndexID(ctx); err != nil || latestID != id1 {
		t.Fatalf("latest during scan = %d, %v; want %d", latestID, err, id1)
	}
	if err := store.PublishGeneration(ctx, id2, 0, 0, 0); err != nil {
		t.Fatalf("publish second generation: %v", err)
	}
	if _, err := store.PruneOldIndexes(ctx, 1); err != nil {
		t.Fatalf("prune: %v", err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM indexes`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("generation count = %d, %v; want 1", count, err)
	}
}

func TestBeginGenerationDiscardsUnpublishedLeftovers(t *testing.T) {
	ctx, db, dbPath := setupTestDB(t)
	store := NewStoreWithDB(db, dbPath, nil)
	stale, err := store.BeginGeneration(ctx)
	if err != nil {
		t.Fatalf("begin stale generation: %v", err)
	}
	if _, err := store.BeginGeneration(ctx); err != nil {
		t.Fatalf("begin next generation: %v", err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM indexes WHERE id = ?`, stale).Scan(&count); err != nil || count != 0 {
		t.Fatalf("stale generation rows = %d, %v; want 0", count, err)
	}
}

func TestPruneOldIndexesCountOnly(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "index.db")
	db, openErr := OpenContext(context.Background(), dbPath)
	if openErr != nil {
		t.Fatalf("open db: %v", openErr)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("close db: %v", err)
		}
	}()

	for i := 1; i <= 4; i++ {
		result, err := db.ExecContext(ctx, `
			INSERT INTO indexes (num_dirs, num_files, total_size, last_indexed)
			VALUES (0, 1, 1, ?);
		`, int64(i))
		if err != nil {
			t.Fatalf("insert index %d: %v", i, err)
		}
		indexID, err := result.LastInsertId()
		if err != nil {
			t.Fatalf("last insert id: %v", err)
		}
		if _, err := db.ExecContext(ctx, `
			INSERT INTO entries (
				index_id, relative_path, path_depth, name, size,
				mod_time, type, hidden, inode, last_seen
			) VALUES (?, '/', 0, '/', 1, ?, 'directory', 0, 0, ?);
		`, indexID, int64(i), int64(i)); err != nil {
			t.Fatalf("insert entry %d: %v", i, err)
		}
	}

	stats, err := NewStoreWithDB(db, dbPath, nil).PruneOldIndexes(ctx, 2)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if stats.DeletedIndexes != 2 {
		t.Fatalf("deleted indexes = %d, want 2", stats.DeletedIndexes)
	}

	var remaining int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM indexes;`).Scan(&remaining); err != nil {
		t.Fatalf("count indexes: %v", err)
	}
	if remaining != 2 {
		t.Fatalf("remaining indexes = %d, want 2", remaining)
	}

	var oldestRemaining int64
	if err := db.QueryRowContext(ctx, `SELECT MIN(last_indexed) FROM indexes;`).Scan(&oldestRemaining); err != nil {
		t.Fatalf("min last_indexed: %v", err)
	}
	if oldestRemaining != 3 {
		t.Fatalf("oldest remaining last_indexed = %d, want 3", oldestRemaining)
	}
}

func TestPruneOldIndexesReclaimsFreelist(t *testing.T) {
	ctx, db, dbPath := setupTestDB(t)

	for generation := 1; generation <= 2; generation++ {
		result, execErr := db.ExecContext(ctx, `
			INSERT INTO indexes (num_files, last_indexed) VALUES (1000, ?);
		`, generation)
		if execErr != nil {
			t.Fatalf("insert index %d: %v", generation, execErr)
		}
		indexID, idErr := result.LastInsertId()
		if idErr != nil {
			t.Fatalf("read index %d id: %v", generation, idErr)
		}
		if _, execErr = db.ExecContext(ctx, `
			WITH RECURSIVE counter(value) AS (
				VALUES(1) UNION ALL SELECT value + 1 FROM counter WHERE value < 1000
			)
			INSERT INTO entries (index_id, relative_path, name, size, mod_time, type)
			SELECT ?, printf('/%04d-%s', value, hex(zeroblob(256))), printf('%04d', value), 1, 1, 'file'
			FROM counter;
		`, indexID); execErr != nil {
			t.Fatalf("insert entries for index %d: %v", generation, execErr)
		}
	}
	if _, err := NewStoreWithDB(db, dbPath, nil).PruneOldIndexes(ctx, 1); err != nil {
		t.Fatalf("prune: %v", err)
	}
	var freePages int64
	if err := db.QueryRowContext(ctx, `PRAGMA freelist_count;`).Scan(&freePages); err != nil {
		t.Fatalf("read freelist count: %v", err)
	}
	if freePages != 0 {
		t.Fatalf("freelist pages after prune = %d, want 0", freePages)
	}
}

// TestPruneKeepsNewestOnLastIndexedTie covers the second-resolution tie:
// two generations published within the same second must prune the OLDER row
// (lower id), matching LatestIndexID's `last_indexed DESC, id DESC` order.
// Without the id tiebreaker the keep window could retain the older row and
// delete the generation that was just published.
func TestPruneKeepsNewestOnLastIndexedTie(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "index.db")
	db, openErr := OpenContext(context.Background(), dbPath)
	if openErr != nil {
		t.Fatalf("open db: %v", openErr)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("close db: %v", err)
		}
	}()

	const sameSecond = int64(1700000000)
	var lastID int64
	for i := 1; i <= 2; i++ {
		result, err := db.ExecContext(ctx, `
			INSERT INTO indexes (num_dirs, num_files, total_size, last_indexed)
			VALUES (0, 1, 1, ?);
		`, sameSecond)
		if err != nil {
			t.Fatalf("insert index %d: %v", i, err)
		}
		if lastID, err = result.LastInsertId(); err != nil {
			t.Fatalf("last insert id: %v", err)
		}
	}

	store := NewStoreWithDB(db, dbPath, nil)
	latestBefore, err := store.LatestIndexID(ctx)
	if err != nil {
		t.Fatalf("latest before prune: %v", err)
	}
	if latestBefore != lastID {
		t.Fatalf("latest before prune = %d, want %d", latestBefore, lastID)
	}

	stats, err := store.PruneOldIndexes(ctx, 1)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if stats.DeletedIndexes != 1 {
		t.Fatalf("deleted indexes = %d, want 1", stats.DeletedIndexes)
	}

	latestAfter, err := store.LatestIndexID(ctx)
	if err != nil {
		t.Fatalf("latest after prune: %v", err)
	}
	if latestAfter != lastID {
		t.Fatalf("prune deleted the just-published generation: latest = %d, want %d", latestAfter, lastID)
	}
}

func TestBeginGenerationBypassesFTSTriggersForLargeLeftover(t *testing.T) {
	skipWithoutFTS(t)
	ctx, db, dbPath := setupTestDB(t)
	store := NewStoreWithDB(db, dbPath, nil)
	if _, err := db.ExecContext(ctx, `INSERT INTO indexes (last_indexed) VALUES (1), (0);`); err != nil {
		t.Fatalf("insert generations: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i <= ?)
		INSERT INTO entries (index_id, relative_path, name, size, mod_time, type)
		SELECT CASE WHEN i = 1 THEN 1 ELSE 2 END, '/file-' || i, 'file-' || i, 1, 1, 'file' FROM n;
	`, ftsPruneBypassEntries); err != nil {
		t.Fatalf("insert entries: %v", err)
	}

	if _, err := store.BeginGeneration(ctx); err != nil {
		t.Fatalf("begin generation: %v", err)
	}

	var entries, triggers, matches int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM entries;`).Scan(&entries); err != nil || entries != 1 {
		t.Fatalf("entries = %d, %v; want 1", entries, err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'trigger' AND name LIKE 'entries_fts%';`).Scan(&triggers); err != nil || triggers != len(ftsTriggers) {
		t.Fatalf("fts triggers = %d, %v; want %d", triggers, err, len(ftsTriggers))
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM entries_fts WHERE entries_fts MATCH '"file-1"';`).Scan(&matches); err != nil || matches != 1 {
		t.Fatalf("fts matches = %d, %v; want 1", matches, err)
	}
}
