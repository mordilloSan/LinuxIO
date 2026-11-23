package daemon

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/indexer/api"
	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/configfile"
	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/indexing"
	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/storage"
)

// Refresh of a folder that was opened. The file browser asks for it with
// every listing, so that a change made outside LinuxIO shows up where someone
// is looking without waiting for the next scan. It reads the folder's own
// entries and never goes into its subfolders: one it finds new gets a reindex
// of its own.

// refreshWindow is how long a refreshed folder is not read again. Going in
// and out of a folder then costs nothing; a change made outside LinuxIO
// inside the window shows up one visit later.
const refreshWindow = 30 * time.Second

// refreshEntryLimit is how many entries a folder may have to be refreshed
// when it is opened. A larger one waits for the scan: reading it would cost
// more than the listing that asked. A variable for tests.
var refreshEntryLimit = 10_000

// refreshLog remembers when each folder was last refreshed, for the length of
// the window.
type refreshLog struct {
	mu sync.Mutex
	at map[string]time.Time
}

func (d *daemon) refreshedRecently(path string) bool {
	d.refreshes.mu.Lock()
	defer d.refreshes.mu.Unlock()
	at, ok := d.refreshes.at[path]
	return ok && time.Since(at) < refreshWindow
}

// markRefreshed records that path was just refreshed and forgets the folders
// whose window has passed, so the log holds no more than was opened in one
// window.
func (d *daemon) markRefreshed(path string) {
	now := time.Now()
	d.refreshes.mu.Lock()
	defer d.refreshes.mu.Unlock()
	if d.refreshes.at == nil {
		d.refreshes.at = make(map[string]time.Time)
	}
	for folder, at := range d.refreshes.at {
		if now.Sub(at) >= refreshWindow {
			delete(d.refreshes.at, folder)
		}
	}
	d.refreshes.at[path] = now
}

// handleRefresh queues the refresh of one folder and answers at once: the
// listing that asked for it has already been sent, and nothing waits for the
// index. 200 means there was nothing to queue.
func (d *daemon) handleRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "use POST", http.StatusMethodNotAllowed)
		return
	}
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		http.Error(w, "path parameter is required", http.StatusBadRequest)
		return
	}
	if !validateRelativePath(path) {
		http.Error(w, "invalid path: path traversal not allowed", http.StatusBadRequest)
		return
	}
	folder := api.NormalizePath(path)
	if d.refreshedRecently(folder) {
		writeJSON(w, api.OperationResponse{Status: api.StatusOK})
		return
	}

	queued, _ := d.enqueue(&job{key: "folder:" + folder, paths: []string{folder}, run: func(ctx context.Context) error {
		// Nobody waits for the answer.
		if err := d.refreshFolder(ctx, folder); err != nil {
			slog.Warn("folder refresh failed", "path", folder, "error", err)
		}
		return nil
	}})
	if refused(queued) {
		http.Error(w, errShuttingDown.Error(), http.StatusServiceUnavailable)
		return
	}
	writeJSONStatus(w, http.StatusAccepted, api.OperationResponse{Status: api.StatusQueued})
}

// refreshFolder makes the index agree with the disk for the entries of one
// folder.
func (d *daemon) refreshFolder(ctx context.Context, folder string) error {
	indexID, err := d.store.LatestIndexID(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		// No index yet: the first scan finds the folder as it is.
		return nil
	}
	if err != nil {
		return fmt.Errorf("read index: %w", err)
	}

	cfg := d.settings.config()
	index := indexing.Initialize(
		"/",
		indexing.WithNetworkMounts(cfg.IncludeNetworkMounts),
		indexing.WithExcludePaths(configfile.EffectiveExcludePaths(cfg)),
	)
	// ReadFolder decides that the index leaves the folder out before it
	// touches the disk: such a folder may be on a mount that does not answer.
	contents, err := index.ReadFolder(ctx, folder, refreshEntryLimit)
	switch {
	case errors.Is(err, indexing.ErrNotIndexed), errors.Is(err, indexing.ErrFolderTooLarge):
		slog.Debug("folder not refreshed", "path", folder, "error", err)
		d.markRefreshed(folder)
		return nil
	case indexing.NotThere(err):
		// Gone, a file now, or named through a symlink: the index follows
		// the disk for the path itself.
		_, err = d.reconcileEntry(ctx, folder)
		return err
	case err != nil:
		return fmt.Errorf("read folder: %w", err)
	}

	result, err := d.store.RefreshFolder(ctx, indexID, folder, contents)
	if errors.Is(err, storage.ErrFolderNotIndexed) {
		// Marked too: a folder that gets no row, one a scan cannot read,
		// would otherwise be walked again on every visit.
		d.enqueueReindex(d.workContext(), folder)
		d.markRefreshed(folder)
		return nil
	}
	if err != nil {
		return err
	}
	for _, found := range result.NewFolders {
		d.enqueueReindex(d.workContext(), found)
	}
	d.markRefreshed(folder)
	slog.Debug("folder refreshed", "path", folder, "changed", result.Changed, "new_folders", len(result.NewFolders))
	return nil
}
