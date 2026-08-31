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
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/indexer/indexing"
	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/configfile"
	"github.com/mordilloSan/LinuxIO/backend/indexer/storage"
)

// Exercise the manual add/delete handlers to ensure DB mutations and size propagation work.
func TestHandleAddAndDelete(t *testing.T) {
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

	d := &daemon{
		cfg: DaemonConfig{
			IndexName: "test",
			IndexPath: "/",
			DBPath:    dbPath,
		},
		db:    db,
		store: storage.NewStoreWithDB(db, dbPath),
	}

	// Seed an index row so latestIndexID works.
	if _, err := db.Exec(`
		INSERT INTO indexes (
			name, root_path, source, include_hidden,
			num_dirs, num_files, total_size, disk_used,
			disk_total, last_indexed, index_duration_ms,
			export_duration_ms, vacuum_duration_ms
		) VALUES (?, ?, ?, 0, 0, 0, 0, 0, 0, strftime('%s','now'), 0, 0, 0);
	`, d.cfg.IndexName, d.cfg.IndexPath, d.cfg.IndexPath); err != nil {
		t.Fatalf("insert index: %v", err)
	}
	store := storage.NewStoreWithDB(db, dbPath)
	indexID, err := store.LatestIndexID(context.Background())
	if err != nil {
		t.Fatalf("latest index id: %v", err)
	}

	// Seed root and parent directory rows so size propagation has targets.
	seedDir := func(rel string) {
		entry := indexing.IndexEntry{
			RelativePath: rel,
			AbsolutePath: rel,
			Name:         rel,
			Size:         0,
			ModTime:      time.Now(),
			Type:         "directory",
			Hidden:       false,
			Inode:        0,
		}
		if _, updateErr := storage.UpdateEntry(context.Background(), db, indexID, entry); updateErr != nil {
			t.Fatalf("seed dir %s: %v", rel, updateErr)
		}
	}
	seedDir("/")
	seedDir("/docs")

	// Add a file.
	addPayload := map[string]any{
		"path":   "/docs/file.txt",
		"name":   "file.txt",
		"size":   120,
		"type":   "file",
		"hidden": false,
	}
	body, err := json.Marshal(addPayload)
	if err != nil {
		t.Fatalf("marshal add payload: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/add", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	d.handleAdd(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("handleAdd status = %d, body=%s", rr.Code, rr.Body.String())
	}

	assertSize := func(rel string, want int64) {
		var got int64
		if err := db.QueryRow(`SELECT size FROM entries WHERE relative_path = ?`, rel).Scan(&got); err != nil {
			t.Fatalf("query size %s: %v", rel, err)
		}
		if got != want {
			t.Fatalf("size for %s = %d, want %d", rel, got, want)
		}
	}
	assertSize("/docs", 120)
	assertSize("/", 120)

	// Delete the file.
	delReq := httptest.NewRequest(http.MethodDelete, "/delete?path=/docs/file.txt", nil)
	delRec := httptest.NewRecorder()
	d.handleDelete(delRec, delReq)
	if delRec.Code != http.StatusOK {
		t.Fatalf("handleDelete status = %d, body=%s", delRec.Code, delRec.Body.String())
	}

	assertSize("/docs", 0)
	assertSize("/", 0)

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM entries WHERE relative_path = '/docs/file.txt'`).Scan(&count); err != nil {
		t.Fatalf("count file row: %v", err)
	}
	if count != 0 {
		t.Fatalf("file row still present after delete")
	}
}

// Deleting a directory must remove the directory row plus every descendant,
// roll up the total size to ancestors, and leave unrelated siblings alone.
func TestHandleDeleteDirectorySubtree(t *testing.T) {
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

	d := &daemon{
		cfg: DaemonConfig{
			IndexName: "test",
			IndexPath: "/",
			DBPath:    dbPath,
		},
		db:    db,
		store: storage.NewStoreWithDB(db, dbPath),
	}

	if _, err := db.Exec(`
		INSERT INTO indexes (
			name, root_path, source, include_hidden,
			num_dirs, num_files, total_size, disk_used,
			disk_total, last_indexed, index_duration_ms,
			export_duration_ms, vacuum_duration_ms
		) VALUES (?, ?, ?, 0, 0, 0, 0, 0, 0, strftime('%s','now'), 0, 0, 0);
	`, d.cfg.IndexName, d.cfg.IndexPath, d.cfg.IndexPath); err != nil {
		t.Fatalf("insert index: %v", err)
	}
	store := storage.NewStoreWithDB(db, dbPath)
	indexID, err := store.LatestIndexID(context.Background())
	if err != nil {
		t.Fatalf("latest index id: %v", err)
	}

	seedDir := func(rel string) {
		entry := indexing.IndexEntry{
			RelativePath: rel,
			AbsolutePath: rel,
			Name:         rel,
			Size:         0,
			ModTime:      time.Now(),
			Type:         "directory",
		}
		if _, err := storage.UpdateEntry(context.Background(), db, indexID, entry); err != nil {
			t.Fatalf("seed dir %s: %v", rel, err)
		}
	}
	seedDir("/")
	seedDir("/docs")
	seedDir("/docs/sub")
	seedDir("/other")

	addFile := func(path string, size int64) {
		payload := map[string]any{
			"path":   path,
			"name":   filepath.Base(path),
			"size":   size,
			"type":   "file",
			"hidden": false,
		}
		body, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal add payload: %v", err)
		}
		req := httptest.NewRequest(http.MethodPost, "/add", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		d.handleAdd(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("handleAdd %s status = %d, body=%s", path, rec.Code, rec.Body.String())
		}
	}
	addFile("/docs/sub/a.txt", 100)
	addFile("/docs/sub/b.txt", 200)
	addFile("/other/c.txt", 50)

	assertSize := func(rel string, want int64) {
		t.Helper()
		var got int64
		if err := db.QueryRow(`SELECT size FROM entries WHERE relative_path = ?`, rel).Scan(&got); err != nil {
			t.Fatalf("query size %s: %v", rel, err)
		}
		if got != want {
			t.Fatalf("size for %s = %d, want %d", rel, got, want)
		}
	}
	assertSize("/docs/sub", 300)
	assertSize("/docs", 300)
	assertSize("/other", 50)
	assertSize("/", 350)

	delReq := httptest.NewRequest(http.MethodDelete, "/delete?path=/docs/sub", nil)
	delRec := httptest.NewRecorder()
	d.handleDelete(delRec, delReq)
	if delRec.Code != http.StatusOK {
		t.Fatalf("handleDelete status = %d, body=%s", delRec.Code, delRec.Body.String())
	}

	var subtreeCount int
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM entries
		WHERE relative_path = '/docs/sub' OR relative_path LIKE '/docs/sub/%'
	`).Scan(&subtreeCount); err != nil {
		t.Fatalf("count subtree rows: %v", err)
	}
	if subtreeCount != 0 {
		t.Fatalf("subtree rows still present after delete: %d", subtreeCount)
	}

	assertSize("/docs", 0)
	assertSize("/", 50)

	var otherCount int
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM entries
		WHERE relative_path = '/other' OR relative_path LIKE '/other/%'
	`).Scan(&otherCount); err != nil {
		t.Fatalf("count sibling rows: %v", err)
	}
	if otherCount != 2 {
		t.Fatalf("sibling subtree row count = %d, want 2", otherCount)
	}
	assertSize("/other", 50)
	assertSize("/other/c.txt", 50)
}

// Deleting the root must be rejected so the cascading delete cannot wipe the
// whole index in one call.
func TestHandleDeleteRejectsRoot(t *testing.T) {
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

	d := &daemon{
		cfg: DaemonConfig{
			IndexName: "test",
			IndexPath: "/",
			DBPath:    dbPath,
		},
		db:    db,
		store: storage.NewStoreWithDB(db, dbPath),
	}

	if _, err := db.Exec(`
		INSERT INTO indexes (
			name, root_path, source, include_hidden,
			num_dirs, num_files, total_size, disk_used,
			disk_total, last_indexed, index_duration_ms,
			export_duration_ms, vacuum_duration_ms
		) VALUES (?, ?, ?, 0, 0, 0, 0, 0, 0, strftime('%s','now'), 0, 0, 0);
	`, d.cfg.IndexName, d.cfg.IndexPath, d.cfg.IndexPath); err != nil {
		t.Fatalf("insert index: %v", err)
	}
	store := storage.NewStoreWithDB(db, dbPath)
	indexID, err := store.LatestIndexID(context.Background())
	if err != nil {
		t.Fatalf("latest index id: %v", err)
	}
	if _, err := storage.UpdateEntry(context.Background(), db, indexID, indexing.IndexEntry{
		RelativePath: "/",
		AbsolutePath: "/",
		Name:         "/",
		ModTime:      time.Now(),
		Type:         "directory",
	}); err != nil {
		t.Fatalf("seed root: %v", err)
	}

	for _, path := range []string{"/delete?path=/", "/delete?path=", "/delete?path=%2F"} {
		// "" is rejected by the empty-path guard; "/" and "%2F" exercise the root guard.
		req := httptest.NewRequest(http.MethodDelete, path, nil)
		rec := httptest.NewRecorder()
		d.handleDelete(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("handleDelete %s status = %d, want 400, body=%s", path, rec.Code, rec.Body.String())
		}
	}

	var rootCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM entries WHERE relative_path = '/'`).Scan(&rootCount); err != nil {
		t.Fatalf("count root row: %v", err)
	}
	if rootCount != 1 {
		t.Fatalf("root row count = %d, want 1 (root delete should not run)", rootCount)
	}
}

