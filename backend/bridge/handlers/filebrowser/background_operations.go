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
	bridgejobs "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
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

func newProgressLimiter(jobSettings config.PersistedJobSettings, total int64) *progressLimiter {
	jobSettings = config.EffectiveJobSettings(jobSettings)
	minBytes := int64(jobSettings.ProgressMinBytesMB) * 1024 * 1024
	if minBytes <= 0 {
		minBytes = progressReportIntervalBytes
	}
	minInterval := time.Duration(jobSettings.ProgressMinIntervalMs) * time.Millisecond
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

func newCountProgressLimiter(jobSettings config.PersistedJobSettings) *countProgressLimiter {
	jobSettings = config.EffectiveJobSettings(jobSettings)
	minInterval := time.Duration(jobSettings.ProgressMinIntervalMs) * time.Millisecond
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

func jobSettingsForJob(ctx context.Context, job *bridgejobs.Job, store *config.UserStore) config.PersistedJobSettings {
	if job == nil || strings.TrimSpace(job.Owner().Username) == "" {
		return config.DefaultJobSettings()
	}
	cfg, _, err := config.SnapshotForUser(ctx, job.Owner().Username, store)
	if err != nil || cfg == nil {
		return config.DefaultJobSettings()
	}
	return config.EffectiveJobSettings(cfg.Jobs)
}

func archiveCompressionWorkers(jobSettings config.PersistedJobSettings) int {
	workers := jobSettings.ArchiveCompressionWorkers
	if workers <= 0 {
		return runtime.GOMAXPROCS(0)
	}
	return workers
}

func archiveExtractWorkers(jobSettings config.PersistedJobSettings) int {
	workers := jobSettings.ArchiveExtractWorkers
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

var fileJobRoutes = fileJobBindings(nil).Routes()

func fileJobBindings(store *config.UserStore) apischema.BindingSet {
	return apischema.Bindings(
		apischema.Runner[apischema.FileCompressRequest, apischema.JobSnapshot]("filebrowser.compress").Run(
			func(ctx context.Context, job *bridgejobs.Job, req apischema.FileCompressRequest) (any, error) {
				return runCompressJob(ctx, job, store, req)
			},
			bridgejobs.ActionDefault,
		),
		apischema.Runner[apischema.FileExtractRequest, apischema.JobSnapshot]("filebrowser.extract").Run(
			func(ctx context.Context, job *bridgejobs.Job, req apischema.FileExtractRequest) (any, error) {
				return runExtractJob(ctx, job, store, req)
			},
			bridgejobs.ActionDefault,
		),
		apischema.Runner[apischema.BatchTransferRequest, apischema.JobSnapshot]("filebrowser.copy_batch").Run(
			func(ctx context.Context, job *bridgejobs.Job, req apischema.BatchTransferRequest) (any, error) {
				return runCopyBatchJob(ctx, job, store, req)
			},
			bridgejobs.ActionDefault,
		),
		apischema.Runner[apischema.BatchTransferRequest, apischema.JobSnapshot]("filebrowser.move_batch").Run(
			func(ctx context.Context, job *bridgejobs.Job, req apischema.BatchTransferRequest) (any, error) {
				return runMoveBatchJob(ctx, job, store, req)
			},
			bridgejobs.ActionDefault,
		),
		apischema.Runner[apischema.BatchPathRequest, apischema.JobSnapshot]("filebrowser.delete_batch").Run(
			runDeleteBatchJob,
			bridgejobs.ActionDefault,
		),
		apischema.Runner[apischema.OptionalPathRequest, apischema.JobSnapshot]("filebrowser.index").Run(runIndexerJob, bridgejobs.SingletonSystem),
		apischema.Runner[apischema.FileUploadRequest, apischema.JobSnapshot](routeUpload).Run(runUploadJob, bridgejobs.StreamDefault),
		apischema.Runner[apischema.FileUploadBatchRequest, apischema.JobSnapshot](routeUploadBatch).Run(runUploadBatchJob, bridgejobs.StreamDefault),
		apischema.Runner[apischema.PathRequest, apischema.JobSnapshot](routeDownload).Run(runDownloadJob, bridgejobs.StreamDefault),
		apischema.Runner[apischema.FileArchiveRequest, apischema.JobSnapshot](routeArchive).Run(
			func(ctx context.Context, job *bridgejobs.Job, req apischema.FileArchiveRequest) (any, error) {
				return runArchiveJob(ctx, job, store, req)
			},
			bridgejobs.StreamDefault,
		),
		apischema.Runner[apischema.FileChmodBatchRequest, apischema.JobSnapshot]("filebrowser.chmod_batch").Run(
			func(ctx context.Context, job *bridgejobs.Job, req apischema.FileChmodBatchRequest) (any, error) {
				return runChmodBatchJob(ctx, job, store, req)
			},
			bridgejobs.ActionDefault,
		),
	)
}

func RegisterJobRoutes(router *bridgejobs.Router, store *config.UserStore) {
	fileJobBindings(store).Register(router)
	bridgejobs.RegisterDataAttacher(routeUpload, attachFileTransferData)
	bridgejobs.RegisterDataAttacher(routeUploadBatch, attachFileTransferData)
	bridgejobs.RegisterDataAttacher(routeDownload, attachFileTransferData)
	bridgejobs.RegisterDataAttacher(routeArchive, attachFileTransferData)
}

func newJobPhaseCallbacks(ctx context.Context, job *bridgejobs.Job, store *config.UserStore, totalSize int64, phase string) *ipc.OperationCallbacks {
	limiter := newProgressLimiter(jobSettingsForJob(ctx, job, store), totalSize)
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
			job.ReportProgress(FileProgress{
				Bytes: processed,
				Total: totalSize,
				Pct:   pct,
				Phase: phase,
			})
		},
	}
}

func writeJobPhaseProgress(job *bridgejobs.Job, total int64, phase string) {
	job.ReportProgress(FileProgress{
		Total: total,
		Phase: phase,
	})
}

func abortErr(ctx context.Context) error {
	if ctx.Err() != nil {
		return context.Canceled
	}
	return bridgejobs.NewError("operation aborted", 499)
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

func runCompressJob(ctx context.Context, job *bridgejobs.Job, store *config.UserStore, req apischema.FileCompressRequest) (any, error) {
	if req.Format == "" || req.TargetPath == "" || len(req.Paths) == 0 {
		return nil, bridgejobs.NewError("missing format, destination, or paths", 400)
	}

	extension, err := archiveExtension(req.Format)
	if err != nil {
		return nil, bridgejobs.NewError(fmt.Sprintf("unsupported format: %s", req.Format), 400)
	}
	settings := jobSettingsForJob(ctx, job, store)
	release, err := heavyArchiveLimiter.acquire(ctx, settings.HeavyArchiveConcurrency)
	if err != nil {
		return nil, context.Canceled
	}
	defer release()

	targetPath := normalizeArchiveTargetPath(req.TargetPath, extension)
	root, err := fsroot.Open()
	if err != nil {
		return nil, bridgejobs.NewError("failed to access filesystem", 500)
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
		return nil, bridgejobs.NewError(message, status)
	}

	totalSize := computeArchiveSize(req.Paths)
	writeJobPhaseProgress(job, totalSize, "preparing")
	opts := newJobPhaseCallbacks(ctx, job, store, totalSize, "compressing")
	err = createArchive(req.Format, tempPath, opts, archiveCompressionWorkers(settings), req.Paths)
	if err == ipc.ErrAborted {
		slog.Info("compress aborted, cleaning up", "path", targetPath)
		removeWithDebug(root, tempRel, tempPath)
		return nil, abortErr(ctx)
	}
	if err != nil {
		removeWithDebug(root, tempRel, tempPath)
		return nil, bridgejobs.NewError(fmt.Sprintf("compression failed: %v", err), 500)
	}
	if err := root.Root.Rename(tempRel, targetRel); err != nil {
		removeWithDebug(root, tempRel, tempPath)
		return nil, bridgejobs.NewError(fmt.Sprintf("finalize archive: %v", err), 500)
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

func runExtractJob(ctx context.Context, job *bridgejobs.Job, store *config.UserStore, req apischema.FileExtractRequest) (any, error) {
	archivePath, destination, err := parseExtractRequest(req)
	if err != nil {
		return nil, bridgejobs.NewError("missing archive path", 400)
	}

	root, err := fsroot.Open()
	if err != nil {
		return nil, bridgejobs.NewError("failed to access filesystem", 500)
	}
	defer root.Close()

	_, statErr := root.Root.Stat(fsroot.ToRel(destination))
	destExistedBefore := statErr == nil

	archiveStat, err := root.Root.Stat(fsroot.ToRel(archivePath))
	if err != nil {
		return nil, bridgejobs.NewError(fmt.Sprintf("archive not found: %v", err), 404)
	}
	if archiveStat.IsDir() {
		return nil, bridgejobs.NewError("path is a directory, not an archive", 400)
	}

	settings := jobSettingsForJob(ctx, job, store)
	release, err := heavyArchiveLimiter.acquire(ctx, settings.HeavyArchiveConcurrency)
	if err != nil {
		return nil, context.Canceled
	}
	defer release()

	totalSize := computeExtractSize(archivePath, archiveStat.Size())
	writeJobPhaseProgress(job, totalSize, "preparing")
	opts := newJobPhaseCallbacks(ctx, job, store, totalSize, "extracting")
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
		return nil, bridgejobs.NewError(fmt.Sprintf("extraction failed: %v", err), 500)
	}

	notifyExtractedFiles(destination)
	slog.Info("extract complete", "archive", archivePath, "destination", destination)
	return map[string]any{
		"destination": destination,
	}, nil
}

func runIndexerJob(ctx context.Context, job *bridgejobs.Job, req apischema.OptionalPathRequest) (any, error) {
	path := "/"
	if req.Path != nil && *req.Path != "" {
		path = filepath.Clean(*req.Path)
	}
	return runIndexerOperation(ctx, job, path, false)
}

func runIndexerOperation(ctx context.Context, job *bridgejobs.Job, path string, attachOnly bool) (any, error) {
	var result any
	var jobErr *bridgejobs.Error
	cb := indexer.IndexerCallbacks{
		OnProgress: func(p indexer.IndexerProgress) error {
			job.ReportProgress(p)
			return nil
		},
		OnResult: func(r indexer.IndexerResult) error {
			result = r
			return nil
		},
		OnError: func(msg string, code int) error {
			jobErr = bridgejobs.NewError(msg, code)
			return nil
		},
	}

	var err error
	if attachOnly {
		err = indexer.StreamIndexerAttach(ctx, cb)
	} else {
		err = indexer.StreamIndexer(ctx, path, cb)
		if err != nil && jobErr != nil && jobErr.Code == 409 {
			jobErr = nil
			err = indexer.StreamIndexerAttach(ctx, cb)
		}
	}
	if err != nil {
		if ctx.Err() != nil || errors.Is(err, ipc.ErrAborted) {
			return nil, context.Canceled
		}
		if jobErr != nil {
			return nil, jobErr
		}
		return nil, err
	}

	if result == nil {
		return map[string]any{}, nil
	}
	return result, nil
}
