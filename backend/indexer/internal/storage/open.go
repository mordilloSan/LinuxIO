package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type OpenOptions struct {
	BusyTimeout     time.Duration
	JournalMode     string
	Synchronous     string
	AutoVacuum      string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxIdleTime time.Duration
	// StmtCacheSize is the number of prepared statements the driver caches
	// per connection (go-sqlite3 _stmt_cache_size); 0 disables the cache.
	StmtCacheSize int
}

func NormalizeOpenOptions(opts OpenOptions) (OpenOptions, error) {
	defaults := DefaultOpenOptions()
	if opts.JournalMode == "" {
		opts.JournalMode = defaults.JournalMode
	}
	if opts.Synchronous == "" {
		opts.Synchronous = defaults.Synchronous
	}
	if opts.AutoVacuum == "" {
		opts.AutoVacuum = defaults.AutoVacuum
	}

	if opts.BusyTimeout < 0 {
		return OpenOptions{}, fmt.Errorf("db busy timeout must be non-negative")
	}
	if opts.MaxOpenConns < 0 {
		return OpenOptions{}, fmt.Errorf("db max open conns must be non-negative")
	}
	if opts.MaxIdleConns < 0 {
		return OpenOptions{}, fmt.Errorf("db max idle conns must be non-negative")
	}
	if opts.ConnMaxIdleTime < 0 {
		return OpenOptions{}, fmt.Errorf("db conn max idle time must be non-negative")
	}
	if opts.StmtCacheSize < 0 {
		return OpenOptions{}, fmt.Errorf("db stmt cache size must be non-negative")
	}
	opts.JournalMode = strings.ToUpper(strings.TrimSpace(opts.JournalMode))
	opts.Synchronous = strings.ToUpper(strings.TrimSpace(opts.Synchronous))
	opts.AutoVacuum = strings.ToUpper(strings.TrimSpace(opts.AutoVacuum))
	if !slices.Contains([]string{"DELETE", "TRUNCATE", "PERSIST", "MEMORY", "WAL", "OFF"}, opts.JournalMode) {
		return OpenOptions{}, fmt.Errorf("invalid db journal mode %q", opts.JournalMode)
	}
	if !slices.Contains([]string{"OFF", "NORMAL", "FULL", "EXTRA"}, opts.Synchronous) {
		return OpenOptions{}, fmt.Errorf("invalid db synchronous %q", opts.Synchronous)
	}
	if !slices.Contains([]string{"NONE", "FULL", "INCREMENTAL"}, opts.AutoVacuum) {
		return OpenOptions{}, fmt.Errorf("invalid db auto vacuum %q", opts.AutoVacuum)
	}
	return opts, nil
}

// OpenContext creates (or reuses) a SQLite database with the supplied options
// and ensures the schema exists, honoring ctx cancellation.
func OpenContext(parent context.Context, path string, opts OpenOptions) (*sql.DB, error) {
	if parent == nil {
		parent = context.TODO()
	}
	if path == "" {
		path = defaultDBPath
	}
	if opts == (OpenOptions{}) {
		opts = DefaultOpenOptions()
	}
	opts, err := NormalizeOpenOptions(opts)
	if err != nil {
		return nil, err
	}

	// Use WAL for concurrent readers while streaming writes happen.
	// auto_vacuum=INCREMENTAL allows pruning to reclaim deleted generations.
	// The path is URI-escaped (keeping '/' literal) inside a file: URI so that
	// reserved characters like '?' or '#' in filenames cannot be misparsed as
	// DSN parameters.
	escapedPath := strings.ReplaceAll(url.PathEscape(path), "%2F", "/")
	dsn := fmt.Sprintf("file:%s?_busy_timeout=%d&_foreign_keys=on&_journal_mode=%s&_synchronous=%s&_auto_vacuum=%s&_stmt_cache_size=%d",
		escapedPath,
		int(opts.BusyTimeout/time.Millisecond),
		opts.JournalMode,
		opts.Synchronous,
		opts.AutoVacuum,
		opts.StmtCacheSize,
	)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(parent, schemaTimeout)
	defer cancel()

	var journalMode string
	if err := db.QueryRowContext(ctx, `PRAGMA journal_mode = `+opts.JournalMode+`;`).Scan(&journalMode); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			slog.Warn("failed to close DB after journal mode setup error", "err", closeErr)
		}
		return nil, err
	}

	// Configure connection pool for WAL mode concurrent access
	// WAL mode allows multiple readers + 1 writer simultaneously
	db.SetMaxOpenConns(opts.MaxOpenConns)
	db.SetMaxIdleConns(opts.MaxIdleConns)
	db.SetConnMaxLifetime(0)
	db.SetConnMaxIdleTime(opts.ConnMaxIdleTime)

	if err := initSchema(ctx, db); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			slog.Warn("failed to close DB after schema init error", "err", closeErr)
		}
		return nil, err
	}

	return db, nil
}

// OpenStore opens the cache at path. sameFile is the filesystem check the
// hardlink accounting needs; see SameFileFunc.
func OpenStore(ctx context.Context, path string, opts OpenOptions, sameFile SameFileFunc) (*Store, error) {
	db, err := OpenContext(ctx, path, opts)
	if err != nil {
		return nil, err
	}
	return NewStoreWithDB(db, path, sameFile), nil
}

// OpenStoreRecovering is OpenStore for the long-lived daemon. It replaces the
// rebuildable cache only when SQLite confirms corruption or the schema is
// incompatible. Operational failures (busy, timeout, and I/O errors) preserve
// the last good cache.
func OpenStoreRecovering(ctx context.Context, path string, opts OpenOptions, sameFile SameFileFunc) (*Store, error) {
	db, err := OpenContext(ctx, path, opts)
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
		if db, err = OpenContext(ctx, path, opts); err != nil {
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
			slog.Warn("failed to quarantine database sidecar", "path", dbPath+sidecar, "err", err)
		}
	}
	slog.Warn("database quarantined", "path", dbPath+suffix, "reason", reason)
	return nil
}