func TestHandleConfigPutRequiresUnixSocket(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "indexer.yaml")
	d := &daemon{
		cfg: DaemonConfig{
			IndexName:     "root",
			IndexPath:     "/",
			IncludeHidden: true,
			DBPath:        "/tmp/indexer.db",
			SocketPath:    "/var/run/linuxio/indexer.sock",
			ConfigPath:    configPath,
		},
		savedConfig: configfile.Defaults(),
	}

	req := httptest.NewRequest(http.MethodPut, "/config", bytes.NewBufferString(`{"index_path":"/data"}`))
	rr := httptest.NewRecorder()
	d.handleConfig(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("handleConfig status = %d, want 403; body=%s", rr.Code, rr.Body.String())
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("config file should not have been written, stat err=%v", err)
	}
}

func TestHandleConfigPutWritesFileAndAppliesRuntimeFields(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "indexer.yaml")
	d := &daemon{
		cfg: DaemonConfig{
			IndexName:     "root",
			IndexPath:     "/",
			IncludeHidden: true,
			DBPath:        "/tmp/indexer.db",
			SocketPath:    "/var/run/linuxio/indexer.sock",
			Interval:      time.Hour,
			ConfigPath:    configPath,
		},
		savedConfig: configfile.Defaults(),
	}

	body := `{"index_path":"/data","exclude_paths":["/data/cache"],"include_hidden":false,"integrity_check":"quick","interval":"30m","idle_timeout":"45s","listen_addr":":8080"}`
	req := httptest.NewRequest(http.MethodPut, "/config", bytes.NewBufferString(body))
	req = req.WithContext(withConnectionKind(req.Context(), connectionKindUnix))
	rr := httptest.NewRecorder()
	d.handleConfig(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleConfig status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	if rr.Header().Get("X-Indexer-Restart-Required") != "true" {
		t.Fatalf("expected restart-required header for listen_addr change")
	}

	saved, err := configfile.Load(configPath)
	if err != nil {
		t.Fatalf("load saved config: %v", err)
	}
	if saved.IndexPath != "/data" || !slices.Equal(saved.ExcludePaths, []string{"/data/cache"}) || saved.IncludeHidden || saved.IntegrityCheck != configfile.IntegrityCheckQuick || saved.Interval != "30m0s" || saved.IdleTimeout != "45s" || saved.ListenAddr != ":8080" {
		t.Fatalf("unexpected saved config: %#v", saved)
	}

	active := d.configSnapshot()
	if active.IndexPath != "/data" || !slices.Equal(active.ExcludePaths, []string{"/data/cache"}) || active.IncludeHidden || active.IntegrityCheck != configfile.IntegrityCheckQuick || active.Interval != 30*time.Minute || active.IdleTimeout != 45*time.Second {
		t.Fatalf("runtime-safe fields were not applied: %#v", active)
	}
	if active.ListenAddr != "" {
		t.Fatalf("listen_addr should require restart before runtime apply, got %q", active.ListenAddr)
	}
}

