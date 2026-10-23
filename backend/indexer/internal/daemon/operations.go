package daemon

import (
	"context"
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
	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/configfile"
	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/indexing"
)

// Index lifecycle. One operation (full index, reindex, or a single-entry
// add/delete) runs at a time; admission is the running flag.

var operationSequence atomic.Uint64

func newOperationID() string {
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), operationSequence.Add(1))
}

func (d *daemon) tryLockIndex() bool {
	return d.running.CompareAndSwap(false, true)
}

func (d *daemon) unlockIndex() {
	d.markActivity()
	d.running.Store(false)
}

func (d *daemon) handleIndex(ctx context.Context, w http.ResponseWriter, r *http.Request) {
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
		sendSSEEvent(stream, api.EventStarted, api.WorkStartedEvent{
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
				sendSSEEvent(stream, api.EventProgress, api.WorkProgressEvent{
					Operation:   "index",
					OperationID: operationID,
					Path:        "/",
					Message:     evt.Message,
					CurrentPath: evt.Message + "...",
				})
			case "scan":
				sendSSEEvent(stream, api.EventProgress, api.WorkProgressEvent{
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

		complete := api.WorkCompleteEvent{
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
		sendSSEEvent(stream, api.EventComplete, complete)
	})
	writeJSONStatus(w, http.StatusAccepted, api.OperationResponse{Status: "running", Path: "/", OperationID: operationID})
}

func (d *daemon) handleReindex(ctx context.Context, w http.ResponseWriter, r *http.Request) {
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
	if !validateRelativePath(path) {
		http.Error(w, "invalid path: path traversal not allowed", http.StatusBadRequest)
		return
	}

	// Normalize the path
	normalizedPath := api.NormalizePath(path)

	if !d.tryLockIndex() {
		http.Error(w, "indexer already running", http.StatusConflict)
		return
	}

	operationID := newOperationID()
	stream := d.beginWorkStream("reindex", operationID, normalizedPath)
	d.goBackground(func() {
		defer d.unlockIndex()
		defer d.endWorkStream(stream)
		sendSSEEvent(stream, api.EventStarted, api.WorkStartedEvent{
			Status:      "running",
			Operation:   "reindex",
			OperationID: operationID,
			Path:        normalizedPath,
		})
		d.reindexPathWithProgress(ctx, operationID, normalizedPath, stream)
	})

	writeJSONStatus(w, http.StatusAccepted, api.OperationResponse{Status: "running", Path: normalizedPath, OperationID: operationID})
}

func (d *daemon) handleAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "use POST", http.StatusMethodNotAllowed)
		return
	}
	var payload api.EntryRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBodyBytes))
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
	if !validateRelativePath(payload.Path) {
		http.Error(w, "invalid path: path traversal not allowed", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	relPath := api.NormalizePath(payload.Path)
	if relPath == "/" {
		http.Error(w, "path must not be root; use /reindex", http.StatusBadRequest)
		return
	}
	cfg := d.settings.config()
	if indexing.IsPathExcludedFromIndex("/", configfile.EffectiveExcludePaths(cfg), cfg.IncludeNetworkMounts, relPath) {
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

	if err := d.store.UpsertEntry(ctx, indexID, entry); err != nil {
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
	if !validateRelativePath(path) {
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

	relPath := api.NormalizePath(path)
	if relPath == "/" {
		http.Error(w, "path must not be root; use /reindex to reconcile the index", http.StatusBadRequest)
		return
	}
	if err := d.store.DeletePath(ctx, indexID, relPath); err != nil {
		http.Error(w, fmt.Sprintf("delete failed: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, api.OperationResponse{Status: "ok"})
}
