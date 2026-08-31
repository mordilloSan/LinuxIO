package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	sqlite3 "github.com/mattn/go-sqlite3"
)

// ErrDBStatUnavailable indicates the binary was built without the
// sqlite_dbstat build tag, so the DBSTAT virtual table is not compiled in.
var ErrDBStatUnavailable = errors.New("dbstat virtual table unavailable (build with -tags sqlite_dbstat)")

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

// TableDiskUsage reports DBSTAT page usage aggregated per table or index.
type TableDiskUsage struct {
	Name        string
	Pages       int64
	Bytes       int64
	UnusedBytes int64
}

// DatabaseDiskUsage returns per-table/index disk usage from the DBSTAT
// virtual table, largest first. The scan reads every page of every btree,
// so reserve it for maintenance operations such as post-vacuum reporting.
func DatabaseDiskUsage(ctx context.Context, db *sql.DB) ([]TableDiskUsage, error) {
	ctx = ensureContext(ctx)
	if db == nil {
		return nil, fmt.Errorf("db is nil")
	}

	rows, err := db.QueryContext(ctx, `
		SELECT name, COUNT(*), SUM(pgsize), SUM(unused)
		FROM dbstat
		GROUP BY name
		ORDER BY SUM(pgsize) DESC;
	`)
	if err != nil {
		if strings.Contains(err.Error(), "no such table: dbstat") {
			return nil, ErrDBStatUnavailable
		}
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			slog.Warn("failed to close dbstat rows", "err", closeErr)
		}
	}()

	var usage []TableDiskUsage
	for rows.Next() {
		var t TableDiskUsage
		if err := rows.Scan(&t.Name, &t.Pages, &t.Bytes, &t.UnusedBytes); err != nil {
			return nil, err
		}
		usage = append(usage, t)
	}
	return usage, rows.Err()
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

type VacuumStats struct {
	Duration time.Duration
}

// Vacuum rebuilds the SQLite database file to reclaim free space and defragment pages.
// Note: VACUUM requires an exclusive lock and can be slow on large databases.
func Vacuum(ctx context.Context, db *sql.DB) (VacuumStats, error) {
	ctx = ensureContext(ctx)
	if db == nil {
		return VacuumStats{}, fmt.Errorf("db is nil")
	}

	start := time.Now()
	if _, err := db.ExecContext(ctx, `VACUUM;`); err != nil {
		return VacuumStats{}, err
	}
	return VacuumStats{Duration: time.Since(start).Truncate(time.Millisecond)}, nil
}

// PruneStats holds statistics about the pruning operation
type PruneStats struct {
	DeletedIndexes int
	DeletedEntries int64
	Duration       time.Duration
}

// PruneOldIndexes removes index records outside the requested retention window.
// This also cascades to delete all associated entries due to the FOREIGN KEY constraint.
// keepLatest specifies how many most recent indexes to always keep (minimum 1).
// maxAge specifies the maximum age for indexes to keep (e.g., 30 days).
// If maxAge is zero or negative, pruning is count-only.
func PruneOldIndexes(ctx context.Context, db *sql.DB, keepLatest int, maxAge time.Duration) (PruneStats, error) {
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
	if maxAge > 0 {
		where += ` AND last_indexed < ?`
		args = append(args, time.Now().Add(-maxAge).Unix())
	}

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

	// Run incremental vacuum to reclaim space
	if err := incrementalVacuum(ctx, db); err != nil {
		// Log warning but don't fail the operation
		slog.Warn("incremental vacuum failed after pruning", "err", err)
	}

	return stats, nil
}

// incrementalVacuum reclaims the whole freelist and truncates the file.
// The pragma frees pages as its result rows are stepped through, so it must
// be executed as a query and drained — Exec performs a single step and frees
// only one page, silently leaving the rest of the freelist in place.
func incrementalVacuum(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `PRAGMA incremental_vacuum;`)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			slog.Warn("incremental_vacuum rows close failed", "err", closeErr)
		}
	}()
	for rows.Next() {
	}
	return rows.Err()
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
