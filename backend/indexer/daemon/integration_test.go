package daemon

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/indexer/storage"
)

func init() {
	indexerSpawnOverride = func(*daemon, context.Context, func(indexWireEvent)) (*IndexRunStats, error) {
		return nil, nil
	}
}

// TestFullIndexIntegration tests the /index endpoint with real filesystem indexing
func TestFullIndexIntegration(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "index.db")
	db, openErr := storage.Open(dbPath, storage.DefaultOpenOptions())
	if openErr != nil {
		t.Fatalf("open db: %v", openErr)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()
	enableInlineIndexing(t)

	// Create test filesystem
	testRoot := t.TempDir()
	testDir := filepath.Join(testRoot, "documents")
	if err := os.MkdirAll(testDir, 0755); err != nil {
		t.Fatalf("create test dir: %v", err)
	}

	// Create test files
	if err := os.WriteFile(filepath.Join(testDir, "readme.md"), []byte("# README"), 0644); err != nil {
		t.Fatalf("create file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(testRoot, "config.json"), []byte("{}"), 0644); err != nil {
		t.Fatalf("create file: %v", err)
	}

	d := &daemon{
		cfg: DaemonConfig{
			IndexName:     "test",
			IndexPath:     testRoot,
			IncludeHidden: false,
			DBPath:        dbPath,
		},
		db:    db,
		store: storage.NewStoreWithDB(db, dbPath),
	}

	ctx := context.Background()

	// Trigger full index
	req := httptest.NewRequest(http.MethodPost, "/index", nil)
	rr := httptest.NewRecorder()
	d.handleIndex(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("handleIndex status = %d, want 202", rr.Code)
	}

	// Wait for indexing to complete
	waitForIndexCompletion(t, d, 15*time.Second)

	// Verify entries were created
	indexID, err := d.store.LatestIndexID(ctx)
	if err != nil {
		t.Fatalf("get index id: %v", err)
	}

	// Check that root exists
	var rootExists bool
	err = db.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM entries WHERE index_id = ? AND relative_path = ?)
	`, indexID, "/").Scan(&rootExists)
	if err != nil {
		t.Fatalf("query root: %v", err)
	}
	if !rootExists {
		t.Fatal("root directory should exist after full index")
	}

	// Check that files were indexed
	var configExists bool
	err = db.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM entries WHERE index_id = ? AND relative_path = ?)
	`, indexID, "/config.json").Scan(&configExists)
	if err != nil {
		t.Fatalf("query config: %v", err)
	}
	if !configExists {
		t.Fatal("config.json should exist after full index")
	}

	var readmeExists bool
	err = db.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM entries WHERE index_id = ? AND relative_path = ?)
	`, indexID, "/documents/readme.md").Scan(&readmeExists)
	if err != nil {
		t.Fatalf("query readme: %v", err)
	}
	if !readmeExists {
		t.Fatal("readme.md should exist after full index")
	}

	// Verify directory sizes were calculated
	var rootSize int64
	err = db.QueryRowContext(ctx, `SELECT size FROM entries WHERE index_id = ? AND relative_path = ?`, indexID, "/").Scan(&rootSize)
	if err != nil {
		t.Fatalf("query root size: %v", err)
	}
	if rootSize <= 0 {
		t.Fatalf("root size = %d, should be > 0", rootSize)
	}

	t.Logf("Full index completed: root size = %d bytes", rootSize)
}

// TestSearchIntegration tests the /search endpoint with real data
func TestSearchIntegration(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "index.db")
	db, err := storage.Open(dbPath, storage.DefaultOpenOptions())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()
	enableInlineIndexing(t)

	// Create test filesystem
	testRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(testRoot, "readme.txt"), []byte("docs"), 0644); err != nil {
		t.Fatalf("create file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(testRoot, "config.yaml"), []byte("config"), 0644); err != nil {
		t.Fatalf("create file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(testRoot, "readme.md"), []byte("markdown"), 0644); err != nil {
		t.Fatalf("create file: %v", err)
	}

	d := &daemon{
		cfg: DaemonConfig{
			IndexName:     "test",
			IndexPath:     testRoot,
			IncludeHidden: false,
			DBPath:        dbPath,
		},
		db:    db,
		store: storage.NewStoreWithDB(db, dbPath),
	}

	// Trigger index
	req := httptest.NewRequest(http.MethodPost, "/index", nil)
	rr := httptest.NewRecorder()
	d.handleIndex(rr, req)
	waitForIndexCompletion(t, d, 15*time.Second)

	// Test search for "readme"
	searchReq := httptest.NewRequest(http.MethodGet, "/search?q=readme&limit=10", nil)
	searchRR := httptest.NewRecorder()
	d.handleSearch(searchRR, searchReq)

	if searchRR.Code != http.StatusOK {
		t.Fatalf("search status = %d, want 200; body=%s", searchRR.Code, searchRR.Body.String())
	}

	var results []storage.EntryResult
	if err := json.Unmarshal(searchRR.Body.Bytes(), &results); err != nil {
		t.Fatalf("decode search results: %v", err)
	}

	// Should find 2 files with "readme" in name
	if len(results) != 2 {
		t.Fatalf("search for 'readme' returned %d results, want 2", len(results))
	}

	// Verify both are files with correct type
	for _, r := range results {
		if r.Type != "file" {
			t.Errorf("result %s has type %s, want 'file'", r.Path, r.Type)
		}
		if r.Path != "/readme.txt" && r.Path != "/readme.md" {
			t.Errorf("unexpected result: %s", r.Path)
		}
	}

	// Test search with limit
	limitReq := httptest.NewRequest(http.MethodGet, "/search?q=readme&limit=1", nil)
	limitRR := httptest.NewRecorder()
	d.handleSearch(limitRR, limitReq)

	var limitResults []storage.EntryResult
	if err := json.Unmarshal(limitRR.Body.Bytes(), &limitResults); err != nil {
		t.Fatalf("decode limited results: %v", err)
	}

	if len(limitResults) != 1 {
		t.Fatalf("search with limit=1 returned %d results, want 1", len(limitResults))
	}

	t.Logf("Search found %d results for 'readme'", len(results))
}

// TestEntriesIntegration tests the /entries endpoint with real data and recursive flag
//
//nolint:gocyclo // integration test intentionally exercises many variations
func TestEntriesIntegration(t *testing.T) {
	env := newIndexedDaemon(t, func(root string) {
		docsDir := filepath.Join(root, "docs")
		apiDir := filepath.Join(docsDir, "api")
		mustMkdirAll(t, apiDir, 0o755)
		mustWriteFile(t, filepath.Join(root, "root.txt"), []byte("root"))
		mustWriteFile(t, filepath.Join(docsDir, "guide.md"), []byte("guide"))
		mustWriteFile(t, filepath.Join(apiDir, "endpoints.md"), []byte("api"))
	})

	nonRecResults := fetchEntries(t, env.d, "/", false, 100, 0)
	assertNonRecursiveRoot(t, nonRecResults)

	recResults := fetchEntries(t, env.d, "/", true, 100, 0)
	assertRecursiveEntries(t, recResults, "/docs/api/endpoints.md", 5)

	offsetResults := fetchEntries(t, env.d, "/", true, 2, 1)
	assertCount(t, offsetResults, 2, "offset entries")

	t.Logf("Non-recursive: %d entries, Recursive: %d entries", len(nonRecResults), len(recResults))
}

// TestDirSizeIntegration tests the /dirsize endpoint with real calculations
func TestDirSizeIntegration(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "index.db")
	db, err := storage.Open(dbPath, storage.DefaultOpenOptions())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()
	enableInlineIndexing(t)

	// Create filesystem with known sizes
	testRoot := t.TempDir()
	projectDir := filepath.Join(testRoot, "project")
	srcDir := filepath.Join(projectDir, "src")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatalf("create dirs: %v", err)
	}

	// Create files with specific sizes
	if err := os.WriteFile(filepath.Join(srcDir, "main.go"), make([]byte, 1000), 0644); err != nil {
		t.Fatalf("create file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "utils.go"), make([]byte, 500), 0644); err != nil {
		t.Fatalf("create file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "readme.md"), make([]byte, 200), 0644); err != nil {
		t.Fatalf("create file: %v", err)
	}

	d := &daemon{
		cfg: DaemonConfig{
			IndexName:     "test",
			IndexPath:     testRoot,
			IncludeHidden: false,
			DBPath:        dbPath,
		},
		db:    db,
		store: storage.NewStoreWithDB(db, dbPath),
	}

	// Index the filesystem
	req := httptest.NewRequest(http.MethodPost, "/index", nil)
	rr := httptest.NewRecorder()
	d.handleIndex(rr, req)
	waitForIndexCompletion(t, d, 15*time.Second)

	// Test /project/src size
	sizeReq := httptest.NewRequest(http.MethodGet, "/dirsize?path=/project/src", nil)
	sizeRR := httptest.NewRecorder()
	d.handleDirSize(sizeRR, sizeReq)

	if sizeRR.Code != http.StatusOK {
		t.Fatalf("dirsize status = %d, want 200", sizeRR.Code)
	}

	var sizeResp struct {
		Path string `json:"path"`
		Size int64  `json:"size"`
	}
	if err := json.Unmarshal(sizeRR.Body.Bytes(), &sizeResp); err != nil {
		t.Fatalf("decode dirsize response: %v", err)
	}

	if sizeResp.Path != "/project/src" {
		t.Errorf("path = %s, want /project/src", sizeResp.Path)
	}

	// Size should be sum of main.go (1000) + utils.go (500) + directory overhead
	// At minimum should have the file sizes
	if sizeResp.Size < 1500 {
		t.Errorf("src size = %d, want >= 1500 (1000 + 500)", sizeResp.Size)
	}

	// Test /project size (should include src + readme.md)
	projectReq := httptest.NewRequest(http.MethodGet, "/dirsize?path=/project", nil)
	projectRR := httptest.NewRecorder()
	d.handleDirSize(projectRR, projectReq)

	var projectResp struct {
		Path string `json:"path"`
		Size int64  `json:"size"`
	}
	if err := json.Unmarshal(projectRR.Body.Bytes(), &projectResp); err != nil {
		t.Fatalf("decode project dirsize: %v", err)
	}

	// Should be at least 1700 (1000 + 500 + 200)
	if projectResp.Size < 1700 {
		t.Errorf("project size = %d, want >= 1700", projectResp.Size)
	}

	// Project size should be >= src size (since it contains src)
	if projectResp.Size < sizeResp.Size {
		t.Errorf("project size (%d) should be >= src size (%d)", projectResp.Size, sizeResp.Size)
	}

	t.Logf("Src size: %d bytes, Project size: %d bytes", sizeResp.Size, projectResp.Size)
}

// TestSubfoldersIntegration tests the /subfolders endpoint with real directory structure
//
//nolint:gocyclo // integration test intentionally exercises many variations
func TestSubfoldersIntegration(t *testing.T) {
	env := newIndexedDaemon(t, func(root string) {
		for _, dir := range []string{"docs", "src", "tests"} {
			mustMkdirAll(t, filepath.Join(root, dir), 0o755)
		}
		mustWriteFile(t, filepath.Join(root, "docs", "readme.md"), make([]byte, 1000))
		mustWriteFile(t, filepath.Join(root, "src", "main.go"), make([]byte, 5000))
		mustWriteFile(t, filepath.Join(root, "tests", "test.go"), make([]byte, 2000))
		mustMkdirAll(t, filepath.Join(root, "src", "utils"), 0o755)
	})

	rootSubfolders := fetchSubfolders(t, env.d, "/")
	assertCount(t, rootSubfolders, 3, "root subfolders")
	assertHasSubfolders(t, rootSubfolders, map[string]int64{
		"docs":  0,
		"src":   5000,
		"tests": 0,
	})

	srcSubfolders := fetchSubfolders(t, env.d, "/src")
	assertCount(t, srcSubfolders, 1, "src subfolders")
	if srcSubfolders[0].Name != "utils" {
		t.Errorf("src subfolder name = %s, want 'utils'", srcSubfolders[0].Name)
	}
}

// TestReindexIntegration verifies that partial reindexing updates files, removes stale entries,
// and recalculates directory sizes for the affected subtree.
//
//nolint:gocyclo // integration test covers setup/mutation/verification stages
func TestReindexIntegration(t *testing.T) {
	env := newIndexedDaemon(t, func(root string) {
		docsDir := filepath.Join(root, "docs")
		nestedDir := filepath.Join(docsDir, "nested")
		mustMkdirAll(t, nestedDir, 0o755)
		mustWriteFile(t, filepath.Join(docsDir, "legacy.txt"), bytes.Repeat([]byte("a"), 512))
		mustWriteFile(t, filepath.Join(nestedDir, "keep.txt"), bytes.Repeat([]byte("k"), 256))
	})

	indexID := latestIndexID(t, env.d.store)
	if !entryExists(t, env.db, indexID, "/docs/legacy.txt") {
		t.Fatalf("legacy file should exist after initial index")
	}

	mutateReindexFilesystem(t, env.root)
	triggerReindexAndWait(t, env.d, "/docs")

	indexID = latestIndexID(t, env.d.store)

	assertReindexResults(t, env.db, indexID)
}

// TestAddDeleteIntegration tests /add and /delete with size propagation
func TestAddDeleteSizePropagation(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "index.db")
	db, openErr := storage.Open(dbPath, storage.DefaultOpenOptions())
	if openErr != nil {
		t.Fatalf("open db: %v", openErr)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()
	enableInlineIndexing(t)

	testRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(testRoot, "data"), 0755); err != nil {
		t.Fatalf("create dir: %v", err)
	}

	d := &daemon{
		cfg: DaemonConfig{
			IndexName:     "test",
			IndexPath:     testRoot,
			IncludeHidden: false,
			DBPath:        dbPath,
		},
		db:    db,
		store: storage.NewStoreWithDB(db, dbPath),
	}

	ctx := context.Background()

	// Index the filesystem
	req := httptest.NewRequest(http.MethodPost, "/index", nil)
	rr := httptest.NewRecorder()
	d.handleIndex(rr, req)
	waitForIndexCompletion(t, d, 15*time.Second)

	// Get initial sizes
	indexID, err := d.store.LatestIndexID(ctx)
	if err != nil {
		t.Fatalf("get index id: %v", err)
	}

	initialRootSize := entrySize(t, db, indexID, "/")
	initialDataSize := entrySize(t, db, indexID, "/data")

	// Add a new file via API
	addPayload := map[string]any{
		"path":   "/data/newfile.txt",
		"name":   "newfile.txt",
		"size":   5000,
		"type":   "file",
		"hidden": false,
	}
	body, err := json.Marshal(addPayload)
	if err != nil {
		t.Fatalf("marshal add payload: %v", err)
	}
	addReq := httptest.NewRequest(http.MethodPost, "/add", bytes.NewReader(body))
	addRR := httptest.NewRecorder()
	d.handleAdd(addRR, addReq)

	if addRR.Code != http.StatusOK {
		t.Fatalf("add status = %d, want 200; body=%s", addRR.Code, addRR.Body.String())
	}

	// Verify file was added
	if !entryExists(t, db, indexID, "/data/newfile.txt") {
		t.Fatal("file should exist after /add")
	}

	// Verify parent sizes were updated
	afterAddRootSize := entrySize(t, db, indexID, "/")
	afterAddDataSize := entrySize(t, db, indexID, "/data")

	if afterAddRootSize != initialRootSize+5000 {
		t.Errorf("root size after add = %d, want %d (initial %d + 5000)", afterAddRootSize, initialRootSize+5000, initialRootSize)
	}
	if afterAddDataSize != initialDataSize+5000 {
		t.Errorf("data size after add = %d, want %d (initial %d + 5000)", afterAddDataSize, initialDataSize+5000, initialDataSize)
	}

	// Delete the file
	delReq := httptest.NewRequest(http.MethodDelete, "/delete?path=/data/newfile.txt", nil)
	delRR := httptest.NewRecorder()
	d.handleDelete(delRR, delReq)

	if delRR.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want 200; body=%s", delRR.Code, delRR.Body.String())
	}

	// Verify file was deleted
	if entryExists(t, db, indexID, "/data/newfile.txt") {
		t.Fatal("file should not exist after /delete")
	}

	// Verify parent sizes were restored
	afterDelRootSize := entrySize(t, db, indexID, "/")
	afterDelDataSize := entrySize(t, db, indexID, "/data")

	if afterDelRootSize != initialRootSize {
		t.Errorf("root size after delete = %d, want %d (initial)", afterDelRootSize, initialRootSize)
	}
	if afterDelDataSize != initialDataSize {
		t.Errorf("data size after delete = %d, want %d (initial)", afterDelDataSize, initialDataSize)
	}

	t.Logf("Size propagation verified: add +5000, delete -5000 (root: %d -> %d -> %d)",
		initialRootSize, afterAddRootSize, afterDelRootSize)
}

// TestStatusIntegration tests /status endpoint with real index data
func TestStatusIntegration(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "index.db")
	db, err := storage.Open(dbPath, storage.DefaultOpenOptions())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()
	enableInlineIndexing(t)

	testRoot := t.TempDir()
	// Create some files and directories
	if err := os.MkdirAll(filepath.Join(testRoot, "dir1"), 0755); err != nil {
		t.Fatalf("create dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(testRoot, "dir2"), 0755); err != nil {
		t.Fatalf("create dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(testRoot, "file1.txt"), []byte("content"), 0644); err != nil {
		t.Fatalf("create file: %v", err)
	}

	d := &daemon{
		cfg: DaemonConfig{
			IndexName:     "test",
			IndexPath:     testRoot,
			IncludeHidden: false,
			DBPath:        dbPath,
		},
		db:    db,
		store: storage.NewStoreWithDB(db, dbPath),
	}

	// Check status before indexing
	statusReq := httptest.NewRequest(http.MethodGet, "/status", nil)
	statusRR := httptest.NewRecorder()
	d.handleStatus(statusRR, statusReq)

	if statusRR.Code != http.StatusOK {
		t.Fatalf("status before index: status = %d, want 200", statusRR.Code)
	}

	var beforeStatus struct {
		Status       string `json:"status"`
		NumDirs      int64  `json:"num_dirs"`
		NumFiles     int64  `json:"num_files"`
		TotalSize    int64  `json:"total_size"`
		TotalIndexes int    `json:"total_indexes"`
	}
	if err := json.Unmarshal(statusRR.Body.Bytes(), &beforeStatus); err != nil {
		t.Fatalf("decode status: %v", err)
	}

	if beforeStatus.Status != "idle" {
		t.Errorf("status before index = %s, want 'idle'", beforeStatus.Status)
	}

	// Trigger indexing
	indexReq := httptest.NewRequest(http.MethodPost, "/index", nil)
	indexRR := httptest.NewRecorder()
	d.handleIndex(indexRR, indexReq)
	waitForIndexCompletion(t, d, 15*time.Second)

	// Check status after indexing
	statusReq2 := httptest.NewRequest(http.MethodGet, "/status", nil)
	statusRR2 := httptest.NewRecorder()
	d.handleStatus(statusRR2, statusReq2)

	var afterStatus struct {
		Status       string `json:"status"`
		NumDirs      int64  `json:"num_dirs"`
		NumFiles     int64  `json:"num_files"`
		TotalSize    int64  `json:"total_size"`
		LastIndexed  string `json:"last_indexed"`
		TotalIndexes int    `json:"total_indexes"`
		TotalEntries int64  `json:"total_entries"`
		DatabaseSize int64  `json:"database_size"`
	}
	if err := json.Unmarshal(statusRR2.Body.Bytes(), &afterStatus); err != nil {
		t.Fatalf("decode after status: %v", err)
	}

	// Verify counts
	if afterStatus.NumDirs < 2 {
		t.Errorf("num_dirs = %d, want >= 2 (root + at least one child)", afterStatus.NumDirs)
	}
	if afterStatus.NumFiles < 1 {
		t.Errorf("num_files = %d, want >= 1", afterStatus.NumFiles)
	}
	if afterStatus.TotalSize <= 0 {
		t.Errorf("total_size = %d, want > 0", afterStatus.TotalSize)
	}
	if afterStatus.LastIndexed == "" {
		t.Error("last_indexed should be set after indexing")
	}
	if afterStatus.TotalIndexes != 1 {
		t.Errorf("total_indexes = %d, want 1", afterStatus.TotalIndexes)
	}
	if afterStatus.TotalEntries <= 0 {
		t.Errorf("total_entries = %d, want > 0", afterStatus.TotalEntries)
	}

	t.Logf("Status after index: %d dirs, %d files, %d bytes total",
		afterStatus.NumDirs, afterStatus.NumFiles, afterStatus.TotalSize)
}

type indexedDaemon struct {
	d    *daemon
	db   *sql.DB
	root string
}

func newIndexedDaemon(t *testing.T, buildFS func(root string)) indexedDaemon {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "index.db")
	db, err := storage.Open(dbPath, storage.DefaultOpenOptions())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})
	enableInlineIndexing(t)

	root := t.TempDir()
	buildFS(root)

	d := &daemon{
		cfg: DaemonConfig{
			IndexName:     "test",
			IndexPath:     root,
			IncludeHidden: false,
			DBPath:        dbPath,
		},
		db:    db,
		store: storage.NewStoreWithDB(db, dbPath),
	}

	req := httptest.NewRequest(http.MethodPost, "/index", nil)
	rr := httptest.NewRecorder()
	d.handleIndex(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("handleIndex status = %d, want 202; body=%s", rr.Code, rr.Body.String())
	}
	waitForIndexCompletion(t, d, 15*time.Second)

	return indexedDaemon{d: d, db: db, root: root}
}

func fetchEntries(t *testing.T, d *daemon, path string, recursive bool, limit, offset int) []storage.EntryResult {
	t.Helper()

	reqPath := fmt.Sprintf("/entries?path=%s&recursive=%t&limit=%d", path, recursive, limit)
	if offset > 0 {
		reqPath += fmt.Sprintf("&offset=%d", offset)
	}

	req := httptest.NewRequest(http.MethodGet, reqPath, nil)
	rr := httptest.NewRecorder()
	d.handleEntries(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("entries status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var results []storage.EntryResult
	if err := json.Unmarshal(rr.Body.Bytes(), &results); err != nil {
		t.Fatalf("decode entries: %v", err)
	}
	return results
}

func fetchSubfolders(t *testing.T, d *daemon, path string) []storage.SubfolderResult {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/subfolders?path="+path, nil)
	rr := httptest.NewRecorder()
	d.handleSubfolders(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("subfolders status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var results []storage.SubfolderResult
	if err := json.Unmarshal(rr.Body.Bytes(), &results); err != nil {
		t.Fatalf("decode subfolders: %v", err)
	}
	return results
}

func assertNonRecursiveRoot(t *testing.T, results []storage.EntryResult) {
	t.Helper()
	if len(results) != 1 {
		t.Fatalf("non-recursive entries returned %d items, want 1 (root entry)", len(results))
	}
	if results[0].Path != "/" {
		t.Fatalf("non-recursive entry path = %s, want /", results[0].Path)
	}
}

func assertRecursiveEntries(t *testing.T, results []storage.EntryResult, target string, minCount int) {
	t.Helper()
	if len(results) < minCount {
		t.Fatalf("recursive entries returned %d items, want at least %d", len(results), minCount)
	}
	for _, r := range results {
		if r.Path == target && r.Type != "file" {
			t.Fatalf("%s has type %s, want 'file'", target, r.Type)
		}
	}
}

func assertCount[T any](t *testing.T, items []T, want int, label string) {
	t.Helper()
	if len(items) != want {
		t.Fatalf("%s returned %d items, want %d", label, len(items), want)
	}
}

func assertHasSubfolders(t *testing.T, results []storage.SubfolderResult, expected map[string]int64) {
	t.Helper()
	found := make(map[string]int64)
	for _, r := range results {
		found[r.Name] = r.Size
	}
	for name := range expected {
		if _, ok := found[name]; !ok {
			t.Fatalf("missing %q subfolder", name)
		}
	}
	if size, ok := expected["src"]; ok && found["src"] < size {
		t.Fatalf("src size = %d, want >= %d", found["src"], size)
	}
}

func mutateReindexFilesystem(t *testing.T, root string) {
	t.Helper()
	legacyFile := filepath.Join(root, "docs", "legacy.txt")
	mustRemove(t, legacyFile)
	nestedDir := filepath.Join(root, "docs", "nested")
	mustWriteFile(t, filepath.Join(nestedDir, "fresh.bin"), bytes.Repeat([]byte("f"), 4096))
	mustWriteFile(t, filepath.Join(nestedDir, "keep.txt"), bytes.Repeat([]byte("K"), 2048))
}

func triggerReindexAndWait(t *testing.T, d *daemon, path string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/reindex?path="+path, nil)
	rr := httptest.NewRecorder()
	d.handleReindex(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("handleReindex status = %d, want 202; body=%s", rr.Code, rr.Body.String())
	}
	waitForIndexCompletion(t, d, 15*time.Second)
}

func assertReindexResults(t *testing.T, db *sql.DB, indexID int64) {
	t.Helper()
	if entryExists(t, db, indexID, "/docs/legacy.txt") {
		t.Fatal("legacy file should be removed after reindex")
	}
	if !entryExists(t, db, indexID, "/docs/nested/fresh.bin") {
		t.Fatal("fresh.bin should exist after reindex")
	}
	if !entryExists(t, db, indexID, "/docs/nested/keep.txt") {
		t.Fatal("keep.txt should still exist after reindex")
	}

	newSize := entrySize(t, db, indexID, "/docs/nested/fresh.bin")
	if newSize < 4096 {
		t.Fatalf("fresh.bin size = %d, want >= 4096", newSize)
	}
	keepSize := entrySize(t, db, indexID, "/docs/nested/keep.txt")
	if keepSize < 2048 {
		t.Fatalf("keep.txt size = %d, want >= 2048", keepSize)
	}
	docsSize := entrySize(t, db, indexID, "/docs")
	if docsSize < newSize+keepSize {
		t.Fatalf("docs size = %d, want >= %d", docsSize, newSize+keepSize)
	}
	rootSize := entrySize(t, db, indexID, "/")
	if rootSize < docsSize {
		t.Fatalf("root size = %d, want >= %d", rootSize, docsSize)
	}
}

func mustMkdirAll(t *testing.T, path string, perm os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(path, perm); err != nil {
		t.Fatalf("create dir %s: %v", path, err)
	}
}

func mustWriteFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("create file %s: %v", path, err)
	}
}

func mustRemove(t *testing.T, path string) {
	t.Helper()
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove file %s: %v", path, err)
	}
}

func enableInlineIndexing(t *testing.T) {
	t.Helper()
	prev := indexerSpawnOverride
	indexerSpawnOverride = func(d *daemon, ctx context.Context, _ func(indexWireEvent)) (*IndexRunStats, error) {
		stats, err := runIndex(ctx, d.db, d.cfg.IndexName, d.cfg.IndexPath, d.cfg.ExcludePaths, d.cfg.IncludeHidden, d.cfg.IncludeNetworkMounts, d.cfg.FreshIndex, nil)
		if err != nil {
			return nil, err
		}
		return &stats, nil
	}
	t.Cleanup(func() { indexerSpawnOverride = prev })
}

func waitForIndexCompletion(t *testing.T, d *daemon, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !d.running.Load() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("indexing did not finish within %v", timeout)
}

func latestIndexID(t *testing.T, store *storage.Store) int64 {
	t.Helper()
	id, err := store.LatestIndexID(context.Background())
	if err != nil {
		t.Fatalf("latest index id: %v", err)
	}
	return id
}

func entryExists(t *testing.T, db *sql.DB, indexID int64, relPath string) bool {
	t.Helper()
	var exists bool
	err := db.QueryRowContext(context.Background(), `
		SELECT EXISTS(SELECT 1 FROM entries WHERE index_id = ? AND relative_path = ?)
	`, indexID, relPath).Scan(&exists)
	if err != nil {
		t.Fatalf("query %s: %v", relPath, err)
	}
	return exists
}

func entrySize(t *testing.T, db *sql.DB, indexID int64, relPath string) int64 {
	t.Helper()
	var size int64
	err := db.QueryRowContext(context.Background(), `
		SELECT size FROM entries WHERE index_id = ? AND relative_path = ?
	`, indexID, relPath).Scan(&size)
	if err != nil {
		t.Fatalf("size for %s: %v", relPath, err)
	}
	return size
}
