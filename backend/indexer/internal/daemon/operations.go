package daemon

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/indexer/api"
	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/configfile"
	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/indexing"
)

// Index lifecycle. One operation (full index, reindex, or the update of one
// entry) runs at a time; the others wait in the work queue (queue.go).

var operationSequence atomic.Uint64

func newOperationID() string {
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), operationSequence.Add(1))
}

// errNoIndex means an update arrived before the first full index completed.
var errNoIndex = errors.New("no index present")

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

	queued := d.enqueueReindex(ctx, normalizedPath)
	if refused(queued) {
		http.Error(w, errShuttingDown.Error(), http.StatusServiceUnavailable)
		return
	}
	writeJSONStatus(w, http.StatusAccepted, api.OperationResponse{Status: api.StatusRunning, Path: normalizedPath, OperationID: queued.stream.OperationID()})
}

// enqueueReindex queues the reindex of one folder, or returns the one that
// already waits for it.
func (d *daemon) enqueueReindex(ctx context.Context, path string) *job {
	operationID := newOperationID()
	stream := newWorkStreamBroadcaster("reindex", operationID, path)
	queued, _ := d.enqueue(&job{key: "tree:" + path, paths: []string{path}, covers: path, ctx: ctx, stream: stream, run: func(ctx context.Context) error {
		sendSSEEvent(stream, api.EventStarted, api.WorkStartedEvent{
			Status:      api.StatusRunning,
			Operation:   "reindex",
			OperationID: operationID,
			Path:        path,
		})
		d.reindexPath(ctx, operationID, path, stream)
		return nil
	}})
	return queued
}

func (d *daemon) handleAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "use POST", http.StatusMethodNotAllowed)
		return
	}
	var payload api.EntryRequest
	if err := json.UnmarshalRead(http.MaxBytesReader(w, r.Body, maxRequestBodyBytes), &payload, json.RejectUnknownMembers(true)); err != nil {
		http.Error(w, fmt.Sprintf("invalid body: %v", err), http.StatusBadRequest)
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

	relPath := api.NormalizePath(payload.Path)
	if relPath == "/" {
		http.Error(w, "path must not be root; use /reindex", http.StatusBadRequest)
		return
	}
	if d.excludedFromIndex(relPath) {
		writeJSON(w, api.OperationResponse{Status: api.StatusOK})
		return
	}
	if info, err := indexing.Lstat(relPath); err == nil && info.IsDir() {
		http.Error(w, "path must not be a directory; use /reindex", http.StatusBadRequest)
		return
	}
	d.updateEntry(w, r, relPath)
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

	relPath := api.NormalizePath(path)
	if relPath == "/" {
		http.Error(w, "path must not be root; use /reindex to reconcile the index", http.StatusBadRequest)
		return
	}
	d.updateEntry(w, r, relPath)
}

// updateEntry queues the update of one path. On a free daemon it answers when
// the update has been applied. Behind other work it answers queued at once:
// its caller is a file operation in the UI, which must not wait for a scan,
// and the update is applied when its turn comes.
func (d *daemon) updateEntry(w http.ResponseWriter, r *http.Request, relPath string) {
	update := &job{key: "entry:" + relPath, paths: []string{relPath}}
	update.run = func(ctx context.Context) error {
		applied, err := d.reconcileEntry(ctx, relPath)
		update.deferred = !applied
		return err
	}
	queued, joined := d.enqueue(update)
	if joined || queued.behind {
		writeJSONStatus(w, http.StatusAccepted, api.OperationResponse{Status: api.StatusQueued})
		return
	}
	select {
	case <-queued.done:
	case <-r.Context().Done():
		// The daemon is stopping, or the caller went away and reads no
		// answer; the update runs in its turn either way.
		if d.workContext().Err() != nil {
			http.Error(w, errShuttingDown.Error(), http.StatusServiceUnavailable)
		}
		return
	}
	switch err := queued.err; {
	case err == nil && queued.deferred:
		writeJSONStatus(w, http.StatusAccepted, api.OperationResponse{Status: api.StatusQueued})
	case err == nil:
		writeJSON(w, api.OperationResponse{Status: api.StatusOK})
	case errors.Is(err, errNoIndex):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, errShuttingDown), errors.Is(err, context.Canceled):
		http.Error(w, errShuttingDown.Error(), http.StatusServiceUnavailable)
	default:
		http.Error(w, fmt.Sprintf("update failed: %v", err), http.StatusInternalServerError)
	}
}

// reconcileEntry makes the index agree with the disk for one path, whichever
// way the request named the change: an add and a delete that are waiting can
// both be overtaken by what happened to the path since. A path that is gone,
// or that the index leaves out, loses its rows; a file is added or replaced.
// A folder gets a reindex of its own, which a request for the same folder
// that follows joins; applied is then false, the index not being up to date
// for it yet.
func (d *daemon) reconcileEntry(ctx context.Context, relPath string) (applied bool, err error) {
	indexID, err := d.store.LatestIndexID(ctx)
	if err != nil {
		return false, fmt.Errorf("%w: %w", errNoIndex, err)
	}
	// Before the disk is touched: a path left out may be on a mount that no
	// longer answers, and this runs ahead of all other work.
	excluded := d.excludedFromIndex(relPath)
	var info os.FileInfo
	if !excluded {
		// Never through a symlink: see indexing.Lstat.
		info, err = indexing.Lstat(relPath)
	}
	switch {
	case excluded, indexing.NotThere(err):
		if deleteErr := d.store.DeletePath(ctx, indexID, relPath); deleteErr != nil {
			return false, fmt.Errorf("delete entries: %w", deleteErr)
		}
		return true, nil
	case err != nil:
		return false, fmt.Errorf("stat path: %w", err)
	case info.IsDir():
		d.enqueueReindex(d.workContext(), relPath)
		return false, nil
	}
	if err := d.store.UpsertEntry(ctx, indexID, indexing.EntryFromFileInfo(relPath, info)); err != nil {
		return false, fmt.Errorf("upsert entry: %w", err)
	}
	return true, nil
}

func (d *daemon) excludedFromIndex(relPath string) bool {
	cfg := d.settings.config()
	return indexing.IsPathExcludedFromIndex("/", configfile.EffectiveExcludePaths(cfg), cfg.IncludeNetworkMounts, relPath)
}