func TestHandleConfigPutRejectsInvalidIntegrityCheck(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "indexer.yaml")
	d := &daemon{
		cfg: DaemonConfig{
			IndexName:  "root",
			IndexPath:  "/",
			DBPath:     "/tmp/indexer.db",
			SocketPath: "/var/run/linuxio/indexer.sock",
			ConfigPath: configPath,
		},
		savedConfig: configfile.Defaults(),
	}

	req := httptest.NewRequest(http.MethodPut, "/config", bytes.NewBufferString(`{"integrity_check":"fast"}`))
	req = req.WithContext(withConnectionKind(req.Context(), connectionKindUnix))
	rr := httptest.NewRecorder()
	d.handleConfig(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("handleConfig status = %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("invalid config should not have been written, stat err=%v", err)
	}
}

func TestHandleAddRequiresIndex(t *testing.T) {
	d, _ := newDaemonWithDB(t)

	body := bytes.NewBufferString(`{"path":"/docs/file.txt","name":"file.txt"}`)
	req := httptest.NewRequest(http.MethodPost, "/add", body)
	rr := httptest.NewRecorder()
	d.handleAdd(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("handleAdd without index status = %d, want 400", rr.Code)
	}
}

func TestHandleAddInvalidBody(t *testing.T) {
	d, _ := newDaemonWithIndex(t)

	req := httptest.NewRequest(http.MethodPost, "/add", bytes.NewBufferString("{not-json"))
	rr := httptest.NewRecorder()
	d.handleAdd(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("handleAdd invalid body status = %d, want 400", rr.Code)
	}
}

