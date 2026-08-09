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
	"sync"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/services"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/indexer"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/config"
	bridgetasks "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
	ipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
	"github.com/mordilloSan/LinuxIO/backend/common/utils"
)

var heavyArchiveLimiter archiveResourceLimiter

type archiveResourceLimiter struct {
	mu     sync.Mutex
	active int
}

func (l *archiveResourceLimiter) acquire(ctx context.Context, max int) (func(), error) {
	if max <= 0 {
		max = 1
	}
	for {
		l.mu.Lock()
		if l.active < max {
			l.active++
			l.mu.Unlock()
			return func() {
				l.mu.Lock()
				if l.active > 0 {
					l.active--
				}
				l.mu.Unlock()
			}, nil
		}
		l.mu.Unlock()

		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, context.Canceled
		case <-timer.C:
		}
	}
}

type progressLimiter struct {
	mu          sync.Mutex
	total       int64
	minBytes    int64
	minInterval time.Duration
	processed   int64
	lastBytes   int64
	lastPct     int
	lastAt      time.Time
}

type countProgressLimiter struct {
	mu          sync.Mutex
	minInterval time.Duration
	processed   int64
	total       int64
	lastCount   int64
	lastPct     int
	lastAt      time.Time
}

func newProgressLimiter(taskSettings config.PersistedJobSettings, total int64) *progressLimiter {
	taskSettings = config.EffectiveJobSettings(taskSettings)
	minBytes := int64(taskSettings.ProgressMinBytesMB) * 1024 * 1024
	if minBytes <= 0 {
		minBytes = progressReportIntervalBytes
	}
	minInterval := time.Duration(taskSettings.ProgressMinIntervalMs) * time.Millisecond
	if minInterval <= 0 {
		minInterval = 250 * time.Millisecond
	}
	return &progressLimiter{
		total:       total,
		minBytes:    minBytes,
		minInterval: minInterval,
		lastPct:     -1,
	}
}

func newCountProgressLimiter(taskSettings config.PersistedJobSettings) *countProgressLimiter {
	taskSettings = config.EffectiveJobSettings(taskSettings)
	minInterval := time.Duration(taskSettings.ProgressMinIntervalMs) * time.Millisecond
	if minInterval <= 0 {
		minInterval = 250 * time.Millisecond
	}
	return &countProgressLimiter{
		minInterval: minInterval,
		lastPct:     -1,
	}
}

func (l *progressLimiter) Add(n int64) (int64, int, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if n > 0 {
		l.processed += n
	}
	pct := 0
	if l.total > 0 {
		pct = min(int(l.processed*100/l.total), 100)
	}
	final := l.total > 0 && l.processed >= l.total
	now := time.Now()
	if !final && !l.lastAt.IsZero() && now.Sub(l.lastAt) < l.minInterval {
		return l.processed, pct, false
	}
	bytesChanged := l.processed-l.lastBytes >= l.minBytes
	pctChanged := pct > l.lastPct
	if !final && !bytesChanged && !pctChanged {
		return l.processed, pct, false
	}
	l.lastAt = now
	l.lastBytes = l.processed
	l.lastPct = pct
	return l.processed, pct, true
}

func (l *countProgressLimiter) Set(processed, total int64) (int64, int, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.processed = processed
	l.total = total
	pct := 0
	if l.total > 0 {
		pct = min(int(l.processed*100/l.total), 100)
	}
	final := l.total > 0 && l.processed >= l.total
	now := time.Now()
	if !final && !l.lastAt.IsZero() && now.Sub(l.lastAt) < l.minInterval {
		return l.processed, pct, false
	}
	if !final && l.processed == l.lastCount && pct == l.lastPct {
		return l.processed, pct, false
	}
	l.lastAt = now
	l.lastCount = l.processed
	l.lastPct = pct
	return l.processed, pct, true
}

func taskSettingsForTask(ctx context.Context, task *bridgetasks.Task, store *config.UserStore) config.PersistedJobSettings {
	if task == nil || strings.TrimSpace(task.Owner().Username) == "" {
		return config.DefaultJobSettings()
	}
	cfg, _, err := config.SnapshotForUser(ctx, task.Owner().Username, store)
	if err != nil || cfg == nil {
		return config.DefaultJobSettings()
	}
	return config.EffectiveJobSettings(cfg.Jobs)
}

