package storage

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

// Index generations. A full scan inserts a new generation row with
// last_indexed=0, writes its entries, and publishes it by setting
// last_indexed. Readers only ever see the newest published generation, so a
// failed or interrupted scan leaves the previous one untouched. Everything
// that creates, publishes, selects or retires a generation lives here.

// latestGenerationSubquery selects the newest published generation. The id
// tiebreaker orders scans that publish within the same second.
const latestGenerationSubquery = `(SELECT id FROM indexes WHERE last_indexed > 0 ORDER BY last_indexed DESC, id DESC LIMIT 1)`

// ErrNoGeneration means no generation has been published yet.
var ErrNoGeneration = sql.ErrNoRows

// Generation is the metadata of a published generation.
type Generation struct {
	NumDirs     int64
	NumFiles    int64
	TotalSize   int64
	LastIndexed time.Time
}

// BeginGeneration discards unpublished leftovers from earlier scans and
// inserts the unpublished generation the next full scan writes into. Rows
// with last_indexed=0 are invisible to readers until PublishGeneration.
func (s *Store) BeginGeneration(ctx context.Context) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin transaction: %w", err)
	}
	defer rollbackTx(tx, "BeginGeneration")

	const deleteLeftovers = `DELETE FROM indexes WHERE last_indexed = 0;`
	var leftovers int64
	if countErr := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM entries
		WHERE index_id IN (SELECT id FROM indexes WHERE last_indexed = 0);
	`).Scan(&leftovers); countErr != nil {
		return 0, fmt.Errorf("count incomplete generation entries: %w", countErr)
	}
	// A large leftover would fire the per-row FTS delete trigger for every
	// entry inside this transaction, so it takes the same bypass as pruning.
	if shouldBypassFTS(ctx, s.db, leftovers) {
		_, err = deleteWithFTSRebuild(ctx, tx, deleteLeftovers, nil)
	} else {
		_, err = tx.ExecContext(ctx, deleteLeftovers)
	}
	if err != nil {
		return 0, fmt.Errorf("clean up incomplete generations: %w", err)
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO indexes (last_indexed) VALUES (0);`)
	if err != nil {
		return 0, fmt.Errorf("insert index generation: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("read generation id: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit transaction: %w", err)
	}
	return id, nil
}

// PublishGeneration records the final counts of generation id and makes it the
// one readers see.
func (s *Store) PublishGeneration(ctx context.Context, id, dirs, files, totalSize int64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE indexes SET
			num_dirs = ?,
			num_files = ?,
			total_size = ?,
			last_indexed = ?
		WHERE id = ?;
	`, dirs, files, totalSize, time.Now().UTC().Unix(), id)
	return err
}

// LatestIndexID returns the ID of the most recently published generation.
// ErrNoGeneration means no generation has been published yet; any other error
// is a real DB problem and should be surfaced.
func (s *Store) LatestIndexID(ctx context.Context) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM indexes WHERE id = `+latestGenerationSubquery).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

// LatestGeneration returns the metadata of the most recently published
// generation, or ErrNoGeneration.
func (s *Store) LatestGeneration(ctx context.Context) (Generation, error) {
	var g Generation
	var lastIndexed int64
	err := s.db.QueryRowContext(ctx, `
		SELECT num_dirs, num_files, total_size, last_indexed
		FROM indexes
		WHERE id = `+latestGenerationSubquery).Scan(&g.NumDirs, &g.NumFiles, &g.TotalSize, &lastIndexed)
	if err != nil {
		return Generation{}, err
	}
	g.LastIndexed = time.Unix(lastIndexed, 0).UTC()
	return g, nil
}

// completedGeneration reports whether any generation has been published.
func (s *Store) completedGeneration(ctx context.Context) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM indexes WHERE last_indexed > 0)`).Scan(&exists)
	return exists, err
}

func (s *Store) hasCompletedGeneration(ctx context.Context) bool {
	exists, err := s.completedGeneration(ctx)
	return err == nil && exists
}

// updateIndexMetadata applies entry-count deltas to generation indexID and
// synchronizes its stored total and timestamp with the queryable entries.
func updateIndexMetadata(ctx context.Context, db dbExecutor, indexID, dirDelta, fileDelta int64) error {
	_, err := db.ExecContext(ctx, `
		UPDATE indexes
		SET num_dirs = num_dirs + ?,
			num_files = num_files + ?,
			total_size = COALESCE((
				SELECT size FROM entries
				WHERE index_id = ? AND relative_path = '/'
			), 0),
			last_indexed = CAST(strftime('%s', 'now') AS INTEGER)
		WHERE id = ?;
	`, dirDelta, fileDelta, indexID, indexID)
	return err
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
func (s *Store) PruneOldIndexes(ctx context.Context, keepLatest int) (PruneStats, error) {
	db := s.db
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
	if shouldBypassFTS(ctx, db, entriesToDelete) {
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

	if err := incrementalVacuum(ctx, db); err != nil {
		slog.Warn("incremental vacuum failed after pruning", "error", err)
	}

	return stats, nil
}

// shouldBypassFTS reports whether deleting entries rows is big enough, and
// FTS present, for a trigger-less delete plus rebuild to beat per-row triggers.
func shouldBypassFTS(ctx context.Context, db *sql.DB, entries int64) bool {
	if entries <= ftsPruneBypassEntries {
		return false
	}
	exists, err := ftsTableExists(ctx, db)
	return err == nil && exists
}

// deleteIndexesWithFTSRebuild runs deleteWithFTSRebuild in its own transaction.
func deleteIndexesWithFTSRebuild(ctx context.Context, db *sql.DB, deleteQuery string, args []any) (int64, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer rollbackTx(tx, "deleteIndexesWithFTSRebuild")

	deleted, err := deleteWithFTSRebuild(ctx, tx, deleteQuery, args)
	if err != nil {
		return 0, err
	}
	return deleted, tx.Commit()
}

// deleteWithFTSRebuild runs a cascading index delete with the FTS sync
// triggers dropped, then rebuilds entries_fts from the surviving entries.
// It runs in the caller's transaction, so concurrent writers are blocked
// while the triggers are absent and a rollback undoes the trigger DDL too.
func deleteWithFTSRebuild(ctx context.Context, tx *sql.Tx, deleteQuery string, args []any) (int64, error) {
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
	return deleted, nil
}
