package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
)

// Reindex rebuilds one subtree of a generation inside a single transaction.
// The caller streams the rescanned entries through NewWriter, closes the
// writer, then calls Commit with its ScanTime. Rollback abandons the work and
// is safe to defer after Commit.
type Reindex struct {
	tx        *sql.Tx
	indexID   int64
	path      string
	sameFile  SameFileFunc
	oldSize   int64
	oldDirs   int64
	oldFiles  int64
	hardlinks *hardlinkSnapshot
}

// BeginReindex opens the reindex transaction and records the subtree's prior
// size, entry counts and hardlink contributors, which the rescan overwrites.
func (s *Store) BeginReindex(ctx context.Context, indexID int64, relativePath string) (*Reindex, error) {
	ctx = ensureContext(ctx)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin reindex transaction: %w", err)
	}
	r := &Reindex{tx: tx, indexID: indexID, path: relativePath, sameFile: s.sameFile}
	if r.oldSize, err = storedSize(ctx, tx, indexID, relativePath); err != nil {
		r.Rollback()
		return nil, fmt.Errorf("read prior directory size: %w", err)
	}
	if r.oldDirs, r.oldFiles, err = countEntriesUnderPath(ctx, tx, indexID, relativePath); err != nil {
		r.Rollback()
		return nil, fmt.Errorf("count prior entries: %w", err)
	}
	if r.hardlinks, err = snapshotHardlinksUnderPath(ctx, tx, indexID, relativePath, s.sameFile); err != nil {
		r.Rollback()
		return nil, fmt.Errorf("snapshot hardlinks: %w", err)
	}
	return r, nil
}

// NewWriter returns a streaming writer that writes every batch through the
// reindex transaction. progressCb may be nil.
func (r *Reindex) NewWriter(ctx context.Context, bufferSize int, progressCb ProgressCallback) *StreamingWriter {
	return newStreamingWriter(ctx, nil, r.tx, r.indexID, bufferSize, progressCb)
}

// Rollback abandons the reindex. It is a no-op after Commit.
func (r *Reindex) Rollback() {
	rollbackTx(r.tx, "reindex")
}

// Commit removes entries the rescan did not see, reconciles hardlinks and
// directory aggregates, updates the generation metadata and commits. scanTime
// is the writer's ScanTime. It returns the subtree's new size.
func (r *Reindex) Commit(ctx context.Context, scanTime int64) (int64, error) {
	ctx = ensureContext(ctx)
	tx, indexID, path := r.tx, r.indexID, r.path
	deleted, err := cleanupDeletedEntriesUnderPath(ctx, tx, indexID, path, scanTime)
	if err != nil {
		return 0, fmt.Errorf("cleanup deleted entries: %w", err)
	}
	if deleted > 0 {
		slog.Debug("cleaned up deleted entries under path", "deleted", deleted, "path", path)
	}
	if err = reconcileHardlinksAfterReindex(ctx, tx, indexID, path, scanTime, r.hardlinks, r.sameFile); err != nil {
		return 0, fmt.Errorf("reconcile hardlinks: %w", err)
	}

	// Directory rows already aggregate descendants, so read the reindexed
	// path's own row instead of summing and double-counting its subtree.
	newSize, err := storedSize(ctx, tx, indexID, path)
	if err != nil {
		return 0, fmt.Errorf("query new size: %w", err)
	}
	// The root has no parent; propagating its size would double its own row.
	if path != "/" && newSize != r.oldSize {
		if err = updateParentDirectorySizes(ctx, tx, indexID, path, newSize-r.oldSize); err != nil {
			return 0, fmt.Errorf("update parent sizes: %w", err)
		}
	}
	newDirs, newFiles, err := countEntriesUnderPath(ctx, tx, indexID, path)
	if err != nil {
		return 0, fmt.Errorf("count reindexed entries: %w", err)
	}
	if err = updateIndexMetadata(ctx, tx, indexID, newDirs-r.oldDirs, newFiles-r.oldFiles); err != nil {
		return 0, fmt.Errorf("update index metadata: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit reindex: %w", err)
	}
	return newSize, nil
}

// storedSize returns the stored size of path, or 0 when it has no row.
func storedSize(ctx context.Context, db dbExecutor, indexID int64, path string) (int64, error) {
	var size int64
	err := db.QueryRowContext(ctx, `SELECT size FROM entries WHERE index_id = ? AND relative_path = ?`, indexID, path).Scan(&size)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return size, err
}