func TestHandleAddIgnoresExcludedPath(t *testing.T) {
	d, indexID := newDaemonWithIndex(t)
	d.cfg.ExcludePaths = []string{"/private"}

	req := httptest.NewRequest(http.MethodPost, "/add", bytes.NewBufferString(`{"path":"/private/file.txt","name":"file.txt"}`))
	rr := httptest.NewRecorder()
	d.handleAdd(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleAdd status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	var count int
	if err := d.db.QueryRow(`SELECT COUNT(*) FROM entries WHERE index_id = ? AND relative_path = '/private/file.txt'`, indexID).Scan(&count); err != nil {
		t.Fatalf("count excluded entry: %v", err)
	}
	if count != 0 {
		t.Fatal("excluded entry was added")
	}
}

func TestHandleDeleteRequiresIndex(t *testing.T) {
	d, _ := newDaemonWithDB(t)

	req := httptest.NewRequest(http.MethodDelete, "/delete?path=/missing", nil)
	rr := httptest.NewRecorder()
	d.handleDelete(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("handleDelete without index status = %d, want 400", rr.Code)
	}
}

func TestHandleDeleteValidatesPath(t *testing.T) {
	d, _ := newDaemonWithIndex(t)

	req := httptest.NewRequest(http.MethodDelete, "/delete", nil)
	rr := httptest.NewRecorder()
	d.handleDelete(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("handleDelete missing path status = %d, want 400", rr.Code)
	}
}

func TestHandleDeleteMissingEntryIsNoop(t *testing.T) {
	d, indexID := newDaemonWithIndex(t)
	seedDirEntry(t, d.db, indexID, "/data", 100)

	req := httptest.NewRequest(http.MethodDelete, "/delete?path=/data/missing.txt", nil)
	rr := httptest.NewRecorder()
	d.handleDelete(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleDelete missing entry status = %d, want 200", rr.Code)
	}

	// Size should remain unchanged.
	size := querySizeForPath(t, d.db, context.Background(), indexID, "/data")
	if size != 100 {
		t.Fatalf("data dir size changed after deleting missing entry: got %d, want 100", size)
	}
}

// Ensure /status stays responsive while an index is running, even if DB reads fail.
func TestHandleStatusWhileIndexingIgnoresDBErrors(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "index.db")
	db, err := storage.Open(dbPath, storage.DefaultOpenOptions())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	d := &daemon{
		cfg: DaemonConfig{
			IndexName: "test",
			IndexPath: "/",
			DBPath:    dbPath,
		},
		db:    db,
		store: storage.NewStoreWithDB(db, dbPath),
	}

	// Simulate an in-progress index and a temporarily unavailable DB.
	d.running.Store(true)
	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	rr := httptest.NewRecorder()
	d.handleStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status HTTP code = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Status  string `json:"status"`
		Warning string `json:"warning"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != "running" {
		t.Fatalf("status = %q, want \"running\"", resp.Status)
	}
	if resp.Warning == "" {
		t.Fatalf("expected warning when DB is unavailable; body=%s", rr.Body.String())
	}
}

// Test /index endpoint triggers indexing
func TestHandleIndex(t *testing.T) {
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

	d := &daemon{
		cfg: DaemonConfig{
			IndexName: "test",
			IndexPath: "/",
			DBPath:    dbPath,
		},
		db:    db,
		store: storage.NewStoreWithDB(db, dbPath),
	}

	// Test POST request
	req := httptest.NewRequest(http.MethodPost, "/index", nil)
	rr := httptest.NewRecorder()
	d.handleIndex(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("handleIndex status = %d, want 202; body=%s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != "running" {
		t.Fatalf("status = %q, want \"running\"", resp.Status)
	}

	// Wait for index to complete
	time.Sleep(100 * time.Millisecond)

	// Test GET request (should fail)
	req2 := httptest.NewRequest(http.MethodGet, "/index", nil)
	rr2 := httptest.NewRecorder()
	d.handleIndex(rr2, req2)

	if rr2.Code != http.StatusMethodNotAllowed {
		t.Fatalf("handleIndex GET status = %d, want 405", rr2.Code)
	}
}

// Test /index endpoint rejects concurrent requests
func TestHandleIndexConcurrency(t *testing.T) {
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

	d := &daemon{
		cfg: DaemonConfig{
			IndexName: "test",
			IndexPath: "/",
			DBPath:    dbPath,
		},
		db:    db,
		store: storage.NewStoreWithDB(db, dbPath),
	}

	// Simulate index already running
	d.running.Store(true)

	req := httptest.NewRequest(http.MethodPost, "/index", nil)
	rr := httptest.NewRecorder()
	d.handleIndex(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("handleIndex concurrent status = %d, want 409; body=%s", rr.Code, rr.Body.String())
	}
}

// Test /vacuum endpoint triggers a VACUUM run
func TestHandleVacuum(t *testing.T) {
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

	d := &daemon{
		cfg: DaemonConfig{
			IndexName: "test",
			IndexPath: "/",
			DBPath:    dbPath,
		},
		db:    db,
		store: storage.NewStoreWithDB(db, dbPath),
	}

	req := httptest.NewRequest(http.MethodPost, "/vacuum", nil)
	rr := httptest.NewRecorder()
	d.handleVacuum(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("handleVacuum status = %d, want 202; body=%s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != "running" {
		t.Fatalf("status = %q, want \"running\"", resp.Status)
	}

	// Wait for vacuum to complete
	time.Sleep(100 * time.Millisecond)

	req2 := httptest.NewRequest(http.MethodGet, "/vacuum", nil)
	rr2 := httptest.NewRecorder()
	d.handleVacuum(rr2, req2)
	if rr2.Code != http.StatusMethodNotAllowed {
		t.Fatalf("handleVacuum GET status = %d, want 405", rr2.Code)
	}
}

func TestHandleVacuumConcurrency(t *testing.T) {
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

	d := &daemon{
		cfg: DaemonConfig{
			IndexName: "test",
			IndexPath: "/",
			DBPath:    dbPath,
		},
		db:    db,
		store: storage.NewStoreWithDB(db, dbPath),
	}

	d.running.Store(true)

	req := httptest.NewRequest(http.MethodPost, "/vacuum", nil)
	rr := httptest.NewRecorder()
	d.handleVacuum(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("handleVacuum concurrent status = %d, want 409; body=%s", rr.Code, rr.Body.String())
	}
}

// Test /search endpoint
func TestHandleSearch(t *testing.T) {
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

	d := &daemon{
		cfg: DaemonConfig{
			IndexName: "test",
			IndexPath: "/",
			DBPath:    dbPath,
		},
		db:    db,
		store: storage.NewStoreWithDB(db, dbPath),
	}

	// Seed an index
	if _, err := db.Exec(`
		INSERT INTO indexes (
			name, root_path, source, include_hidden,
			num_dirs, num_files, total_size, disk_used,
			disk_total, last_indexed, index_duration_ms,
			export_duration_ms, vacuum_duration_ms
		) VALUES (?, ?, ?, 0, 0, 0, 0, 0, 0, strftime('%s','now'), 0, 0, 0);
	`, d.cfg.IndexName, d.cfg.IndexPath, d.cfg.IndexPath); err != nil {
		t.Fatalf("insert index: %v", err)
	}

	store := storage.NewStoreWithDB(db, dbPath)
	indexID, err := store.LatestIndexID(context.Background())
	if err != nil {
		t.Fatalf("latest index id: %v", err)
	}

	// Seed some entries
	entries := []indexing.IndexEntry{
		{
			RelativePath: "/",
			AbsolutePath: "/",
			Name:         "/",
			Size:         0,
			ModTime:      time.Now(),
			Type:         "directory",
			Hidden:       false,
		},
		{
			RelativePath: "/readme.txt",
			AbsolutePath: "/readme.txt",
			Name:         "readme.txt",
			Size:         100,
			ModTime:      time.Now(),
			Type:         "file",
			Hidden:       false,
		},
		{
			RelativePath: "/test.md",
			AbsolutePath: "/test.md",
			Name:         "test.md",
			Size:         200,
			ModTime:      time.Now(),
			Type:         "file",
			Hidden:       false,
		},
	}

	for _, entry := range entries {
		if _, err := storage.UpdateEntry(context.Background(), db, indexID, entry); err != nil {
			t.Fatalf("seed entry %s: %v", entry.RelativePath, err)
		}
	}

	// Test search
	req := httptest.NewRequest(http.MethodGet, "/search?q=readme&limit=10", nil)
	rr := httptest.NewRecorder()
	d.handleSearch(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleSearch status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var results []map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &results); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(results) == 0 {
		t.Fatalf("expected at least one result for 'readme'")
	}

	for i := range maxSearchLimit + 1 {
		name := fmt.Sprintf("cap-search-%03d.txt", i)
		entry := indexing.IndexEntry{
			RelativePath: "/" + name,
			AbsolutePath: "/" + name,
			Name:         name,
			Size:         int64(i),
			ModTime:      time.Now(),
			Type:         "file",
			Hidden:       false,
		}
		if _, err := storage.UpdateEntry(context.Background(), db, indexID, entry); err != nil {
			t.Fatalf("seed capped search entry %s: %v", entry.RelativePath, err)
		}
	}

	req = httptest.NewRequest(http.MethodGet, "/search?q=cap-search&limit=10000000", nil)
	rr = httptest.NewRecorder()
	d.handleSearch(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleSearch capped status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	results = nil
	if err := json.Unmarshal(rr.Body.Bytes(), &results); err != nil {
		t.Fatalf("decode capped response: %v", err)
	}
	if len(results) != maxSearchLimit {
		t.Fatalf("capped search returned %d results, want %d", len(results), maxSearchLimit)
	}

	d.cfg.SearchDefaultLimit = defaultSearchLimit
	d.cfg.SearchMaxLimit = maxSearchLimit + 1
	req = httptest.NewRequest(http.MethodGet, "/search?q=cap-search&limit=10000000", nil)
	rr = httptest.NewRecorder()
	d.handleSearch(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleSearch configured cap status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	results = nil
	if err := json.Unmarshal(rr.Body.Bytes(), &results); err != nil {
		t.Fatalf("decode configured cap response: %v", err)
	}
	if len(results) != maxSearchLimit+1 {
		t.Fatalf("configured search cap returned %d results, want %d", len(results), maxSearchLimit+1)
	}
}

// Test /dirsize endpoint
func TestHandleDirSize(t *testing.T) {
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

	d := &daemon{
		cfg: DaemonConfig{
			IndexName: "test",
			IndexPath: "/",
			DBPath:    dbPath,
		},
		db:    db,
		store: storage.NewStoreWithDB(db, dbPath),
	}

	// Seed an index
	if _, err := db.Exec(`
		INSERT INTO indexes (
			name, root_path, source, include_hidden,
			num_dirs, num_files, total_size, disk_used,
			disk_total, last_indexed, index_duration_ms,
			export_duration_ms, vacuum_duration_ms
		) VALUES (?, ?, ?, 0, 0, 0, 0, 0, 0, strftime('%s','now'), 0, 0, 0);
	`, d.cfg.IndexName, d.cfg.IndexPath, d.cfg.IndexPath); err != nil {
		t.Fatalf("insert index: %v", err)
	}

	store := storage.NewStoreWithDB(db, dbPath)
	indexID, err := store.LatestIndexID(context.Background())
	if err != nil {
		t.Fatalf("latest index id: %v", err)
	}

	// Seed directory with known size
	entry := indexing.IndexEntry{
		RelativePath: "/docs",
		AbsolutePath: "/docs",
		Name:         "docs",
		Size:         500,
		ModTime:      time.Now(),
		Type:         "directory",
		Hidden:       false,
	}
	if _, err := storage.UpdateEntry(context.Background(), db, indexID, entry); err != nil {
		t.Fatalf("seed entry: %v", err)
	}

	// Test dirsize
	req := httptest.NewRequest(http.MethodGet, "/dirsize?path=/docs", nil)
	rr := httptest.NewRecorder()
	d.handleDirSize(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleDirSize status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Path string `json:"path"`
		Size int64  `json:"size"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.Path != "/docs" {
		t.Fatalf("path = %q, want \"/docs\"", resp.Path)
	}
	if resp.Size != 500 {
		t.Fatalf("size = %d, want 500", resp.Size)
	}

	// Missing directory should return 400, not 500.
	missingReq := httptest.NewRequest(http.MethodGet, "/dirsize?path=/missing", nil)
	missingRR := httptest.NewRecorder()
	d.handleDirSize(missingRR, missingReq)
	if missingRR.Code != http.StatusBadRequest {
		t.Fatalf("handleDirSize missing status = %d, want 400; body=%s", missingRR.Code, missingRR.Body.String())
	}
}

