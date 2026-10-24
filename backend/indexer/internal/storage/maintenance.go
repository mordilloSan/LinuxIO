package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	sqlite3 "github.com/mattn/go-sqlite3"
)

// IsCorruptionError reports whether err is a SQLite corruption-class error
// (corrupt database image or not a database file), as opposed to an
// operational failure such as I/O errors or a busy database. Callers use this
// to decide whether a database should be recreated versus the operation
// simply retried.
func IsCorruptionError(err error) bool {
	if serr, ok := errors.AsType[sqlite3.Error](err); ok {
		return serr.Code == sqlite3.ErrCorrupt || serr.Code == sqlite3.ErrNotADB
	}
	return false
}

type WALCheckpointStats struct {
	Busy         int
	Log          int
	Checkpointed int
	Duration     time.Duration
}

// WALCheckpointTruncate checkpoints the WAL and truncates the -wal file.
// This helps prevent unbounded WAL growth in long-running processes.
func WALCheckpointTruncate(ctx context.Context, db *sql.DB) (WALCheckpointStats, error) {
	ctx = ensureContext(ctx)
	if db == nil {
		return WALCheckpointStats{}, fmt.Errorf("db is nil")
	}

	start := time.Now()
	var stats WALCheckpointStats
	err := db.QueryRowContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE);`).Scan(&stats.Busy, &stats.Log, &stats.Checkpointed)
	stats.Duration = time.Since(start).Truncate(time.Millisecond)
	if err != nil {
		return WALCheckpointStats{}, err
	}
	return stats, nil
}

// incrementalVacuum reclaims the whole freelist and truncates the file.
// The pragma must be drained: Exec only steps once and frees one page.
func incrementalVacuum(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `PRAGMA incremental_vacuum;`)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			slog.Warn("incremental_vacuum rows close failed", "error", closeErr)
		}
	}()
	for rows.Next() {
	}
	return rows.Err()
}

// ReleaseSQLiteMemory forces SQLite to release cached memory.
// Call this after heavy write operations to return memory to the OS.
func ReleaseSQLiteMemory(ctx context.Context, db *sql.DB) error {
	ctx = ensureContext(ctx)
	if db == nil {
		return fmt.Errorf("db is nil")
	}

	var releaseErr error
	// Shrink SQLite's page cache
	if _, err := db.ExecContext(ctx, `PRAGMA shrink_memory;`); err != nil {
		releaseErr = errors.Join(releaseErr, fmt.Errorf("shrink SQLite memory: %w", err))
	}

	// Optimize database (lightweight, doesn't rebuild like VACUUM)
	if _, err := db.ExecContext(ctx, `PRAGMA optimize;`); err != nil {
		releaseErr = errors.Join(releaseErr, fmt.Errorf("optimize SQLite database: %w", err))
	}

	return releaseErr
}

// CheckpointAndRelease checkpoints the WAL and returns SQLite's cached memory
// after a heavy write. Failures are logged, not returned: the write itself
// already succeeded. operation names the write for the log.
func (s *Store) CheckpointAndRelease(ctx context.Context, operation string) {
	if stats, err := WALCheckpointTruncate(ctx, s.db); err != nil {
		slog.Warn("wal checkpoint failed", "operation", operation, "error", err)
	} else {
		slog.Debug("wal checkpoint complete", "operation", operation, "duration", stats.Duration, "busy", stats.Busy, "log", stats.Log, "checkpointed", stats.Checkpointed)
	}
	if err := ReleaseSQLiteMemory(ctx, s.db); err != nil {
		slog.Warn("failed to release sqlite memory", "operation", operation, "error", err)
	}
}
