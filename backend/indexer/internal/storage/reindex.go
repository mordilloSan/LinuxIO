package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/indexing"
)

// Reindex brings one subtree of a generation up to date with the disk. The
// caller sends the rescanned entries through NewReconciler, closes it, then
// calls Commit with its ScanTime. Rollback abandons the work and is safe to
// defer after Commit.
type Reindex struct {
	db        *sql.DB
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
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin reindex transaction: %w", err)
	}
	r := &Reindex{db: s.db, tx: tx, indexID: indexID, path: relativePath, sameFile: s.sameFile}
	// What the folders above count for the subtree today. For a folder with
	// a row that is its size. One without a row can have rows below it all
	// the same, from the reindex of a folder inside it, and the folders
	// above already count those: taking its size for nothing would add them
	// a second time.
	if relativePath != "/" {
		if r.oldSize, err = removedContributionForPath(ctx, tx, indexID, relativePath); err != nil {
			r.Rollback()
			return nil, fmt.Errorf("read prior directory size: %w", err)
		}
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

// Rollback abandons the reindex. It is a no-op after Commit. Rows a long scan
// has already committed stay; the next scan compares them again.
func (r *Reindex) Rollback() {
	if r.tx != nil {
		rollbackTx(r.tx, "reindex")
	}
}

// commitChunk commits what the scan has written so far and carries on in a
// new transaction.
func (r *Reindex) commitChunk(ctx context.Context) error {
	if err := r.tx.Commit(); err != nil {
		r.tx = nil
		return fmt.Errorf("commit scanned rows: %w", err)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		r.tx = nil
		return fmt.Errorf("continue reindex transaction: %w", err)
	}
	r.tx = tx
	return nil
}

// HardlinkContributors names, for each hard link group with a link in the
// subtree, the path that carries the group's size today. The scanner counts
// that path and no other, so that a scan which meets another link first does
// not move the size, and with it the sizes of two folders, only for the
// reconcile step to move it back.
func (r *Reindex) HardlinkContributors() map[indexing.FileID]string {
	contributors := make(map[indexing.FileID]string, len(r.hardlinks.groups))
	for identity, path := range r.hardlinks.groups {
		if path != "" {
			contributors[indexing.FileID{Dev: uint64(identity.device), Ino: uint64(identity.inode)}] = path
		}
	}
	return contributors
}

// Commit reconciles hardlinks and directory aggregates, updates the generation
// metadata and commits. scanTime is the reconciler's ScanTime. It returns the
// subtree's new size.
func (r *Reindex) Commit(ctx context.Context, scanTime int64) (int64, error) {
	tx, indexID, path := r.tx, r.indexID, r.path
	if tx == nil {
		return 0, errors.New("the reindex transaction was lost")
	}
	var err error
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
	if path == "/" {
		// Everything was scanned: the counts are what was counted, whatever
		// an earlier scan that stopped half-way left in them.
		err = setIndexCounts(ctx, tx, indexID, newDirs, newFiles)
	} else {
		err = updateIndexMetadata(ctx, tx, indexID, newDirs-r.oldDirs, newFiles-r.oldFiles)
	}
	if err != nil {
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
