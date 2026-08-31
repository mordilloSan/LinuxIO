package storage

import (
	"context"
	"fmt"
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
			INSERT INTO indexes (
				name, root_path, source, include_hidden,
				num_dirs, num_files, total_size, disk_used,
				disk_total, last_indexed
			) VALUES (?, ?, ?, 0, 0, 1, 1, 1, 1, ?);
		`, fmt.Sprintf("idx%d", i), "/tmp", "/tmp", int64(i))
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

	stats, err := PruneOldIndexes(ctx, db, 2, 0)
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
			INSERT INTO indexes (
				name, root_path, source, include_hidden,
				num_dirs, num_files, total_size, disk_used,
				disk_total, last_indexed
			) VALUES ('root', '/tmp', '/tmp', 0, 0, 1, 1, 1, 1, ?);
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

	stats, err := PruneOldIndexes(ctx, db, 1, 0)
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