// Test /entrycount endpoint
//
//nolint:gocognit // The endpoint cases share one database fixture and are easier to audit together.
func TestHandleEntryCount(t *testing.T) {
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

	d := &daemon{
		cfg: DaemonConfig{
			IndexName: "test",
			IndexPath: "/",
			DBPath:    dbPath,
		},
		db:    db,
		store: storage.NewStoreWithDB(db, dbPath),
	}

	if _, err := db.Exec(`
		INSERT INTO indexes (
			name, root_path, source, include_hidden,
			num_dirs, num_files, total_size, disk_used,
			disk_total, last_indexed, index_duration_ms,
			export_duration_ms, vacuum_duration_ms
		) VALUES (?, ?, ?, 0, 0, 0, 0, 0, 0, strftime('%s','now'), 0, 0, 0);
	`, d.cfg.IndexName, d.cfg.IndexPath, d.cfg.IndexPath); err != nil {
		t.Fatalf("insert index: %v", err)
	}

	store := storage.NewStoreWithDB(db, dbPath)
	indexID, err := store.LatestIndexID(context.Background())
	if err != nil {
		t.Fatalf("latest index id: %v", err)
	}

	entries := []indexing.IndexEntry{
		{RelativePath: "/", AbsolutePath: "/", Name: "/", Size: 0, ModTime: time.Now(), Type: "directory"},
		{RelativePath: "/docs", AbsolutePath: "/docs", Name: "docs", Size: 0, ModTime: time.Now(), Type: "directory"},
		{RelativePath: "/docs/readme.md", AbsolutePath: "/docs/readme.md", Name: "readme.md", Size: 100, ModTime: time.Now(), Type: "file"},
		{RelativePath: "/docs/intro.md", AbsolutePath: "/docs/intro.md", Name: "intro.md", Size: 200, ModTime: time.Now(), Type: "file"},
	}
	for _, e := range entries {
		if _, err := storage.UpdateEntry(context.Background(), db, indexID, e); err != nil {
			t.Fatalf("seed %s: %v", e.RelativePath, err)
		}
	}

	type response struct {
		Path  string `json:"path"`
		Files int64  `json:"files"`
		Dirs  int64  `json:"dirs"`
	}

	t.Run("subtree includes self", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/entrycount?path=/docs", nil)
		rr := httptest.NewRecorder()
		d.handleEntryCount(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
		}
		var resp response
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp.Path != "/docs" || resp.Files != 2 || resp.Dirs != 1 {
			t.Fatalf("got %+v, want {/docs 2 1}", resp)
		}
	})

	t.Run("no path defaults to root", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/entrycount", nil)
		rr := httptest.NewRecorder()
		d.handleEntryCount(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
		}
		var resp response
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp.Path != "/" || resp.Files != 2 || resp.Dirs != 2 {
			t.Fatalf("got %+v, want {/ 2 2}", resp)
		}
	})

	t.Run("traversal rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/entrycount?path=../etc", nil)
		rr := httptest.NewRecorder()
		d.handleEntryCount(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400; body=%s", rr.Code, rr.Body.String())
		}
	})
}

// Test /subfolders endpoint
func TestHandleSubfolders(t *testing.T) {
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

	d := &daemon{
		cfg: DaemonConfig{
			IndexName: "test",
			IndexPath: "/",
			DBPath:    dbPath,
		},
		db:    db,
		store: storage.NewStoreWithDB(db, dbPath),
	}

	// Seed an index
	if _, err := db.Exec(`
		INSERT INTO indexes (
			name, root_path, source, include_hidden,
			num_dirs, num_files, total_size, disk_used,
			disk_total, last_indexed, index_duration_ms,
			export_duration_ms, vacuum_duration_ms
		) VALUES (?, ?, ?, 0, 0, 0, 0, 0, 0, strftime('%s','now'), 0, 0, 0);
	`, d.cfg.IndexName, d.cfg.IndexPath, d.cfg.IndexPath); err != nil {
		t.Fatalf("insert index: %v", err)
	}

	store := storage.NewStoreWithDB(db, dbPath)
	indexID, err := store.LatestIndexID(context.Background())
	if err != nil {
		t.Fatalf("latest index id: %v", err)
	}

	// Seed root and subdirectories
	dirs := []indexing.IndexEntry{
		{
			RelativePath: "/",
			AbsolutePath: "/",
			Name:         "/",
			Size:         0,
			ModTime:      time.Now(),
			Type:         "directory",
			Hidden:       false,
		},
		{
			RelativePath: "/docs",
			AbsolutePath: "/docs",
			Name:         "docs",
			Size:         300,
			ModTime:      time.Now(),
			Type:         "directory",
			Hidden:       false,
		},
		{
			RelativePath: "/src",
			AbsolutePath: "/src",
			Name:         "src",
			Size:         500,
			ModTime:      time.Now(),
			Type:         "directory",
			Hidden:       false,
		},
	}

	for _, dir := range dirs {
		if _, err := storage.UpdateEntry(context.Background(), db, indexID, dir); err != nil {
			t.Fatalf("seed dir %s: %v", dir.RelativePath, err)
		}
	}

	// Test subfolders
	req := httptest.NewRequest(http.MethodGet, "/subfolders?path=/", nil)
	rr := httptest.NewRecorder()
	d.handleSubfolders(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleSubfolders status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var results []map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &results); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	// Should return /docs and /src
	if len(results) != 2 {
		t.Fatalf("expected 2 subfolders, got %d", len(results))
	}
}

