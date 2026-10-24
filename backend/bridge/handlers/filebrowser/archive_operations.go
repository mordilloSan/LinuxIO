package filebrowser

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/services"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/userconfig"
)

func archiveExtension(format string) (string, error) {
	switch format {
	case "zip":
		return ".zip", nil
	case "tar.gz":
		return ".tar.gz", nil
	default:
		return "", services.ErrUnsupportedFormat
	}
}

func createArchive(format, targetPath string, opts *services.OperationCallbacks, compressionWorkers int, paths []string) error {
	switch format {
	case "zip":
		return services.CreateZip(targetPath, opts, targetPath, paths...)
	case "tar.gz":
		return services.CreateTarGz(targetPath, opts, targetPath, compressionWorkers, paths...)
	default:
		return services.ErrUnsupportedFormat
	}
}

func archiveCompressionWorkers(taskSettings userconfig.PersistedJobSettings) int {
	workers := taskSettings.ArchiveCompressionWorkers
	if workers <= 0 {
		return runtime.GOMAXPROCS(0)
	}
	return workers
}

func archiveExtractWorkers(taskSettings userconfig.PersistedJobSettings) int {
	workers := taskSettings.ArchiveExtractWorkers
	if workers <= 0 {
		return runtime.GOMAXPROCS(0)
	}
	return workers
}

func normalizeArchiveTargetPath(destination, extension string) string {
	targetPath := filepath.Clean(destination)
	lowerTarget := strings.ToLower(targetPath)
	switch extension {
	case ".zip":
		if !strings.HasSuffix(lowerTarget, ".zip") {
			targetPath += ".zip"
		}
	case ".tar.gz":
		if !(strings.HasSuffix(lowerTarget, ".tar.gz") || strings.HasSuffix(lowerTarget, ".tgz")) {
			targetPath += ".tar.gz"
		}
	}
	return targetPath
}

var errArchiveDestinationIsDir = errors.New("destination is a directory")

func prepareArchiveTarget(root *fsroot.FSRoot, targetPath string) (targetRel, tempRel, tempPath string, err error) {
	targetRel = fsroot.ToRel(targetPath)
	if info, statErr := root.Root.Stat(targetRel); statErr == nil {
		if info.IsDir() {
			return "", "", "", errArchiveDestinationIsDir
		}
	}

	if mkdirErr := root.Root.MkdirAll(fsroot.ToRel(filepath.Dir(targetPath)), services.PermDir); mkdirErr != nil {
		return "", "", "", fmt.Errorf("create parent dir: %w", mkdirErr)
	}
	tempFile, tempRel, err := root.CreateTemp(fsroot.ToRel(filepath.Dir(targetPath)), "."+filepath.Base(targetPath)+".linuxio-compress-*.part")
	if err != nil {
		return "", "", "", fmt.Errorf("create temp archive: %w", err)
	}
	if closeErr := tempFile.Close(); closeErr != nil {
		removeWithDebug(root, tempRel, targetPath)
		return "", "", "", fmt.Errorf("close temp archive: %w", closeErr)
	}
	tempPath = filepath.Clean("/" + tempRel)
	return targetRel, tempRel, tempPath, nil
}

// publishArchive moves the finished temp archive to its name without ever
// replacing a file that appeared there meanwhile: link then unlink, with a
// plain rename only on filesystems that have no hard links.
func publishArchive(root *fsroot.FSRoot, tempRel, targetRel string) error {
	err := root.Root.Link(tempRel, targetRel)
	if err == nil {
		return root.Root.Remove(tempRel)
	}
	if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EOPNOTSUPP) {
		return root.Root.Rename(tempRel, targetRel)
	}
	return err
}

func removeWithDebug(root *fsroot.FSRoot, targetRel, targetPath string) {
	if err := root.Root.Remove(targetRel); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Debug("failed to remove failed archive", "path", targetPath, "error", err)
	}
}

func parseExtractRequest(req apischema.FileExtractRequest) (string, string, error) {
	if req.ArchivePath == "" {
		return "", "", fmt.Errorf("missing archive path")
	}

	archivePath := filepath.Clean(req.ArchivePath)
	destination := defaultExtractDestination(archivePath)
	if req.Destination != nil && *req.Destination != "" {
		destination = filepath.Clean(*req.Destination)
	}
	return archivePath, destination, nil
}

func defaultExtractDestination(archivePath string) string {
	baseDir := filepath.Dir(archivePath)
	baseName := filepath.Base(archivePath)
	lowerName := strings.ToLower(baseName)

	switch {
	case strings.HasSuffix(lowerName, ".tar.gz"):
		baseName = strings.TrimSuffix(baseName, ".tar.gz")
	case strings.HasSuffix(lowerName, ".tgz"):
		baseName = strings.TrimSuffix(baseName, ".tgz")
	default:
		baseName = strings.TrimSuffix(baseName, filepath.Ext(baseName))
	}

	if baseName == "" || baseName == "/" || baseName == "." {
		baseName = "extracted"
	}

	return filepath.Join(baseDir, baseName)
}

