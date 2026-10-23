package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ErrIncompatibleSchema means the cache belongs to a different schema version.
var ErrIncompatibleSchema = errors.New("incompatible database schema")

func initSchema(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys = ON;`); err != nil {
		return err
	}
	if err := validateSchemaVersion(ctx, db); err != nil {
		return err
	}

	// Every full scan inserts a new generation with last_indexed=0 and publishes it by
	// setting last_indexed on success, so readers atomically switch from the
	// previous generation and a failed scan leaves the old one untouched.
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS indexes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			num_dirs INTEGER NOT NULL DEFAULT 0,
			num_files INTEGER NOT NULL DEFAULT 0,
			total_size INTEGER NOT NULL DEFAULT 0,
			last_indexed INTEGER NOT NULL,
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now'))
		);
	`); err != nil {
		return err
	}

	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS entries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			index_id INTEGER NOT NULL,
			relative_path TEXT NOT NULL,
			path_depth INTEGER NOT NULL DEFAULT 0,
			name TEXT NOT NULL,
			size INTEGER NOT NULL,
			mod_time INTEGER NOT NULL,
			type TEXT NOT NULL,
			hidden INTEGER NOT NULL DEFAULT 0,
			device INTEGER NOT NULL DEFAULT 0,
			inode INTEGER NOT NULL DEFAULT 0,
			size_contribution INTEGER NOT NULL DEFAULT 0,
			last_seen INTEGER NOT NULL DEFAULT 0,
			FOREIGN KEY (index_id) REFERENCES indexes(id) ON DELETE CASCADE
		);
	`); err != nil {
		return err
	}

	if _, err := db.ExecContext(ctx, `
		CREATE INDEX IF NOT EXISTS idx_entries_index_id ON entries(index_id);
	`); err != nil {
		return err
	}

	if _, err := db.ExecContext(ctx, `
		CREATE UNIQUE INDEX IF NOT EXISTS idx_entries_path ON entries(index_id, relative_path);
	`); err != nil {
		return err
	}

	if _, err := db.ExecContext(ctx, `
		CREATE INDEX IF NOT EXISTS idx_entries_subfolders ON entries(index_id, type, path_depth, relative_path);
	`); err != nil {
		return err
	}

	if _, err := db.ExecContext(ctx, `
		CREATE INDEX IF NOT EXISTS idx_entries_hardlinks ON entries(index_id, device, inode);
	`); err != nil {
		return err
	}

	if err := ensureFTS(ctx, db); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d;`, schemaVersion))
	return err
}

func validateSchemaVersion(ctx context.Context, db *sql.DB) error {
	var version int
	if err := db.QueryRowContext(ctx, `PRAGMA user_version;`).Scan(&version); err != nil {
		return fmt.Errorf("read database schema version: %w", err)
	}
	if version == schemaVersion {
		return nil
	}
	if version != 0 {
		return fmt.Errorf("%w: got %d, want %d", ErrIncompatibleSchema, version, schemaVersion)
	}

	var tables int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sqlite_schema
		WHERE type = 'table' AND name NOT LIKE 'sqlite_%';
	`).Scan(&tables); err != nil {
		return fmt.Errorf("inspect unversioned database schema: %w", err)
	}
	if tables != 0 {
		return fmt.Errorf("%w: existing database is unversioned", ErrIncompatibleSchema)
	}
	return nil
}
