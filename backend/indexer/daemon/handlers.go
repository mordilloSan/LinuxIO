package daemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/indexer/api"
	"github.com/mordilloSan/LinuxIO/backend/indexer/indexing"
	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/configfile"
	"github.com/mordilloSan/LinuxIO/backend/indexer/storage"
)

var operationSequence atomic.Uint64

func newOperationID() string {
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), operationSequence.Add(1))
}

const (
	// maxSearchQueryBytes bounds the raw q parameter. Combined with
	// iteminfo.MaxSearchTerms it caps the per-request scan cost of the
	// Search is served only through the root-owned Unix socket.
	maxSearchQueryBytes = 256
)

func (d *daemon) handleIndexContext(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "use POST", http.StatusMethodNotAllowed)
		return
	}
	if !d.tryLockIndex() {
		http.Error(w, "indexer already running", http.StatusConflict)
		return
	}

	operationID := newOperationID()
	stream := d.beginWorkStream("index", operationID, "/")
	d.goBackground(func() {
		defer d.unlockIndex()
		defer d.endWorkStream(stream)

		start := time.Now()
		sendSSEEvent(stream, "started", WorkStartedEvent{
			Status:      "running",
			Operation:   "index",
			OperationID: operationID,
			Path:        "/",
		})

		stats, err := d.runIndexSubprocess(ctx, func(evt indexWireEvent) {
			switch evt.Type {
			case "step":
				// CurrentPath mirrors reindex's "Deleting old entries..."
				// convention so path-displaying clients show phases too.
				sendSSEEvent(stream, "progress", WorkProgressEvent{
					Operation:   "index",
					OperationID: operationID,
					Path:        "/",
					Message:     evt.Message,
					CurrentPath: evt.Message + "...",
				})
			case "scan":
				sendSSEEvent(stream, "progress", WorkProgressEvent{
					Operation:    "index",
					OperationID:  operationID,
					Path:         "/",
					Phase:        "scan",
					FilesIndexed: int64(evt.Files),
					DirsIndexed:  int64(evt.Dirs),
					BytesIndexed: int64(evt.Size),
				})
			}
		})
		if err != nil {
			slog.Error("manual index failed", "err", err)
			sendSSEError(stream, fmt.Sprintf("index failed: %v", err))
			return
		}

		complete := WorkCompleteEvent{
			Status:      "complete",
			Operation:   "index",
			OperationID: operationID,
			Path:        "/",
			DurationMs:  time.Since(start).Milliseconds(),
		}
		if stats != nil {
			complete.FilesIndexed = stats.Files
			complete.DirsIndexed = stats.Dirs
			complete.TotalSize = stats.TotalSize
			complete.DeletedEntries = stats.DeletedEntries
		}
		sendSSEEvent(stream, "complete", complete)
	})
	writeJSONStatus(w, http.StatusAccepted, api.OperationResponse{Status: "running", Path: "/", OperationID: operationID})
}

func (d *daemon) handleReindexContext(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "use POST", http.StatusMethodNotAllowed)
		return
	}

	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		http.Error(w, "path parameter is required", http.StatusBadRequest)
		return
	}

	// Reject path traversal attempts
	if !indexing.ValidateRelativePath(path) {
		http.Error(w, "invalid path: path traversal not allowed", http.StatusBadRequest)
		return
	}

	// Normalize the path
	normalizedPath := indexing.NormalizeIndexPath(path)

	if !d.tryLockIndex() {
		http.Error(w, "indexer already running", http.StatusConflict)
		return
	}

	operationID := newOperationID()
	stream := d.beginWorkStream("reindex", operationID, normalizedPath)
	d.goBackground(func() {
		defer d.unlockIndex()
		defer d.endWorkStream(stream)
		sendSSEEvent(stream, "started", WorkStartedEvent{
			Status:      "running",
			Operation:   "reindex",
			OperationID: operationID,
			Path:        normalizedPath,
		})
		d.reindexPathWithProgress(ctx, operationID, normalizedPath, stream)
	})

	writeJSONStatus(w, http.StatusAccepted, api.OperationResponse{Status: "running", Path: normalizedPath, OperationID: operationID})
}

type statusResponse = api.StatusResponse

