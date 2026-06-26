package filebrowser

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/services"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/config"
	bridgejobs "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
	ipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
	"github.com/mordilloSan/LinuxIO/backend/common/utils"
)

// batchItemFailure records one item a batch job could not process, so the job
// continues past it and reports partial success to the caller.
type batchItemFailure struct {
	Path  string `json:"path"`
	Error string `json:"error"`
}

func batchResult(total, succeeded int, failures []batchItemFailure) map[string]any {
	if failures == nil {
		failures = []batchItemFailure{}
	}
	return map[string]any{
		"total":     total,
		"succeeded": succeeded,
		"failed":    failures,
	}
}

// resolveBatchDestinationDir validates that the batch destination is an existing
// directory and returns its cleaned absolute path.
func resolveBatchDestinationDir(root *fsroot.FSRoot, destination string) (string, error) {
	dir := utils.CleanAbsPath(destination)
	info, err := root.Root.Stat(fsroot.ToRel(dir))
	if err != nil {
		return "", bridgejobs.NewError("destination directory not found", 404)
	}
	if !info.IsDir() {
		return "", bridgejobs.NewError("destination is not a directory", 400)
	}
	return dir, nil
}

type batchTransferItem struct {
	source   string
	dest     string
	size     computedTransferSize
	replaced bool
}

// planBatchTransfer validates each source, computes its destination directory
// landing path and size, and sums a grand total for aggregate progress. Invalid
// items are returned as failures so the job can still process the rest.
func planBatchTransfer(ctx context.Context, root *fsroot.FSRoot, destDir string, sources []string, overwrite bool) ([]batchTransferItem, int64, []batchItemFailure) {
	items := make([]batchTransferItem, 0, len(sources))
	failures := make([]batchItemFailure, 0)
	var grandTotal int64

	for _, raw := range sources {
		src := utils.CleanAbsPath(raw)
		if src == "/" {
			failures = append(failures, batchItemFailure{Path: raw, Error: "cannot transfer root"})
			continue
		}
		info, err := root.Root.Stat(fsroot.ToRel(src))
		if err != nil {
			failures = append(failures, batchItemFailure{Path: raw, Error: "source not found"})
			continue
		}

		dest := filepath.Join(destDir, filepath.Base(src))
		replaced := false
		if destInfo, derr := root.Root.Stat(fsroot.ToRel(dest)); derr == nil {
			if !overwrite {
				failures = append(failures, batchItemFailure{Path: raw, Error: "destination already exists"})
				continue
			}
			if destInfo.IsDir() != info.IsDir() {
				failures = append(failures, batchItemFailure{Path: raw, Error: "destination type mismatch"})
				continue
			}
			replaced = true
		}

		size := computeTransferSize(ctx, src, info)
		grandTotal += size.total
		items = append(items, batchTransferItem{source: src, dest: dest, size: size, replaced: replaced})
	}
	return items, grandTotal, failures
}

// runCopyBatchJob copies many sources into one destination directory as a single
// job, sharing one progress callback so the UI shows one aggregate bar.
func runCopyBatchJob(ctx context.Context, job *bridgejobs.Job, store *config.UserStore, req apischema.BatchTransferRequest) (any, error) {
	if len(req.Sources) == 0 {
		return nil, bridgejobs.NewError("no sources provided", 400)
	}
	overwrite := req.Overwrite != nil && *req.Overwrite

	root, err := fsroot.Open()
	if err != nil {
		return nil, bridgejobs.NewError("failed to access filesystem", 500)
	}
	defer root.Close()

	destDir, err := resolveBatchDestinationDir(root, req.Destination)
	if err != nil {
		return nil, err
	}

	items, grandTotal, failures := planBatchTransfer(ctx, root, destDir, req.Sources, overwrite)
	writeJobPhaseProgress(job, grandTotal, "preparing")

	// One shared callback/limiter across all items so byte progress accumulates
	// into a single aggregate bar instead of resetting per file.
	opts := newJobPhaseCallbacks(ctx, job, store, grandTotal, "copying")

	succeeded := 0
	for _, item := range items {
		if ctx.Err() != nil {
			return nil, abortErr(ctx)
		}
		err := services.CopyFileWithCallbacks(item.source, item.dest, overwrite, opts)
		if err == ipc.ErrAborted {
			return nil, abortErr(ctx)
		}
		if err != nil {
			slog.Debug("batch copy item failed", "source", item.source, "destination", item.dest, "error", err)
			failures = append(failures, batchItemFailure{Path: item.source, Error: err.Error()})
			continue
		}
		succeeded++

		if info, statErr := root.Root.Stat(fsroot.ToRel(item.dest)); statErr == nil {
			dest, size, replaced := item.dest, item.size, item.replaced
			runDetachedIndexerUpdate("copy_batch", func(ctx context.Context) error {
				return addCopiedPathToIndexer(ctx, dest, info, size, replaced)
			})
		}
	}

	slog.Info("batch copy complete", "total", len(req.Sources), "succeeded", succeeded, "failed", len(failures))
	return batchResult(len(req.Sources), succeeded, failures), nil
}

