package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	indexerapi "github.com/mordilloSan/LinuxIO/backend/indexer/api"
	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/indexing"
)

// UpdateEntry updates or inserts a single entry in the database.
// Returns the old size if the entry existed, or 0 if it's new.
func UpdateEntry(ctx context.Context, db dbExecutor, indexID int64, entry indexing.IndexEntry) (oldSize int64, err error) {
	ctx = ensureContext(ctx)

	// Check if entry already exists and get old size
	var existingSize sql.NullInt64
	err = db.QueryRowContext(ctx, `
		SELECT size FROM entries
		WHERE index_id = ? AND relative_path = ?;
	`, indexID, entry.RelativePath).Scan(&existingSize)

	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("query existing entry %q: %w", entry.RelativePath, err)
	}

	if existingSize.Valid {
		oldSize = existingSize.Int64
	}

	// Upsert the entry
	pathDepth := 0
	if entry.RelativePath != "/" {
		pathDepth = strings.Count(entry.RelativePath, "/")
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO entries (
			index_id, relative_path, path_depth, name, size, mod_time,
			type, hidden, device, inode, size_contribution
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(index_id, relative_path) DO UPDATE SET
			name = excluded.name,
			size = excluded.size,
			mod_time = excluded.mod_time,
			type = excluded.type,
			hidden = excluded.hidden,
			device = excluded.device,
			inode = excluded.inode,
			size_contribution = excluded.size_contribution;
	`,
		indexID,
		entry.RelativePath,
		pathDepth,
		entry.Name,
		entry.Size,
		entry.ModTime.Unix(),
		entry.Type,
		boolToInt(entry.Hidden),
		int64(entry.Device),
		int64(entry.Inode),
		entry.SizeContribution,
	)

	if err != nil {
		return oldSize, fmt.Errorf("upsert entry %q: %w", entry.RelativePath, err)
	}
	return oldSize, nil
}

// updateParentDirectorySizes propagates size changes up the directory tree.
// sizeDelta is the change in size (positive for additions, negative for deletions).
func updateParentDirectorySizes(ctx context.Context, db dbExecutor, indexID int64, childPath string, sizeDelta int64) error {
	return updateParentDirectorySizesThrough(ctx, db, indexID, childPath, sizeDelta, "")
}

func updateParentDirectorySizesThrough(ctx context.Context, db dbExecutor, indexID int64, childPath string, sizeDelta int64, stopPath string) error {
	ctx = ensureContext(ctx)
	if sizeDelta == 0 {
		return nil
	}
	if stopPath != "" {
		stopPath = indexerapi.NormalizePath(stopPath)
	}

	currentPath := parentDirKey(childPath)

	// Walk up the directory tree
	for currentPath != "" {
		_, err := db.ExecContext(ctx, `
			UPDATE entries
			SET size = size + ?
			WHERE index_id = ? AND relative_path = ? AND type = 'directory';
		`, sizeDelta, indexID, currentPath)

		if err != nil {
			return fmt.Errorf("update parent directory %q: %w", currentPath, err)
		}

		// Move to parent
		if currentPath == "/" || currentPath == stopPath {
			break
		}
		currentPath = parentDirKey(currentPath)
	}

	return nil
}

// countEntriesUnderPath counts queryable directory and non-directory entries
// in one subtree.
func countEntriesUnderPath(ctx context.Context, db dbExecutor, indexID int64, relativePath string) (dirs, files int64, err error) {
	ctx = ensureContext(ctx)
	lo, childLo, hi := subtreeBounds(relativePath)
	err = db.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(CASE WHEN type = 'directory' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN type = 'directory' THEN 0 ELSE 1 END), 0)
		FROM entries
		WHERE index_id = ?
		  AND relative_path >= ? AND relative_path < ?
		  AND (relative_path = ? OR relative_path >= ?);
	`, indexID, lo, hi, lo, childLo).Scan(&dirs, &files)
	return dirs, files, err
}

func entryTypeDelta(oldType string, existed bool, newType string) (dirs, files int64) {
	if existed && oldType == newType {
		return 0, 0
	}
	if existed {
		if oldType == "directory" {
			dirs--
		} else {
			files--
		}
	}
	if newType == "directory" {
		dirs++
	} else {
		files++
	}
	return dirs, files
}