func (d *daemon) handleStatus(w http.ResponseWriter, r *http.Request) {
	if wantsStatusSSE(r) && d.attachStatusSSEIfActive(w, r) {
		return
	}

	running := d.running.Load()
	resp := d.newStatusResponse(running)
	if err := d.populateLatestIndexStatus(r.Context(), &resp); err != nil {
		if !running {
			http.Error(w, fmt.Sprintf("error loading status: %v", err), http.StatusInternalServerError)
			return
		}
		resp.AddWarning(fmt.Sprintf("latest index unavailable: %v", err))
		slog.Warn("status: latest index unavailable while indexing", "err", err)
	}
	if err := d.populateDatabaseStatus(r.Context(), &resp); err != nil {
		if !running {
			http.Error(w, fmt.Sprintf("error loading stats: %v", err), http.StatusInternalServerError)
			return
		}
		resp.AddWarning(fmt.Sprintf("stats unavailable: %v", err))
		slog.Warn("status: global stats unavailable while indexing", "err", err)
	}
	writeJSON(w, resp)
}

func (d *daemon) newStatusResponse(running bool) statusResponse {
	resp := statusResponse{Status: "uninitialized"}
	if !running {
		return resp
	}
	resp.Status = "running"
	if active := d.getWorkStreamBroadcaster(); active != nil {
		resp.ActiveOperation = active.Operation()
		resp.ActiveOperationID = active.OperationID()
		resp.ActivePath = active.Path()
	}
	return resp
}

func (d *daemon) populateLatestIndexStatus(ctx context.Context, resp *statusResponse) error {
	latest, err := loadLatestIndex(ctx, d.db)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	resp.NumDirs = latest.NumDirs
	resp.NumFiles = latest.NumFiles
	resp.TotalSize = latest.TotalSize
	if resp.Status != "running" {
		resp.Status = "idle"
	}
	if latest.LastIndexed.Valid && latest.LastIndexed.Int64 > 0 {
		resp.LastIndexed = time.Unix(latest.LastIndexed.Int64, 0).UTC().Format(time.RFC3339)
	}
	return nil
}

func (d *daemon) populateDatabaseStatus(ctx context.Context, resp *statusResponse) error {
	stats, err := d.store.GetStats(ctx)
	if err != nil {
		return err
	}
	resp.DatabaseSize = stats.DatabaseSize
	return nil
}

func (d *daemon) handleSearch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) > maxSearchQueryBytes {
		http.Error(w, "search query too long", http.StatusBadRequest)
		return
	}
	if !api.SearchQueryAllowed(q) {
		http.Error(w, fmt.Sprintf("search query must contain at least %d characters", api.MinSearchQueryRunes), http.StatusBadRequest)
		return
	}
	limit := configfile.SearchLimit
	basePath, ok := queryPathOrRoot(r.URL.Query().Get("base"))
	if !ok {
		http.Error(w, "invalid base path: path traversal not allowed", http.StatusBadRequest)
		return
	}
	results, err := d.store.SearchEntriesUnder(ctx, q, basePath, limit)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, storage.ErrNotInitialized) {
			status = http.StatusServiceUnavailable
		}
		http.Error(w, err.Error(), status)
		return
	}
	writeJSON(w, results)
}

func (d *daemon) handleDirSize(w http.ResponseWriter, r *http.Request) {
	path, ok := queryPathOrRoot(r.URL.Query().Get("path"))
	if !ok {
		http.Error(w, "invalid path: path traversal not allowed", http.StatusBadRequest)
		return
	}
	total, files, dirs, err := d.store.DirDetails(r.Context(), path)
	if err != nil {
		if errors.Is(err, storage.ErrNotInitialized) {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		if errors.Is(err, storage.ErrDirectoryNotFound) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, api.DirSizeResponse{Path: path, Size: total, Files: files, Dirs: dirs})
}

func (d *daemon) handleSubfolders(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	path, ok := queryPathOrRoot(r.URL.Query().Get("path"))
	if !ok {
		http.Error(w, "invalid path: path traversal not allowed", http.StatusBadRequest)
		return
	}

	results, err := d.store.GetDirectSubfolders(ctx, path)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, storage.ErrNotInitialized) {
			status = http.StatusServiceUnavailable
		}
		http.Error(w, err.Error(), status)
		return
	}
	writeJSON(w, results)
}

