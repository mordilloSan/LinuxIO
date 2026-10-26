package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/configfile"
	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/indexing"
	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/storage"
)

// The full scan, run by the private index-mode worker process. The daemon
// supervises it (supervisor.go); the worker reports back over wire.go.

// IndexRunStats summarizes a completed index run and its API progress events.
type IndexRunStats struct {
	Files          int64
	Dirs           int64
	TotalSize      int64
	DeletedEntries int64
	SkippedDirs    uint64
	Duration       time.Duration
}

// IndexProgress receives user-facing updates during RunIndexMode; nil
// disables reporting (the daemon-spawned subprocess only logs). Step marks
// the start of a phase. ScanProgress fires from a sampling goroutine while
// traversal runs, never concurrently with Step.
type IndexProgress interface {
	Step(message string)
	ScanProgress(dirs, files, size uint64)
}

// startScanProgress samples the traversal counters twice a second and
// forwards them to progress. The returned stop function halts sampling,
// waits for the sampler to exit, and emits one final exact snapshot.
func startScanProgress(progress IndexProgress, index *indexing.Index) (stop func()) {
	if progress == nil {
		return func() {}
	}
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		ticker := time.NewTicker(scanProgressInterval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				progress.ScanProgress(index.Counts())
			}
		}
	}()
	return func() {
		close(done)
		<-finished
		progress.ScanProgress(index.Counts())
	}
}

// RunIndexMode performs one full atomic-generation scan.
func RunIndexMode(ctx context.Context, cfg configfile.Config, dbPath string, progress IndexProgress) (IndexRunStats, error) {
	start := time.Now()
	cfg, err := configfile.Normalize(cfg)
	if err != nil {
		return IndexRunStats{}, err
	}
	slog.Debug("running in index mode",
		"include_network_mounts", cfg.IncludeNetworkMounts,
		"exclude_paths", cfg.ExcludePaths,
	)

	if progress != nil {
		progress.Step("Opening database")
	}
	store, err := storage.OpenStore(ctx, dbPath, indexing.SameFile)
	if err != nil {
		return IndexRunStats{}, fmt.Errorf("open database: %w", err)
	}
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			slog.Warn("failed to close database", "error", closeErr)
		}
	}()

	runStats, err := runIndex(ctx, store, cfg, progress)
	if err != nil {
		return IndexRunStats{}, fmt.Errorf("run index: %w", err)
	}

	if progress != nil {
		progress.Step("Pruning old index generation")
	}
	stats, err := store.PruneOldIndexes(ctx, retainedGenerations)
	if err != nil {
		return IndexRunStats{}, fmt.Errorf("prune old indexes: %w", err)
	}
	runStats.DeletedEntries = stats.DeletedEntries

	if progress != nil {
		progress.Step("Checkpointing database")
	}
	store.CheckpointAndRelease(ctx, "index")

	slog.Debug("index complete, subprocess exiting")
	runStats.Duration = time.Since(start)
	return runStats, nil
}

// runIndex scans the filesystem into a new generation and publishes it.
func runIndex(ctx context.Context, store *storage.Store, cfg configfile.Config, progress IndexProgress) (IndexRunStats, error) {
	index := indexing.Initialize("/", indexing.WithNetworkMounts(cfg.IncludeNetworkMounts), indexing.WithExcludePaths(configfile.EffectiveExcludePaths(cfg)))

	start := time.Now()

	generationID, err := store.BeginGeneration(ctx)
	if err != nil {
		return IndexRunStats{}, fmt.Errorf("prepare index record: %w", err)
	}

	// The channel buffer is bounded; storage batches the SQL writes separately.
	writer := store.NewStreamingWriter(ctx, generationID, writerBufferEntries, nil)
	index.EnableStreaming(writer)

	// Start filesystem traversal
	if progress != nil {
		progress.Step("Scanning filesystem")
	}
	slog.Debug("starting filesystem traversal")
	stopScanProgress := startScanProgress(progress, index)
	indexErr := index.StartIndexing(ctx)
	stopScanProgress()
	if indexErr != nil {
		if closeErr := writer.Close(); closeErr != nil {
			slog.Warn("failed to close streaming writer after index error", "error", closeErr)
		}
		return IndexRunStats{}, fmt.Errorf("indexing failed: %w", indexErr)
	}

	// Flush remaining batches.
	if closeErr := writer.Close(); closeErr != nil {
		return IndexRunStats{}, fmt.Errorf("streaming writer close: %w", closeErr)
	}

	if err := store.PublishGeneration(ctx, generationID, int64(index.NumDirs), int64(index.NumFiles), int64(index.GetTotalSize())); err != nil {
		return IndexRunStats{}, fmt.Errorf("update index metadata: %w", err)
	}

	slog.Info("index complete",
		"duration", time.Since(start).Truncate(time.Millisecond),
		"dirs", index.NumDirs,
		"files", index.NumFiles,
		"size", index.GetTotalSize(),
		"skipped_dirs", index.SkippedDirCount(),
	)

	return IndexRunStats{
		Files:       int64(index.NumFiles),
		Dirs:        int64(index.NumDirs),
		TotalSize:   int64(index.GetTotalSize()),
		SkippedDirs: index.SkippedDirCount(),
		Duration:    time.Since(start),
	}, nil
}
