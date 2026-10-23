package daemon

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/indexing"
	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/storage"
)

// newDaemonWithDB returns a daemon backed by a fresh cache and the raw handle
// tests use to seed and inspect rows.
func newDaemonWithDB(t *testing.T) (*daemon, *sql.DB) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "indexer.db")
	db, err := storage.OpenContext(context.Background(), dbPath, storage.DefaultOpenOptions())
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return &daemon{store: storage.NewStoreWithDB(db, dbPath, indexing.SameFile)}, db
}
