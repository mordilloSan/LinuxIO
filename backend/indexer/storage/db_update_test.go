package storage

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/indexer/indexing"
)

// testHelper provides helper methods for storage size update tests.
type testHelper struct {
	t       *testing.T
	ctx     context.Context
	db      *sql.DB
	indexID int64
}

func (h *testHelper) seedDir(relPath string) {
	h.t.Helper()
	entry := indexing.IndexEntry{
		RelativePath: relPath,
		Name:         filepath.Base(relPath),
		Size:         0,
		ModTime:      time.Now(),
		Type:         "directory",
		Hidden:       false,
		Inode:        0,
	}
	if relPath == "/" {
		entry.Name = "/"
	}
	if _, err := UpdateEntry(h.ctx, h.db, h.indexID, entry); err != nil {
		h.t.Fatalf("seed dir %s: %v", relPath, err)
	}
}

func (h *testHelper) getSize(relPath string) int64 {
	h.t.Helper()
	var size int64
	err := h.db.QueryRowContext(h.ctx, `SELECT size FROM entries WHERE index_id = ? AND relative_path = ?`, h.indexID, relPath).Scan(&size)
	if err != nil {
		h.t.Fatalf("query size %s: %v", relPath, err)
	}
	return size
}

func (h *testHelper) assertSize(relPath string, want int64, desc string) {
	h.t.Helper()
	if got := h.getSize(relPath); got != want {
		h.t.Fatalf("%s: size at %s = %d, want %d", desc, relPath, got, want)
	}
}

func (h *testHelper) upsertFile(entry indexing.IndexEntry) {
	h.t.Helper()
	if err := UpsertEntryWithSizeUpdate(h.ctx, h.db, h.indexID, entry); err != nil {
		h.t.Fatalf("upsert file: %v", err)
	}
}

func (h *testHelper) deletePath(relPath string) {
	h.t.Helper()
	if err := DeletePathRecursive(h.ctx, h.db, h.indexID, relPath); err != nil {
		h.t.Fatalf("delete path: %v", err)
	}
}

func (h *testHelper) assertFileDeleted(relPath string) {
	h.t.Helper()
	var count int
	if err := h.db.QueryRowContext(h.ctx, `SELECT COUNT(*) FROM entries WHERE index_id = ? AND relative_path = ?`, h.indexID, relPath).Scan(&count); err != nil {
		h.t.Fatalf("count file row: %v", err)
	}
	if count != 0 {
		h.t.Fatalf("file row still present after delete (count=%d)", count)
	}
}

func (h *testHelper) assertMetadata(wantDirs, wantFiles, wantSize int64, desc string) {
	h.t.Helper()
	var dirs, files, size, lastIndexed int64
	if err := h.db.QueryRowContext(h.ctx, `
		SELECT num_dirs, num_files, total_size, last_indexed
		FROM indexes WHERE id = ?;
	`, h.indexID).Scan(&dirs, &files, &size, &lastIndexed); err != nil {
		h.t.Fatalf("%s: query metadata: %v", desc, err)
	}
	if dirs != wantDirs || files != wantFiles || size != wantSize {
		h.t.Fatalf("%s: metadata = dirs %d, files %d, size %d; want %d, %d, %d", desc, dirs, files, size, wantDirs, wantFiles, wantSize)
	}
	if lastIndexed <= 1 {
		h.t.Fatalf("%s: last indexed = %d, want refreshed timestamp", desc, lastIndexed)
	}
}

func (h *testHelper) hardlinkAccounting(relPath string) (int64, int64) {
	h.t.Helper()
	var size, contribution int64
	if err := h.db.QueryRowContext(h.ctx, `
		SELECT size, size_contribution FROM entries
		WHERE index_id = ? AND relative_path = ?
	`, h.indexID, relPath).Scan(&size, &contribution); err != nil {
		h.t.Fatalf("read hardlink accounting for %s: %v", relPath, err)
	}
	return size, contribution
}

func hardlinkTestEntry(t *testing.T, path string, size int64) indexing.IndexEntry {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("lstat %s: %v", path, err)
	}
	entry := indexing.EntryFromFileInfo(path, info)
	entry.Size = size
	entry.SizeContribution = 0
	return entry
}

