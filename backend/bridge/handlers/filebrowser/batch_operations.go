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
	"syscall"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/services"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/userconfig"
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
	dir := fsroot.CleanAbsPath(destination)
	info, err := root.Root.Stat(fsroot.ToRel(dir))
	if err != nil {
		return "", bridgeipc.NewError("destination directory not found", 404)
	}
	if !info.IsDir() {
		return "", bridgeipc.NewError("destination is not a directory", 400)
	}
	return dir, nil
}

type batchTransferItem struct {
	source   string
	dest     string
	size     computedTransferSize
	replaced bool
	// needsSpace is false only for a move within one filesystem, which renames.
	needsSpace bool
}

// deviceID is the filesystem path lives on; ok is false when it cannot be read.
func deviceID(root *fsroot.FSRoot, path string) (uint64, bool) {
	info, err := root.Root.Lstat(fsroot.ToRel(path))
	if err != nil {
		return 0, false
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return st.Dev, true
}

func spaceNeeded(items []batchTransferItem) int64 {
	var need int64
	for _, item := range items {
		if item.needsSpace {
			need += item.size.total
		}
	}
	return need
}

type batchIndexerUpdate struct {
	source   string
	dest     string
	info     os.FileInfo
	replaced bool
}

// planBatchTransfer validates each source, computes its destination directory
// landing path and size, and sums a grand total for aggregate progress. Invalid
// items are returned as failures so the task can still process the rest.
func planBatchTransfer(ctx context.Context, root *fsroot.FSRoot, destDir string, req apischema.BatchTransferRequest, action string) ([]batchTransferItem, int64, []FileBatchItemFailure) {
	plan := transferPlan{
		root:      root,
		destDir:   destDir,
		action:    action,
		overwrite: req.Overwrite != nil && *req.Overwrite,
		keepBoth:  make(map[string]bool, len(req.KeepBoth)),
	}
	for _, raw := range req.KeepBoth {
		plan.keepBoth[fsroot.CleanAbsPath(raw)] = true
	}
	plan.destDev, plan.destDevKnown = deviceID(root, destDir)

	items := make([]batchTransferItem, 0, len(req.Sources))
	failures := make([]FileBatchItemFailure, 0)
	var grandTotal int64
	for _, raw := range req.Sources {
		item, failure := plan.item(ctx, raw)
		if failure != "" {
			failures = append(failures, FileBatchItemFailure{Path: raw, Error: failure})
			continue
		}
		grandTotal += item.size.total
		items = append(items, item)
	}
	return items, grandTotal, failures
}

type transferPlan struct {
	root         *fsroot.FSRoot
	destDir      string
	action       string
	overwrite    bool
	keepBoth     map[string]bool
	destDev      uint64
	destDevKnown bool
}

// item plans one source; a non-empty failure means the source is skipped.
func (p *transferPlan) item(ctx context.Context, raw string) (batchTransferItem, string) {
	src := fsroot.CleanAbsPath(raw)
	if src == "/" {
		return batchTransferItem{}, "cannot transfer root"
	}
	if p.action == "move" {
		if err := services.CheckNotProtected(p.root, src); err != nil {
			return batchTransferItem{}, err.Error()
		}
	}
	info, err := p.root.Root.Lstat(fsroot.ToRel(src))
	if err != nil {
		return batchTransferItem{}, "source not found"
	}

	dest := filepath.Join(p.destDir, filepath.Base(src))
	if dest == src {
		if p.action != "copy" {
			return batchTransferItem{}, "already in the destination folder"
		}
		// A paste into the item's own folder makes a copy next to it, as
		// Explorer's "name - Copy" does.
		dest = generateUniquePath(dest, info.IsDir(), p.root)
	}
	dest, replaced, failure := p.resolveExisting(dest, src, info.IsDir())
	if failure != "" {
		return batchTransferItem{}, failure
	}

	needsSpace := p.action == "copy"
	if !needsSpace {
		srcDev, ok := deviceID(p.root, src)
		needsSpace = !ok || !p.destDevKnown || srcDev != p.destDev
	}
	// A same-filesystem move is a rename and needs neither a size nor a walk.
	var size computedTransferSize
	if needsSpace {
		size = computeTransferSize(ctx, src, info)
	}
	return batchTransferItem{source: src, dest: dest, size: size, replaced: replaced, needsSpace: needsSpace}, ""
}

// resolveExisting applies the user's conflict choice to a destination that is
// already taken: keep both under a unique name, replace it, or fail the item.
func (p *transferPlan) resolveExisting(dest, src string, isDir bool) (string, bool, string) {
	destInfo, err := p.root.Root.Lstat(fsroot.ToRel(dest))
	if err != nil {
		return dest, false, ""
	}
	switch {
	case p.keepBoth[src]:
		return generateUniquePath(dest, isDir, p.root), false, ""
	case !p.overwrite:
		return "", false, "destination already exists"
	case destInfo.IsDir() != isDir:
		return "", false, "destination type mismatch"
	}
	return dest, true, ""
}

type batchTransferFunc func(batchTransferItem, *services.OperationCallbacks, bool) error
type batchTransferReconciler func(context.Context, batchIndexerUpdate) error

func runBatchTransferTask(ctx context.Context, task *bridgeipc.Task, store *userconfig.UserStore, req apischema.BatchTransferRequest, action, phase string, transfer batchTransferFunc, reconcile batchTransferReconciler) (FileBatchResult, error) {
	if len(req.Sources) == 0 {
		return FileBatchResult{}, bridgeipc.NewError("no sources provided", 400)
	}
	overwrite := req.Overwrite != nil && *req.Overwrite

	root, err := fsroot.Open()
	if err != nil {
		return FileBatchResult{}, bridgeipc.NewError("failed to access filesystem", 500)
	}
	rootClosed := false
	defer func() {
		if !rootClosed {
			_ = root.Close()
		}
	}()

	destDir, err := resolveBatchDestinationDir(root, req.Destination)
	if err != nil {
		return FileBatchResult{}, err
	}

	items, grandTotal, failures := planBatchTransfer(ctx, root, destDir, req, action)
	if spaceErr := services.EnsureFreeSpace(destDir, spaceNeeded(items)); spaceErr != nil {
		return FileBatchResult{}, bridgeipc.NewError(spaceErr.Error(), 507)
	}
	writeTaskPhaseProgress(task, grandTotal, "preparing")

	// One shared callback/limiter across all items so byte progress accumulates
	// into a single aggregate bar instead of resetting per file.
	opts := newTaskPhaseCallbacks(ctx, task, store, knownSize(grandTotal), phase)

	succeeded, failures, updates, err := executeBatchTransfer(ctx, root, items, overwrite, opts, action, transfer, failures)
	if err != nil {
		return FileBatchResult{}, err
	}
	if err := root.Close(); err != nil {
		slog.Debug("failed to close filesystem root", "error", err)
	}
	rootClosed = true
	reconcileDetached(action+"_batch", updates, func(ctx context.Context, update batchIndexerUpdate) error {
		if err := reconcile(ctx, update); err != nil {
			return fmt.Errorf("%s -> %s: %w", update.source, update.dest, err)
		}
		return nil
	})

	slog.Info("batch operation complete", "action", action, "total", len(req.Sources), "succeeded", succeeded, "failed", len(failures))
	return batchResult(len(req.Sources), succeeded, failures), nil
}

func executeBatchTransfer(ctx context.Context, root *fsroot.FSRoot, items []batchTransferItem, overwrite bool, opts *services.OperationCallbacks, action string, transfer batchTransferFunc, failures []FileBatchItemFailure) (int, []FileBatchItemFailure, []batchIndexerUpdate, error) {
	succeeded := 0
	updates := make([]batchIndexerUpdate, 0, len(items))
	for _, item := range items {
		if ctx.Err() != nil {
			return 0, failures, nil, abortErr(ctx)
		}
		err := transfer(item, opts, overwrite)
		if errors.Is(err, services.ErrAborted) {
			return 0, failures, nil, abortErr(ctx)
		}
		if err != nil {
			slog.Debug("batch item failed", "action", action, "source", item.source, "destination", item.dest, "error", err)
			failures = append(failures, FileBatchItemFailure{Path: item.source, Error: err.Error()})
			continue
		}
		succeeded++
		if info, statErr := root.Root.Lstat(fsroot.ToRel(item.dest)); statErr == nil {
			updates = append(updates, batchIndexerUpdate{source: item.source, dest: item.dest, info: info, replaced: item.replaced})
		}
	}
	return succeeded, failures, updates, nil
}

// runCopyBatchTask copies many sources into one destination directory as a single
// task, sharing one progress callback so the UI shows one aggregate bar.
func runCopyBatchTask(ctx context.Context, task *bridgeipc.Task, store *userconfig.UserStore, req apischema.BatchTransferRequest) (FileBatchResult, error) {
	return runBatchTransferTask(ctx, task, store, req, "copy", "copying", func(item batchTransferItem, opts *services.OperationCallbacks, overwrite bool) error {
		return services.CopyFileWithCallbacks(item.source, item.dest, overwrite, opts)
	}, func(ctx context.Context, update batchIndexerUpdate) error {
		return addCopiedPathToIndexer(ctx, update.dest, update.info, update.replaced)
	})
}

// runMoveBatchTask moves many sources into one destination directory as a single
// task, sharing one progress callback for an aggregate bar.
func runMoveBatchTask(ctx context.Context, task *bridgeipc.Task, store *userconfig.UserStore, req apischema.BatchTransferRequest) (FileBatchResult, error) {
	return runBatchTransferTask(ctx, task, store, req, "move", "moving", func(item batchTransferItem, opts *services.OperationCallbacks, overwrite bool) error {
		return services.MoveFileWithCallbacks(item.source, item.dest, overwrite, opts, moveFileOptions(item.size))
	}, func(ctx context.Context, update batchIndexerUpdate) error {
		return movePathInIndexer(ctx, update.source, update.dest, update.replaced, func() (os.FileInfo, error) {
			return update.info, nil
		})
	})
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
	root, err := fsroot.Open()
	if err != nil {
		for _, raw := range paths {
			failures = append(failures, FileBatchItemFailure{Path: raw, Error: "failed to access filesystem"})
		}
		return nil, 0, failures
	}
	defer root.Close()
	allKnown := true

	for _, raw := range paths {
		if ctx.Err() != nil {
			return nil, 0, failures
		}
		path := fsroot.CleanAbsPath(raw)
		if err := services.CheckNotProtected(root, path); err != nil {
			failures = append(failures, FileBatchItemFailure{Path: raw, Error: err.Error()})
			continue
		}
		info, err := root.Root.Lstat(fsroot.ToRel(path))
		if err != nil {
			failures = append(failures, FileBatchItemFailure{Path: raw, Error: "not found"})
			continue
		}
		total := deleteEntryTotalForPath(ctx, path, info.IsDir())
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

func deleteBatchItem(ctx context.Context, task *bridgeipc.Task, path, raw string, base, total int64, indeterminate bool, limiter *progressThrottle) (int64, bool, *FileBatchItemFailure, error) {
	count, err := services.DeleteFilesWithProgress(ctx, path, services.DeleteOptions{
		Progress: func(p int64) {
			cur, pct, ok := limiter.Set(base+p, total)
			if !ok {
				return
			}
			task.ReportProgress(CountProgress{
				Processed:     cur,
				Total:         total,
				Pct:           pct,
				Phase:         "deleting",
				Indeterminate: indeterminate,
			})
		},
	})
	if errors.Is(err, context.Canceled) {
		return count, false, nil, err
	}
	if err != nil {
		slog.Debug("batch delete item failed", "path", path, "error", err)
		return count, false, &FileBatchItemFailure{Path: raw, Error: err.Error()}, nil
	}
	return count, true, nil, nil
}

// runDeleteBatchTask deletes many paths as a single task. It resolves entry
// totals up front (indexer, else a bounded prescan) so progress reports a real
// aggregate percentage; when a total stays unknown the task reports an
// indeterminate running count instead.
func runDeleteBatchTask(ctx context.Context, task *bridgeipc.Task, store *userconfig.UserStore, req apischema.BatchPathRequest) (FileBatchResult, error) {
	if len(req.Paths) == 0 {
		return FileBatchResult{}, bridgeipc.NewError("no paths provided", 400)
	}

	task.ReportProgress(CountProgress{Phase: "preparing", Indeterminate: true})
	items, grandTotal, failures := planDeleteBatch(ctx, req.Paths)
	if ctx.Err() != nil {
		return FileBatchResult{}, context.Canceled
	}

	indeterminate := grandTotal <= 0
	limiter := newCountThrottle(taskSettingsForTask(ctx, task, store))
	var processed int64
	succeeded := 0
	updates := make([]string, 0, len(items))

	for _, item := range items {
		if ctx.Err() != nil {
			return FileBatchResult{}, context.Canceled
		}

		count, ok, failure, err := deleteBatchItem(ctx, task, item.path, item.raw, processed, grandTotal, indeterminate, limiter)
		// count reflects entries actually removed, even when the item failed
		// partway through, so aggregate progress stays monotonic.
		processed += count
		if err != nil {
			return FileBatchResult{}, err
		}
		if failure != nil {
			failures = append(failures, *failure)
			continue
		}
		if ok {
			succeeded++
		}

		updates = append(updates, item.path)
	}
	reconcileDetached("delete_batch", updates, func(ctx context.Context, path string) error {
		if err := deleteFromIndexer(ctx, path); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		return nil
	})

	slog.Info("batch delete complete", "total", len(req.Paths), "succeeded", succeeded, "failed", len(failures), "processed", processed)
	return batchResult(len(req.Paths), succeeded, failures), nil
}

func parseChmodBatchRequest(req apischema.FileChmodBatchRequest) (paths []string, mode os.FileMode, owner, group string, recursive bool, err error) {
	if len(req.Paths) == 0 || req.Mode == "" {
		return nil, 0, "", "", false, fmt.Errorf("missing paths or mode")
	}
	mode, err = parseOctalMode(req.Mode)
	if err != nil {
		return nil, 0, "", "", false, err
	}
	return req.Paths, mode, req.Owner, req.Group, req.Recursive != nil && *req.Recursive, nil
}

// parseOctalMode parses a chmod-style octal mode (at most 0o7777) and maps the
// setuid, setgid and sticky bits onto their os.FileMode flags; os.Chmod ignores
// the raw 0o7000 bits.
func parseOctalMode(value string) (os.FileMode, error) {
	parsed, err := strconv.ParseUint(value, 8, 12)
	if err != nil {
		return 0, fmt.Errorf("invalid mode: %w", err)
	}
	mode := os.FileMode(parsed) & os.ModePerm
	if parsed&0o4000 != 0 {
		mode |= os.ModeSetuid
	}
	if parsed&0o2000 != 0 {
		mode |= os.ModeSetgid
	}
	if parsed&0o1000 != 0 {
		mode |= os.ModeSticky
	}
	return mode, nil
}

// chmodBatchReporter accumulates a running processed-entry count across all
// items and phases of a chmod batch task, throttled by one shared limiter and
// reported against the planned grand total (0 when unknown).
type chmodBatchReporter struct {
	task      *bridgeipc.Task
	limiter   *progressThrottle
	total     int64
	processed int64
}

func (r *chmodBatchReporter) phase(phase string) func(processed int64) {
	base := r.processed
	return func(processed int64) {
		r.processed = base + processed
		count, pct, ok := r.limiter.Set(r.processed, r.total)
		if !ok {
			return
		}
		r.task.ReportProgress(CountProgress{
			Processed:     count,
			Total:         r.total,
			Pct:           pct,
			Phase:         phase,
			Indeterminate: r.total <= 0,
		})
	}
}

// planChmodBatchTotal sums the entries every pass of the batch will touch:
// each item's entry count times the number of passes (chmod, plus chown when
// ownership changes). Items whose count fails contribute nothing — they fail
// again during apply and report no progress, so excluding them keeps the
// percentage honest. Returns 0 only when no item could be counted.
func planChmodBatchTotal(ctx context.Context, paths []string, recursive bool, passes int64) int64 {
	var grandTotal int64
	for _, raw := range paths {
		if ctx.Err() != nil {
			return 0
		}
		total, err := services.CountEntries(ctx, fsroot.CleanAbsPath(raw), recursive)
		if err != nil {
			slog.Debug("failed to count chmod entries", "path", raw, "error", err)
			continue
		}
		grandTotal += total * passes
	}
	return grandTotal
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
func runChmodBatchTask(ctx context.Context, task *bridgeipc.Task, store *userconfig.UserStore, req apischema.FileChmodBatchRequest) (FileBatchResult, error) {
	paths, mode, owner, group, recursive, err := parseChmodBatchRequest(req)
	if err != nil {
		return FileBatchResult{}, bridgeipc.NewError(err.Error(), 400)
	}
	ownership, err := resolveChmodOwnership(owner, group)
	if err != nil {
		return FileBatchResult{}, bridgeipc.NewError(err.Error(), 400)
	}

	task.ReportProgress(CountProgress{Phase: "preparing", Indeterminate: true})
	passes := int64(1)
	if ownership != nil {
		passes = 2
	}
	reporter := &chmodBatchReporter{
		task:    task,
		limiter: newCountThrottle(taskSettingsForTask(ctx, task, store)),
		total:   planChmodBatchTotal(ctx, paths, recursive, passes),
	}

	succeeded := 0
	failures := make([]FileBatchItemFailure, 0)
	for _, raw := range paths {
		if ctx.Err() != nil {
			return FileBatchResult{}, context.Canceled
		}
		path := fsroot.CleanAbsPath(raw)
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

func batchTaskLabel(paths []string) string {
	if len(paths) == 1 {
		trimmed := strings.TrimRight(paths[0], "/")
		if trimmed == "" {
			return "item"
		}
		return filepath.Base(trimmed)
	}
	return fmt.Sprintf("%d items", len(paths))
}

func abortErr(ctx context.Context) error {
	if ctx.Err() != nil {
		return context.Canceled
	}
	return bridgeipc.NewError("operation aborted", 499)
}