// runMoveBatchJob moves many sources into one destination directory as a single
// job, sharing one progress callback for an aggregate bar.
func runMoveBatchJob(ctx context.Context, job *bridgejobs.Job, store *config.UserStore, req apischema.BatchTransferRequest) (any, error) {
	if len(req.Sources) == 0 {
		return nil, bridgejobs.NewError("no sources provided", 400)
	}
	overwrite := req.Overwrite != nil && *req.Overwrite

	root, err := fsroot.Open()
	if err != nil {
		return nil, bridgejobs.NewError("failed to access filesystem", 500)
	}
	defer root.Close()

	destDir, err := resolveBatchDestinationDir(root, req.Destination)
	if err != nil {
		return nil, err
	}

	items, grandTotal, failures := planBatchTransfer(ctx, root, destDir, req.Sources, overwrite)
	writeJobPhaseProgress(job, grandTotal, "preparing")

	opts := newJobPhaseCallbacks(ctx, job, store, grandTotal, "moving")

	succeeded := 0
	for _, item := range items {
		if ctx.Err() != nil {
			return nil, abortErr(ctx)
		}
		err := services.MoveFileWithCallbacks(item.source, item.dest, overwrite, opts, moveFileOptions(item.size))
		if err == ipc.ErrAborted {
			return nil, abortErr(ctx)
		}
		if err != nil {
			slog.Debug("batch move item failed", "source", item.source, "destination", item.dest, "error", err)
			failures = append(failures, batchItemFailure{Path: item.source, Error: err.Error()})
			continue
		}
		succeeded++

		source, dest, size, replaced := item.source, item.dest, item.size, item.replaced
		runDetachedIndexerUpdate("move_batch", func(ctx context.Context) error {
			return movePathInIndexer(ctx, source, dest, size, replaced, func() (os.FileInfo, error) {
				return root.Root.Stat(fsroot.ToRel(dest))
			})
		})
	}

	slog.Info("batch move complete", "total", len(req.Sources), "succeeded", succeeded, "failed", len(failures))
	return batchResult(len(req.Sources), succeeded, failures), nil
}

// runDeleteBatchJob deletes many paths as a single job, reporting a running
// processed-item count across all paths.
func runDeleteBatchJob(ctx context.Context, job *bridgejobs.Job, req apischema.BatchPathRequest) (any, error) {
	if len(req.Paths) == 0 {
		return nil, bridgejobs.NewError("no paths provided", 400)
	}

	var processed int64
	succeeded := 0
	failures := make([]batchItemFailure, 0)

	for _, raw := range req.Paths {
		if ctx.Err() != nil {
			return nil, context.Canceled
		}
		path := utils.CleanAbsPath(raw)
		if path == "/" {
			failures = append(failures, batchItemFailure{Path: raw, Error: "cannot delete root"})
			continue
		}

		isDir, err := deleteTargetIsDir(path)
		if err != nil {
			failures = append(failures, batchItemFailure{Path: raw, Error: "not found"})
			continue
		}

		opts := deleteOptionsForPath(ctx, path, isDir)
		base := processed
		opts.Progress = func(p, _ int64, _ bool) {
			job.ReportProgress(DeleteProgress{
				Processed:     base + p,
				Phase:         "deleting",
				Indeterminate: true,
			})
		}

		count, err := services.DeleteFilesWithProgress(ctx, path, opts)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return nil, err
			}
			slog.Debug("batch delete item failed", "path", path, "error", err)
			failures = append(failures, batchItemFailure{Path: raw, Error: err.Error()})
			continue
		}
		processed += count
		succeeded++

		p := path
		runDetachedIndexerUpdate("delete_batch", func(ctx context.Context) error {
			return deleteFromIndexer(ctx, p)
		})
	}

	slog.Info("batch delete complete", "total", len(req.Paths), "succeeded", succeeded, "failed", len(failures), "processed", processed)
	return batchResult(len(req.Paths), succeeded, failures), nil
}