// Test /entries endpoint
//
//nolint:gocognit // The endpoint cases share one database fixture and are easier to audit together.
func TestHandleEntries(t *testing.T) {
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

	d := &daemon{
		cfg: DaemonConfig{
			IndexName: "test",
			IndexPath: "/",
			DBPath:    dbPath,
		},
		db:    db,
		store: storage.NewStoreWithDB(db, dbPath),
	}

	// Seed an index
	if _, err := db.Exec(`
		INSERT INTO indexes (
			name, root_path, source, include_hidden,
			num_dirs, num_files, total_size, disk_used,
			disk_total, last_indexed, index_duration_ms,
			export_duration_ms, vacuum_duration_ms
		) VALUES (?, ?, ?, 0, 0, 0, 0, 0, 0, strftime('%s','now'), 0, 0, 0);
	`, d.cfg.IndexName, d.cfg.IndexPath, d.cfg.IndexPath); err != nil {
		t.Fatalf("insert index: %v", err)
	}

	store := storage.NewStoreWithDB(db, dbPath)
	indexID, err := store.LatestIndexID(context.Background())
	if err != nil {
		t.Fatalf("latest index id: %v", err)
	}

	// Seed entries
	entries := []indexing.IndexEntry{
		{
			RelativePath: "/",
			AbsolutePath: "/",
			Name:         "/",
			Size:         0,
			ModTime:      time.Now(),
			Type:         "directory",
			Hidden:       false,
		},
		{
			RelativePath: "/file1.txt",
			AbsolutePath: "/file1.txt",
			Name:         "file1.txt",
			Size:         100,
			ModTime:      time.Now(),
			Type:         "file",
			Hidden:       false,
		},
		{
			RelativePath: "/file2.txt",
			AbsolutePath: "/file2.txt",
			Name:         "file2.txt",
			Size:         200,
			ModTime:      time.Now(),
			Type:         "file",
			Hidden:       false,
		},
	}

	for _, entry := range entries {
		if _, err := storage.UpdateEntry(context.Background(), db, indexID, entry); err != nil {
			t.Fatalf("seed entry %s: %v", entry.RelativePath, err)
		}
	}

	// Test entries without recursive
	req := httptest.NewRequest(http.MethodGet, "/entries?path=/&limit=10", nil)
	rr := httptest.NewRecorder()
	d.handleEntries(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleEntries status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var results []map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &results); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(results) == 0 {
		t.Fatalf("expected at least one entry")
	}

	// Test with recursive flag
	req2 := httptest.NewRequest(http.MethodGet, "/entries?path=/&recursive=true&limit=100", nil)
	rr2 := httptest.NewRecorder()
	d.handleEntries(rr2, req2)

	if rr2.Code != http.StatusOK {
		t.Fatalf("handleEntries recursive status = %d, want 200", rr2.Code)
	}

	for i := range maxEntriesLimit + 1 {
		name := fmt.Sprintf("cap-entry-%03d.txt", i)
		entry := indexing.IndexEntry{
			RelativePath: "/" + name,
			AbsolutePath: "/" + name,
			Name:         name,
			Size:         int64(i),
			ModTime:      time.Now(),
			Type:         "file",
			Hidden:       false,
		}
		if _, err := storage.UpdateEntry(context.Background(), db, indexID, entry); err != nil {
			t.Fatalf("seed capped entry %s: %v", entry.RelativePath, err)
		}
	}

	req2 = httptest.NewRequest(http.MethodGet, "/entries?path=/&recursive=true&limit=10000000", nil)
	rr2 = httptest.NewRecorder()
	d.handleEntries(rr2, req2)

	if rr2.Code != http.StatusOK {
		t.Fatalf("handleEntries capped status = %d, want 200; body=%s", rr2.Code, rr2.Body.String())
	}
	results = nil
	if err := json.Unmarshal(rr2.Body.Bytes(), &results); err != nil {
		t.Fatalf("decode capped entries response: %v", err)
	}
	if len(results) != maxEntriesLimit {
		t.Fatalf("capped entries returned %d results, want %d", len(results), maxEntriesLimit)
	}

	d.cfg.EntriesDefaultLimit = defaultEntriesLimit
	d.cfg.EntriesMaxLimit = maxEntriesLimit + 1
	req2 = httptest.NewRequest(http.MethodGet, "/entries?path=/&recursive=true&limit=10000000", nil)
	rr2 = httptest.NewRecorder()
	d.handleEntries(rr2, req2)

	if rr2.Code != http.StatusOK {
		t.Fatalf("handleEntries configured cap status = %d, want 200; body=%s", rr2.Code, rr2.Body.String())
	}
	results = nil
	if err := json.Unmarshal(rr2.Body.Bytes(), &results); err != nil {
		t.Fatalf("decode configured entries cap response: %v", err)
	}
	if len(results) != maxEntriesLimit+1 {
		t.Fatalf("configured entries cap returned %d results, want %d", len(results), maxEntriesLimit+1)
	}

	// Keyset cursor: entries strictly after the given path, offset ignored.
	req2 = httptest.NewRequest(http.MethodGet, "/entries?path=/&recursive=true&limit=2&offset=5&after=/cap-entry-000.txt", nil)
	rr2 = httptest.NewRecorder()
	d.handleEntries(rr2, req2)

	if rr2.Code != http.StatusOK {
		t.Fatalf("handleEntries keyset status = %d, want 200; body=%s", rr2.Code, rr2.Body.String())
	}
	results = nil
	if err := json.Unmarshal(rr2.Body.Bytes(), &results); err != nil {
		t.Fatalf("decode keyset response: %v", err)
	}
	if len(results) != 2 || results[0]["path"] != "/cap-entry-001.txt" || results[1]["path"] != "/cap-entry-002.txt" {
		t.Fatalf("keyset page = %+v, want /cap-entry-001.txt then /cap-entry-002.txt", results)
	}
}