// Integration-style test to ensure manual add/delete operations keep sizes and metadata in sync.
func TestUpsertAndDeletePropagatesSizes(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "index.db")

	db, err := Open(dbPath, DefaultOpenOptions())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() {
		if cerr := db.Close(); cerr != nil {
			t.Fatalf("close db: %v", cerr)
		}
	}()

	// Seed an index row.
	res, err := db.Exec(`
		INSERT INTO indexes (last_indexed)
		VALUES (1);
	`)
	if err != nil {
		t.Fatalf("insert index: %v", err)
	}
	indexID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("index id: %v", err)
	}

	h := &testHelper{t: t, ctx: ctx, db: db, indexID: indexID}

	// Seed root and a child directory so parent size updates have somewhere to land.
	h.seedDir("/")
	h.seedDir("/data")
	if _, err := db.Exec(`UPDATE indexes SET num_dirs = 2 WHERE id = ?`, indexID); err != nil {
		t.Fatalf("seed directory count: %v", err)
	}

	fileEntry := indexing.IndexEntry{
		RelativePath: "/data/file.txt",
		Name:         "file.txt",
		Size:         200,
		ModTime:      time.Now(),
		Type:         "file",
		Hidden:       false,
	}

	h.upsertFile(fileEntry)
	h.assertSize("/data", 200, "after add")
	h.assertSize("/", 200, "after add")
	h.assertMetadata(2, 1, 200, "after add")

	// Increase file size and ensure deltas propagate.
	fileEntry.Size = 350
	h.upsertFile(fileEntry)
	h.assertSize("/data", 350, "after resize")
	h.assertSize("/", 350, "after resize")
	h.assertMetadata(2, 1, 350, "after resize")

	// Delete and ensure sizes drop.
	h.deletePath("/data/file.txt")
	h.assertSize("/data", 0, "after delete")
	h.assertSize("/", 0, "after delete")
	h.assertMetadata(2, 0, 0, "after file delete")
	h.assertFileDeleted("/data/file.txt")

	h.deletePath("/data")
	h.assertMetadata(1, 0, 0, "after directory delete")
}

func TestHardlinkMutationSequenceCountsAllocationOnce(t *testing.T) {
	ctx, db, _ := setupTestDB(t)
	indexID := insertTestIndex(t, db)
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "a"), 0o700); err != nil {
		t.Fatalf("mkdir a: %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, "b"), 0o700); err != nil {
		t.Fatalf("mkdir b: %v", err)
	}
	leftPath := filepath.Join(root, "a", "link")
	rightPath := filepath.Join(root, "b", "link")
	if err := os.WriteFile(leftPath, make([]byte, 4096), 0o600); err != nil {
		t.Fatalf("write hardlink fixture: %v", err)
	}
	if _, err := db.Exec(`UPDATE indexes SET num_dirs = 4, last_indexed = 1 WHERE id = ?`, indexID); err != nil {
		t.Fatalf("seed index metadata: %v", err)
	}
	h := &testHelper{t: t, ctx: ctx, db: db, indexID: indexID}
	h.seedDir("/")
	h.seedDir(root)
	h.seedDir(filepath.Join(root, "a"))
	h.seedDir(filepath.Join(root, "b"))

	h.upsertFile(hardlinkTestEntry(t, leftPath, 4096))
	if err := os.Link(leftPath, rightPath); err != nil {
		t.Fatalf("create hardlink: %v", err)
	}
	h.upsertFile(hardlinkTestEntry(t, rightPath, 4096))
	h.assertMetadata(4, 2, 4096, "after adding both hardlinks")
	if err := os.Remove(rightPath); err != nil {
		t.Fatalf("remove non-contributor: %v", err)
	}
	h.deletePath(rightPath)
	h.assertMetadata(4, 1, 4096, "after deleting non-contributor")
	if err := os.Link(leftPath, rightPath); err != nil {
		t.Fatalf("restore hardlink: %v", err)
	}
	h.upsertFile(hardlinkTestEntry(t, rightPath, 4096))
	if err := os.Remove(leftPath); err != nil {
		t.Fatalf("remove contributor: %v", err)
	}
	h.deletePath(leftPath)
	h.assertSize(filepath.Join(root, "a"), 0, "after deleting contributor")
	h.assertSize(filepath.Join(root, "b"), 4096, "after promoting surviving hardlink")
	h.assertMetadata(4, 1, 4096, "after contributor promotion")
	if err := os.Truncate(rightPath, 8192); err != nil {
		t.Fatalf("resize hardlink: %v", err)
	}
	h.upsertFile(hardlinkTestEntry(t, rightPath, 8192))
	h.assertMetadata(4, 1, 8192, "after hardlink allocation change")
}