func runCompressTask(ctx context.Context, task *bridgeipc.Task, store *userconfig.UserStore, req apischema.FileCompressRequest) (FileCompressResult, error) {
	if req.Format == "" || req.TargetPath == "" || len(req.Paths) == 0 {
		return FileCompressResult{}, bridgeipc.NewError("missing format, destination, or paths", 400)
	}

	extension, err := archiveExtension(req.Format)
	if err != nil {
		return FileCompressResult{}, bridgeipc.NewError(fmt.Sprintf("unsupported format: %s", req.Format), 400)
	}
	settings := taskSettingsForTask(ctx, task, store)
	release, err := heavyArchiveLimiter.acquire(ctx, settings.HeavyArchiveConcurrency)
	if err != nil {
		return FileCompressResult{}, context.Canceled
	}
	defer release()

	targetPath := normalizeArchiveTargetPath(req.TargetPath, extension)
	root, err := fsroot.Open()
	if err != nil {
		return FileCompressResult{}, bridgeipc.NewError("failed to access filesystem", 500)
	}
	defer root.Close()
	targetRel, tempRel, tempPath, err := prepareArchiveTarget(root, targetPath)
	if err != nil {
		status := 500
		message := fmt.Sprintf("cannot prepare archive target: %v", err)
		if errors.Is(err, errArchiveDestinationIsDir) {
			status = 400
			message = "destination is a directory"
		}
		return FileCompressResult{}, bridgeipc.NewError(message, status)
	}

	size := &sizeEstimate{}
	defer estimateArchiveSizeAsync(ctx, req.Paths, size)()

	writeTaskPhaseProgress(task, 0, "preparing")
	opts := newTaskPhaseCallbacks(ctx, task, store, size, "compressing")
	err = createArchive(req.Format, tempPath, opts, archiveCompressionWorkers(settings), req.Paths)
	if errors.Is(err, services.ErrAborted) {
		slog.Info("compress aborted, cleaning up", "path", targetPath)
		removeWithDebug(root, tempRel, tempPath)
		return FileCompressResult{}, abortErr(ctx)
	}
	if err != nil {
		removeWithDebug(root, tempRel, tempPath)
		return FileCompressResult{}, bridgeipc.NewError(fmt.Sprintf("compression failed: %v", err), 500)
	}
	if err := publishArchive(root, tempRel, targetRel); err != nil {
		removeWithDebug(root, tempRel, tempPath)
		if errors.Is(err, fs.ErrExist) {
			return FileCompressResult{}, bridgeipc.NewError("an archive with that name already exists", 409)
		}
		return FileCompressResult{}, bridgeipc.NewError(fmt.Sprintf("finalize archive: %v", err), 500)
	}

	var archiveSize int64
	if info, err := root.Root.Stat(targetRel); err == nil {
		archiveSize = info.Size()
		addToIndexerDetached("archive_create", targetPath)
	}

	slog.Info("compress complete", "path", targetPath, "count", len(req.Paths), "size", archiveSize, "format", req.Format)
	return FileCompressResult{Path: targetPath, Size: archiveSize, Format: req.Format}, nil
}

func runExtractTask(ctx context.Context, task *bridgeipc.Task, store *userconfig.UserStore, req apischema.FileExtractRequest) (FileExtractResult, error) {
	archivePath, destination, err := parseExtractRequest(req)
	if err != nil {
		return FileExtractResult{}, bridgeipc.NewError("missing archive path", 400)
	}

	root, err := fsroot.Open()
	if err != nil {
		return FileExtractResult{}, bridgeipc.NewError("failed to access filesystem", 500)
	}
	defer root.Close()

	archiveStat, err := root.Root.Stat(fsroot.ToRel(archivePath))
	if err != nil {
		return FileExtractResult{}, bridgeipc.NewError(fmt.Sprintf("archive not found: %v", err), 404)
	}
	if archiveStat.IsDir() {
		return FileExtractResult{}, bridgeipc.NewError("path is a directory, not an archive", 400)
	}

	settings := taskSettingsForTask(ctx, task, store)
	release, err := heavyArchiveLimiter.acquire(ctx, settings.HeavyArchiveConcurrency)
	if err != nil {
		return FileExtractResult{}, context.Canceled
	}
	defer release()

	totalSize := computeExtractSize(archivePath, archiveStat.Size())
	if spaceErr := services.EnsureFreeSpace(filepath.Dir(destination), totalSize); spaceErr != nil {
		return FileExtractResult{}, bridgeipc.NewError(spaceErr.Error(), 507)
	}
	writeTaskPhaseProgress(task, totalSize, "preparing")
	opts := newTaskPhaseCallbacks(ctx, task, store, knownSize(totalSize), "extracting")
	err = services.ExtractArchive(archivePath, destination, opts, archiveExtractWorkers(settings))
	if errors.Is(err, services.ErrAborted) {
		slog.Info("extract aborted, cleaning up", "path", destination)
		// The destination was created by this task, so it is ours to drop.
		if removeErr := root.Root.RemoveAll(fsroot.ToRel(destination)); removeErr != nil {
			slog.Debug("failed to clean up extraction directory", "path", destination, "error", removeErr)
		}
		return FileExtractResult{}, abortErr(ctx)
	}
	if err != nil {
		return FileExtractResult{}, bridgeipc.NewError(fmt.Sprintf("extraction failed: %v", err), 500)
	}

	reindexDetached("archive_extract", destination)
	slog.Info("extract complete", "archive", archivePath, "destination", destination)
	return FileExtractResult{Destination: destination}, nil
}
