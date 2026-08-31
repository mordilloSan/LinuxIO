package daemon

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/indexer/storage"
)

func TestFullScanGenerationPublication(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "gen.db")
	db, err := storage.Open(dbPath, storage.DefaultOpenOptions())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store := storage.NewStoreWithDB(db, dbPath)

	id1, err := prepareIndexRecord(ctx, db)
	if err != nil {
		t.Fatalf("prepare first generation: %v", err)
	}
	if latest, latestErr := store.LatestIndexID(ctx); !errors.Is(latestErr, sql.ErrNoRows) {
		t.Fatalf("unpublished generation visible: %d, %v", latest, latestErr)
	}
	if _, execErr := db.Exec(`UPDATE indexes SET last_indexed = 1 WHERE id = ?`, id1); execErr != nil {
		t.Fatalf("publish first generation: %v", execErr)
	}
	id2, err := prepareIndexRecord(ctx, db)
	if err != nil {
		t.Fatalf("prepare second generation: %v", err)
	}
	if latest, err := store.LatestIndexID(ctx); err != nil || latest != id1 {
		t.Fatalf("latest during scan = %d, %v; want %d", latest, err, id1)
	}
	if _, err := db.Exec(`UPDATE indexes SET last_indexed = 2 WHERE id = ?`, id2); err != nil {
		t.Fatalf("publish second generation: %v", err)
	}
	if _, err := storage.PruneOldIndexes(ctx, db, 1); err != nil {
		t.Fatalf("prune: %v", err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM indexes`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("generation count = %d, %v; want 1", count, err)
	}
}

func TestOpenDaemonDatabaseQuarantinesConfirmedCorruption(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "indexer.db")
	if err := os.WriteFile(dbPath, []byte("not a sqlite database"), 0o600); err != nil {
		t.Fatalf("write corrupt database: %v", err)
	}

	db, err := openDaemonDatabase(dbPath, storage.DefaultOpenOptions())
	if err != nil {
		t.Fatalf("openDaemonDatabase: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	quarantined, err := filepath.Glob(dbPath + ".corrupt-*")
	if err != nil {
		t.Fatalf("glob quarantined database: %v", err)
	}
	if len(quarantined) != 1 {
		t.Fatalf("quarantined files = %v, want one", quarantined)
	}
}

func TestOpenDaemonDatabaseQuarantinesIncompatibleSchema(t *testing.T) {
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

	db, err := openDaemonDatabase(dbPath, storage.DefaultOpenOptions())
	if err != nil {
		t.Fatalf("openDaemonDatabase: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	quarantined, err := filepath.Glob(dbPath + ".incompatible-*")
	if err != nil {
		t.Fatalf("glob quarantined database: %v", err)
	}
	if len(quarantined) != 1 {
		t.Fatalf("quarantined files = %v, want one", quarantined)
	}
}

func TestOpenDaemonDatabasePreservesOperationalFailure(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "database-directory")
	if err := os.Mkdir(dbPath, 0o700); err != nil {
		t.Fatalf("mkdir database path: %v", err)
	}
	if db, err := openDaemonDatabase(dbPath, storage.DefaultOpenOptions()); err == nil {
		_ = db.Close()
		t.Fatal("openDaemonDatabase accepted a directory")
	}
	if info, err := os.Stat(dbPath); err != nil || !info.IsDir() {
		t.Fatalf("operational failure changed database path: info=%v err=%v", info, err)
	}
}
