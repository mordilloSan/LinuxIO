package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// Fixed SQLite connection settings. WAL allows concurrent readers while
// streaming writes happen; auto_vacuum=INCREMENTAL lets pruning reclaim
// deleted generations. stmtCacheSize is go-sqlite3's per-connection
// prepared statement cache.
const (
	busyTimeoutMillis = 5000
	stmtCacheSize     = 16
	maxOpenConns      = 5
	maxIdleConns      = 2
	connMaxIdleTime   = 5 * time.Minute
)

// OpenContext creates (or reuses) a SQLite database and ensures the schema exists, honoring ctx cancellation.
func OpenContext(parent context.Context, path string) (*sql.DB, error) {
	if parent == nil {
		parent = context.TODO()
	}
	if path == "" {
		path = defaultDBPath
	}

	// The path is URI-escaped (keeping '/' literal) inside a file: URI so that
	// reserved characters like '?' or '#' in filenames cannot be misparsed as
	// DSN parameters.
	escapedPath := strings.ReplaceAll(url.PathEscape(path), "%2F", "/")
	dsn := fmt.Sprintf("file:%s?_busy_timeout=%d&_foreign_keys=on&_journal_mode=WAL&_synchronous=OFF&_auto_vacuum=INCREMENTAL&_stmt_cache_size=%d",
		escapedPath, busyTimeoutMillis, stmtCacheSize)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(parent, schemaTimeout)
	defer cancel()

	var journalMode string
	if err := db.QueryRowContext(ctx, `PRAGMA journal_mode = WAL;`).Scan(&journalMode); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			slog.Warn("failed to close DB after journal mode setup error", "error", closeErr)
		}
		return nil, err
	}

	// Configure connection pool for WAL mode concurrent access
	// WAL mode allows multiple readers + 1 writer simultaneously
	db.SetMaxOpenConns(maxOpenConns)
	db.SetMaxIdleConns(maxIdleConns)
	db.SetConnMaxLifetime(0)
	db.SetConnMaxIdleTime(connMaxIdleTime)

	if err := initSchema(ctx, db); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			slog.Warn("failed to close DB after schema init error", "error", closeErr)
		}
		return nil, err
	}

	return db, nil
}

// OpenStore opens the cache at path. sameFile is the filesystem check the
// hardlink accounting needs; see SameFileFunc.
func OpenStore(ctx context.Context, path string, sameFile SameFileFunc) (*Store, error) {
	db, err := OpenContext(ctx, path)
	if err != nil {
		return nil, err
	}
	return NewStoreWithDB(db, path, sameFile), nil
}

// OpenStoreRecovering is OpenStore for the long-lived daemon. It replaces the
// rebuildable cache only when SQLite confirms corruption or the schema is
// incompatible. Operational failures (busy, timeout, and I/O errors) preserve
// the last good cache.
func OpenStoreRecovering(ctx context.Context, path string, sameFile SameFileFunc) (*Store, error) {
	db, err := OpenContext(ctx, path)
	if err != nil {
		var reason string
		switch {
		case IsCorruptionError(err):
			reason = "corrupt"
		case errors.Is(err, ErrIncompatibleSchema):
			reason = "incompatible"
		default:
			return nil, err
		}
		if err = quarantineDatabaseFiles(path, reason); err != nil {
			return nil, fmt.Errorf("quarantine %s database: %w", reason, err)
		}
		if db, err = OpenContext(ctx, path); err != nil {
			return nil, err
		}
	}
	return NewStoreWithDB(db, path, sameFile), nil
}

// quarantineDatabaseFiles renames a database and its sidecars aside instead of
// deleting them, so the files stay available for recovery or inspection.
func quarantineDatabaseFiles(dbPath, reason string) error {
	suffix := "." + reason + "-" + time.Now().UTC().Format("20060102T150405Z")
	if err := os.Rename(dbPath, dbPath+suffix); err != nil {
		return fmt.Errorf("failed to quarantine database: %w", err)
	}
	for _, sidecar := range []string{"-wal", "-shm"} {
		if err := os.Rename(dbPath+sidecar, dbPath+suffix+sidecar); err != nil && !errors.Is(err, os.ErrNotExist) {
			slog.Warn("failed to quarantine database sidecar", "path", dbPath+sidecar, "error", err)
		}
	}
	slog.Warn("database quarantined", "path", dbPath+suffix, "reason", reason)
	return nil
}
