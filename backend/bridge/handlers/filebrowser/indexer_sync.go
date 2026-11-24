package filebrowser

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/indexer"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/fsroot"
)

// Indexer sync policy: the filesystem change is already done when the indexer
// hears about it, so a failed notification is logged at debug level and never
// fails the operation. Request-scoped changes (create, rename) notify inline
// under the caller's context; task-completion changes (upload, archive, batch)
// notify detached so the update outlives the task that finished the work.

var (
	detachedIndexerUpdates sync.WaitGroup
	// detachedInFlight counts the detached notifications that have not been
	// answered yet.
	detachedInFlight      atomic.Int64
	errIndexerUnavailable = errors.New("indexer unavailable")
)

// indexerUpdatesInFlight reports whether a detached notification is still on
// its way to the indexer. A finished task reports done first and tells the
// indexer after, so a size the page asks for right then can reach the indexer
// before the update does: the size is pending although the indexer does not
// know it yet. It is read before the indexer is asked, so an update that is
// answered in between is one the indexer already counts as waiting or has
// applied. It is not narrowed to a path: a size asked for elsewhere in those
// milliseconds is asked for once more.
func indexerUpdatesInFlight() bool {
	return detachedInFlight.Load() > 0
}

// indexerError tags an unreachable indexer with errIndexerUnavailable so
// callers can degrade, and keeps every other failure's identity under %w.
func indexerError(op string, err error) error {
	if errors.Is(err, indexer.ErrUnavailable) {
		return fmt.Errorf("%w: indexer %s request failed: %v", errIndexerUnavailable, op, err)
	}
	return fmt.Errorf("indexer %s request failed: %w", op, err)
}

// addToIndexer notifies the indexer daemon about a new or updated file.
func addToIndexer(ctx context.Context, path string) error {
	if err := indexer.Add(ctx, indexer.EntryForPath(path)); err != nil {
		return indexerError("add", err)
	}
	return nil
}

// deleteFromIndexer notifies the indexer daemon about a deleted file or directory.
func deleteFromIndexer(ctx context.Context, path string) error {
	if err := indexer.Delete(ctx, path); err != nil {
		return indexerError("delete", err)
	}
	return nil
}

func requestIndexerReindex(ctx context.Context, path string) error {
	if err := indexer.Reindex(ctx, path); err != nil {
		return indexerError("reindex", err)
	}
	return nil
}

// indexPath registers a new or replaced path: directories are rescanned,
// files are added.
func indexPath(ctx context.Context, path string, isDir bool) error {
	if isDir {
		return requestIndexerReindex(ctx, path)
	}
	return addToIndexer(ctx, path)
}

func addCopiedPathToIndexer(ctx context.Context, path string, info os.FileInfo, removeExisting bool) error {
	if removeExisting {
		if err := deleteFromIndexer(ctx, path); err != nil {
			slog.Debug("failed to remove overwritten indexer entry", "path", path, "error", err)
		}
	}
	if info == nil {
		return nil
	}
	return indexPath(ctx, path, info.IsDir())
}

// movePathInIndexer tells the indexer that source is now at destination. The
// indexer renames the rows when the move was a rename, and otherwise lets the
// index follow the disk for both paths: a move to another filesystem, into a
// folder that was already there, or one that stopped part-way.
func movePathInIndexer(ctx context.Context, source, destination string) error {
	if err := indexer.Move(ctx, source, destination); err != nil {
		return indexerError("move", err)
	}
	return nil
}

func notifyIndexerForCreatedResource(ctx context.Context, root *fsroot.FSRoot, cleanPath, relPath string) {
	info, err := root.Root.Stat(relPath)
	if err != nil {
		return
	}
	if err := indexPath(ctx, cleanPath, info.IsDir()); err != nil {
		kind := "file"
		if info.IsDir() {
			kind = "directory"
		}
		slog.Debug("failed to update indexer after create", "path", cleanPath, "type", kind, "error", err)
	}
}

func notifyIndexerAfterPatch(ctx context.Context, req resourcePatchRequest) {
	if err := movePathInIndexer(ctx, req.realSrc, req.realDest); err != nil {
		slog.Debug("failed to update indexer after move", "source", req.realSrc, "destination", req.realDest, "error", err)
	}
}

// runDetachedIndexerUpdate bounds intentionally fire-and-forget indexer notifications
// that should outlive the request/task which already completed the filesystem change.
func runDetachedIndexerUpdate(label string, fn func(context.Context) error) {
	detachedInFlight.Add(1)
	detachedIndexerUpdates.Go(func() {
		defer detachedInFlight.Add(-1)
		if err := withIndexerTimeout(fn); err != nil {
			slog.Debug("detached indexer update failed", "operation", label, "error", err)
		}
	})
}

// withIndexerTimeout runs one indexer notification under its own deadline,
// detached from the request or task that finished the filesystem change.
func withIndexerTimeout(fn func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return fn(ctx)
}

func addToIndexerDetached(label, path string) {
	runDetachedIndexerUpdate(label, func(ctx context.Context) error {
		return addToIndexer(ctx, path)
	})
}

// refreshInIndexerDetached asks the indexer to bring the entries of a folder
// that was just listed up to date. The indexer queues it and skips a folder it
// refreshed a short while ago.
func refreshInIndexerDetached(path string) {
	runDetachedIndexerUpdate("refresh", func(ctx context.Context) error {
		if err := indexer.Refresh(ctx, path); err != nil {
			return indexerError("refresh", err)
		}
		return nil
	})
}

func reindexDetached(label, path string) {
	runDetachedIndexerUpdate(label, func(ctx context.Context) error {
		return requestIndexerReindex(ctx, path)
	})
}

// reconcileDetached applies fn to every item in one detached goroutine, each
// under its own timeout. A failing item is logged and skipped so the rest
// still reach the indexer.
func reconcileDetached[T any](label string, items []T, fn func(context.Context, T) error) {
	if len(items) == 0 {
		return
	}
	detachedInFlight.Add(1)
	detachedIndexerUpdates.Go(func() {
		defer detachedInFlight.Add(-1)
		for _, item := range items {
			if err := withIndexerTimeout(func(ctx context.Context) error { return fn(ctx, item) }); err != nil {
				slog.Debug("indexer reconciliation failed", "operation", label, "error", err)
			}
		}
	})
}
