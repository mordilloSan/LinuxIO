package filebrowser

import (
	"context"
	"log/slog"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/services"
	ipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
)

func archiveExtension(format string) (string, error) {
	switch format {
	case "zip":
		return ".zip", nil
	case "tar.gz":
		return ".tar.gz", nil
	default:
		return "", ipc.ErrUnsupportedFormat
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

func createArchive(format, targetPath string, opts *ipc.OperationCallbacks, compressionWorkers int, paths []string) error {
	switch format {
	case "zip":
		return services.CreateZip(targetPath, opts, targetPath, paths...)
	case "tar.gz":
		return services.CreateTarGz(targetPath, opts, targetPath, compressionWorkers, paths...)
	default:
		return ipc.ErrUnsupportedFormat
	}
}