func TestDeletePathRecursiveMissingTargetSubtractsStoredRoots(t *testing.T) {
	ctx, db, _ := setupTestDB(t)
	indexID := insertTestIndex(t, db)
	h := &testHelper{t: t, ctx: ctx, db: db, indexID: indexID}
	h.seedDir("/")
	h.seedDir("/parent")
	h.seedDir("/parent/missing/nested")
	if _, err := db.Exec(`UPDATE indexes SET num_dirs = 3 WHERE id = ?`, indexID); err != nil {
		t.Fatalf("seed directory count: %v", err)
	}

	h.upsertFile(indexing.IndexEntry{
		RelativePath: "/parent/missing/file.txt", Name: "file.txt", Size: 10,
		ModTime: time.Now(), Type: "file",
	})
	h.upsertFile(indexing.IndexEntry{
		RelativePath: "/parent/missing/nested/child.txt", Name: "child.txt", Size: 20,
		ModTime: time.Now(), Type: "file",
	})
	h.upsertFile(indexing.IndexEntry{
		RelativePath: "/parent/missing/untracked/deeper.txt", Name: "deeper.txt", Size: 5,
		ModTime: time.Now(), Type: "file",
	})
	h.assertSize("/parent", 35, "before missing-target delete")
	h.assertSize("/", 35, "before missing-target delete")

	h.deletePath("/parent/missing")
	h.assertMetadata(2, 0, 0, "after missing-target delete")
	h.assertFileDeleted("/parent/missing/file.txt")
	if got := h.getSize("/parent"); got != 0 {
		t.Fatalf("parent size after missing-target delete = %d, want 0", got)
	}
}

func TestUpdateParentDirectorySizesThroughNormalizesStopPath(t *testing.T) {
	ctx, db, _ := setupTestDB(t)
	indexID := insertTestIndex(t, db)
	h := &testHelper{t: t, ctx: ctx, db: db, indexID: indexID}
	h.seedDir("/")
	h.seedDir("/tree")
	h.seedDir("/tree/child")

	if err := updateParentDirectorySizesThrough(ctx, db, indexID, "/tree/child/file.txt", 10, "/tree/"); err != nil {
		t.Fatalf("update parent sizes through trailing stop path: %v", err)
	}
	h.assertSize("/tree/child", 10, "after trailing stop path update")
	h.assertSize("/tree", 10, "after trailing stop path update")
	h.assertSize("/", 0, "after trailing stop path update")
}

