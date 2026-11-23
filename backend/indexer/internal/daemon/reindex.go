package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/indexer/api"
	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/configfile"
	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/indexing"
)

// reindexPath reindexes a path and emits progress events.
func (d *daemon) reindexPath(ctx context.Context, operationID, relativePath string, sender sseEventSender) {
	start := time.Now()
	slog.Debug("starting reindex", "path", relativePath)

	indexID, err := d.store.LatestIndexID(ctx)
	if err != nil {
		failReindex(sender, relativePath, fmt.Sprintf("no index present: %v", err))
		return
	}

	cfg := d.settings.config()
	// Never through a symlink: see indexing.Lstat. A path that leads through
	// one names nothing the index holds, and loses its rows like one that is
	// gone.
	fi, statErr := indexing.Lstat(relativePath)
	if statErr != nil && !indexing.NotThere(statErr) {
		failReindex(sender, relativePath, fmt.Sprintf("cannot access %s: %v", relativePath, statErr))
		return
	}
	if statErr == nil && !fi.IsDir() {
		failReindex(sender, relativePath, fmt.Sprintf("%s is not a directory; reindex its parent directory instead", relativePath))
		return
	}

	if statErr != nil {
		if deleteErr := d.store.DeletePath(ctx, indexID, relativePath); deleteErr != nil {
			failReindex(sender, relativePath, fmt.Sprintf("failed to delete stale entries: %v", deleteErr))
			return
		}
		sendSSEEvent(sender, api.EventComplete, api.WorkCompleteEvent{Status: api.StatusComplete, Operation: "reindex", OperationID: operationID, Path: relativePath, DurationMs: time.Since(start).Milliseconds()})
		return
	}
	d.reindexExistingPath(ctx, operationID, relativePath, sender, start, cfg, indexID)
}

func (d *daemon) reindexExistingPath(ctx context.Context, operationID, relativePath string, sender sseEventSender, start time.Time, cfg configfile.Config, indexID int64) {
	reindex, err := d.store.BeginReindex(ctx, indexID, relativePath)
	if err != nil {
		failReindex(sender, relativePath, err.Error())
		return
	}
	defer reindex.Rollback()

	index := indexing.Initialize(
		"/",
		indexing.WithNetworkMounts(cfg.IncludeNetworkMounts),
		indexing.WithExcludePaths(configfile.EffectiveExcludePaths(cfg)),
		indexing.WithHardlinkContributors(reindex.HardlinkContributors()),
	)
	writer := reindex.NewReconciler(ctx, reindexProgressCallback(operationID, relativePath, sender))
	index.EnableStreaming(writer)

	if indexErr := index.StartIndexingFromPath(ctx, relativePath); indexErr != nil {
		// Not closed: closing writes what the reconciler still holds, and the
		// deferred rollback is what a reindex that failed wants.
		failReindex(sender, relativePath, fmt.Sprintf("indexing failed: %v", indexErr))
		return
	}

	if closeErr := writer.Close(); closeErr != nil {
		failReindex(sender, relativePath, fmt.Sprintf("streaming writer close: %v", closeErr))
		return
	}

	newSize, commitErr := reindex.Commit(ctx, writer.ScanTime())
	if commitErr != nil {
		failReindex(sender, relativePath, commitErr.Error())
		return
	}
	d.store.CheckpointAndRelease(ctx, "reindex")

	duration := time.Since(start)
	slog.Info("reindex complete", "path", relativePath, "duration", duration)

	sendSSEEvent(sender, api.EventComplete, api.WorkCompleteEvent{
		Status:       api.StatusComplete,
		Operation:    "reindex",
		OperationID:  operationID,
		Path:         relativePath,
		FilesIndexed: int64(index.NumFiles),
		DirsIndexed:  int64(index.NumDirs),
		TotalSize:    newSize,
		DurationMs:   duration.Milliseconds(),
	})
}

func reindexProgressCallback(operationID, operationPath string, sender sseEventSender) func(int64, int64, string) {
	return func(filesWritten, dirsWritten int64, lastPath string) {
		if (filesWritten+dirsWritten)%reindexProgressStride == 0 {
			sendSSEEvent(sender, api.EventProgress, api.WorkProgressEvent{
				Operation:    "reindex",
				OperationID:  operationID,
				Path:         operationPath,
				FilesIndexed: filesWritten,
				DirsIndexed:  dirsWritten,
				CurrentPath:  lastPath,
			})
		}
	}
}

// failReindex reports a reindex that could not be done to whoever follows its
// stream, and to the journal: a reindex the bridge asked for after a file
// operation has nobody following it.
func failReindex(sender sseEventSender, path, msg string) {
	slog.Warn("reindex failed", "path", path, "error", errors.New(msg))
	sendSSEError(sender, msg)
}
