package storage

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOpenStoreRecoveringQuarantinesConfirmedCorruption(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "indexer.db")
	if err := os.WriteFile(dbPath, []byte("not a sqlite database"), 0o600); err != nil {
		t.Fatalf("write corrupt database: %v", err)
	}

	store, err := OpenStoreRecovering(context.Background(), dbPath, nil)
	if err != nil {
		t.Fatalf("OpenStoreRecovering: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	quarantined, err := filepath.Glob(dbPath + ".corrupt-*")
	if err != nil {
		t.Fatalf("glob quarantined database: %v", err)
	}
	if len(quarantined) != 1 {
		t.Fatalf("quarantined files = %v, want one", quarantined)
	}
}

func TestOpenStoreRecoveringQuarantinesIncompatibleSchema(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "indexer.db")
	legacy, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	if _, execErr := legacy.Exec(`CREATE TABLE indexes (id INTEGER PRIMARY KEY, name TEXT NOT NULL);`); execErr != nil {
		_ = legacy.Close()
		t.Fatalf("create legacy schema: %v", execErr)
	}
	if closeErr := legacy.Close(); closeErr != nil {
		t.Fatalf("close legacy db: %v", closeErr)
	}

	store, err := OpenStoreRecovering(context.Background(), dbPath, nil)
	if err != nil {
		t.Fatalf("OpenStoreRecovering: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	quarantined, err := filepath.Glob(dbPath + ".incompatible-*")
	if err != nil {
		t.Fatalf("glob quarantined database: %v", err)
	}
	if len(quarantined) != 1 {
		t.Fatalf("quarantined files = %v, want one", quarantined)
	}
}

func TestOpenStoreRecoveringPreservesOperationalFailure(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "database-directory")
	if err := os.Mkdir(dbPath, 0o700); err != nil {
		t.Fatalf("mkdir database path: %v", err)
	}
	if store, err := OpenStoreRecovering(context.Background(), dbPath, nil); err == nil {
		_ = store.Close()
		t.Fatal("OpenStoreRecovering accepted a directory")
	}
	if info, err := os.Stat(dbPath); err != nil || !info.IsDir() {
		t.Fatalf("operational failure changed database path: info=%v err=%v", info, err)
	}
}

func TestOpenStoreRecoveringKeepsOnlyTheNewestQuarantinedCopy(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "indexer.db")
	older := []string{
		dbPath + ".corrupt-20240101T000000Z",
		dbPath + ".corrupt-20240101T000000Z-wal",
		dbPath + ".incompatible-20250101T000000Z",
		dbPath + ".incompatible-20250101T000000Z-shm",
	}
	// Not LinuxIO's: another file, and a copy someone renamed to keep.
	unrelated := []string{
		filepath.Join(dir, "indexer.db.backup"),
		dbPath + ".corrupt-keep-for-bug-report",
		dbPath + ".corrupt-20240101T000000Z.notes",
	}
	for _, path := range append(append(older, unrelated...), dbPath) {
		if err := os.WriteFile(path, []byte("not a sqlite database"), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	store, err := OpenStoreRecovering(context.Background(), dbPath, nil)
	if err != nil {
		t.Fatalf("OpenStoreRecovering: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	for _, path := range older {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Errorf("older quarantined file %s is still there (stat error %v)", filepath.Base(path), statErr)
		}
	}
	quarantined, err := filepath.Glob(dbPath + ".corrupt-20[0-9]*Z")
	if err != nil {
		t.Fatalf("glob quarantined database: %v", err)
	}
	if len(quarantined) != 1 {
		t.Fatalf("quarantined files = %v, want only the copy just made", quarantined)
	}
	for _, path := range unrelated {
		if _, statErr := os.Stat(path); statErr != nil {
			t.Errorf("a file that is not a quarantined copy was removed: %v", statErr)
		}
	}
}

func TestQuarantineThatFailsKeepsTheEarlierCopy(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "indexer.db")
	earlier := dbPath + ".corrupt-20240101T000000Z"
	if err := os.WriteFile(earlier, []byte("earlier copy"), 0o600); err != nil {
		t.Fatalf("write earlier copy: %v", err)
	}

	// There is no database to set aside, so the rename fails.
	if err := quarantineDatabaseFiles(dbPath, "corrupt"); err == nil {
		t.Fatal("quarantine of a missing database reported success")
	}
	if _, statErr := os.Stat(earlier); statErr != nil {
		t.Fatalf("the earlier copy was deleted although no new one was made: %v", statErr)
	}
}

func TestAQueryOnANewConnectionDoesNotWaitForAWriteInProgress(t *testing.T) {
	ctx := context.Background()
	db, err := OpenContext(ctx, filepath.Join(t.TempDir(), "indexer.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// A reindex holds one write transaction for as long as it walks.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `INSERT INTO indexes (last_indexed) VALUES (0)`); err != nil {
		t.Fatalf("write: %v", err)
	}

	// A search that needs a connection the pool does not have yet must not
	// queue behind it: opening a connection takes no write lock.
	for i := range 3 {
		queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		conn, err := db.Conn(queryCtx)
		if err != nil {
			cancel()
			t.Fatalf("connection %d: %v", i, err)
		}
		defer conn.Close()
		var generations int
		err = conn.QueryRowContext(queryCtx, `SELECT COUNT(*) FROM indexes`).Scan(&generations)
		cancel()
		if err != nil {
			t.Fatalf("query on new connection %d while a write is in progress: %v", i, err)
		}
	}
}

// The indexer is a root daemon and loads no SQLite extension, so the build
// leaves the feature out.
func TestTheBuildCannotLoadSQLiteExtensions(t *testing.T) {
	db, err := OpenContext(context.Background(), filepath.Join(t.TempDir(), "indexer.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`SELECT load_extension('/nonexistent')`)
	if err == nil || !strings.Contains(err.Error(), "no such function") {
		t.Fatalf("load_extension answered %v, want the function to be absent", err)
	}
}
