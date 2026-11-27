package filebrowser

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/services"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/indexer"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/fsroot"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/linksafe"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/userconfig"
	indexerapi "github.com/mordilloSan/LinuxIO/backend/indexer/api"
)

// sessionOwner holds the session user's ids, which the trash needs to find
// that user's home. Unset, the bridge's own home and ids are used.
type sessionOwner struct {
	uid uint32
	gid uint32
	set bool
	// sudo is whether the session may use the routes that need root, the
	// index among them.
	sudo bool
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
	// A root session is no less trusted than the bridge: no hand-over and
	// no link rule (see actingFor).
	u.chownNew = os.Geteuid() == 0 && owner.uid != 0
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
		entry := apischema.TrashEntry{Path: item.Path, Name: item.Name, OriginalPath: item.OriginalPath, IsDir: item.IsDir, Size: item.Size, ConfirmRestore: item.ConfirmRestore}
		if !item.DeletedAt.IsZero() {
			entry.DeletedAt = item.DeletedAt.Format(time.RFC3339)
		}
		response.Entries = append(response.Entries, entry)
	}
	if owner.sudo {
		refreshTrashesInIndexer(ctx, items)
	}
	return response, nil
}

// refreshTrashesInIndexer asks the indexer, for a session that may use the
// index, to refresh the files folder of every trash the list read an item
// from, as the listing of a folder does (see listOpenedDirectory): an item
// another program trashed then gets a size in the Trash. The list does not
// wait for it.
func refreshTrashesInIndexer(ctx context.Context, items []services.TrashItem) {
	var refreshed []string
	for _, item := range items {
		files := filepath.Dir(item.Path)
		if slices.Contains(refreshed, files) {
			continue
		}
		refreshed = append(refreshed, files)
		// The indexer knows a folder by its real path.
		resolved, err := indexedPath(ctx, files)
		if err != nil {
			slog.Debug("failed to resolve a trash for the indexer", "path", files, "error", err)
			continue
		}
		refreshInIndexerDetached(resolved)
	}
}

// trashUsage is the disk space the session user's trashes take, read from
// the index and never walked: the page shows it wherever the Trash button is.
// A trash with nothing in it is not asked about, so an emptied Trash takes
// nothing. What the index holds for a trash is its files folder's size among
// the subfolders of the trash, a read of rows the index keeps sizes for,
// where the folder's own details would count every entry below it.
func trashUsage(ctx context.Context, owner sessionOwner) (apischema.TrashUsageResponse, error) {
	u, err := trashUserOrError(owner)
	if err != nil {
		return apischema.TrashUsageResponse{}, err
	}
	inUse, err := services.TrashFilesInUse(ctx, u.home, u.uid)
	if err != nil {
		return apischema.TrashUsageResponse{}, fmt.Errorf("find trashes: %w", err)
	}
	// Read before the indexer is asked: see indexerUpdatesInFlight.
	usage := apischema.TrashUsageResponse{Pending: len(inUse) > 0 && indexerUpdatesInFlight()}
	for _, files := range inUse {
		trash := filepath.Dir(files)
		size, pending, known, err := indexedTrashSize(ctx, trash, files)
		if err != nil {
			return apischema.TrashUsageResponse{}, err
		}
		if !known {
			// A trash made since the last scan, as by the first move into it,
			// has rows for its items and none for its own folders. The indexer
			// reads it as it reads a folder someone opens, at most once in a
			// while; asked again, it says whether that read is still to run.
			if err = indexer.Refresh(ctx, trash); err != nil {
				return apischema.TrashUsageResponse{}, fmt.Errorf("size trash: %w", indexerError("refresh", err))
			}
			if size, pending, known, err = indexedTrashSize(ctx, trash, files); err != nil {
				return apischema.TrashUsageResponse{}, err
			}
		}
		usage.Pending = usage.Pending || pending
		usage.Partial = usage.Partial || !known
		usage.Size += size
	}
	return usage, nil
}

// indexedTrashSize is the size the index holds for files, the files folder of
// trash, read among the subfolders of trash; known is false when it holds no
// row for it.
func indexedTrashSize(ctx context.Context, trash, files string) (size int64, pending, known bool, err error) {
	found, err := indexer.Subfolders(ctx, trash)
	if err != nil {
		return 0, false, false, fmt.Errorf("size trash: %w", indexerError("subfolders", err))
	}
	i := slices.IndexFunc(found.Subfolders, func(folder indexerapi.SubfolderResult) bool { return folder.Path == files })
	if i < 0 {
		return 0, found.Pending, false, nil
	}
	return found.Subfolders[i].Size, found.Pending, true, nil
}

