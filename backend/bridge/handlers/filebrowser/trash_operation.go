package filebrowser

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/user"
	"strconv"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/services"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/userconfig"
)

// sessionOwner holds the session user's ids, which the trash needs to find
// that user's home. Unset, the bridge's own home and ids are used.
type sessionOwner struct {
	uid uint32
	gid uint32
	set bool
}

// trashUser is whose trash a request works on: the session user's home and
// ids, and whether a root bridge must hand new entries to that user.
type trashUser struct {
	home     string
	uid, gid int
	chownNew bool
}

func resolveTrashUser(owner sessionOwner) (trashUser, error) {
	u := trashUser{uid: os.Geteuid(), gid: os.Getegid()}
	if !owner.set {
		home, err := os.UserHomeDir()
		u.home = home
		return u, err
	}
	account, err := user.LookupId(strconv.FormatUint(uint64(owner.uid), 10))
	if err != nil {
		return u, err
	}
	u.home = account.HomeDir
	u.uid, u.gid = int(owner.uid), int(owner.gid)
	u.chownNew = os.Geteuid() == 0
	return u, nil
}

func trashUserOrError(owner sessionOwner) (trashUser, error) {
	u, err := resolveTrashUser(owner)
	if err != nil {
		return u, fmt.Errorf("resolve home directory for trash: %w", err)
	}
	if u.home == "" {
		return u, errors.New("cannot resolve home directory for trash")
	}
	return u, nil
}

func listTrash(ctx context.Context, owner sessionOwner) (apischema.TrashListResponse, error) {
	u, err := trashUserOrError(owner)
	if err != nil {
		return apischema.TrashListResponse{}, err
	}
	items, err := services.ListTrash(ctx, u.home, u.uid)
	if err != nil {
		return apischema.TrashListResponse{}, fmt.Errorf("list trash: %w", err)
	}
	response := apischema.TrashListResponse{Entries: make([]apischema.TrashEntry, 0, len(items))}
	for _, item := range items {
		entry := apischema.TrashEntry{Path: item.Path, Name: item.Name, OriginalPath: item.OriginalPath, IsDir: item.IsDir, Size: item.Size}
		if !item.DeletedAt.IsZero() {
			entry.DeletedAt = item.DeletedAt.Format(time.RFC3339)
		}
		response.Entries = append(response.Entries, entry)
	}
	return response, nil
}

// runPathBatch runs fn over every path as one task with a running item count,
// collecting per-item failures the way the other batch tasks do.
func runPathBatch(ctx context.Context, task *bridgeipc.Task, store *userconfig.UserStore, paths []string, phase string, fn func(ctx context.Context, path string) error) (FileBatchResult, error) {
	if len(paths) == 0 {
		return FileBatchResult{}, fmt.Errorf("%w: no paths provided", bridgeipc.ErrInvalidArgs)
	}
	task.ReportProgress(CountProgress{Phase: "preparing", Indeterminate: true})
	limiter := newCountThrottle(taskSettingsForTask(ctx, task, store))
	total := int64(len(paths))
	succeeded := 0
	var failures []FileBatchItemFailure

	for i, raw := range paths {
		if ctx.Err() != nil {
			return FileBatchResult{}, context.Canceled
		}
		if err := fn(ctx, fsroot.CleanAbsPath(raw)); err != nil {
			slog.Debug("batch item failed", "phase", phase, "path", raw, "error", err)
			failures = append(failures, FileBatchItemFailure{Path: raw, Error: err.Error()})
		} else {
			succeeded++
		}
		if cur, pct, ok := limiter.Set(int64(i+1), total); ok {
			task.ReportProgress(CountProgress{Processed: cur, Total: total, Pct: pct, Phase: phase})
		}
	}
	slog.Info("batch complete", "phase", phase, "total", len(paths), "succeeded", succeeded, "failed", len(failures))
	return batchResult(len(paths), succeeded, failures), nil
}

func reconcileMoves(label string, updates []batchIndexerUpdate) {
	reconcileDetached(label, updates, func(ctx context.Context, update batchIndexerUpdate) error {
		return movePathInIndexer(ctx, update.source, update.dest, false, func() (os.FileInfo, error) {
			return update.info, nil
		})
	})
}

// runTrashMoveTask runs a move into or out of the trash over every path and
// tells the indexer about each item that moved.
func runTrashMoveTask(ctx context.Context, task *bridgeipc.Task, store *userconfig.UserStore, paths []string, label, phase string, owner sessionOwner, move func(ctx context.Context, path string, u trashUser) (string, error)) (FileBatchResult, error) {
	u, err := trashUserOrError(owner)
	if err != nil {
		return FileBatchResult{}, err
	}
	root, err := fsroot.Open()
	if err != nil {
		return FileBatchResult{}, fmt.Errorf("open filesystem root: %w", err)
	}
	defer root.Close()

	var updates []batchIndexerUpdate
	result, err := runPathBatch(ctx, task, store, paths, phase, func(ctx context.Context, source string) error {
		if protectedErr := services.CheckNotProtected(root, source); protectedErr != nil {
			return protectedErr
		}
		info, statErr := root.Root.Lstat(fsroot.ToRel(source))
		if statErr != nil {
			return statErr
		}
		dest, moveErr := move(ctx, source, u)
		if moveErr != nil {
			return moveErr
		}
		updates = append(updates, batchIndexerUpdate{source: source, dest: dest, info: info})
		return nil
	})
	reconcileMoves(label, updates)
	return result, err
}

// runTrashBatchTask moves many paths into the session user's trash as a single task.
func runTrashBatchTask(ctx context.Context, task *bridgeipc.Task, store *userconfig.UserStore, req apischema.BatchPathRequest, owner sessionOwner) (FileBatchResult, error) {
	return runTrashMoveTask(ctx, task, store, req.Paths, "trash_batch", "trashing", owner, func(ctx context.Context, path string, u trashUser) (string, error) {
		return services.MoveToTrash(ctx, path, u.home, u.uid, u.gid, u.chownNew)
	})
}

// runTrashRestoreTask puts trashed items back where they came from.
func runTrashRestoreTask(ctx context.Context, task *bridgeipc.Task, store *userconfig.UserStore, req apischema.BatchPathRequest, owner sessionOwner) (FileBatchResult, error) {
	return runTrashMoveTask(ctx, task, store, req.Paths, "trash_restore", "restoring", owner, func(ctx context.Context, path string, u trashUser) (string, error) {
		return services.RestoreFromTrash(ctx, path, u.home, u.uid, u.gid, u.chownNew)
	})
}

// runTrashPurgeTask deletes trashed items for good.
func runTrashPurgeTask(ctx context.Context, task *bridgeipc.Task, store *userconfig.UserStore, req apischema.BatchPathRequest, owner sessionOwner) (FileBatchResult, error) {
	u, err := trashUserOrError(owner)
	if err != nil {
		return FileBatchResult{}, err
	}

	var purged []string
	result, err := runPathBatch(ctx, task, store, req.Paths, "deleting", func(ctx context.Context, trashed string) error {
		if _, purgeErr := services.PurgeFromTrash(ctx, trashed, u.home, u.uid, nil); purgeErr != nil {
			return purgeErr
		}
		purged = append(purged, trashed)
		return nil
	})
	reconcileDetached("trash_purge", purged, func(ctx context.Context, path string) error {
		if indexErr := deleteFromIndexer(ctx, path); indexErr != nil {
			return fmt.Errorf("%s: %w", path, indexErr)
		}
		return nil
	})
	return result, err
}
