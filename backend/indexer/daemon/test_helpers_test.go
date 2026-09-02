package daemon

import (
	"path/filepath"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/indexer/storage"
)

func newDaemonWithDB(t *testing.T) (*daemon, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "indexer.db")
	db, err := storage.Open(dbPath, storage.DefaultOpenOptions())
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return &daemon{
		db:    db,
		store: storage.NewStoreWithDB(db, dbPath),
	}, dbPath
}
