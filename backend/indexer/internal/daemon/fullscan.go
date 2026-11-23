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

// The full scan, which the scanner runs once the daemon has given it its
// turn (scanlink.go; the daemon's side is scan.go).

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
// disables reporting. Step marks the start of a phase. ScanProgress fires from a sampling goroutine while
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

// RunIndexMode performs one full scan: it brings the index up to date with the
// whole filesystem, writing only what changed since the last scan.
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
		progress.Step("Checkpointing database")
	}
	store.CheckpointAndRelease(ctx, "index")

	slog.Debug("index complete, subprocess exiting")
	runStats.Duration = time.Since(start)
	return runStats, nil
}

// runIndex scans the filesystem and reconciles the index with it. The first
// scan fills a generation readers do not see until it commits; later scans
// update that generation in place.
func runIndex(ctx context.Context, store *storage.Store, cfg configfile.Config, progress IndexProgress) (IndexRunStats, error) {
	start := time.Now()

	generationID, err := store.EnsureGeneration(ctx)
	if err != nil {
		return IndexRunStats{}, fmt.Errorf("prepare index record: %w", err)
	}
	reindex, err := store.BeginReindex(ctx, generationID, "/")
	if err != nil {
		return IndexRunStats{}, fmt.Errorf("begin index: %w", err)
	}
	defer reindex.Rollback()

	index := indexing.Initialize("/",
		indexing.WithNetworkMounts(cfg.IncludeNetworkMounts),
		indexing.WithExcludePaths(configfile.EffectiveExcludePaths(cfg)),
		indexing.WithHardlinkContributors(reindex.HardlinkContributors()),
	)
	writer := reindex.NewReconciler(ctx, nil)
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
		return IndexRunStats{}, fmt.Errorf("indexing failed: %w", indexErr)
	}
	if closeErr := writer.Close(); closeErr != nil {
		return IndexRunStats{}, fmt.Errorf("write index: %w", closeErr)
	}
	if progress != nil {
		progress.Step("Updating folder sizes")
	}
	totalSize, err := reindex.Commit(ctx, writer.ScanTime())
	if err != nil {
		return IndexRunStats{}, fmt.Errorf("update index metadata: %w", err)
	}

	slog.Info("index complete",
		"duration", time.Since(start).Truncate(time.Millisecond),
		"dirs", index.NumDirs,
		"files", index.NumFiles,
		"size", totalSize,
		"rows_changed", writer.Changed(),
		"rows_deleted", writer.Deleted(),
		"skipped_dirs", index.SkippedDirCount(),
	)

	return IndexRunStats{
		Files:          int64(index.NumFiles),
		Dirs:           int64(index.NumDirs),
		TotalSize:      totalSize,
		DeletedEntries: writer.Deleted(),
		SkippedDirs:    index.SkippedDirCount(),
		Duration:       time.Since(start),
	}, nil
}