func TestHandleSearchRejectsOverlongQuery(t *testing.T) {
	d := &daemon{}
	req := httptest.NewRequest(http.MethodGet, "/search?q="+strings.Repeat("a", maxSearchQueryBytes+1), nil)
	rr := httptest.NewRecorder()
	d.handleSearch(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("overlong query status = %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
}

func TestQueryBoundedInt(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		def  int
		min  int
		max  int
		want int
	}{
		{name: "default", raw: "", def: 100, min: 1, max: 100, want: 100},
		{name: "invalid", raw: "many", def: 100, min: 1, max: 100, want: 100},
		{name: "below min", raw: "0", def: 100, min: 1, max: 100, want: 100},
		{name: "within range", raw: "25", def: 100, min: 1, max: 100, want: 25},
		{name: "above max", raw: "10000000", def: 100, min: 1, max: 100, want: 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := queryBoundedInt(tt.raw, tt.def, tt.min, tt.max); got != tt.want {
				t.Fatalf("queryBoundedInt(%q, %d, %d, %d) = %d, want %d", tt.raw, tt.def, tt.min, tt.max, got, tt.want)
			}
		})
	}
}

// Test /openapi.json endpoint
func TestServeOpenapi(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	rr := httptest.NewRecorder()
	serveOpenapi(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("serveOpenapi status = %d, want 200", rr.Code)
	}

	contentType := rr.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Fatalf("Content-Type = %q, want \"application/json\"", contentType)
	}

	var spec map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &spec); err != nil {
		t.Fatalf("decode openapi spec: %v", err)
	}

	if spec["openapi"] != "3.0.0" {
		t.Fatalf("openapi version = %v, want \"3.0.0\"", spec["openapi"])
	}

	info, ok := spec["info"].(map[string]any)
	if !ok {
		t.Fatalf("info field missing or invalid")
	}

	if info["version"] != "2.3.0" {
		t.Fatalf("API version = %v, want \"2.3.0\"", info["version"])
	}
}

