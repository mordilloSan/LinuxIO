package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/indexer/indexing"
)

func TestGetDirectSubfolders(t *testing.T) {
	ctx, db, dbPath := setupTestDB(t)
	indexID := insertTestIndex(t, db)
	seedDirectories(t, ctx, db, indexID, []directorySeed{
		{path: "/", size: 0},
		{path: "/data", size: 100},
		{path: "/home", size: 200},
		{path: "/home/user", size: 50},
		{path: "/etc", size: 300},
	})

	store := NewStoreWithDB(db, dbPath)

	t.Run("root", func(t *testing.T) {
		assertSubfolders(t, ctx, store, "/", []folderExpectation{
			{name: "data", size: 100},
			{name: "etc", size: 300},
			{name: "home", size: 200},
		})
	})

	t.Run("home", func(t *testing.T) {
		assertSubfolders(t, ctx, store, "/home", []folderExpectation{
			{name: "user", size: 50},
		})
	})
}

func TestSearchEntriesWithType(t *testing.T) {
	ctx, db, dbPath := setupTestDB(t)
	indexID := insertTestIndex(t, db)

	// Create a folder and a file with similar names
	folder := indexing.IndexEntry{
		RelativePath: "/data",
		Name:         "data",
		Size:         100,
		ModTime:      time.Now(),
		Type:         "directory",
		Hidden:       false,
		Inode:        1,
	}
	file := indexing.IndexEntry{
		RelativePath: "/data.txt",
		Name:         "data.txt",
		Size:         50,
		ModTime:      time.Now(),
		Type:         "file",
		Hidden:       false,
		Inode:        2,
	}

	if _, err := UpdateEntry(ctx, db, indexID, folder); err != nil {
		t.Fatalf("insert folder: %v", err)
	}
	if _, err := UpdateEntry(ctx, db, indexID, file); err != nil {
		t.Fatalf("insert file: %v", err)
	}

	store := NewStoreWithDB(db, dbPath)

	// Search for "data" should return both
	results, err := store.SearchEntriesUnder(ctx, "data", "/", 100)
	if err != nil {
		t.Fatalf("search entries: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// Verify type field is set correctly
	for _, result := range results {
		if result.Type != "folder" && result.Type != "file" {
			t.Errorf("entry has invalid type=%s, want folder or file", result.Type)
		}
	}
}

func TestSearchEntriesUsesParsedTerms(t *testing.T) {
	ctx, db, dbPath := setupTestDB(t)
	indexID := insertTestIndex(t, db)

	now := time.Now()
	entries := []indexing.IndexEntry{
		{RelativePath: "/alpha.txt", Name: "alpha.txt", Size: 1, ModTime: now, Type: "file", Inode: 1},
		{RelativePath: "/beta.txt", Name: "beta.txt", Size: 1, ModTime: now.Add(time.Second), Type: "file", Inode: 2},
		{RelativePath: "/AlphaExact.txt", Name: "AlphaExact.txt", Size: 1, ModTime: now.Add(2 * time.Second), Type: "file", Inode: 3},
	}
	for _, entry := range entries {
		if _, err := UpdateEntry(ctx, db, indexID, entry); err != nil {
			t.Fatalf("insert %s: %v", entry.RelativePath, err)
		}
	}

	store := NewStoreWithDB(db, dbPath)
	results, err := store.SearchEntriesUnder(ctx, "alpha|beta", "/", 100)
	if err != nil {
		t.Fatalf("search OR terms: %v", err)
	}
	assertResultNames(t, results, map[string]bool{
		"alpha.txt":      true,
		"beta.txt":       true,
		"AlphaExact.txt": true,
	})

	results, err = store.SearchEntriesUnder(ctx, "case:exact Alpha", "/", 100)
	if err != nil {
		t.Fatalf("search case exact: %v", err)
	}
	assertResultNames(t, results, map[string]bool{
		"AlphaExact.txt": true,
	})
}

func TestSearchEntriesUnderRestrictsResultsToSubtree(t *testing.T) {
	ctx, db, dbPath := setupTestDB(t)
	indexID := insertTestIndex(t, db)
	for inode, path := range []string{"/docs/report.txt", "/other/report.txt"} {
		entry := indexing.IndexEntry{
			RelativePath: path, Name: "report.txt",
			Size: 1, ModTime: time.Now(), Type: "file", Inode: uint64(inode + 1),
		}
		if _, err := UpdateEntry(ctx, db, indexID, entry); err != nil {
			t.Fatalf("insert %s: %v", path, err)
		}
	}

	results, err := NewStoreWithDB(db, dbPath).SearchEntriesUnder(ctx, "report", "/docs", 100)
	if err != nil {
		t.Fatalf("SearchEntriesUnder: %v", err)
	}
	if len(results) != 1 || results[0].Path != "/docs/report.txt" {
		t.Fatalf("results = %#v", results)
	}
}

func TestPathQueriesEscapeLikeWildcards(t *testing.T) {
	ctx, db, dbPath := setupTestDB(t)
	indexID := insertTestIndex(t, db)
	seedDirectories(t, ctx, db, indexID, []directorySeed{
		{path: "/data%_set", size: 100},
		{path: "/data%_set/child", size: 50},
		{path: "/dataXXset", size: 200},
		{path: "/dataXXset/child", size: 75},
	})

	store := NewStoreWithDB(db, dbPath)
	subfolders, err := store.GetDirectSubfolders(ctx, "/data%_set")
	if err != nil {
		t.Fatalf("query escaped subfolders: %v", err)
	}
	if len(subfolders) != 1 || subfolders[0].Path != "/data%_set/child" {
		t.Fatalf("subfolders = %+v, want only /data%%_set/child", subfolders)
	}
}

func TestDeletePathRecursiveEscapesLikeWildcards(t *testing.T) {
	ctx, db, _ := setupTestDB(t)
	indexID := insertTestIndex(t, db)
	seedDirectories(t, ctx, db, indexID, []directorySeed{
		{path: "/", size: 425},
		{path: "/data%_set", size: 150},
		{path: "/data%_set/child", size: 50},
		{path: "/dataXXset", size: 275},
		{path: "/dataXXset/child", size: 75},
	})

	if err := DeletePathRecursive(ctx, db, indexID, "/data%_set"); err != nil {
		t.Fatalf("delete escaped path: %v", err)
	}

	var deletedCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM entries WHERE relative_path = ? OR relative_path LIKE ? ESCAPE '\'`, "/data%_set", SubtreeLikePattern("/data%_set")).Scan(&deletedCount); err != nil {
		t.Fatalf("count deleted subtree: %v", err)
	}
	if deletedCount != 0 {
		t.Fatalf("deleted subtree count = %d, want 0", deletedCount)
	}

	var siblingCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM entries WHERE relative_path = ? OR relative_path LIKE ? ESCAPE '\'`, "/dataXXset", SubtreeLikePattern("/dataXXset")).Scan(&siblingCount); err != nil {
		t.Fatalf("count sibling subtree: %v", err)
	}
	if siblingCount != 2 {
		t.Fatalf("sibling subtree count = %d, want 2", siblingCount)
	}
}

