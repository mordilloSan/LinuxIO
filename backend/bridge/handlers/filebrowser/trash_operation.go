package filebrowser

import (
	"context"
	"log/slog"
	"os"
	"os/user"
	"strconv"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/services"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/userconfig"
)

// trashHomeDir resolves the session user's home, which holds their trash.
func trashHomeDir(owner uploadOwner) (string, error) {
	if owner.set {
		u, err := user.LookupId(strconv.FormatUint(uint64(owner.uid), 10))
		if err != nil {
			return "", err
		}
		return u.HomeDir, nil
	}
	return os.UserHomeDir()
}

// runTrashBatchTask moves many paths into the session user's trash as a single
// task, reporting one processed-item count across the batch.
func runTrashBatchTask(ctx context.Context, task *bridgeipc.Task, store *userconfig.UserStore, req apischema.BatchPathRequest, owner uploadOwner) (FileBatchResult, error) {
	if len(req.Paths) == 0 {
		return FileBatchResult{}, bridgeipc.NewError("no paths provided", 400)
	}
	home, err := trashHomeDir(owner)
	if err != nil || home == "" {
		slog.Warn("trash: cannot resolve home directory", "error", err)
		return FileBatchResult{}, bridgeipc.NewError("cannot resolve home directory for trash", 500)
	}
	uid, gid := os.Geteuid(), os.Getegid()
	if owner.set {
		uid, gid = int(owner.uid), int(owner.gid)
	}
	chownNew := owner.set && os.Geteuid() == 0

	root, err := fsroot.Open()
	if err != nil {
		return FileBatchResult{}, bridgeipc.NewError("cannot open filesystem", 500)
	}
	defer root.Close()

	task.ReportProgress(CountProgress{Phase: "preparing", Indeterminate: true})
	limiter := newCountThrottle(taskSettingsForTask(ctx, task, store))
	total := int64(len(req.Paths))
	var failures []FileBatchItemFailure
	var updates []batchIndexerUpdate

	for i, raw := range req.Paths {
		if ctx.Err() != nil {
			return FileBatchResult{}, context.Canceled
		}
		source := fsroot.CleanAbsPath(raw)
		info, err := root.Root.Lstat(fsroot.ToRel(source))
		if err == nil {
			var dest string
			if dest, err = services.MoveToTrash(ctx, source, home, uid, gid, chownNew); err == nil {
				updates = append(updates, batchIndexerUpdate{source: source, dest: dest, info: info})
			}
		}
		if err != nil {
			slog.Debug("batch trash item failed", "path", source, "error", err)
			failures = append(failures, FileBatchItemFailure{Path: raw, Error: err.Error()})
		}
		if cur, pct, ok := limiter.Set(int64(i+1), total); ok {
			task.ReportProgress(CountProgress{Processed: cur, Total: total, Pct: pct, Phase: "trashing"})
		}
	}

	reconcileDetached("trash_batch", updates, func(ctx context.Context, update batchIndexerUpdate) error {
		return movePathInIndexer(ctx, update.source, update.dest, false, func() (os.FileInfo, error) {
			return update.info, nil
		})
	})

	slog.Info("batch trash complete", "total", len(req.Paths), "succeeded", len(updates), "failed", len(failures))
	return batchResult(len(req.Paths), len(updates), failures), nil
}