func archiveCompressionWorkers(taskSettings config.PersistedJobSettings) int {
	workers := taskSettings.ArchiveCompressionWorkers
	if workers <= 0 {
		return runtime.GOMAXPROCS(0)
	}
	return workers
}

func archiveExtractWorkers(taskSettings config.PersistedJobSettings) int {
	workers := taskSettings.ArchiveExtractWorkers
	if workers <= 0 {
		return runtime.GOMAXPROCS(0)
	}
	return workers
}

const (
	routeArchive     = "filebrowser.archive"
	routeDownload    = "filebrowser.download"
	routeUpload      = "filebrowser.upload"
	routeUploadBatch = "filebrowser.upload_batch"
)

var fileTaskRoutes = fileTaskBindings(nil).Routes()

func fileTaskBindings(store *config.UserStore) apischema.BindingSet {
	return apischema.Bindings(
		apischema.TaskRunner[apischema.FileCompressRequest, apischema.TaskSnapshot]("filebrowser.compress", apischema.WithTaskMetadata(func(req apischema.FileCompressRequest) bridgetasks.TaskMetadata {
			return bridgetasks.TaskMetadata{Identity: []string{req.TargetPath}, Label: req.TargetPath, Path: req.TargetPath}
		})).Run(
			func(ctx context.Context, task *bridgetasks.Task, req apischema.FileCompressRequest) (any, error) {
				return runCompressTask(ctx, task, store, req)
			},
			bridgetasks.TaskDefault,
		),
		apischema.TaskRunner[apischema.FileExtractRequest, apischema.TaskSnapshot]("filebrowser.extract", apischema.WithTaskMetadata(func(req apischema.FileExtractRequest) bridgetasks.TaskMetadata {
			identity := []string{req.ArchivePath}
			if req.Destination != nil {
				identity = append(identity, *req.Destination)
			}
			return bridgetasks.TaskMetadata{Identity: identity, Label: req.ArchivePath, Path: req.ArchivePath}
		})).Run(
			func(ctx context.Context, task *bridgetasks.Task, req apischema.FileExtractRequest) (any, error) {
				return runExtractTask(ctx, task, store, req)
			},
			bridgetasks.TaskDefault,
		),
		apischema.TaskRunner[apischema.BatchTransferRequest, apischema.TaskSnapshot]("filebrowser.copy_batch", apischema.WithTaskMetadata(func(req apischema.BatchTransferRequest) bridgetasks.TaskMetadata {
			return bridgetasks.TaskMetadata{Identity: append(append([]string{}, req.Sources...), req.Destination), Label: batchTaskLabel(req.Sources), Path: req.Destination}
		})).Run(
			func(ctx context.Context, task *bridgetasks.Task, req apischema.BatchTransferRequest) (any, error) {
				return runCopyBatchTask(ctx, task, store, req)
			},
			bridgetasks.TaskDefault,
		),
		apischema.TaskRunner[apischema.BatchTransferRequest, apischema.TaskSnapshot]("filebrowser.move_batch", apischema.WithTaskMetadata(func(req apischema.BatchTransferRequest) bridgetasks.TaskMetadata {
			return bridgetasks.TaskMetadata{Identity: append(append([]string{}, req.Sources...), req.Destination), Label: batchTaskLabel(req.Sources), Path: req.Destination}
		})).Run(
			func(ctx context.Context, task *bridgetasks.Task, req apischema.BatchTransferRequest) (any, error) {
				return runMoveBatchTask(ctx, task, store, req)
			},
			bridgetasks.TaskDefault,
		),
		apischema.TaskRunner[apischema.BatchPathRequest, apischema.TaskSnapshot]("filebrowser.delete_batch", apischema.WithTaskMetadata(func(req apischema.BatchPathRequest) bridgetasks.TaskMetadata {
			return bridgetasks.TaskMetadata{Identity: append([]string{}, req.Paths...), Label: batchTaskLabel(req.Paths)}
		})).Run(
			runDeleteBatchTask,
			bridgetasks.TaskDefault,
		),
		apischema.TaskRunner[apischema.OptionalPathRequest, apischema.TaskSnapshot]("filebrowser.index", apischema.WithTaskMetadata(func(req apischema.OptionalPathRequest) bridgetasks.TaskMetadata {
			path := ""
			if req.Path != nil {
				path = *req.Path
			}
			return bridgetasks.TaskMetadata{Identity: []string{path}, Path: path, Label: path}
		})).Run(runIndexerTask, bridgetasks.TaskSingletonSystem),
		apischema.TaskRunner[apischema.FileUploadRequest, apischema.TaskSnapshot](routeUpload, apischema.WithTaskMetadata(func(req apischema.FileUploadRequest) bridgetasks.TaskMetadata {
			return bridgetasks.TaskMetadata{Identity: []string{req.TargetPath}, Path: req.TargetPath, Label: req.TargetPath}
		})).Run(runUploadTask, bridgetasks.TaskStreamDefault),
		apischema.TaskRunner[apischema.FileUploadBatchRequest, apischema.TaskSnapshot](routeUploadBatch, apischema.WithTaskMetadata(func(req apischema.FileUploadBatchRequest) bridgetasks.TaskMetadata {
			identity := []string{req.Destination}
			for _, file := range req.Files {
				identity = append(identity, "file", file.Path, "size", file.Size)
			}
			for _, directory := range req.Directories {
				identity = append(identity, "directory", directory)
			}
			if req.Overwrite != nil && *req.Overwrite {
				identity = append(identity, "overwrite", "true")
			} else {
				identity = append(identity, "overwrite", "false")
			}
			return bridgetasks.TaskMetadata{Identity: identity, Path: req.Destination, Label: req.Destination}
		})).Run(runUploadBatchTask, bridgetasks.TaskStreamDefault),
		apischema.TaskRunner[apischema.PathRequest, apischema.TaskSnapshot](routeDownload, apischema.WithTaskMetadata(func(req apischema.PathRequest) bridgetasks.TaskMetadata {
			return bridgetasks.TaskMetadata{Identity: []string{req.Path}, Path: req.Path, Label: req.Path}
		})).Run(runDownloadTask, bridgetasks.TaskStreamDefault),
		apischema.TaskRunner[apischema.FileArchiveRequest, apischema.TaskSnapshot](routeArchive, apischema.WithTaskMetadata(func(req apischema.FileArchiveRequest) bridgetasks.TaskMetadata {
			return bridgetasks.TaskMetadata{Identity: append([]string{req.Format}, req.Paths...), Label: batchTaskLabel(req.Paths)}
		})).Run(
			func(ctx context.Context, task *bridgetasks.Task, req apischema.FileArchiveRequest) (any, error) {
				return runArchiveTask(ctx, task, store, req)
			},
			bridgetasks.TaskStreamDefault,
		),
		apischema.TaskRunner[apischema.FileChmodBatchRequest, apischema.TaskSnapshot]("filebrowser.chmod_batch", apischema.WithTaskMetadata(func(req apischema.FileChmodBatchRequest) bridgetasks.TaskMetadata {
			return bridgetasks.TaskMetadata{Identity: append([]string{req.Mode, req.Owner, req.Group}, req.Paths...), Label: batchTaskLabel(req.Paths)}
		})).Run(
			func(ctx context.Context, task *bridgetasks.Task, req apischema.FileChmodBatchRequest) (any, error) {
				return runChmodBatchTask(ctx, task, store, req)
			},
			bridgetasks.TaskDefault,
		),
	)
}

