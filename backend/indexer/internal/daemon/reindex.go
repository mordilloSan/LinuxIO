package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/indexer/api"
	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/configfile"
	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/indexing"
)

// reindexPathWithProgress reindexes a path and emits progress events.
func (d *daemon) reindexPathWithProgress(ctx context.Context, operationID, relativePath string, sender sseEventSender) {
	start := time.Now()
	slog.Info("starting reindex", "path", relativePath)

	indexID, err := d.store.LatestIndexID(ctx)
	if err != nil {
		sendSSEError(sender, fmt.Sprintf("no index present: %v", err))
		return
	}

	cfg := d.settings.config()
	fi, statErr := os.Lstat(relativePath)
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		sendSSEError(sender, fmt.Sprintf("cannot access %s: %v", relativePath, statErr))
		return
	}
	if statErr == nil && !fi.IsDir() {
		sendSSEError(sender, fmt.Sprintf("%s is not a directory; reindex its parent directory instead", relativePath))
		return
	}

	if statErr != nil {
		if deleteErr := d.store.DeletePath(ctx, indexID, relativePath); deleteErr != nil {
			sendSSEError(sender, fmt.Sprintf("failed to delete stale entries: %v", deleteErr))
			return
		}
		sendSSEEvent(sender, api.EventComplete, api.WorkCompleteEvent{Status: "complete", Operation: "reindex", OperationID: operationID, Path: relativePath, DurationMs: time.Since(start).Milliseconds()})
		return
	}
	d.reindexExistingPath(ctx, operationID, relativePath, sender, start, cfg, indexID)
}

func (d *daemon) reindexExistingPath(ctx context.Context, operationID, relativePath string, sender sseEventSender, start time.Time, cfg configfile.Config, indexID int64) {
	reindex, err := d.store.BeginReindex(ctx, indexID, relativePath)
	if err != nil {
		sendSSEError(sender, err.Error())
		return
	}
	defer reindex.Rollback()

	index := indexing.Initialize(
		"/",
		indexing.WithNetworkMounts(cfg.IncludeNetworkMounts),
		indexing.WithExcludePaths(configfile.EffectiveExcludePaths(cfg)),
	)
	writer := reindex.NewWriter(ctx, writerBufferEntries, reindexProgressCallback(operationID, relativePath, sender))
	index.EnableStreaming(writer)

	if indexErr := index.StartIndexingFromPath(ctx, relativePath); indexErr != nil {
		if closeErr := writer.Close(); closeErr != nil {
			slog.Warn("failed to close streaming writer after reindex stream error", "err", closeErr)
		}
		sendSSEError(sender, fmt.Sprintf("indexing failed: %v", indexErr))
		return
	}

	if closeErr := writer.Close(); closeErr != nil {
		sendSSEError(sender, fmt.Sprintf("streaming writer close: %v", closeErr))
		return
	}

	newSize, commitErr := reindex.Commit(ctx, writer.ScanTime())
	if commitErr != nil {
		sendSSEError(sender, commitErr.Error())
		return
	}
	d.store.CheckpointAndRelease(ctx, "reindex")

	duration := time.Since(start)
	slog.Info("reindex complete", "path", relativePath, "duration", duration)

	sendSSEEvent(sender, api.EventComplete, api.WorkCompleteEvent{
		Status:       "complete",
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
