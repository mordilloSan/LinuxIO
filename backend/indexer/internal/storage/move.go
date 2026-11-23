package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/indexing"
)

// MovePath renames the rows of from, and of everything below it, to the path
// of moved, which is what the disk holds there now. The files are the same
// files, so nothing is read from disk: the paths change, and the size moves
// from the folders above the old path to those above the new one. The names
// below a moved folder do not change, so the search index is touched for one
// row at most.
//
// It reports false and changes nothing when it cannot tell that moved is what
// the index holds at from: there is no row for from, the row is of another
// kind, or its device and inode are not moved's, as after a move to another
// filesystem (a copy and a delete) or into a folder that was already there (a
// merge). It reports false too when the folder moved went into has no row,
// since the rows would hang below nothing, and when the index holds a row at
// the new path or below it. Such rows can be what the move replaced, or the
// moved files themselves, which a refresh or a scan that ran since the move
// has already written there, taking the size from the rows at the old path:
// renaming those over the new ones would leave the files counting nothing.
// The caller then lets the index follow the disk for both paths.
func (s *Store) MovePath(ctx context.Context, indexID int64, from string, moved indexing.IndexEntry) (bool, error) {
	to := moved.RelativePath
	if from == "/" || to == "/" || pathInSubtree(to, from) || pathInSubtree(from, to) {
		return false, nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin move: %w", err)
	}
	defer rollbackTx(tx, "MovePath")

	row, found, err := storedEntry(ctx, tx, indexID, from)
	if err != nil {
		return false, err
	}
	if !found || row.typ != typeCode(moved.Type) || row.device != int64(moved.Device) || row.inode != int64(moved.Inode) || row.inode == 0 {
		return false, nil
	}
	if parent, parentFound, parentErr := storedEntry(ctx, tx, indexID, parentDirKey(to)); parentErr != nil {
		return false, parentErr
	} else if !parentFound || parent.typ != typeDirectory {
		return false, nil
	}

	// What the folders above count for it: a folder's size is everything
	// counted below it, a file's is its own share.
	counted := row.contribution
	if row.typ == typeDirectory {
		counted = row.size
	}

	if held, heldErr := holdsRowsAt(ctx, tx, indexID, to); heldErr != nil || held {
		return false, heldErr
	}
	if err := updateParentDirectorySizes(ctx, tx, indexID, from, -counted); err != nil {
		return false, fmt.Errorf("take the moved size from the old folders: %w", err)
	}
	if err := renameRows(ctx, tx, indexID, from, to); err != nil {
		return false, err
	}
	if err := updateParentDirectorySizes(ctx, tx, indexID, to, counted); err != nil {
		return false, fmt.Errorf("add the moved size to the new folders: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit move: %w", err)
	}
	return true, nil
}

// holdsRowsAt reports whether the index holds a row for path or for anything
// below it.
func holdsRowsAt(ctx context.Context, db dbExecutor, indexID int64, path string) (bool, error) {
	lo, childLo, hi := subtreeBounds(path)
	var held bool
	err := db.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM entries
			WHERE index_id = ?
			  AND relative_path >= ? AND relative_path < ?
			  AND (relative_path = ? OR relative_path >= ?)
		);
	`, indexID, lo, hi, lo, childLo).Scan(&held)
	if err != nil {
		return false, fmt.Errorf("look for entries at %q: %w", path, err)
	}
	return held, nil
}

// renameRows gives the row of from, and every row below it, the path it has
// below to.
func renameRows(ctx context.Context, db dbExecutor, indexID int64, from, to string) error {
	lo, childLo, hi := subtreeBounds(from)
	// The rest of each path is cut as bytes, not characters: a file name
	// need not be valid UTF-8.
	_, err := db.ExecContext(ctx, `
		UPDATE entries
		SET relative_path = ? || CAST(substr(CAST(relative_path AS BLOB), ?) AS TEXT),
		    path_depth = path_depth + ?
		WHERE index_id = ?
		  AND relative_path >= ? AND relative_path < ?
		  AND (relative_path = ? OR relative_path >= ?);
	`, to, len(from)+1, strings.Count(to, "/")-strings.Count(from, "/"), indexID, lo, hi, lo, childLo)
	if err != nil {
		return fmt.Errorf("rename entries below %q: %w", from, err)
	}
	if filepath.Base(from) == filepath.Base(to) {
		return nil
	}
	if _, err := db.ExecContext(ctx, `UPDATE entries SET name = ? WHERE index_id = ? AND relative_path = ?;`, filepath.Base(to), indexID, to); err != nil {
		return fmt.Errorf("rename entry %q: %w", to, err)
	}
	return nil
}

// storedEntry reads the row of one path.
func storedEntry(ctx context.Context, db dbExecutor, indexID int64, path string) (storedRow, bool, error) {
	var row storedRow
	err := db.QueryRowContext(ctx, `
		SELECT id, type, size, mod_time, device, inode, nlink, size_contribution
		FROM entries WHERE index_id = ? AND relative_path = ?;
	`, indexID, path).Scan(&row.id, &row.typ, &row.size, &row.modTime, &row.device, &row.inode, &row.nlink, &row.contribution)
	if errors.Is(err, sql.ErrNoRows) {
		return storedRow{}, false, nil
	}
	if err != nil {
		return storedRow{}, false, fmt.Errorf("read stored entry %q: %w", path, err)
	}
	return row, true, nil
}