// trashSize sizes a folder in the session user's trash for its properties;
// the list gives no size for a folder.
func trashSize(ctx context.Context, owner sessionOwner, req apischema.PathRequest) (apischema.DirectorySizeData, error) {
	if req.Path == "" {
		return apischema.DirectorySizeData{}, fmt.Errorf("%w: missing path", bridgeipc.ErrInvalidArgs)
	}
	u, err := trashUserOrError(owner)
	if err != nil {
		return apischema.DirectorySizeData{}, err
	}
	size, err := services.TrashedFolderSize(ctx, fsroot.CleanAbsPath(req.Path), u.home, u.uid)
	if err != nil {
		return apischema.DirectorySizeData{}, fsError("size trashed folder", err)
	}
	return apischema.DirectorySizeData{Size: size.Size, FileCount: size.Files, FolderCount: size.Folders}, nil
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
			failures = append(failures, FileBatchItemFailure{Path: raw, Error: err.Error(), Reason: failureReason(err)})
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

// failureReason names the failures the page offers a way out of: a restore
// whose name is taken, which it can send again with replace or keep both,
// and a move to the trash on a filesystem without one, which it can offer to
// delete for good instead.
func failureReason(err error) apischema.FileBatchFailureReason {
	switch {
	case errors.Is(err, services.ErrNameTaken):
		return apischema.FileBatchFailureExists
	case errors.Is(err, services.ErrNoTrash):
		return apischema.FileBatchFailureNoTrash
	}
	return ""
}

func reconcileMoves(label string, updates []batchIndexerUpdate) {
	reconcileDetached(label, updates, func(ctx context.Context, update batchIndexerUpdate) error {
		return movePathInIndexer(ctx, update.source, update.dest)
	})
}

// trashMove moves the item at path into or out of u's trash and returns
// where it went and whether it replaced what was there.
type trashMove func(ctx context.Context, path string, u trashUser) (dest string, replaced bool, err error)

// runTrashMoveTask runs a move into or out of the trash over every path and
// tells the indexer about each item that moved. Each item is judged where it
// really is (see linksafe.ResolveFolders). With pagePaths the paths are the
// page's, and the move takes the item by that real path, resolved for the
// session user it acts for. Without it they name items of the trash as the
// trash list gave them, which the trash checks as they are, so the move takes
// them as given.
func runTrashMoveTask(ctx context.Context, task *bridgeipc.Task, store *userconfig.UserStore, paths []string, label, phase string, owner sessionOwner, pagePaths bool, move trashMove) (FileBatchResult, error) {
	u, err := trashUserOrError(owner)
	if err != nil {
		return FileBatchResult{}, err
	}
	root, err := fsroot.Open()
	if err != nil {
		return FileBatchResult{}, fmt.Errorf("open filesystem root: %w", err)
	}
	defer root.Close()

	linkRule := actingFor(owner)
	// A restore's item is resolved with no link rule, for the stat and the
	// protected check below only. Both are safe so: neither moves anything,
	// the trash reaches the item it restores under its own rules, and what
	// the stat reads only goes to the indexer with a name resolved under the
	// rule.
	judgeRule := linksafe.Owner{}
	if pagePaths {
		judgeRule = linkRule
	}
	var updates []batchIndexerUpdate
	result, err := runPathBatch(ctx, task, store, paths, phase, func(ctx context.Context, source string) error {
		real, resolveErr := linksafe.ResolveFolders(ctx, source, judgeRule)
		if resolveErr != nil {
			return resolveErr
		}
		if pagePaths {
			source = real
		}
		if protectedErr := services.CheckNotProtected(root, real); protectedErr != nil {
			return protectedErr
		}
		info, statErr := root.Root.Lstat(fsroot.ToRel(real))
		if statErr != nil {
			return statErr
		}
		dest, replaced, moveErr := move(ctx, source, u)
		if moveErr != nil {
			return moveErr
		}
		sourceName, sourceOK := indexedName(ctx, source, linkRule)
		destName, destOK := indexedName(ctx, dest, linkRule)
		if sourceOK && destOK {
			updates = append(updates, batchIndexerUpdate{source: sourceName, dest: destName, info: info, replaced: replaced})
		}
		return nil
	})
	reconcileMoves(label, updates)
	return result, err
}

// indexedName is the name the indexer knows what a trash move took from or
// put at path by: its real path, since the trash keeps the home folder's
// path, which may lead through a link. It is resolved under linkRule, the
// session user's (see actingFor), so a link that user swapped into their
// trash's folders meanwhile does not have the admin's index record an entry
// where it leads. When the rule refuses, the indexer is not told of the item
// and learns of it at the next reindex.
func indexedName(ctx context.Context, path string, linkRule linksafe.Owner) (string, bool) {
	real, err := linksafe.ResolveFolders(ctx, path, linkRule)
	if err != nil {
		slog.Debug("left a trash move out of the index", "path", path, "error", err)
		return "", false
	}
	return real, true
}

// runTrashBatchTask moves many paths into the session user's trash as a single task.
func runTrashBatchTask(ctx context.Context, task *bridgeipc.Task, store *userconfig.UserStore, req apischema.BatchPathRequest, owner sessionOwner) (FileBatchResult, error) {
	return runTrashMoveTask(ctx, task, store, req.Paths, "trash_batch", "trashing", owner, true, func(ctx context.Context, path string, u trashUser) (string, bool, error) {
		dest, err := services.MoveToTrash(ctx, path, u.home, u.uid, u.gid, u.chownNew)
		return dest, false, err
	})
}

// runTrashRestoreTask puts trashed items back where they came from, or into
// the folder the request names under the names it gives. An item a root
// bridge has no record of trashing goes back into a folder the session user
// does not own, or over what took its name, only when the request confirms
// that destination for that item: a destination confirmed for another item
// of the request does not count. An item whose name is taken where it goes
// is kept both or replaced as the request says, and fails otherwise.
func runTrashRestoreTask(ctx context.Context, task *bridgeipc.Task, store *userconfig.UserStore, req apischema.TrashRestoreRequest, owner sessionOwner) (FileBatchResult, error) {
	into := ""
	if req.Destination != "" {
		root, err := fsroot.Open()
		if err != nil {
			return FileBatchResult{}, fmt.Errorf("open filesystem root: %w", err)
		}
		into, err = resolveBatchDestinationDir(ctx, root, req.Destination, actingFor(owner))
		_ = root.Close()
		if err != nil {
			return FileBatchResult{}, err
		}
	}
	overwrite := req.Overwrite != nil && *req.Overwrite
	keepBoth := make(map[string]bool, len(req.KeepBoth))
	for _, raw := range req.KeepBoth {
		keepBoth[fsroot.CleanAbsPath(raw)] = true
	}
	names := make(map[string]string, len(req.Names))
	for trashed, name := range req.Names {
		names[fsroot.CleanAbsPath(trashed)] = name
	}
	confirmed := confirmedRestores(req)
	return runTrashMoveTask(ctx, task, store, req.Paths, "trash_restore", "restoring", owner, false, func(ctx context.Context, path string, u trashUser) (string, bool, error) {
		opts := services.RestoreOptions{
			Confirmed: confirmed[path],
			Into:      into,
			KeepBoth:  keepBoth[path],
			Replace:   overwrite && !keepBoth[path],
		}
		if into != "" {
			opts.Name = names[path]
		}
		dest, err := services.RestoreFromTrash(ctx, path, u.home, u.uid, u.gid, u.chownNew, opts)
		return dest, opts.Replace, err
	})
}

// confirmedRestores returns, by trashed path, the destination the request
// confirmed for that item.
func confirmedRestores(req apischema.TrashRestoreRequest) map[string][]string {
	confirmed := make(map[string][]string, len(req.ConfirmedDestinations))
	for trashed, destination := range req.ConfirmedDestinations {
		confirmed[fsroot.CleanAbsPath(trashed)] = []string{fsroot.CleanAbsPath(destination)}
	}
	return confirmed
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
	// Records of items another program purged go too (see services.PruneTrashRecords).
	services.PruneTrashRecords(ctx, u.home, u.uid)
	reconcileDetached("trash_purge", purged, func(ctx context.Context, path string) error {
		if indexErr := deleteFromIndexer(ctx, path); indexErr != nil {
			return fmt.Errorf("%s: %w", path, indexErr)
		}
		return nil
	})
	return result, err
}
