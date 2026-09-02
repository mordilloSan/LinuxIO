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

// PruneStats holds statistics about the pruning operation
type PruneStats struct {
	DeletedIndexes int
	DeletedEntries int64
	Duration       time.Duration
}

// PruneOldIndexes removes index records outside the requested retention count.
// This also cascades to delete all associated entries due to the FOREIGN KEY constraint.
// keepLatest specifies how many most recent indexes to always keep (minimum 1).
func PruneOldIndexes(ctx context.Context, db *sql.DB, keepLatest int) (PruneStats, error) {
	ctx = ensureContext(ctx)
	if db == nil {
		return PruneStats{}, fmt.Errorf("db is nil")
	}
	if keepLatest < 1 {
		keepLatest = 1
	}

	start := time.Now()
	var stats PruneStats

	// id DESC breaks last_indexed ties (second resolution) toward the newest
	// generation, matching LatestIndexID; without it two scans publishing in
	// the same second could prune the generation that was just published.
	where := `
		WHERE id NOT IN (
			SELECT id FROM indexes ORDER BY last_indexed DESC, id DESC LIMIT ?
		)
	`
	args := []any{keepLatest}

	// First, count entries that will be deleted (for stats)
	var entriesToDelete int64
	countQuery := `
		SELECT COALESCE(SUM(num_files + num_dirs), 0)
		FROM indexes
	` + where + `;`
	err := db.QueryRowContext(ctx, countQuery, args...).Scan(&entriesToDelete)
	if err != nil {
		return PruneStats{}, fmt.Errorf("count entries to delete: %w", err)
	}

	// Delete old indexes (entries will cascade delete). Large cascades would
	// fire the per-row FTS delete trigger for every entry, which is far
	// slower than rebuilding the FTS index from the surviving rows, so big
	// prunes bypass the triggers inside one transaction.
	deleteQuery := `DELETE FROM indexes ` + where + `;`
	var deleted int64
	bypassFTS := entriesToDelete > 50000
	if bypassFTS {
		if exists, ftsErr := ftsTableExists(ctx, db); ftsErr != nil || !exists {
			bypassFTS = false
		}
	}
	if bypassFTS {
		deleted, err = deleteIndexesWithFTSRebuild(ctx, db, deleteQuery, args)
		if err != nil {
			return PruneStats{}, err
		}
	} else {
		result, execErr := db.ExecContext(ctx, deleteQuery, args...)
		if execErr != nil {
			return PruneStats{}, fmt.Errorf("delete old indexes: %w", execErr)
		}
		deleted, err = result.RowsAffected()
		if err != nil {
			return PruneStats{}, fmt.Errorf("get rows affected: %w", err)
		}
	}

	stats.DeletedIndexes = int(deleted)
	stats.DeletedEntries = entriesToDelete
	stats.Duration = time.Since(start).Truncate(time.Millisecond)

	return stats, nil
}

// deleteIndexesWithFTSRebuild runs a cascading index delete with the FTS sync
// triggers dropped, then rebuilds entries_fts from the surviving entries.
// Everything happens in one transaction, so concurrent writers are blocked
// while the triggers are absent and a failure rolls the trigger DDL back too.
func deleteIndexesWithFTSRebuild(ctx context.Context, db *sql.DB, deleteQuery string, args []any) (int64, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			slog.Warn("deleteIndexesWithFTSRebuild rollback failed", "err", rollbackErr)
		}
	}()

	if dropErr := dropFTSTriggers(ctx, tx); dropErr != nil {
		return 0, dropErr
	}
	result, err := tx.ExecContext(ctx, deleteQuery, args...)
	if err != nil {
		return 0, fmt.Errorf("delete old indexes: %w", err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("get rows affected: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO entries_fts(entries_fts) VALUES ('rebuild');`); err != nil {
		return 0, fmt.Errorf("rebuild entries_fts after prune: %w", err)
	}
	if err := markFTSRebuilt(ctx, tx); err != nil {
		return 0, err
	}
	if err := createFTSTriggers(ctx, tx); err != nil {
		return 0, err
	}
	return deleted, tx.Commit()
}
