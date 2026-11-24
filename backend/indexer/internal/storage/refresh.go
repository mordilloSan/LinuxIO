package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/indexing"
)

// ErrFolderNotIndexed is returned by RefreshFolder for a folder that has no
// row: there is nothing to compare its entries with, and it wants a reindex.
var ErrFolderNotIndexed = errors.New("folder is not in the index")

// FolderRefresh is what the refresh of one folder did.
type FolderRefresh struct {
	// NewFolders are the subfolders found on disk that the index does not
	// hold. The refresh does not go into them: each wants a reindex.
	NewFolders []string
	// Changed is how many entries of the folder were written or removed.
	Changed int64
}

// RefreshFolder makes the rows of one folder's own entries agree with
// contents, what was just read of it from disk, without looking below its
// subfolders. A file that is new or differs is written, an entry that is gone
// is removed with everything below it, and the sizes of the folders above
// follow. A folder that did not change costs one read of its rows and no
// write.
func (s *Store) RefreshFolder(ctx context.Context, indexID int64, path string, contents indexing.FolderContents) (FolderRefresh, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return FolderRefresh{}, fmt.Errorf("begin folder refresh: %w", err)
	}
	defer rollbackTx(tx, "RefreshFolder")

	var typ int
	err = tx.QueryRowContext(ctx, `SELECT type FROM entries WHERE index_id = ? AND relative_path = ?`, indexID, path).Scan(&typ)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && typ != typeDirectory) {
		return FolderRefresh{}, ErrFolderNotIndexed
	}
	if err != nil {
		return FolderRefresh{}, fmt.Errorf("read folder entry %q: %w", path, err)
	}
	stored, err := storedChildren(ctx, tx, indexID, path)
	if err != nil {
		return FolderRefresh{}, err
	}

	refresh := folderRefresh{ctx: ctx, tx: tx, indexID: indexID, sameFile: s.sameFile}
	if err := refresh.compare(contents, stored); err != nil {
		return FolderRefresh{}, err
	}

	if refresh.result.Changed == 0 {
		// Nothing to commit, and the index row keeps its time.
		return refresh.result, nil
	}
	if err := updateIndexMetadata(ctx, tx, indexID, refresh.dirDelta, refresh.fileDelta); err != nil {
		return FolderRefresh{}, fmt.Errorf("update index metadata: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return FolderRefresh{}, fmt.Errorf("commit folder refresh: %w", err)
	}
	return refresh.result, nil
}

// folderRefresh carries one refresh's transaction and what it has changed.
type folderRefresh struct {
	ctx      context.Context
	tx       *sql.Tx
	indexID  int64
	sameFile SameFileFunc

	result              FolderRefresh
	dirDelta, fileDelta int64
}

// compare brings the stored entries of the folder in line with contents.
// What is left of stored once every entry on disk has been taken out of it is
// gone from the disk.
func (r *folderRefresh) compare(contents indexing.FolderContents, stored map[string]storedRow) error {
	for _, file := range contents.Files {
		row, found := stored[file.RelativePath]
		delete(stored, file.RelativePath)
		if err := r.file(file, row, found); err != nil {
			return err
		}
	}
	for _, folder := range contents.Folders {
		row, found := stored[folder]
		delete(stored, folder)
		if found && row.typ == typeDirectory {
			continue
		}
		if found {
			if err := r.remove(folder); err != nil {
				return err
			}
		}
		r.result.NewFolders = append(r.result.NewFolders, folder)
	}
	for gone := range stored {
		if err := r.remove(gone); err != nil {
			return err
		}
	}
	return nil
}

// file writes a file of the folder when its row is missing or differs.
func (r *folderRefresh) file(file indexing.IndexEntry, row storedRow, found bool) error {
	if found && row.describes(file) {
		return nil
	}
	// A path that held a folder loses what was below it in the same step.
	dirs, files, err := upsertEntry(r.ctx, r.tx, r.indexID, file, r.sameFile)
	if err != nil {
		return fmt.Errorf("refresh entry %q: %w", file.RelativePath, err)
	}
	r.dirDelta += dirs
	r.fileDelta += files
	r.result.Changed++
	return nil
}

// remove deletes an entry of the folder that is gone, or that is something
// else now, with everything below it.
func (r *folderRefresh) remove(path string) error {
	dirs, files, err := deletePath(r.ctx, r.tx, r.indexID, path, r.sameFile)
	if err != nil {
		return fmt.Errorf("remove entry %q: %w", path, err)
	}
	r.dirDelta -= dirs
	r.fileDelta -= files
	r.result.Changed++
	return nil
}
