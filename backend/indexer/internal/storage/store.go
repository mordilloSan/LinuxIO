package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
)

var ErrDirectoryNotFound = errors.New("directory not found")
var ErrNotInitialized = errors.New("indexer is not initialized")

// SameFileFunc reports whether path currently names a non-directory with the
// given device and inode. Hardlink accounting uses it to ignore stored paths
// that no longer match the filesystem; the caller owns the filesystem access.
type SameFileFunc func(path string, device, inode int64) bool

// dbExecutor is an interface that both sql.DB and sql.Tx implement
type dbExecutor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Store owns the SQLite cache: generations, entries, queries and maintenance.
// Raw SQL and transactions stay behind its methods.
type Store struct {
	db       *sql.DB
	dbPath   string
	sameFile SameFileFunc
}

// NewStoreWithDB wraps an existing database handle. dbPath should be the
// actual SQLite file path (for size reporting). A nil sameFile trusts the
// stored hardlink identities, which suits read-only callers.
func NewStoreWithDB(db *sql.DB, dbPath string, sameFile SameFileFunc) *Store {
	if sameFile == nil {
		sameFile = func(string, int64, int64) bool { return true }
	}
	return &Store{db: db, dbPath: dbPath, sameFile: sameFile}
}

// Close closes the database handle.
func (s *Store) Close() error {
	return s.db.Close()
}

// JournalMode returns the SQLite journal mode of the database.
func (s *Store) JournalMode(ctx context.Context) (string, error) {
	var mode string
	if err := s.db.QueryRowContext(ctx, `PRAGMA journal_mode;`).Scan(&mode); err != nil {
		return "", fmt.Errorf("read journal mode: %w", err)
	}
	return mode, nil
}

// DatabaseSize returns the size in bytes of the database file, or 0 when it
// cannot be read.
func (s *Store) DatabaseSize() int64 {
	if s.dbPath == "" {
		return 0
	}
	fi, err := os.Stat(s.dbPath)
	if err != nil {
		return 0
	}
	return fi.Size()
}
