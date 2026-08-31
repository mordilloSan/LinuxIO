package storage

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	sqlite3 "github.com/mattn/go-sqlite3"

	"github.com/mordilloSan/LinuxIO/backend/indexer/indexing"
)

func TestDatabaseDiskUsage(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "smoke.db")
	db, err := Open(dbPath, DefaultOpenOptions())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	res, err := db.ExecContext(ctx, `INSERT INTO indexes (name, root_path, last_indexed) VALUES ('smoke', '/', 0);`)
	if err != nil {
		t.Fatalf("insert index: %v", err)
	}
	indexID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}
	for i := range 50 {
		if _, updateErr := UpdateEntry(ctx, db, indexID, indexing.IndexEntry{
			RelativePath: filepath.Join("/dir", "file", time.Now().String()),
			Name:         "f",
			Type:         "file",
			Size:         int64(i),
			ModTime:      time.Now(),
		}); updateErr != nil {
			t.Fatalf("update entry %d: %v", i, updateErr)
		}
	}

	usage, err := DatabaseDiskUsage(ctx, db)
	if errors.Is(err, ErrDBStatUnavailable) {
		t.Log("dbstat not compiled in (run with -tags sqlite_dbstat)")
		return
	}
	if err != nil {
		t.Fatalf("dbstat: %v", err)
	}
	for _, u := range usage {
		t.Logf("object=%s pages=%d bytes=%d unused=%d", u.Name, u.Pages, u.Bytes, u.UnusedBytes)
	}
}

func TestIsCorruptionError(t *testing.T) {
	if !IsCorruptionError(sqlite3.Error{Code: sqlite3.ErrCorrupt}) {
		t.Fatal("ErrCorrupt should classify as corruption")
	}
	if !IsCorruptionError(fmt.Errorf("wrapped: %w", sqlite3.Error{Code: sqlite3.ErrNotADB})) {
		t.Fatal("wrapped ErrNotADB should classify as corruption")
	}
	if IsCorruptionError(sqlite3.Error{Code: sqlite3.ErrBusy}) {
		t.Fatal("ErrBusy is operational, not corruption")
	}
	if IsCorruptionError(errors.New("disk I/O error")) {
		t.Fatal("plain errors are not corruption")
	}
}
