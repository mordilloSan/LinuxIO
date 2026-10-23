package filebrowser

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/indexer"
)

// Indexer sync policy: the filesystem change is already done when the indexer
// hears about it, so a failed notification is logged at debug level and never
// fails the operation. Request-scoped changes (create, rename) notify inline
// under the caller's context; task-completion changes (upload, archive, batch)
// notify detached so the update outlives the task that finished the work.

var (
	detachedIndexerUpdates sync.WaitGroup
	errIndexerUnavailable  = errors.New("indexer unavailable")
)

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

func movePathInIndexer(ctx context.Context, source, destination string, removeExistingDestination bool, statDestination func() (os.FileInfo, error)) error {
	if err := deleteFromIndexer(ctx, source); err != nil {
		slog.Debug("failed to delete source from indexer after move", "source", source, "error", err)
	}
	if removeExistingDestination {
		if err := deleteFromIndexer(ctx, destination); err != nil {
			slog.Debug("failed to delete overwritten destination from indexer after move", "destination", destination, "error", err)
		}
	}

	info, err := statDestination()
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			slog.Debug("failed to stat destination for indexer update", "destination", destination, "error", err)
		}
		return nil
	}
	return addCopiedPathToIndexer(ctx, destination, info, false)
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

func notifyIndexerAfterPatch(ctx context.Context, root *fsroot.FSRoot, req resourcePatchRequest) {
	if err := movePathInIndexer(ctx, req.realSrc, req.realDest, false, func() (os.FileInfo, error) {
		return root.Root.Lstat(fsroot.ToRel(req.realDest))
	}); err != nil {
		slog.Debug("failed to update indexer after move", "source", req.realSrc, "destination", req.realDest, "error", err)
	}
}

// runDetachedIndexerUpdate bounds intentionally fire-and-forget indexer notifications
// that should outlive the request/task which already completed the filesystem change.
func runDetachedIndexerUpdate(label string, fn func(context.Context) error) {
	detachedIndexerUpdates.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := fn(ctx); err != nil {
			slog.Debug("detached indexer update failed", "operation", label, "error", err)
		}
	})
}

func addToIndexerDetached(label, path string) {
	runDetachedIndexerUpdate(label, func(ctx context.Context) error {
		return addToIndexer(ctx, path)
	})
}

func reindexDetached(label, path string) {
	runDetachedIndexerUpdate(label, func(ctx context.Context) error {
		return requestIndexerReindex(ctx, path)
	})
}

// reconcileDetached applies fn to every item inside one detached update. A
// failing item is logged and skipped so the rest still reach the indexer.
func reconcileDetached[T any](label string, items []T, fn func(context.Context, T) error) {
	if len(items) == 0 {
		return
	}
	runDetachedIndexerUpdate(label, func(ctx context.Context) error {
		for _, item := range items {
			if err := fn(ctx, item); err != nil {
				slog.Debug("indexer reconciliation failed", "operation", label, "error", err)
			}
		}
		return nil
	})
}