func batchTaskLabel(paths []string) string {
	if len(paths) == 1 {
		trimmed := strings.TrimRight(paths[0], "/")
		if trimmed == "" {
			return "item"
		}
		return filepath.Base(trimmed)
	}
	return fmt.Sprintf("%d items", len(paths))
}

func RegisterTaskRoutes(router *bridgetasks.Router, store *config.UserStore) {
	fileTaskBindings(store).Register(router)
	bridgetasks.RegisterTaskDataAttacher(routeUpload, attachFileTransferData)
	bridgetasks.RegisterTaskDataAttacher(routeUploadBatch, attachFileTransferData)
	bridgetasks.RegisterTaskDataAttacher(routeDownload, attachFileTransferData)
	bridgetasks.RegisterTaskDataAttacher(routeArchive, attachFileTransferData)
}

func newTaskPhaseCallbacks(ctx context.Context, task *bridgetasks.Task, store *config.UserStore, totalSize int64, phase string) *ipc.OperationCallbacks {
	limiter := newProgressLimiter(taskSettingsForTask(ctx, task, store), totalSize)
	cancelFn := func() bool {
		select {
		case <-ctx.Done():
			return true
		default:
			return false
		}
	}
	return &ipc.OperationCallbacks{
		Cancel: cancelFn,
		Progress: func(n int64) {
			if totalSize <= 0 {
				return
			}
			processed, pct, ok := limiter.Add(n)
			if !ok {
				return
			}
			task.ReportProgress(FileProgress{
				Bytes: processed,
				Total: totalSize,
				Pct:   pct,
				Phase: phase,
			})
		},
	}
}

