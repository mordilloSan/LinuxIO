package storage

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/indexing"
)

// The reconciler makes the stored rows of a rescanned subtree agree with what
// the scanner found, and writes only where they differ: a rescan of a tree
// that did not change writes nothing.
//
// The scanner sends a folder's files and then the folder itself, subfolders
// first. The reconciler holds a folder's files until the folder arrives,
// reads the folder's stored children with one range read, and inserts,
// updates or deletes what differs. What is stored for a folder and was not
// seen on disk is gone, so nothing has to be remembered beyond the folders
// the scanner is inside of.

// reconcileChunk is how many files of one folder are held before they are
// compared by a lookup each instead, so that a folder with a million files
// costs their names and not their entries. A variable for tests.
var reconcileChunk = 2048

// commitRows is how many written rows a scan of everything lets pile up
// before it commits them. A rescan that changes less is one transaction,
// which readers see all at once; a first scan, or one that changes more,
// becomes visible in parts. The reindex of a folder is always one.
const commitRows = 50_000

// Reconciler receives the entries of a rescan. It is a StreamingWriter for
// the scanner and is not safe for concurrent use.
type Reconciler struct {
	ctx        context.Context
	reindex    *Reindex
	scanTime   int64
	pending    map[string]*pendingFolder
	writes     []indexing.IndexEntry
	progressCb ProgressCallback

	files, dirs int64
	changed     int64
	deleted     int64
	uncommitted int64
}

// pendingFolder is what has been seen of a folder whose own entry has not
// arrived yet.
type pendingFolder struct {
	// entries are the files not compared yet.
	entries []indexing.IndexEntry
	// seen are the paths of the children already dealt with: files compared
	// early, and subfolders, which are compared when they arrive.
	seen map[string]struct{}
}

// storedRow is what a row holds of the values a rescan can change.
type storedRow struct {
	id                  int64
	typ                 int
	size, modTime       int64
	device, inode       int64
	nlink, contribution int64
}

func (s storedRow) matches(entry indexing.IndexEntry) bool {
	return s.describes(entry) && s.contribution == entry.SizeContribution
}

// describes reports whether the row holds what the disk says of entry. What
// the entry counts in the folders above is not the disk's to say.
func (s storedRow) describes(entry indexing.IndexEntry) bool {
	return s.typ == typeCode(entry.Type) &&
		s.size == entry.Size &&
		s.modTime == entry.ModTime.Unix() &&
		s.device == int64(entry.Device) &&
		s.inode == int64(entry.Inode) &&
		s.nlink == storedNlink(entry)
}

// NewReconciler returns the writer a rescan of the reindexed subtree sends
// its entries to. progressCb may be nil.
func (r *Reindex) NewReconciler(ctx context.Context, progressCb ProgressCallback) *Reconciler {
	return &Reconciler{
		ctx:     ctx,
		reindex: r,
		// Nanoseconds: the hard link accounting takes a row with this time for
		// one written by this scan, and two scans can start within a second.
		scanTime:   time.Now().UTC().UnixNano(),
		pending:    make(map[string]*pendingFolder),
		progressCb: progressCb,
	}
}

// ScanTime is the time this scan stamps on the rows it writes.
func (rc *Reconciler) ScanTime() int64 { return rc.scanTime }

// Changed is how many rows the scan has inserted, updated or deleted.
func (rc *Reconciler) Changed() int64 { return rc.changed + int64(len(rc.writes)) }

// Deleted is how many rows the scan has deleted.
func (rc *Reconciler) Deleted() int64 { return rc.deleted }

// Write takes the next entry of the scan.
func (rc *Reconciler) Write(entry indexing.IndexEntry) error {
	if err := rc.ctx.Err(); err != nil {
		return err
	}
	if entry.Type == "directory" {
		rc.dirs++
	} else {
		rc.files++
	}
	if rc.progressCb != nil {
		rc.progressCb(rc.files, rc.dirs, entry.RelativePath)
	}

	if entry.Type == "directory" {
		return rc.finishFolder(entry)
	}
	folder := rc.folder(parentDirKey(entry.RelativePath))
	folder.entries = append(folder.entries, entry)
	if len(folder.entries) >= reconcileChunk {
		return rc.compareByLookup(folder)
	}
	return nil
}

// Close writes what is still held. The entries of a folder that never
// arrived, because it could not be read to its end, are dropped: its parent
// did not see it, and removed it.
func (rc *Reconciler) Close() error {
	return rc.flush()
}

func (rc *Reconciler) folder(path string) *pendingFolder {
	folder, ok := rc.pending[path]
	if !ok {
		folder = &pendingFolder{seen: make(map[string]struct{})}
		rc.pending[path] = folder
	}
	return folder
}

// compareByLookup compares the files held for a folder that has too many to
// wait for its own entry, each by a lookup of its own, and keeps their paths.
func (rc *Reconciler) compareByLookup(folder *pendingFolder) error {
	for _, entry := range folder.entries {
		stored, found, err := rc.lookup(entry.RelativePath)
		if err != nil {
			return err
		}
		if err := rc.compare(entry, stored, found); err != nil {
			return err
		}
		folder.seen[entry.RelativePath] = struct{}{}
	}
	clear(folder.entries)
	folder.entries = folder.entries[:0]
	return nil
}

