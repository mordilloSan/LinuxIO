package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	sqlite3 "github.com/mattn/go-sqlite3"
)

// TestMigrateIndexesDropUniqueName verifies that a legacy database with the
// UNIQUE(name) constraint is rebuilt on open: ids and entries survive, and
// multiple generations per name become insertable.
func TestMigrateIndexesDropUniqueName(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	raw, err := sql.Open("sqlite3", dbPath+"?_foreign_keys=on")
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	legacySchema := `
		CREATE TABLE indexes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			root_path TEXT NOT NULL,
			source TEXT,
			include_hidden INTEGER NOT NULL DEFAULT 0,
			num_dirs INTEGER NOT NULL DEFAULT 0,
			num_files INTEGER NOT NULL DEFAULT 0,
			total_size INTEGER NOT NULL DEFAULT 0,
			disk_used INTEGER NOT NULL DEFAULT 0,
			disk_total INTEGER NOT NULL DEFAULT 0,
			last_indexed INTEGER NOT NULL,
			index_duration_ms INTEGER NOT NULL DEFAULT 0,
			export_duration_ms INTEGER NOT NULL DEFAULT 0,
			vacuum_duration_ms INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now'))
		);
		CREATE TABLE entries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			index_id INTEGER NOT NULL,
			relative_path TEXT NOT NULL,
			path_depth INTEGER NOT NULL DEFAULT 0,
			name TEXT NOT NULL,
			size INTEGER NOT NULL,
			mod_time INTEGER NOT NULL,
			type TEXT NOT NULL,
			hidden INTEGER NOT NULL DEFAULT 0,
			inode INTEGER NOT NULL DEFAULT 0,
			last_seen INTEGER NOT NULL DEFAULT 0,
			FOREIGN KEY (index_id) REFERENCES indexes(id) ON DELETE CASCADE
		);
		INSERT INTO indexes (id, name, root_path, last_indexed) VALUES (7, 'root', '/', 1234);
		INSERT INTO entries (index_id, relative_path, name, size, mod_time, type)
			VALUES (7, '/keep.txt', 'keep.txt', 1, 1234, 'file');
	`
	if _, execErr := raw.Exec(legacySchema); execErr != nil {
		t.Fatalf("create legacy schema: %v", execErr)
	}
	if closeErr := raw.Close(); closeErr != nil {
		t.Fatalf("close raw: %v", closeErr)
	}

	db, err := Open(dbPath, DefaultOpenOptions())
	if err != nil {
		t.Fatalf("open with migration: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	// UNIQUE constraint must be gone: same name insertable twice.
	if _, err := db.Exec(`INSERT INTO indexes (name, root_path, last_indexed) VALUES ('root', '/', 0)`); err != nil {
		t.Fatalf("duplicate name insert should succeed after migration: %v", err)
	}

	// Original row keeps its id and the FK-linked entry survived.
	var name string
	if err := db.QueryRow(`SELECT name FROM indexes WHERE id = 7`).Scan(&name); err != nil || name != "root" {
		t.Fatalf("legacy row not preserved: name=%q err=%v", name, err)
	}
	var entryCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM entries WHERE index_id = 7`).Scan(&entryCount); err != nil || entryCount != 1 {
		t.Fatalf("legacy entries not preserved: count=%d err=%v", entryCount, err)
	}

	// FK cascade must still work against the rebuilt parent table.
	if _, err := db.Exec(`DELETE FROM indexes WHERE id = 7`); err != nil {
		t.Fatalf("delete legacy index: %v", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM entries WHERE index_id = 7`).Scan(&entryCount); err != nil || entryCount != 0 {
		t.Fatalf("cascade delete broken after migration: count=%d err=%v", entryCount, err)
	}
}

func TestIsCorruptionError(t *testing.T) {
	if !IsCorruptionError(sqlite3.Error{Code: sqlite3.ErrCorrupt}) {
		t.Fatal("ErrCorrupt should classify as corruption")
	}
	if !IsCorruptionError(fmt.Errorf("wrapped: %w", sqlite3.Error{Code: sqlite3.ErrNotADB})) {
		t.Fatal("wrapped ErrNotADB should classify as corruption")
	}
	if IsCorruptionError(sqlite3.Error{Code: sqlite3.ErrBusy}) {
		t.Fatal("ErrBusy is operational, not corruption")
	}
	if IsCorruptionError(errors.New("disk I/O error")) {
		t.Fatal("plain errors are not corruption")
	}
}
