package storage

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenStoreRecoveringQuarantinesConfirmedCorruption(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "indexer.db")
	if err := os.WriteFile(dbPath, []byte("not a sqlite database"), 0o600); err != nil {
		t.Fatalf("write corrupt database: %v", err)
	}

	store, err := OpenStoreRecovering(context.Background(), dbPath, DefaultOpenOptions(), nil)
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

	store, err := OpenStoreRecovering(context.Background(), dbPath, DefaultOpenOptions(), nil)
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
	if store, err := OpenStoreRecovering(context.Background(), dbPath, DefaultOpenOptions(), nil); err == nil {
		_ = store.Close()
		t.Fatal("OpenStoreRecovering accepted a directory")
	}
	if info, err := os.Stat(dbPath); err != nil || !info.IsDir() {
		t.Fatalf("operational failure changed database path: info=%v err=%v", info, err)
	}
}
