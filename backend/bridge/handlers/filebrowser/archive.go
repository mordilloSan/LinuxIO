package filebrowser

import (
	"log/slog"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/services"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
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

func computeArchiveSize(paths []string) int64 {
	totalSize, err := services.ComputeArchiveSize(paths)
	if err != nil {
		slog.Debug("failed to compute archive size", "error", err)
		return 0
	}
	return totalSize
}

func createArchive(format, targetPath string, opts *ipc.OperationCallbacks, paths []string) error {
	switch format {
	case "zip":
		return services.CreateZip(targetPath, opts, targetPath, paths...)
	case "tar.gz":
		return services.CreateTarGz(targetPath, opts, targetPath, paths...)
	default:
		return ipc.ErrUnsupportedFormat
	}
}