// Test /reindex endpoint
func TestHandleReindex(t *testing.T) {
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

	d := &daemon{
		cfg: DaemonConfig{
			IndexName: "test",
			IndexPath: "/",
			DBPath:    dbPath,
		},
		db:    db,
		store: storage.NewStoreWithDB(db, dbPath),
	}

	// Seed an index
	if _, err := db.Exec(`
		INSERT INTO indexes (
			name, root_path, source, include_hidden,
			num_dirs, num_files, total_size, disk_used,
			disk_total, last_indexed, index_duration_ms,
			export_duration_ms, vacuum_duration_ms
		) VALUES (?, ?, ?, 0, 0, 0, 0, 0, 0, strftime('%s','now'), 0, 0, 0);
	`, d.cfg.IndexName, d.cfg.IndexPath, d.cfg.IndexPath); err != nil {
		t.Fatalf("insert index: %v", err)
	}

	// Test POST request with path parameter
	req := httptest.NewRequest(http.MethodPost, "/reindex?path=/home/user", nil)
	rr := httptest.NewRecorder()
	d.handleReindex(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("handleReindex status = %d, want 202; body=%s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Status string `json:"status"`
		Path   string `json:"path"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != "running" {
		t.Fatalf("status = %q, want \"running\"", resp.Status)
	}
	if resp.Path != "/home/user" {
		t.Fatalf("path = %q, want \"/home/user\"", resp.Path)
	}

	// Test missing path parameter
	req2 := httptest.NewRequest(http.MethodPost, "/reindex", nil)
	rr2 := httptest.NewRecorder()
	d.handleReindex(rr2, req2)

	if rr2.Code != http.StatusBadRequest {
		t.Fatalf("handleReindex without path status = %d, want 400", rr2.Code)
	}

	// Test GET request (should fail)
	req3 := httptest.NewRequest(http.MethodGet, "/reindex?path=/test", nil)
	rr3 := httptest.NewRecorder()
	d.handleReindex(rr3, req3)

	if rr3.Code != http.StatusMethodNotAllowed {
		t.Fatalf("handleReindex GET status = %d, want 405", rr3.Code)
	}

	// Wait for reindex to complete
	time.Sleep(100 * time.Millisecond)
}

// Test /reindex endpoint with real data verification
//
//nolint:gocyclo // integration test needs multiple setup/assert stages
func TestHandleReindexIntegration(t *testing.T) {
	env := setupHandleReindexEnv(t)
	ctx := context.Background()

	initialRoot := querySizeForPath(t, env.db, ctx, env.indexID, "/")
	assertEqual(t, initialRoot, int64(1000), "initial root size")

	triggerReindexAndWaitHTTP(t, env.d, "/data")

	requireExists(t, env.db, ctx, env.indexID, "/data/file1.txt")
	requireExists(t, env.db, ctx, env.indexID, "/data/subdir/file2.txt")
	requireMissing(t, env.db, ctx, env.indexID, "/data/old_file.txt")

	finalRoot := querySizeForPath(t, env.db, ctx, env.indexID, "/")
	dataSize := querySizeForPath(t, env.db, ctx, env.indexID, "/data")

	if finalRoot < 0 {
		t.Fatalf("final root size = %d, should be >= 0", finalRoot)
	}
	if dataSize < 0 {
		t.Fatalf("data size = %d, should be >= 0", dataSize)
	}
}

type handleReindexEnv struct {
	d       *daemon
	db      *sql.DB
	indexID int64
}

func setupHandleReindexEnv(t *testing.T) handleReindexEnv {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "index.db")
	db, openErr := storage.Open(dbPath, storage.DefaultOpenOptions())
	if openErr != nil {
		t.Fatalf("open db: %v", openErr)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})

	testRoot := t.TempDir()
	testDir := filepath.Join(testRoot, "data")
	testSubdir := filepath.Join(testDir, "subdir")
	mustMkdirAllHTTP(t, testSubdir, 0o755)
	mustWriteFileHTTP(t, filepath.Join(testDir, "file1.txt"), []byte("content1"))
	mustWriteFileHTTP(t, filepath.Join(testSubdir, "file2.txt"), []byte("content2"))

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
	if _, err := db.Exec(`
		INSERT INTO indexes (
			name, root_path, source, include_hidden,
			num_dirs, num_files, total_size, disk_used,
			disk_total, last_indexed, index_duration_ms,
			export_duration_ms, vacuum_duration_ms
		) VALUES (?, ?, ?, 0, 0, 0, 0, 0, 0, strftime('%s','now'), 0, 0, 0);
	`, d.cfg.IndexName, d.cfg.IndexPath, d.cfg.IndexPath); err != nil {
		t.Fatalf("insert index: %v", err)
	}

	store := storage.NewStoreWithDB(db, dbPath)
	indexID, err := store.LatestIndexID(ctx)
	if err != nil {
		t.Fatalf("latest index id: %v", err)
	}

	seedReindexEntries(t, ctx, db, indexID, testRoot, testDir, testSubdir)

	return handleReindexEnv{d: d, db: db, indexID: indexID}
}

func seedReindexEntries(t *testing.T, ctx context.Context, db *sql.DB, indexID int64, testRoot, testDir, testSubdir string) {
	t.Helper()
	entries := []indexing.IndexEntry{
		{
			RelativePath: "/",
			AbsolutePath: testRoot,
			Name:         "/",
			Size:         1000,
			ModTime:      time.Now(),
			Type:         "directory",
			Hidden:       false,
		},
		{
			RelativePath: "/data",
			AbsolutePath: testDir,
			Name:         "data",
			Size:         500,
			ModTime:      time.Now(),
			Type:         "directory",
			Hidden:       false,
		},
		{
			RelativePath: "/data/subdir",
			AbsolutePath: testSubdir,
			Name:         "subdir",
			Size:         200,
			ModTime:      time.Now(),
			Type:         "directory",
			Hidden:       false,
		},
		{
			RelativePath: "/data/old_file.txt",
			AbsolutePath: filepath.Join(testDir, "old_file.txt"),
			Name:         "old_file.txt",
			Size:         100,
			ModTime:      time.Now(),
			Type:         "file",
			Hidden:       false,
		},
	}

	for _, entry := range entries {
		if _, err := storage.UpdateEntry(ctx, db, indexID, entry); err != nil {
			t.Fatalf("seed entry %s: %v", entry.RelativePath, err)
		}
	}
}

func triggerReindexAndWaitHTTP(t *testing.T, d *daemon, path string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/reindex?path="+path, nil)
	rr := httptest.NewRecorder()
	d.handleReindex(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("handleReindex status = %d, want 202; body=%s", rr.Code, rr.Body.String())
	}
	waitForIndexCompletion(t, d, 15*time.Second)
}

func querySizeForPath(t *testing.T, db *sql.DB, ctx context.Context, indexID int64, path string) int64 {
	t.Helper()
	var size int64
	if err := db.QueryRowContext(ctx, `SELECT size FROM entries WHERE index_id = ? AND relative_path = ?`, indexID, path).Scan(&size); err != nil {
		t.Fatalf("query size for %s: %v", path, err)
	}
	return size
}

func newDaemonWithDB(t *testing.T) (*daemon, int64) {
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

	d := &daemon{
		cfg: DaemonConfig{
			IndexName: "test",
			IndexPath: "/",
			DBPath:    dbPath,
		},
		db:    db,
		store: storage.NewStoreWithDB(db, dbPath),
	}
	return d, 0
}

func newDaemonWithIndex(t *testing.T) (*daemon, int64) {
	t.Helper()
	d, _ := newDaemonWithDB(t)
	seedIndexRow(t, d.db, d.cfg)
	indexID, err := d.store.LatestIndexID(context.Background())
	if err != nil {
		t.Fatalf("latest index id: %v", err)
	}
	return d, indexID
}

func seedIndexRow(t *testing.T, db *sql.DB, cfg DaemonConfig) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO indexes (
			name, root_path, source, include_hidden,
			num_dirs, num_files, total_size, disk_used,
			disk_total, last_indexed, index_duration_ms,
			export_duration_ms, vacuum_duration_ms
		) VALUES (?, ?, ?, 0, 0, 0, 0, 0, 0, strftime('%s','now'), 0, 0, 0);
	`, cfg.IndexName, cfg.IndexPath, cfg.IndexPath); err != nil {
		t.Fatalf("insert index: %v", err)
	}
}

func seedDirEntry(t *testing.T, db *sql.DB, indexID int64, relPath string, size int64) {
	t.Helper()
	entry := indexing.IndexEntry{
		RelativePath: relPath,
		AbsolutePath: relPath,
		Name:         relPath,
		Size:         size,
		ModTime:      time.Now(),
		Type:         "directory",
		Hidden:       false,
	}
	if _, err := storage.UpdateEntry(context.Background(), db, indexID, entry); err != nil {
		t.Fatalf("seed dir %s: %v", relPath, err)
	}
}

func requireExists(t *testing.T, db *sql.DB, ctx context.Context, indexID int64, path string) {
	t.Helper()
	if !queryExistsPath(t, db, ctx, indexID, path) {
		t.Fatalf("%s should exist after reindex", path)
	}
}

func requireMissing(t *testing.T, db *sql.DB, ctx context.Context, indexID int64, path string) {
	t.Helper()
	if queryExistsPath(t, db, ctx, indexID, path) {
		t.Fatalf("%s should have been removed during reindex", path)
	}
}

func queryExistsPath(t *testing.T, db *sql.DB, ctx context.Context, indexID int64, path string) bool {
	t.Helper()
	var exists bool
	if err := db.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM entries WHERE index_id = ? AND relative_path = ?)
	`, indexID, path).Scan(&exists); err != nil {
		t.Fatalf("query %s: %v", path, err)
	}
	return exists
}

func assertEqual(t *testing.T, got, want int64, label string) {
	t.Helper()
	if got != want {
		t.Fatalf("%s = %d, want %d", label, got, want)
	}
}

func mustMkdirAllHTTP(t *testing.T, path string, perm os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(path, perm); err != nil {
		t.Fatalf("create dir %s: %v", path, err)
	}
}

func mustWriteFileHTTP(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("create file %s: %v", path, err)
	}
}
