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
