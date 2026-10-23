package filebrowser

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/services"
)

const (
	deleteLocalPrescanMaxBytes           int64 = 512 * 1024 * 1024
	deleteLocalPrescanMaxTopLevelEntries       = 1000
)

type computedTransferSize struct {
	total int64
	known bool
}

// estimate wraps an already computed size as a progress denominator.
func (s computedTransferSize) estimate() *sizeEstimate {
	return &sizeEstimate{total: s.total, known: s.known}
}

// knownSize wraps a total that was already computed, for callers whose
// denominator is cheap enough to establish up front.
func knownSize(total int64) *sizeEstimate {
	return computedTransferSize{total: total, known: true}.estimate()
}

// sizeEstimate carries a progress denominator that need not be known when the
// work starts. Archive size walks cost a stat per file, so compression runs
// alongside its estimate rather than behind it; the callbacks re-read the total
// on every report instead of capturing it once.
type sizeEstimate struct {
	mu    sync.Mutex
	total int64
	known bool
}

func (e *sizeEstimate) set(total int64) {
	e.mu.Lock()
	e.total = total
	e.known = true
	e.mu.Unlock()
}

func (e *sizeEstimate) get() (int64, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.total, e.known
}

// estimateArchiveSizeAsync walks paths for the compression denominator
// alongside the archive instead of ahead of it, so a large tree no longer
// stalls the task in "preparing" for the walk's full cost. Progress reports
// indeterminate until the estimate lands. The returned stop cancels and joins
// the walk; callers defer it so the walk never outlives the task that owns it.
func estimateArchiveSizeAsync(ctx context.Context, paths []string, size *sizeEstimate) (stop func()) {
	sizeCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		if total := computeArchiveSize(sizeCtx, paths); total > 0 {
			size.set(total)
		}
	}()
	return func() {
		cancel()
		<-done
	}
}

// computeArchiveSize returns 0 when the estimate cannot be produced — including
// when ctx cancels the walk. Callers treat 0 as "total unknown" and report
// indeterminate progress, so a failed estimate degrades the progress bar rather
// than the archive.
func computeArchiveSize(ctx context.Context, paths []string) int64 {
	totalSize, err := services.ComputeArchiveSize(ctx, paths)
	if err != nil {
		slog.Debug("failed to compute archive size", "error", err)
		return 0
	}
	return totalSize
}

func computeExtractSize(archivePath string, archiveSize int64) int64 {
	lowerName := strings.ToLower(archivePath)
	if strings.HasSuffix(lowerName, ".tar.gz") || strings.HasSuffix(lowerName, ".tgz") {
		return archiveSize
	}
	totalSize, err := services.ComputeExtractSize(archivePath)
	if err != nil {
		slog.Debug("failed to compute extract size", "path", archivePath, "error", err)
		return 0
	}
	return totalSize
}

func computeTransferSize(ctx context.Context, path string, info os.FileInfo) computedTransferSize {
	if info != nil && info.Mode()&os.ModeSymlink != 0 {
		return computedTransferSize{total: 0, known: true}
	}

	if info != nil && !info.IsDir() {
		if !info.Mode().IsRegular() {
			return computedTransferSize{total: 0, known: true}
		}
		return computedTransferSize{total: info.Size(), known: true}
	}

	if info != nil && info.IsDir() {
		if details, err := fetchDirSizeFromIndexer(ctx, path); err == nil {
			if details.Size > 0 || details.Files+details.Dirs > 0 {
				return computedTransferSize{total: details.Size, known: true}
			}
		} else {
			slog.Debug("failed to get transfer size from indexer", "path", path, "error", err)
		}
	}

	totalSize, err := services.ComputeCopySize(ctx, path)
	if err != nil {
		slog.Debug("failed to compute filebrowser operation size", "path", path, "error", err)
		return computedTransferSize{}
	}
	return computedTransferSize{total: totalSize, known: true}
}

func moveFileOptions(size computedTransferSize) services.MoveFileOptions {
	if !size.known {
		return services.MoveFileOptions{}
	}
	return services.MoveFileOptions{
		KnownSize:    size.total,
		HasKnownSize: true,
	}
}

// deleteEntryTotalForPath resolves how many entries deleting path will remove,
// so the batch task can aggregate a grand total before deletion starts. It
// returns 0 when the total is unknown (indexer miss and the directory is too
// large to prescan cheaply).
func deleteEntryTotalForPath(ctx context.Context, path string, isDir bool) int64 {
	if !isDir {
		return 1
	}

	if details, err := fetchDirSizeFromIndexer(ctx, path); err == nil {
		total := details.Files + details.Dirs
		if total > 0 {
			return total
		}
		if details.Size > deleteLocalPrescanMaxBytes {
			return 0
		}
		if details.Size > 0 {
			return prescanDeleteEntryTotal(ctx, path, true)
		}
		return prescanDeleteEntryTotal(ctx, path, shouldPrescanDeletePath(path))
	} else {
		slog.Debug("failed to get delete directory details from indexer", "path", path, "error", err)
	}

	return prescanDeleteEntryTotal(ctx, path, shouldPrescanDeletePath(path))
}

func prescanDeleteEntryTotal(ctx context.Context, path string, allowed bool) int64 {
	if !allowed {
		return 0
	}
	total, err := services.CountEntries(ctx, path, true)
	if err != nil {
		slog.Debug("failed to prescan delete directory entries", "path", path, "error", err)
		return 0
	}
	return total
}

func shouldPrescanDeletePath(path string) bool {
	ok, err := services.TopLevelEntryCountWithin(path, deleteLocalPrescanMaxTopLevelEntries)
	if err != nil {
		slog.Debug("failed to inspect delete directory top-level entries", "path", path, "error", err)
		return false
	}
	return ok
}
