package filebrowser

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/services"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/config"
	bridgetasks "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
	ipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
	"github.com/mordilloSan/LinuxIO/backend/common/utils"
)

// FileBatchItemFailure records one item a batch task could not process, so the task
// continues past it and reports partial success to the caller.
type FileBatchItemFailure struct {
	Path  string `json:"path"`
	Error string `json:"error"`
}

func batchResult(total, succeeded int, failures []FileBatchItemFailure) FileBatchResult {
	if failures == nil {
		failures = []FileBatchItemFailure{}
	}
	return FileBatchResult{Total: total, Succeeded: succeeded, Failed: failures}
}

// resolveBatchDestinationDir validates that the batch destination is an existing
// directory and returns its cleaned absolute path.
func resolveBatchDestinationDir(root *fsroot.FSRoot, destination string) (string, error) {
	dir := utils.CleanAbsPath(destination)
	info, err := root.Root.Stat(fsroot.ToRel(dir))
	if err != nil {
		return "", bridgetasks.NewError("destination directory not found", 404)
	}
	if !info.IsDir() {
		return "", bridgetasks.NewError("destination is not a directory", 400)
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
// items are returned as failures so the task can still process the rest.
func planBatchTransfer(ctx context.Context, root *fsroot.FSRoot, destDir string, sources []string, overwrite bool) ([]batchTransferItem, int64, []FileBatchItemFailure) {
	items := make([]batchTransferItem, 0, len(sources))
	failures := make([]FileBatchItemFailure, 0)
	var grandTotal int64

	for _, raw := range sources {
		src := utils.CleanAbsPath(raw)
		if src == "/" {
			failures = append(failures, FileBatchItemFailure{Path: raw, Error: "cannot transfer root"})
			continue
		}
		info, err := root.Root.Lstat(fsroot.ToRel(src))
		if err != nil {
			failures = append(failures, FileBatchItemFailure{Path: raw, Error: "source not found"})
			continue
		}

		dest := filepath.Join(destDir, filepath.Base(src))
		replaced := false
		if destInfo, derr := root.Root.Lstat(fsroot.ToRel(dest)); derr == nil {
			if !overwrite {
				failures = append(failures, FileBatchItemFailure{Path: raw, Error: "destination already exists"})
				continue
			}
			if destInfo.IsDir() != info.IsDir() {
				failures = append(failures, FileBatchItemFailure{Path: raw, Error: "destination type mismatch"})
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

// runCopyBatchTask copies many sources into one destination directory as a single
// task, sharing one progress callback so the UI shows one aggregate bar.
func runCopyBatchTask(ctx context.Context, task *bridgetasks.Task, store *config.UserStore, req apischema.BatchTransferRequest) (FileBatchResult, error) {
	if len(req.Sources) == 0 {
		return FileBatchResult{}, bridgetasks.NewError("no sources provided", 400)
	}
	overwrite := req.Overwrite != nil && *req.Overwrite

	root, err := fsroot.Open()
	if err != nil {
		return FileBatchResult{}, bridgetasks.NewError("failed to access filesystem", 500)
	}
	defer root.Close()

	destDir, err := resolveBatchDestinationDir(root, req.Destination)
	if err != nil {
		return FileBatchResult{}, err
	}

	items, grandTotal, failures := planBatchTransfer(ctx, root, destDir, req.Sources, overwrite)
	writeTaskPhaseProgress(task, grandTotal, "preparing")

	// One shared callback/limiter across all items so byte progress accumulates
	// into a single aggregate bar instead of resetting per file.
	opts := newTaskPhaseCallbacks(ctx, task, store, knownSize(grandTotal), "copying")

	succeeded := 0
	for _, item := range items {
		if ctx.Err() != nil {
			return FileBatchResult{}, abortErr(ctx)
		}
		err := services.CopyFileWithCallbacks(item.source, item.dest, overwrite, opts)
		if err == ipc.ErrAborted {
			return FileBatchResult{}, abortErr(ctx)
		}
		if err != nil {
			slog.Debug("batch copy item failed", "source", item.source, "destination", item.dest, "error", err)
			failures = append(failures, FileBatchItemFailure{Path: item.source, Error: err.Error()})
			continue
		}
		succeeded++

		if info, statErr := root.Root.Lstat(fsroot.ToRel(item.dest)); statErr == nil {
			dest, size, replaced := item.dest, item.size, item.replaced
			runDetachedIndexerUpdate("copy_batch", func(ctx context.Context) error {
				return addCopiedPathToIndexer(ctx, dest, info, size, replaced)
			})
		}
	}

	slog.Info("batch copy complete", "total", len(req.Sources), "succeeded", succeeded, "failed", len(failures))
	return batchResult(len(req.Sources), succeeded, failures), nil
}

// runMoveBatchTask moves many sources into one destination directory as a single
// task, sharing one progress callback for an aggregate bar.
func runMoveBatchTask(ctx context.Context, task *bridgetasks.Task, store *config.UserStore, req apischema.BatchTransferRequest) (FileBatchResult, error) {
	if len(req.Sources) == 0 {
		return FileBatchResult{}, bridgetasks.NewError("no sources provided", 400)
	}
	overwrite := req.Overwrite != nil && *req.Overwrite

	root, err := fsroot.Open()
	if err != nil {
		return FileBatchResult{}, bridgetasks.NewError("failed to access filesystem", 500)
	}
	defer root.Close()

	destDir, err := resolveBatchDestinationDir(root, req.Destination)
	if err != nil {
		return FileBatchResult{}, err
	}

	items, grandTotal, failures := planBatchTransfer(ctx, root, destDir, req.Sources, overwrite)
	writeTaskPhaseProgress(task, grandTotal, "preparing")

	opts := newTaskPhaseCallbacks(ctx, task, store, knownSize(grandTotal), "moving")

	succeeded := 0
	for _, item := range items {
		if ctx.Err() != nil {
			return FileBatchResult{}, abortErr(ctx)
		}
		err := services.MoveFileWithCallbacks(item.source, item.dest, overwrite, opts, moveFileOptions(item.size))
		if err == ipc.ErrAborted {
			return FileBatchResult{}, abortErr(ctx)
		}
		if err != nil {
			slog.Debug("batch move item failed", "source", item.source, "destination", item.dest, "error", err)
			failures = append(failures, FileBatchItemFailure{Path: item.source, Error: err.Error()})
			continue
		}
		succeeded++

		source, dest, size, replaced := item.source, item.dest, item.size, item.replaced
		runDetachedIndexerUpdate("move_batch", func(ctx context.Context) error {
			return movePathInIndexer(ctx, source, dest, size, replaced, func() (os.FileInfo, error) {
				return root.Root.Lstat(fsroot.ToRel(dest))
			})
		})
	}

	slog.Info("batch move complete", "total", len(req.Sources), "succeeded", succeeded, "failed", len(failures))
	return batchResult(len(req.Sources), succeeded, failures), nil
}

// deletePlanItem is one validated delete target with its known entry total
// (0 when unknown).
type deletePlanItem struct {
	raw   string
	path  string
	total int64
}

// planDeleteBatch validates each requested path and resolves how many entries
// deleting it will remove, summing a grand total for aggregate progress. The
// grand total is 0 (indeterminate) unless every item's total is known.
func planDeleteBatch(ctx context.Context, paths []string) (items []deletePlanItem, grandTotal int64, failures []FileBatchItemFailure) {
	failures = make([]FileBatchItemFailure, 0)
	allKnown := true

	for _, raw := range paths {
		if ctx.Err() != nil {
			return nil, 0, failures
		}
		path := utils.CleanAbsPath(raw)
		if path == "/" {
			failures = append(failures, FileBatchItemFailure{Path: raw, Error: "cannot delete root"})
			continue
		}
		isDir, err := deleteTargetIsDir(path)
		if err != nil {
			failures = append(failures, FileBatchItemFailure{Path: raw, Error: "not found"})
			continue
		}
		total := deleteEntryTotalForPath(ctx, path, isDir)
		if total <= 0 {
			allKnown = false
		}
		grandTotal += total
		items = append(items, deletePlanItem{raw: raw, path: path, total: total})
	}

	if !allKnown {
		grandTotal = 0
	}
	return items, grandTotal, failures
}

// runDeleteBatchTask deletes many paths as a single task. It resolves entry
// totals up front (indexer, else a bounded prescan) so progress reports a real
// aggregate percentage; when a total stays unknown the task reports an
// indeterminate running count instead.
func runDeleteBatchTask(ctx context.Context, task *bridgetasks.Task, store *config.UserStore, req apischema.BatchPathRequest) (FileBatchResult, error) {
	if len(req.Paths) == 0 {
		return FileBatchResult{}, bridgetasks.NewError("no paths provided", 400)
	}

	task.ReportProgress(DeleteProgress{Phase: "preparing", Indeterminate: true})
	items, grandTotal, failures := planDeleteBatch(ctx, req.Paths)
	if ctx.Err() != nil {
		return FileBatchResult{}, context.Canceled
	}

	indeterminate := grandTotal <= 0
	limiter := newCountProgressLimiter(taskSettingsForTask(ctx, task, store))
	var processed int64
	succeeded := 0

	for _, item := range items {
		if ctx.Err() != nil {
			return FileBatchResult{}, context.Canceled
		}

		base := processed
		count, err := services.DeleteFilesWithProgress(ctx, item.path, services.DeleteOptions{
			Progress: func(p int64) {
				cur, pct, ok := limiter.Set(base+p, grandTotal)
				if !ok {
					return
				}
				task.ReportProgress(DeleteProgress{
					Processed:     cur,
					Total:         grandTotal,
					Pct:           pct,
					Phase:         "deleting",
					Indeterminate: indeterminate,
				})
			},
		})
		// count reflects entries actually removed, even when the item failed
		// partway through, so aggregate progress stays monotonic.
		processed += count
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return FileBatchResult{}, err
			}
			slog.Debug("batch delete item failed", "path", item.path, "error", err)
			failures = append(failures, FileBatchItemFailure{Path: item.raw, Error: err.Error()})
			continue
		}
		succeeded++

		p := item.path
		runDetachedIndexerUpdate("delete_batch", func(ctx context.Context) error {
			return deleteFromIndexer(ctx, p)
		})
	}

	slog.Info("batch delete complete", "total", len(req.Paths), "succeeded", succeeded, "failed", len(failures), "processed", processed)
	return batchResult(len(req.Paths), succeeded, failures), nil
}

func parseChmodBatchRequest(req apischema.FileChmodBatchRequest) (paths []string, mode os.FileMode, owner, group string, recursive bool, err error) {
	if len(req.Paths) == 0 || req.Mode == "" {
		return nil, 0, "", "", false, fmt.Errorf("missing paths or mode")
	}
	parsed, err := strconv.ParseInt(req.Mode, 8, 32)
	if err != nil {
		return nil, 0, "", "", false, fmt.Errorf("invalid mode: %v", err)
	}
	return req.Paths, os.FileMode(parsed), req.Owner, req.Group, req.Recursive != nil && *req.Recursive, nil
}

// chmodBatchReporter accumulates a running processed-entry count across all
// items and phases of a chmod batch task, throttled by one shared limiter.
type chmodBatchReporter struct {
	task      *bridgetasks.Task
	limiter   *countProgressLimiter
	processed int64
}

func (r *chmodBatchReporter) phase(phase string) func(processed, total int64) {
	base := r.processed
	return func(processed, _ int64) {
		r.processed = base + processed
		count, _, ok := r.limiter.Set(r.processed, 0)
		if !ok {
			return
		}
		r.task.ReportProgress(ChmodProgress{
			Processed:     count,
			Phase:         phase,
			Indeterminate: true,
		})
	}
}

type batchOwnership struct {
	uid int
	gid int
}

// resolveChmodOwnership resolves owner/group once for the whole batch, so a
// bad owner or group fails the task before any item is touched. Returns nil
// when no ownership change was requested.
func resolveChmodOwnership(owner, group string) (*batchOwnership, error) {
	if strings.TrimSpace(owner) == "" && strings.TrimSpace(group) == "" {
		return nil, nil
	}
	uid, err := resolveUserID(owner)
	if err != nil {
		slog.Debug("error resolving owner", "owner", owner, "error", err)
		return nil, err
	}
	gid, err := resolveGroupID(group)
	if err != nil {
		slog.Debug("error resolving group", "group", group, "error", err)
		return nil, err
	}
	return &batchOwnership{uid: uid, gid: gid}, nil
}

// chmodBatchItem applies the mode, and the ownership when requested, to one
// path.
func chmodBatchItem(ctx context.Context, path string, mode os.FileMode, ownership *batchOwnership, recursive bool, reporter *chmodBatchReporter) error {
	if err := services.ChangePermissionsCtx(ctx, path, mode, recursive, reporter.phase("chmod")); err != nil {
		return err
	}
	if ownership == nil {
		return nil
	}
	return services.ChangeOwnershipCtx(ctx, path, ownership.uid, ownership.gid, recursive, reporter.phase("chown"))
}

// runChmodBatchTask changes permissions (and optionally ownership) of many
// paths as a single task, reporting a running processed-entry count.
func runChmodBatchTask(ctx context.Context, task *bridgetasks.Task, store *config.UserStore, req apischema.FileChmodBatchRequest) (FileBatchResult, error) {
	paths, mode, owner, group, recursive, err := parseChmodBatchRequest(req)
	if err != nil {
		return FileBatchResult{}, bridgetasks.NewError(err.Error(), 400)
	}
	ownership, err := resolveChmodOwnership(owner, group)
	if err != nil {
		return FileBatchResult{}, bridgetasks.NewError(err.Error(), 400)
	}

	task.ReportProgress(ChmodProgress{Phase: "preparing"})
	reporter := &chmodBatchReporter{
		task:    task,
		limiter: newCountProgressLimiter(taskSettingsForTask(ctx, task, store)),
	}

	succeeded := 0
	failures := make([]FileBatchItemFailure, 0)
	for _, raw := range paths {
		if ctx.Err() != nil {
			return FileBatchResult{}, context.Canceled
		}
		path := utils.CleanAbsPath(raw)
		if err := chmodBatchItem(ctx, path, mode, ownership, recursive, reporter); err != nil {
			if errors.Is(err, context.Canceled) {
				return FileBatchResult{}, context.Canceled
			}
			slog.Debug("batch chmod item failed", "path", path, "error", err)
			failures = append(failures, FileBatchItemFailure{Path: raw, Error: err.Error()})
			continue
		}
		succeeded++
	}

	slog.Info("batch chmod complete", "total", len(paths), "succeeded", succeeded, "failed", len(failures))
	return batchResult(len(paths), succeeded, failures), nil
}
