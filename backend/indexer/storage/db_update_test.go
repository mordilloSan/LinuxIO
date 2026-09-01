package storage

import (
	"context"
	"database/sql"
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
		Inode:        1,
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
