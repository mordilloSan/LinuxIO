package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/indexer/api"
	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/storage"
)

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
	resp.DatabaseSize = d.store.DatabaseSize()
	writeJSON(w, resp)
}

func (d *daemon) newStatusResponse(running bool) api.StatusResponse {
	resp := api.StatusResponse{Status: "uninitialized"}
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

func (d *daemon) populateLatestIndexStatus(ctx context.Context, resp *api.StatusResponse) error {
	latest, err := d.store.LatestGeneration(ctx)
	if errors.Is(err, storage.ErrNoGeneration) {
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
	resp.LastIndexed = latest.LastIndexed.Format(time.RFC3339)
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
	basePath, ok := queryPathOrRoot(r.URL.Query().Get("base"))
	if !ok {
		http.Error(w, "invalid base path: path traversal not allowed", http.StatusBadRequest)
		return
	}
	results, err := d.store.SearchEntriesUnder(ctx, q, basePath, searchResultLimit)
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
