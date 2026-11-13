package filebrowser

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/sys/unix"

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

// createArchive writes an archive of paths to out, the open file at outPath,
// which the archive leaves out should it lie inside one of paths.
func createArchive(format string, out *os.File, outPath string, opts *services.OperationCallbacks, compressionWorkers int, paths []string) error {
	switch format {
	case "zip":
		return services.CreateZip(out, opts, outPath, paths...)
	case "tar.gz":
		return services.CreateTarGz(out, opts, outPath, compressionWorkers, paths...)
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

// archiveTarget is where a compress writes: the open folder the archive
// lands in and the temp file in it the archive is written to before it
// takes its name. Both are made and used through that folder's handle,
// never by path.
type archiveTarget struct {
	dir      *os.File
	name     string
	temp     *os.File
	tempName string
	tempPath string
}

// prepareArchiveTarget opens the folder targetPath sits in, making missing
// folders on the way, and creates the temp archive in it. On a root bridge
// acting for owner the folder is reached without following a link the
// session user could have planted (see services.OpenNearestDirNoUserLinks).
func prepareArchiveTarget(root *fsroot.FSRoot, targetPath string, owner services.Owner) (*archiveTarget, error) {
	dir, missing, err := services.OpenNearestDirNoUserLinks(root, filepath.Dir(targetPath), owner)
	if err != nil {
		return nil, fmt.Errorf("open parent dir: %w", err)
	}
	// New folders stay the bridge's, as the archive does.
	if dir, err = services.MkdirAllAt(dir, missing, services.PermDir, services.Owner{}); err != nil {
		return nil, fmt.Errorf("create parent dir: %w", err)
	}
	target := &archiveTarget{dir: dir, name: filepath.Base(targetPath)}
	var st unix.Stat_t
	if unix.Fstatat(int(dir.Fd()), target.name, &st, 0) == nil && st.Mode&unix.S_IFMT == unix.S_IFDIR {
		_ = dir.Close()
		return nil, errArchiveDestinationIsDir
	}
	target.tempName = "." + target.name + ".linuxio-compress-" + rand.Text() + ".part"
	target.tempPath = filepath.Join(filepath.Dir(targetPath), target.tempName)
	fd, err := unix.Openat(int(dir.Fd()), target.tempName, unix.O_RDWR|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		_ = dir.Close()
		return nil, fmt.Errorf("create temp archive: %w", &fs.PathError{Op: "create", Path: target.tempPath, Err: err})
	}
	target.temp = os.NewFile(uintptr(fd), target.tempPath)
	return target, nil
}

// finish closes the finished temp archive and gives it its name without ever
// replacing a file that appeared there meanwhile: link then unlink, with a
// plain rename only on filesystems that have no hard links. It returns the
// archive's size.
func (t *archiveTarget) finish() (int64, error) {
	info, err := t.temp.Stat()
	if closeErr := t.temp.Close(); err == nil {
		err = closeErr
	}
	t.temp = nil
	if err != nil {
		return 0, err
	}
	dirFd := int(t.dir.Fd())
	err = unix.Linkat(dirFd, t.tempName, dirFd, t.name, 0)
	if err == nil {
		err = unix.Unlinkat(dirFd, t.tempName, 0)
	} else if errors.Is(err, unix.EPERM) || errors.Is(err, unix.EOPNOTSUPP) {
		err = unix.Renameat(dirFd, t.tempName, dirFd, t.name)
	}
	if err != nil {
		return 0, &os.LinkError{Op: "publish", Old: t.tempPath, New: filepath.Join(filepath.Dir(t.tempPath), t.name), Err: err}
	}
	return info.Size(), nil
}

// discard drops the temp archive, through the folder's handle.
func (t *archiveTarget) discard() {
	if t.temp != nil {
		_ = t.temp.Close()
		t.temp = nil
	}
	if err := unix.Unlinkat(int(t.dir.Fd()), t.tempName, 0); err != nil && !errors.Is(err, unix.ENOENT) {
		slog.Debug("failed to remove failed archive", "path", t.tempPath, "error", err)
	}
}

func (t *archiveTarget) close() {
	if t.temp != nil {
		_ = t.temp.Close()
	}
	_ = t.dir.Close()
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

func runCompressTask(ctx context.Context, task *bridgeipc.Task, store *userconfig.UserStore, req apischema.FileCompressRequest, session sessionOwner) (FileCompressResult, error) {
	if req.Format == "" || req.TargetPath == "" || len(req.Paths) == 0 {
		return FileCompressResult{}, fmt.Errorf("%w: missing format, destination, or paths", bridgeipc.ErrInvalidArgs)
	}

	extension, err := archiveExtension(req.Format)
	if err != nil {
		return FileCompressResult{}, fmt.Errorf("%w: unsupported format: %s", bridgeipc.ErrInvalidArgs, req.Format)
	}
	settings := taskSettingsForTask(ctx, task, store)

	targetPath := normalizeArchiveTargetPath(req.TargetPath, extension)
	root, err := fsroot.Open()
	if err != nil {
		return FileCompressResult{}, fmt.Errorf("open filesystem root: %w", err)
	}
	defer root.Close()
	target, err := prepareArchiveTarget(root, targetPath, actingFor(session))
	if err != nil {
		if errors.Is(err, errArchiveDestinationIsDir) {
			return FileCompressResult{}, fmt.Errorf("%w: destination is a directory", bridgeipc.ErrInvalidArgs)
		}
		return FileCompressResult{}, fmt.Errorf("prepare archive target: %w", err)
	}
	defer target.close()

	size := &sizeEstimate{}
	defer estimateArchiveSizeAsync(ctx, req.Paths, size)()

	writeTaskPhaseProgress(task, 0, "preparing")
	opts := newTaskPhaseCallbacks(ctx, task, store, size, "compressing")
	err = createArchive(req.Format, target.temp, target.tempPath, opts, archiveCompressionWorkers(settings), req.Paths)
	if errors.Is(err, services.ErrAborted) {
		slog.Debug("compress aborted, cleaning up", "path", targetPath)
		target.discard()
		return FileCompressResult{}, abortErr(ctx)
	}
	if err != nil {
		target.discard()
		return FileCompressResult{}, fmt.Errorf("compression failed: %w", err)
	}
	archiveSize, err := target.finish()
	if err != nil {
		target.discard()
		if errors.Is(err, fs.ErrExist) {
			return FileCompressResult{}, fmt.Errorf("%w: an archive with that name already exists", bridgeipc.ErrConflict)
		}
		return FileCompressResult{}, fmt.Errorf("finalize archive: %w", err)
	}
	addToIndexerDetached("archive_create", targetPath)

	slog.Info("compress complete", "path", targetPath, "count", len(req.Paths), "size", archiveSize, "format", req.Format)
	return FileCompressResult{Path: targetPath, Size: archiveSize, Format: req.Format}, nil
}

func runExtractTask(ctx context.Context, task *bridgeipc.Task, store *userconfig.UserStore, req apischema.FileExtractRequest, session sessionOwner) (FileExtractResult, error) {
	archivePath, destination, err := parseExtractRequest(req)
	if err != nil {
		return FileExtractResult{}, fmt.Errorf("%w: %w", bridgeipc.ErrInvalidArgs, err)
	}

	root, err := fsroot.Open()
	if err != nil {
		return FileExtractResult{}, fmt.Errorf("open filesystem root: %w", err)
	}
	defer root.Close()

	archiveStat, err := root.Root.Stat(fsroot.ToRel(archivePath))
	if err != nil {
		return FileExtractResult{}, fsError("stat archive", err)
	}
	if archiveStat.IsDir() {
		return FileExtractResult{}, fmt.Errorf("%w: path is a directory, not an archive", bridgeipc.ErrInvalidArgs)
	}

	settings := taskSettingsForTask(ctx, task, store)

	totalSize := computeExtractSize(archivePath, archiveStat.Size())
	if spaceErr := services.EnsureFreeSpace(filepath.Dir(destination), totalSize); spaceErr != nil {
		return FileExtractResult{}, bridgeipc.NewError(spaceErr.Error(), 507)
	}
	writeTaskPhaseProgress(task, totalSize, "preparing")
	opts := newTaskPhaseCallbacks(ctx, task, store, knownSize(totalSize), "extracting")
	// An aborted extract removes the destination it made itself.
	err = services.ExtractArchive(ctx, archivePath, destination, actingFor(session), opts, archiveExtractWorkers(settings))
	if errors.Is(err, services.ErrAborted) {
		slog.Debug("extract aborted", "path", destination)
		return FileExtractResult{}, abortErr(ctx)
	}
	if err != nil {
		return FileExtractResult{}, fmt.Errorf("extraction failed: %w", err)
	}

	reindexDetached("archive_extract", destination)
	slog.Info("extract complete", "archive", archivePath, "destination", destination)
	return FileExtractResult{Destination: destination}, nil
}
