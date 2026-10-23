package storage

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseSQLiteMemorySucceeds(t *testing.T) {
	db, err := OpenContext(context.Background(), filepath.Join(t.TempDir(), "index.db"), DefaultOpenOptions())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			t.Errorf("close db: %v", closeErr)
		}
	}()

	if err := ReleaseSQLiteMemory(context.Background(), db); err != nil {
		t.Fatalf("ReleaseSQLiteMemory: %v", err)
	}
}

func TestReleaseSQLiteMemoryReportsFailures(t *testing.T) {
	db, err := OpenContext(context.Background(), filepath.Join(t.TempDir(), "index.db"), DefaultOpenOptions())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if closeErr := db.Close(); closeErr != nil {
		t.Fatalf("close db: %v", closeErr)
	}

	err = ReleaseSQLiteMemory(context.Background(), db)
	if err == nil {
		t.Fatal("ReleaseSQLiteMemory returned nil for a closed database")
	}
	for _, operation := range []string{"shrink SQLite memory", "optimize SQLite database"} {
		if !strings.Contains(err.Error(), operation) {
			t.Fatalf("ReleaseSQLiteMemory error %q does not mention %q", err, operation)
		}
	}
}

func TestReleaseSQLiteMemoryRejectsNilDatabase(t *testing.T) {
	if err := ReleaseSQLiteMemory(context.Background(), nil); err == nil {
		t.Fatal("ReleaseSQLiteMemory returned nil for a nil database")
	}
}