// finishFolder compares a folder's children with the stored ones, removes
// what is stored and was not seen, and then compares the folder's own row.
func (rc *Reconciler) finishFolder(entry indexing.IndexEntry) error {
	path := entry.RelativePath
	folder := rc.pending[path]
	delete(rc.pending, path)
	if folder == nil {
		folder = &pendingFolder{}
	}

	stored, err := storedChildren(rc.ctx, rc.reindex.tx, rc.reindex.indexID, path)
	if err != nil {
		return err
	}
	for _, child := range folder.entries {
		row, found := stored[child.RelativePath]
		delete(stored, child.RelativePath)
		if compareErr := rc.compare(child, row, found); compareErr != nil {
			return compareErr
		}
	}
	for seen := range folder.seen {
		delete(stored, seen)
	}
	for gone, row := range stored {
		if removeErr := rc.remove(gone, row); removeErr != nil {
			return removeErr
		}
	}

	own, found, err := rc.lookup(path)
	if err != nil {
		return err
	}
	if !found || !own.matches(entry) {
		if err := rc.write(entry); err != nil {
			return err
		}
	}
	if path != "/" {
		rc.folder(parentDirKey(path)).seen[path] = struct{}{}
	}
	return nil
}

// compare writes entry when it differs from its stored row or has none. A
// path that held a folder and now holds something else loses what was below
// it first: nothing would visit those rows again.
func (rc *Reconciler) compare(entry indexing.IndexEntry, stored storedRow, found bool) error {
	if found && stored.matches(entry) {
		return nil
	}
	if found && stored.typ == typeDirectory && entry.Type != "directory" {
		if err := rc.remove(entry.RelativePath, stored); err != nil {
			return err
		}
	}
	return rc.write(entry)
}

// Drop forgets a folder the scan gave up on: one it could not read to its
// end, one that is left out of the index, one that vanished. Its rows go, the
// stored ones and those its subfolders had already sent, so that none is left
// below a folder that has no row.
func (rc *Reconciler) Drop(path string) error {
	for pending := range rc.pending {
		if pathInSubtree(pending, path) {
			delete(rc.pending, pending)
		}
	}
	return rc.remove(path, storedRow{typ: typeDirectory})
}

// storedChildrenQuery reads a folder's direct children: the rows one level
// below it whose path starts with its own. Naming both types lets it seek in
// the index that keeps a folder's children together.
const storedChildrenQuery = `
	SELECT relative_path, id, type, size, mod_time, device, inode, nlink, size_contribution
	FROM entries
	WHERE index_id = ? AND type IN (0, 1) AND path_depth = ?
	  AND relative_path >= ? AND relative_path < ?`

// storedChildren reads the rows of a folder's direct children, by path.
func storedChildren(ctx context.Context, db dbExecutor, indexID int64, path string) (map[string]storedRow, error) {
	depth := 1
	if path != "/" {
		depth = strings.Count(path, "/") + 1
	}
	_, childLo, hi := subtreeBounds(path)
	rows, err := db.QueryContext(ctx, storedChildrenQuery, indexID, depth, childLo, hi)
	if err != nil {
		return nil, fmt.Errorf("read stored children of %q: %w", path, err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			slog.Warn("rows close failed", "query", "stored children", "error", closeErr)
		}
	}()

	children := make(map[string]storedRow)
	for rows.Next() {
		var child string
		var row storedRow
		if err := rows.Scan(&child, &row.id, &row.typ, &row.size, &row.modTime, &row.device, &row.inode, &row.nlink, &row.contribution); err != nil {
			return nil, fmt.Errorf("scan stored child of %q: %w", path, err)
		}
		children[child] = row
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read stored children of %q: %w", path, err)
	}
	return children, nil
}

func (rc *Reconciler) lookup(path string) (storedRow, bool, error) {
	return storedEntry(rc.ctx, rc.reindex.tx, rc.reindex.indexID, path)
}

// write queues one entry to be inserted or replaced.
func (rc *Reconciler) write(entry indexing.IndexEntry) error {
	rc.writes = append(rc.writes, entry)
	if len(rc.writes) >= batchSize {
		return rc.flush()
	}
	return nil
}

// remove deletes a stored entry that is no longer on disk, with everything
// below it when it is a folder.
func (rc *Reconciler) remove(path string, row storedRow) error {
	var result sql.Result
	var err error
	if row.typ == typeDirectory {
		// Rows queued for below this folder are written first, so that the
		// delete takes them too.
		if err = rc.flush(); err != nil {
			return err
		}
		lo, childLo, hi := subtreeBounds(path)
		result, err = rc.reindex.tx.ExecContext(rc.ctx, `
			DELETE FROM entries
			WHERE index_id = ?
			  AND relative_path >= ? AND relative_path < ?
			  AND (relative_path = ? OR relative_path >= ?);
		`, rc.reindex.indexID, lo, hi, lo, childLo)
	} else {
		result, err = rc.reindex.tx.ExecContext(rc.ctx, `DELETE FROM entries WHERE id = ?;`, row.id)
	}
	if err != nil {
		return fmt.Errorf("delete stored entry %q: %w", path, err)
	}
	removed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count deleted rows of %q: %w", path, err)
	}
	rc.changed += removed
	rc.deleted += removed
	rc.uncommitted += removed
	return nil
}

// flush writes the queued entries, and commits when enough rows have piled up.
func (rc *Reconciler) flush() error {
	if len(rc.writes) > 0 {
		if err := insertEntriesBatch(rc.ctx, rc.reindex.tx, rc.reindex.indexID, rc.scanTime, rc.writes); err != nil {
			return fmt.Errorf("write entries: %w", err)
		}
		written := int64(len(rc.writes))
		rc.changed += written
		rc.uncommitted += written
		clear(rc.writes)
		rc.writes = rc.writes[:0]
	}
	// Only the scan of everything commits in parts: it counts what is there
	// when it ends, where the reindex of a folder adds what it changed to
	// counts that a part committed and then abandoned would leave wrong.
	if rc.uncommitted >= commitRows && rc.reindex.path == "/" {
		rc.uncommitted = 0
		return rc.reindex.commitChunk(rc.ctx)
	}
	return nil
}
