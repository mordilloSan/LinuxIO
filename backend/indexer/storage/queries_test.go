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
		AbsolutePath: "/data",
		Name:         "data",
		Size:         100,
		ModTime:      time.Now(),
		Type:         "directory",
		Hidden:       false,
		Inode:        1,
	}
	file := indexing.IndexEntry{
		RelativePath: "/data.txt",
		AbsolutePath: "/data.txt",
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
		{RelativePath: "/alpha.txt", AbsolutePath: "/alpha.txt", Name: "alpha.txt", Size: 1, ModTime: now, Type: "file", Inode: 1},
		{RelativePath: "/beta.txt", AbsolutePath: "/beta.txt", Name: "beta.txt", Size: 1, ModTime: now.Add(time.Second), Type: "file", Inode: 2},
		{RelativePath: "/AlphaExact.txt", AbsolutePath: "/AlphaExact.txt", Name: "AlphaExact.txt", Size: 1, ModTime: now.Add(2 * time.Second), Type: "file", Inode: 3},
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
			RelativePath: path, AbsolutePath: path, Name: "report.txt",
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

func TestQueryPathRecursiveUsesPathBoundary(t *testing.T) {
	ctx, db, dbPath := setupTestDB(t)
	indexID := insertTestIndex(t, db)
	seedDirectories(t, ctx, db, indexID, []directorySeed{
		{path: "/home", size: 100},
		{path: "/home/user", size: 50},
		{path: "/home-backup", size: 200},
		{path: "/homeassistant", size: 300},
	})

	store := NewStoreWithDB(db, dbPath)
	results, err := store.QueryPath(ctx, "/home", true, 0, 0, "")
	if err != nil {
		t.Fatalf("query path: %v", err)
	}
	assertResultPaths(t, results, map[string]bool{
		"/home":      true,
		"/home/user": true,
	})
}

func TestQueryPathKeysetAfter(t *testing.T) {
	ctx, db, dbPath := setupTestDB(t)
	indexID := insertTestIndex(t, db)
	seedDirectories(t, ctx, db, indexID, []directorySeed{
		{path: "/home", size: 0},
		{path: "/home/a", size: 1},
		{path: "/home/b", size: 2},
		{path: "/home/c", size: 3},
		{path: "/home-backup", size: 4},
	})

	store := NewStoreWithDB(db, dbPath)
	page1, err := store.QueryPath(ctx, "/home", true, 2, 0, "")
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if len(page1) != 2 || page1[0].Path != "/home" || page1[1].Path != "/home/a" {
		t.Fatalf("first page = %+v, want /home then /home/a", page1)
	}

	// Cursor continues after the last returned path; offset must be ignored
	// and the sibling /home-backup stays excluded.
	page2, err := store.QueryPath(ctx, "/home", true, 10, 99, page1[1].Path)
	if err != nil {
		t.Fatalf("cursor page: %v", err)
	}
	assertResultPaths(t, page2, map[string]bool{
		"/home/b": true,
		"/home/c": true,
	})
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
	results, err := store.QueryPath(ctx, "/data%_set", true, 0, 0, "")
	if err != nil {
		t.Fatalf("query escaped path: %v", err)
	}
	assertResultPaths(t, results, map[string]bool{
		"/data%_set":       true,
		"/data%_set/child": true,
	})

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

//nolint:gocognit // Table setup and all subtree cases intentionally share one indexed fixture.
func TestEntryCount(t *testing.T) {
	t.Run("empty db returns zeros", func(t *testing.T) {
		ctx, db, dbPath := setupTestDB(t)
		store := NewStoreWithDB(db, dbPath)
		files, dirs, err := store.EntryCount(ctx, "/")
		if err != nil {
			t.Fatalf("entry count: %v", err)
		}
		if files != 0 || dirs != 0 {
			t.Fatalf("empty db counts = (files=%d, dirs=%d), want (0, 0)", files, dirs)
		}
	})

	t.Run("mixed tree includes self", func(t *testing.T) {
		ctx, db, dbPath := setupTestDB(t)
		indexID := insertTestIndex(t, db)
		seedDirectories(t, ctx, db, indexID, []directorySeed{
			{path: "/", size: 0},
			{path: "/a", size: 100},
			{path: "/b", size: 200},
		})
		file := indexing.IndexEntry{
			RelativePath: "/a/file.txt",
			AbsolutePath: "/a/file.txt",
			Name:         "file.txt",
			Size:         42,
			ModTime:      time.Now(),
			Type:         "file",
		}
		if _, err := UpdateEntry(ctx, db, indexID, file); err != nil {
			t.Fatalf("seed file: %v", err)
		}

		store := NewStoreWithDB(db, dbPath)

		files, dirs, err := store.EntryCount(ctx, "/")
		if err != nil {
			t.Fatalf("root count: %v", err)
		}
		if dirs != 3 || files != 1 {
			t.Fatalf("root counts = (files=%d, dirs=%d), want (1, 3)", files, dirs)
		}

		files, dirs, err = store.EntryCount(ctx, "/a")
		if err != nil {
			t.Fatalf("subtree count: %v", err)
		}
		if dirs != 1 || files != 1 {
			t.Fatalf("/a counts = (files=%d, dirs=%d), want (1, 1)", files, dirs)
		}

		files, dirs, err = store.EntryCount(ctx, "/a/file.txt")
		if err != nil {
			t.Fatalf("file path count: %v", err)
		}
		if files != 1 || dirs != 0 {
			t.Fatalf("/a/file.txt counts = (files=%d, dirs=%d), want (1, 0)", files, dirs)
		}

		files, dirs, err = store.EntryCount(ctx, "/does/not/exist")
		if err != nil {
			t.Fatalf("nonexistent path count: %v", err)
		}
		if files != 0 || dirs != 0 {
			t.Fatalf("nonexistent counts = (files=%d, dirs=%d), want (0, 0)", files, dirs)
		}
	})

	t.Run("escapes LIKE wildcards", func(t *testing.T) {
		ctx, db, dbPath := setupTestDB(t)
		indexID := insertTestIndex(t, db)
		seedDirectories(t, ctx, db, indexID, []directorySeed{
			{path: "/data%_set", size: 150},
			{path: "/data%_set/child", size: 50},
			{path: "/dataXXset", size: 275},
			{path: "/dataXXset/child", size: 75},
		})

		store := NewStoreWithDB(db, dbPath)
		files, dirs, err := store.EntryCount(ctx, "/data%_set")
		if err != nil {
			t.Fatalf("escaped count: %v", err)
		}
		if dirs != 2 || files != 0 {
			t.Fatalf("/data%%_set counts = (files=%d, dirs=%d), want (0, 2)", files, dirs)
		}
	})
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
	return indexID
}

func seedDirectories(t *testing.T, ctx context.Context, db *sql.DB, indexID int64, dirs []directorySeed) {
	t.Helper()
	for _, dir := range dirs {
		entry := indexing.IndexEntry{
			RelativePath: dir.path,
			AbsolutePath: dir.path,
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

func assertResultPaths(t *testing.T, results []EntryResult, expected map[string]bool) {
	t.Helper()
	if len(results) != len(expected) {
		t.Fatalf("expected %d results, got %d: %+v", len(expected), len(results), results)
	}
	for _, result := range results {
		if !expected[result.Path] {
			t.Fatalf("unexpected path %s in results %+v", result.Path, results)
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
