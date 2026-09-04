package storage

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseSQLiteMemorySucceeds(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "index.db"), DefaultOpenOptions())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			t.Errorf("close db: %v", closeErr)
		}
	}()

	if err := ReleaseSQLiteMemory(context.Background(), db); err != nil {
		t.Fatalf("ReleaseSQLiteMemory: %v", err)
	}
}

func TestReleaseSQLiteMemoryReportsFailures(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "index.db"), DefaultOpenOptions())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if closeErr := db.Close(); closeErr != nil {
		t.Fatalf("close db: %v", closeErr)
	}

	err = ReleaseSQLiteMemory(context.Background(), db)
	if err == nil {
		t.Fatal("ReleaseSQLiteMemory returned nil for a closed database")
	}
	for _, operation := range []string{"shrink SQLite memory", "optimize SQLite database"} {
		if !strings.Contains(err.Error(), operation) {
			t.Fatalf("ReleaseSQLiteMemory error %q does not mention %q", err, operation)
		}
	}
}

func TestReleaseSQLiteMemoryRejectsNilDatabase(t *testing.T) {
	if err := ReleaseSQLiteMemory(context.Background(), nil); err == nil {
		t.Fatal("ReleaseSQLiteMemory returned nil for a nil database")
	}
}

func TestPruneOldIndexesCountOnly(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "index.db")
	db, openErr := Open(dbPath, DefaultOpenOptions())
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

	stats, err := PruneOldIndexes(ctx, db, 2)
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
	ctx, db, _ := setupTestDB(t)

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
	if _, err := PruneOldIndexes(ctx, db, 1); err != nil {
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
	db, openErr := Open(dbPath, DefaultOpenOptions())
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

	store := NewStoreWithDB(db, dbPath)
	latestBefore, err := store.LatestIndexID(ctx)
	if err != nil {
		t.Fatalf("latest before prune: %v", err)
	}
	if latestBefore != lastID {
		t.Fatalf("latest before prune = %d, want %d", latestBefore, lastID)
	}

	stats, err := PruneOldIndexes(ctx, db, 1)
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
