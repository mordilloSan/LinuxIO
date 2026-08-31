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
		AbsolutePath: relPath,
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

func (h *testHelper) deleteFile(relPath string) {
	h.t.Helper()
	if err := DeletePathRecursive(h.ctx, h.db, h.indexID, relPath); err != nil {
		h.t.Fatalf("delete file: %v", err)
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

// Integration-style test to ensure manual add/delete operations keep ancestor sizes in sync.
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
		INSERT INTO indexes (
			name, root_path, source, include_hidden,
			num_dirs, num_files, total_size, disk_used,
			disk_total, last_indexed, index_duration_ms,
			export_duration_ms, vacuum_duration_ms
		) VALUES (?, ?, ?, 0, 0, 0, 0, 0, 0, strftime('%s','now'), 0, 0, 0);
	`, "test", "/", "/")
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

	fileEntry := indexing.IndexEntry{
		RelativePath: "/data/file.txt",
		AbsolutePath: "/data/file.txt",
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

	// Increase file size and ensure deltas propagate.
	fileEntry.Size = 350
	h.upsertFile(fileEntry)
	h.assertSize("/data", 350, "after resize")
	h.assertSize("/", 350, "after resize")

	// Delete and ensure sizes drop.
	h.deleteFile("/data/file.txt")
	h.assertSize("/data", 0, "after delete")
	h.assertSize("/", 0, "after delete")
	h.assertFileDeleted("/data/file.txt")
}