type folderExpectation struct {
	name string
	size int64
}

type directorySeed struct {
	path string
	size int64
}

func setupTestDB(t *testing.T) (context.Context, *sql.DB, string) {
	t.Helper()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test.db")

	db, err := Open(dbPath, DefaultOpenOptions())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		if cerr := db.Close(); cerr != nil {
			t.Fatalf("close db: %v", cerr)
		}
	})

	return ctx, db, dbPath
}

func insertTestIndex(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	res, err := db.Exec(`
		INSERT INTO indexes (last_indexed)
		VALUES (strftime('%s','now'));
	`)
	if err != nil {
		t.Fatalf("insert index: %v", err)
	}
	indexID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("index id: %v", err)
	}
	return indexID
}

func seedDirectories(t *testing.T, ctx context.Context, db *sql.DB, indexID int64, dirs []directorySeed) {
	t.Helper()
	for _, dir := range dirs {
		entry := indexing.IndexEntry{
			RelativePath: dir.path,
			Name:         filepath.Base(dir.path),
			Size:         dir.size,
			ModTime:      time.Now(),
			Type:         "directory",
			Hidden:       false,
			Inode:        0,
		}
		if dir.path == "/" {
			entry.Name = "/"
		}
		if _, err := UpdateEntry(ctx, db, indexID, entry); err != nil {
			t.Fatalf("seed dir %s: %v", dir.path, err)
		}
	}
}

func assertSubfolders(t *testing.T, ctx context.Context, store *Store, path string, expected []folderExpectation) {
	t.Helper()
	results, err := store.GetDirectSubfolders(ctx, path)
	if err != nil {
		t.Fatalf("get direct subfolders (%s): %v", path, err)
	}
	if len(results) != len(expected) {
		t.Fatalf("expected %d subfolders in %s, got %d", len(expected), path, len(results))
	}
	for i, exp := range expected {
		if results[i].Name != exp.name {
			t.Errorf("result[%d] name = %s, want %s", i, results[i].Name, exp.name)
		}
		if results[i].Size != exp.size {
			t.Errorf("result[%d] size = %d, want %d", i, results[i].Size, exp.size)
		}
	}
}

func assertResultNames(t *testing.T, results []EntryResult, expected map[string]bool) {
	t.Helper()
	if len(results) != len(expected) {
		t.Fatalf("expected %d results, got %d: %+v", len(expected), len(results), results)
	}
	for _, result := range results {
		if !expected[result.Name] {
			t.Fatalf("unexpected name %s in results %+v", result.Name, results)
		}
	}
}