func TestHardlinkReconcileIgnoresStaleContributorAfterIdentityChange(t *testing.T) {
	ctx, db, _ := setupTestDB(t)
	indexID := insertTestIndex(t, db)
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "a"), 0o700); err != nil {
		t.Fatalf("mkdir a: %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, "b"), 0o700); err != nil {
		t.Fatalf("mkdir b: %v", err)
	}
	validPath := filepath.Join(root, "b", "live")
	stalePath := filepath.Join(root, "a", "stale")
	if err := os.WriteFile(validPath, []byte("live"), 0o600); err != nil {
		t.Fatalf("write valid fixture: %v", err)
	}
	if err := os.WriteFile(stalePath, []byte("stale"), 0o600); err != nil {
		t.Fatalf("write stale fixture: %v", err)
	}
	validEntry := hardlinkTestEntry(t, validPath, 4)
	staleEntry := validEntry
	staleEntry.RelativePath = stalePath
	staleEntry.Name = filepath.Base(stalePath)
	staleEntry.SizeContribution = 4

	h := &testHelper{t: t, ctx: ctx, db: db, indexID: indexID}
	h.seedDir("/")
	h.seedDir(filepath.Join(root, "a"))
	h.seedDir(filepath.Join(root, "b"))
	if _, err := db.Exec(`UPDATE indexes SET num_dirs = 3 WHERE id = ?`, indexID); err != nil {
		t.Fatalf("seed directory count: %v", err)
	}
	if _, err := UpdateEntry(ctx, db, indexID, staleEntry); err != nil {
		t.Fatalf("seed stale entry: %v", err)
	}
	if _, err := UpdateEntry(ctx, db, indexID, validEntry); err != nil {
		t.Fatalf("seed valid entry: %v", err)
	}
	if _, err := db.Exec(`
		UPDATE entries SET size = CASE relative_path
			WHEN ? THEN 4 WHEN ? THEN 4 WHEN ? THEN 0 ELSE size END
		WHERE index_id = ? AND relative_path IN (?, ?, ?)
	`, "/", filepath.Join(root, "a"), filepath.Join(root, "b"), indexID,
		"/", filepath.Join(root, "a"), filepath.Join(root, "b")); err != nil {
		t.Fatalf("seed directory sizes: %v", err)
	}

	identity := hardlinkIdentity{device: int64(validEntry.Device), inode: int64(validEntry.Inode)}
	snapshot, err := SnapshotHardlinksUnderPath(ctx, db, indexID, filepath.Join(root, "a"))
	if err != nil {
		t.Fatalf("snapshot stale hardlink: %v", err)
	}
	if got := snapshot.groups[identity]; got != "" {
		t.Fatalf("stale snapshot contributor = %q, want empty", got)
	}
	if err := reconcileAndApplyHardlink(ctx, db, indexID, identity, hardlinkReconcileOptions{}); err != nil {
		t.Fatalf("reconcile stale hardlink: %v", err)
	}

	_, staleContribution := h.hardlinkAccounting(stalePath)
	_, validContribution := h.hardlinkAccounting(validPath)
	if staleContribution != 0 || validContribution != 4 {
		t.Fatalf("contributions = stale %d, valid %d; want 0, 4", staleContribution, validContribution)
	}
	h.assertSize(filepath.Join(root, "a"), 0, "after stale contributor reconciliation")
	h.assertSize(filepath.Join(root, "b"), 4, "after stale contributor reconciliation")

	if err := os.Remove(validPath); err != nil {
		t.Fatalf("remove valid fixture: %v", err)
	}
	if err := reconcileAndApplyHardlink(ctx, db, indexID, identity, hardlinkReconcileOptions{}); err != nil {
		t.Fatalf("reconcile entirely stale hardlink group: %v", err)
	}
	staleSize, staleContribution := h.hardlinkAccounting(stalePath)
	validSize, validContribution := h.hardlinkAccounting(validPath)
	if staleSize != 4 || validSize != 4 || staleContribution != 0 || validContribution != 0 {
		t.Fatalf("stale group = sizes %d/%d contributions %d/%d; want 4/4 and 0/0", staleSize, validSize, staleContribution, validContribution)
	}
	h.assertSize(filepath.Join(root, "a"), 0, "after entirely stale reconciliation")
	h.assertSize(filepath.Join(root, "b"), 0, "after entirely stale reconciliation")
}

func TestTransactionalStreamingWriterRollbackPreservesEntries(t *testing.T) {
	ctx, db, _ := setupTestDB(t)
	indexID := insertTestIndex(t, db)
	original := indexing.IndexEntry{
		RelativePath: "/docs/kept.txt",
		Name:         "kept.txt",
		Size:         10,
		ModTime:      time.Now(),
		Type:         "file",
		Inode:        1,
	}
	if _, err := UpdateEntry(ctx, db, indexID, original); err != nil {
		t.Fatalf("seed original entry: %v", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	writer := NewTransactionalStreamingWriter(ctx, tx, indexID, 2, nil)
	changed := original
	changed.Size = 99
	if err := writer.Write(changed); err != nil {
		t.Fatalf("write changed entry: %v", err)
	}
	if err := writer.Write(indexing.IndexEntry{
		RelativePath: "/docs/new.txt", Name: "new.txt", Size: 5,
		ModTime: time.Now(), Type: "file", Inode: 2,
	}); err != nil {
		t.Fatalf("write new entry: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback transaction: %v", err)
	}

	var size int64
	if err := db.QueryRowContext(ctx, `SELECT size FROM entries WHERE index_id = ? AND relative_path = ?`, indexID, original.RelativePath).Scan(&size); err != nil {
		t.Fatalf("read original entry: %v", err)
	}
	if size != original.Size {
		t.Fatalf("original size = %d, want %d", size, original.Size)
	}
	var newCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM entries WHERE index_id = ? AND relative_path = '/docs/new.txt'`, indexID).Scan(&newCount); err != nil {
		t.Fatalf("count new entry: %v", err)
	}
	if newCount != 0 {
		t.Fatalf("new entry count = %d, want 0 after rollback", newCount)
	}
}