func writeTaskPhaseProgress(task *bridgetasks.Task, total int64, phase string) {
	task.ReportProgress(FileProgress{
		Total: total,
		Phase: phase,
	})
}

func abortErr(ctx context.Context) error {
	if ctx.Err() != nil {
		return context.Canceled
	}
	return bridgetasks.NewError("operation aborted", 499)
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

func prepareArchiveTarget(root *fsroot.FSRoot, targetPath string) (targetRel, tempRel, tempPath string, err error) {
	targetRel = fsroot.ToRel(targetPath)
	if info, statErr := root.Root.Stat(targetRel); statErr == nil {
		if info.IsDir() {
			return "", "", "", fmt.Errorf("destination is a directory")
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

func removeWithDebug(root *fsroot.FSRoot, targetRel, targetPath string) {
	if err := root.Root.Remove(targetRel); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Debug("failed to remove failed archive", "path", targetPath, "error", err)
	}
}

func notifyCompressedArchive(targetPath string, info os.FileInfo) {
	runDetachedIndexerUpdate("archive_create", func(ctx context.Context) error {
		return addToIndexer(ctx, targetPath, info)
	})
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

func notifyExtractedFiles(destination string) {
	runDetachedIndexerUpdate("archive_extract", func(ctx context.Context) error {
		walkRoot, err := fsroot.Open()
		if err != nil {
			return fmt.Errorf("open root for indexer walk: %w", err)
		}
		defer walkRoot.Close()

		if err := walkRoot.WalkDir(destination, func(rel string, entry fs.DirEntry, walkErr error) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if walkErr != nil {
				return nil
			}
			info, infoErr := entry.Info()
			if infoErr != nil {
				return nil
			}
			absPath := utils.CleanAbsPath(rel)
			if err := addToIndexer(ctx, absPath, info); err != nil {
				slog.Debug("failed to update indexer for extracted path", "path", absPath, "error", err)
			}
			return nil
		}); err != nil {
			return fmt.Errorf("walk extracted destination: %w", err)
		}
		return nil
	})
}

func runCompressTask(ctx context.Context, task *bridgetasks.Task, store *config.UserStore, req apischema.FileCompressRequest) (any, error) {
	if req.Format == "" || req.TargetPath == "" || len(req.Paths) == 0 {
		return nil, bridgetasks.NewError("missing format, destination, or paths", 400)
	}

	extension, err := archiveExtension(req.Format)
	if err != nil {
		return nil, bridgetasks.NewError(fmt.Sprintf("unsupported format: %s", req.Format), 400)
	}
	settings := taskSettingsForTask(ctx, task, store)
	release, err := heavyArchiveLimiter.acquire(ctx, settings.HeavyArchiveConcurrency)
	if err != nil {
		return nil, context.Canceled
	}
	defer release()

	targetPath := normalizeArchiveTargetPath(req.TargetPath, extension)
	root, err := fsroot.Open()
	if err != nil {
		return nil, bridgetasks.NewError("failed to access filesystem", 500)
	}
	defer root.Close()
	targetRel, tempRel, tempPath, err := prepareArchiveTarget(root, targetPath)
	if err != nil {
		status := 500
		message := fmt.Sprintf("cannot prepare archive target: %v", err)
		if strings.Contains(err.Error(), "destination is a directory") {
			status = 400
			message = "destination is a directory"
		}
		return nil, bridgetasks.NewError(message, status)
	}

	totalSize := computeArchiveSize(req.Paths)
	writeTaskPhaseProgress(task, totalSize, "preparing")
	opts := newTaskPhaseCallbacks(ctx, task, store, totalSize, "compressing")
	err = createArchive(req.Format, tempPath, opts, archiveCompressionWorkers(settings), req.Paths)
	if err == ipc.ErrAborted {
		slog.Info("compress aborted, cleaning up", "path", targetPath)
		removeWithDebug(root, tempRel, tempPath)
		return nil, abortErr(ctx)
	}
	if err != nil {
		removeWithDebug(root, tempRel, tempPath)
		return nil, bridgetasks.NewError(fmt.Sprintf("compression failed: %v", err), 500)
	}
	if err := root.Root.Rename(tempRel, targetRel); err != nil {
		removeWithDebug(root, tempRel, tempPath)
		return nil, bridgetasks.NewError(fmt.Sprintf("finalize archive: %v", err), 500)
	}

	var archiveSize int64
	if info, err := root.Root.Stat(targetRel); err == nil {
		archiveSize = info.Size()
		notifyCompressedArchive(targetPath, info)
	}

	slog.Info("compress complete", "path", targetPath, "count", len(req.Paths), "size", archiveSize, "format", req.Format)
	return map[string]any{
		"path":   targetPath,
		"size":   archiveSize,
		"format": req.Format,
	}, nil
}

func runExtractTask(ctx context.Context, task *bridgetasks.Task, store *config.UserStore, req apischema.FileExtractRequest) (any, error) {
	archivePath, destination, err := parseExtractRequest(req)
	if err != nil {
		return nil, bridgetasks.NewError("missing archive path", 400)
	}

	root, err := fsroot.Open()
	if err != nil {
		return nil, bridgetasks.NewError("failed to access filesystem", 500)
	}
	defer root.Close()

	_, statErr := root.Root.Stat(fsroot.ToRel(destination))
	destExistedBefore := statErr == nil

	archiveStat, err := root.Root.Stat(fsroot.ToRel(archivePath))
	if err != nil {
		return nil, bridgetasks.NewError(fmt.Sprintf("archive not found: %v", err), 404)
	}
	if archiveStat.IsDir() {
		return nil, bridgetasks.NewError("path is a directory, not an archive", 400)
	}

	settings := taskSettingsForTask(ctx, task, store)
	release, err := heavyArchiveLimiter.acquire(ctx, settings.HeavyArchiveConcurrency)
	if err != nil {
		return nil, context.Canceled
	}
	defer release()

	totalSize := computeExtractSize(archivePath, archiveStat.Size())
	writeTaskPhaseProgress(task, totalSize, "preparing")
	opts := newTaskPhaseCallbacks(ctx, task, store, totalSize, "extracting")
	err = services.ExtractArchive(archivePath, destination, opts, archiveExtractWorkers(settings))
	if err == ipc.ErrAborted {
		slog.Info("extract aborted, cleaning up", "path", destination)
		if !destExistedBefore {
			if removeErr := root.Root.RemoveAll(fsroot.ToRel(destination)); removeErr != nil {
				slog.Debug("failed to clean up extraction directory", "path", destination, "error", removeErr)
			}
		}
		return nil, abortErr(ctx)
	}
	if err != nil {
		return nil, bridgetasks.NewError(fmt.Sprintf("extraction failed: %v", err), 500)
	}

	notifyExtractedFiles(destination)
	slog.Info("extract complete", "archive", archivePath, "destination", destination)
	return map[string]any{
		"destination": destination,
	}, nil
}

func runIndexerTask(ctx context.Context, task *bridgetasks.Task, req apischema.OptionalPathRequest) (any, error) {
	path := "/"
	if req.Path != nil && *req.Path != "" {
		path = filepath.Clean(*req.Path)
	}
	return runIndexerOperation(ctx, task, path, false)
}

func runIndexerOperation(ctx context.Context, task *bridgetasks.Task, path string, attachOnly bool) (any, error) {
	var result any
	var taskErr *bridgetasks.Error
	cb := indexer.IndexerCallbacks{
		OnProgress: func(p indexer.IndexerProgress) error {
			task.ReportProgress(p)
			return nil
		},
		OnResult: func(r indexer.IndexerResult) error {
			result = r
			return nil
		},
		OnError: func(msg string, code int) error {
			taskErr = bridgetasks.NewError(msg, code)
			return nil
		},
	}

	var err error
	if attachOnly {
		err = indexer.StreamIndexerAttach(ctx, cb)
	} else {
		err = indexer.StreamIndexer(ctx, path, cb)
		if err != nil && taskErr != nil && taskErr.Code == 409 {
			taskErr = nil
			err = indexer.StreamIndexerAttach(ctx, cb)
		}
	}
	if err != nil {
		if ctx.Err() != nil || errors.Is(err, ipc.ErrAborted) {
			return nil, context.Canceled
		}
		if taskErr != nil {
			return nil, taskErr
		}
		return nil, err
	}

	if result == nil {
		return map[string]any{}, nil
	}
	return result, nil
}