// UpsertEntry adds or replaces one non-directory entry in generation indexID
// and propagates its size and hardlink accounting to the parent directories.
func (s *Store) UpsertEntry(ctx context.Context, indexID int64, entry indexing.IndexEntry) error {
	ctx = ensureContext(ctx)
	if entry.Type == "directory" {
		return fmt.Errorf("cannot add a directory entry; reindex its subtree")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin entry upsert: %w", err)
	}
	defer rollbackTx(tx, "UpsertEntry")

	old, err := loadStoredEntryAccounting(ctx, tx, indexID, entry.RelativePath)
	if err != nil {
		return fmt.Errorf("read existing entry type: %w", err)
	}
	if old.exists && old.typ == "directory" {
		return fmt.Errorf("cannot replace a directory entry; reindex its parent")
	}
	if err := replaceEntryAccounting(ctx, tx, indexID, entry, old, s.sameFile); err != nil {
		return fmt.Errorf("replace entry accounting: %w", err)
	}

	dirDelta, fileDelta := entryTypeDelta(old.typ, old.exists, entry.Type)
	if err := updateIndexMetadata(ctx, tx, indexID, dirDelta, fileDelta); err != nil {
		return fmt.Errorf("update index metadata: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit entry upsert: %w", err)
	}
	return nil
}

// DeletePath deletes all entries under a path (including the path itself) and propagates size changes.
func (s *Store) DeletePath(ctx context.Context, indexID int64, relativePath string) error {
	ctx = ensureContext(ctx)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin recursive delete: %w", err)
	}
	defer rollbackTx(tx, "DeletePath")

	deletedDirs, deletedFiles, err := countEntriesUnderPath(ctx, tx, indexID, relativePath)
	if err != nil {
		return fmt.Errorf("count deleted entries: %w", err)
	}

	removedContribution, err := removedContributionForPath(ctx, tx, indexID, relativePath)
	if err != nil {
		return fmt.Errorf("read removed contribution: %w", err)
	}
	promotedGroups, err := promotedHardlinksForDelete(ctx, tx, indexID, relativePath)
	if err != nil {
		return fmt.Errorf("find hardlinks requiring promotion: %w", err)
	}

	// Delete all entries under this path (including the path itself)
	lo, childLo, hi := subtreeBounds(relativePath)
	_, err = tx.ExecContext(ctx, `
		DELETE FROM entries
		WHERE index_id = ?
		  AND relative_path >= ? AND relative_path < ?
		  AND (relative_path = ? OR relative_path >= ?);
	`, indexID, lo, hi, lo, childLo)
	if err != nil {
		return fmt.Errorf("delete entries under %q: %w", relativePath, err)
	}

	if removedContribution != 0 {
		if err := updateParentDirectorySizes(ctx, tx, indexID, relativePath, -removedContribution); err != nil {
			return fmt.Errorf("subtract removed contribution: %w", err)
		}
	}
	if err := promoteHardlinks(ctx, tx, indexID, promotedGroups, s.sameFile); err != nil {
		return fmt.Errorf("promote surviving hardlinks: %w", err)
	}
	if err := updateIndexMetadata(ctx, tx, indexID, -deletedDirs, -deletedFiles); err != nil {
		return fmt.Errorf("update index metadata: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit recursive delete: %w", err)
	}
	return nil
}

// cleanupDeletedEntriesUnderPath removes entries under a specific path that were not seen during the latest scan.
// This is used for partial reindexing to avoid deleting entries outside the reindexed path.
// Returns the number of entries deleted.
func cleanupDeletedEntriesUnderPath(ctx context.Context, db dbExecutor, indexID int64, relativePath string, scanTime int64) (int64, error) {
	ctx = ensureContext(ctx)

	lo, childLo, hi := subtreeBounds(relativePath)
	result, err := db.ExecContext(ctx, `
		DELETE FROM entries
		WHERE index_id = ?
		  AND last_seen < ?
		  AND relative_path >= ? AND relative_path < ?
		  AND (relative_path = ? OR relative_path >= ?);
	`, indexID, scanTime, lo, hi, lo, childLo)
	if err != nil {
		return 0, err
	}

	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}

	return deleted, nil
}

// rollbackTx rolls back tx unconditionally: after a successful Commit it
// returns ErrTxDone, which is ignored. Guarding on an error variable instead
// is fragile; a shadowed err on any failure path leaks the transaction and
// pins both a pool connection and SQLite's write lock.
func rollbackTx(tx *sql.Tx, operation string) {
	if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		slog.Warn(operation+" rollback failed", "err", err)
	}
}
