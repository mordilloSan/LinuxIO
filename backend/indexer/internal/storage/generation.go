package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// The index generation. There is one: the first scan creates it with
// last_indexed=0 and publishes it by committing, and every later scan updates
// its rows in place. Readers only see a published generation, so the first
// scan shows nothing until it is done.

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

// EnsureGeneration returns the generation a full scan writes into: the
// published one, or the one a first scan has not completed yet, or a new one.
// A generation is published when a scan of everything commits (setIndexCounts
// sets last_indexed), so readers see no index until the first scan is done.
func (s *Store) EnsureGeneration(ctx context.Context) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM indexes ORDER BY last_indexed DESC, id DESC LIMIT 1;`).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("read index generation: %w", err)
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO indexes (last_indexed) VALUES (0);`)
	if err != nil {
		return 0, fmt.Errorf("insert index generation: %w", err)
	}
	id, err = res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("read generation id: %w", err)
	}
	return id, nil
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

// setIndexCounts ends a full index of generation indexID: it records the
// entry counts as counted and the time, and then the total as
// updateIndexMetadata does. The time is what "Last indexed" shows and what
// publishes a first scan, and nothing else writes it: the updates between two
// full indexes keep the index current without being one.
func setIndexCounts(ctx context.Context, db dbExecutor, indexID, dirs, files int64) error {
	if _, err := db.ExecContext(ctx, `
		UPDATE indexes
		SET num_dirs = ?, num_files = ?, last_indexed = CAST(strftime('%s', 'now') AS INTEGER)
		WHERE id = ?;
	`, dirs, files, indexID); err != nil {
		return err
	}
	return updateIndexMetadata(ctx, db, indexID, 0, 0)
}

// updateIndexMetadata applies entry-count deltas to generation indexID and
// synchronizes its stored total with the queryable entries.
func updateIndexMetadata(ctx context.Context, db dbExecutor, indexID, dirDelta, fileDelta int64) error {
	_, err := db.ExecContext(ctx, `
		UPDATE indexes
		SET num_dirs = num_dirs + ?,
			num_files = num_files + ?,
			total_size = COALESCE((
				SELECT size FROM entries
				WHERE index_id = ? AND relative_path = '/'
			), 0)
		WHERE id = ?;
	`, dirDelta, fileDelta, indexID, indexID)
	return err
}