func (d *daemon) handleAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "use POST", http.StatusMethodNotAllowed)
		return
	}
	var payload api.EntryRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		http.Error(w, fmt.Sprintf("invalid body: %v", err), http.StatusBadRequest)
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		http.Error(w, "invalid body: unexpected trailing JSON", http.StatusBadRequest)
		return
	}
	if payload.Path == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}
	if !indexing.ValidateRelativePath(payload.Path) {
		http.Error(w, "invalid path: path traversal not allowed", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	relPath := indexing.NormalizeIndexPath(payload.Path)
	if relPath == "/" {
		http.Error(w, "path must not be root; use /reindex", http.StatusBadRequest)
		return
	}
	cfg := d.configSnapshot()
	if indexing.IsPathExcludedFromIndex(
		"/",
		configfile.EffectiveExcludePaths(configfile.Config{ExcludePaths: cfg.ExcludePaths}),
		cfg.IncludeNetworkMounts,
		relPath,
	) {
		writeJSON(w, api.OperationResponse{Status: "ok"})
		return
	}
	if !d.tryLockIndex() {
		http.Error(w, "indexer already running", http.StatusConflict)
		return
	}
	defer d.unlockIndex()

	indexID, err := d.store.LatestIndexID(ctx)
	if err != nil {
		http.Error(w, fmt.Sprintf("no index present: %v", err), http.StatusBadRequest)
		return
	}

	info, err := os.Lstat(relPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("stat path: %v", err), http.StatusNotFound)
		return
	}
	if info.IsDir() {
		http.Error(w, "path must not be a directory; use /reindex", http.StatusBadRequest)
		return
	}
	entry := indexing.EntryFromFileInfo(relPath, info)

	if err := storage.UpsertEntryWithSizeUpdate(ctx, d.db, indexID, entry); err != nil {
		http.Error(w, fmt.Sprintf("upsert failed: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, api.OperationResponse{Status: "ok"})
}

func (d *daemon) handleDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "use DELETE", http.StatusMethodNotAllowed)
		return
	}
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}

	// Reject path traversal attempts
	if !indexing.ValidateRelativePath(path) {
		http.Error(w, "invalid path: path traversal not allowed", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	if !d.tryLockIndex() {
		http.Error(w, "indexer already running", http.StatusConflict)
		return
	}
	defer d.unlockIndex()

	indexID, err := d.store.LatestIndexID(ctx)
	if err != nil {
		http.Error(w, fmt.Sprintf("no index present: %v", err), http.StatusBadRequest)
		return
	}

	relPath := indexing.NormalizeIndexPath(path)
	if relPath == "/" {
		http.Error(w, "path must not be root; use /reindex to reconcile the index", http.StatusBadRequest)
		return
	}
	if err := storage.DeletePathRecursive(ctx, d.db, indexID, relPath); err != nil {
		http.Error(w, fmt.Sprintf("delete failed: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, api.OperationResponse{Status: "ok"})
}

func (d *daemon) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, d.savedConfigSnapshot())
	case http.MethodPut:
		d.handleConfigPut(w, r)
	default:
		w.Header().Set("Allow", "GET, PUT")
		http.Error(w, "use GET or PUT", http.StatusMethodNotAllowed)
	}
}

func (d *daemon) handleConfigPut(w http.ResponseWriter, r *http.Request) {
	if !requestFromUnixSocket(r) {
		http.Error(w, "config writes require the Unix socket", http.StatusForbidden)
		return
	}
	if d.running.Load() {
		http.Error(w, "indexer already running", http.StatusConflict)
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		http.Error(w, fmt.Sprintf("read body: %v", err), http.StatusBadRequest)
		return
	}
	patch, err := configfile.DecodePatchJSON(body)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid config JSON: %v", err), http.StatusBadRequest)
		return
	}
	next, err := configfile.ApplyPatch(d.savedConfigSnapshot(), patch)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	configPath := d.configSnapshot().ConfigPath
	if saveErr := configfile.Save(configPath, next); saveErr != nil {
		http.Error(w, fmt.Sprintf("write config file: %v", saveErr), http.StatusInternalServerError)
		return
	}
	if err := d.applySavedConfig(next); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, next)
}

func writeJSON(w http.ResponseWriter, v any) {
	writeJSONStatus(w, http.StatusOK, v)
}

func writeJSONStatus(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("failed to encode JSON response", "err", err)
	}
}

// queryPathOrRoot returns the path query parameter or "/" if empty.
func queryPathOrRoot(path string) (string, bool) {
	if path == "" {
		return "/", true
	}
	// Validate path to prevent traversal attempts
	if !indexing.ValidateRelativePath(path) {
		return "", false
	}
	return indexing.NormalizeIndexPath(path), true
}
