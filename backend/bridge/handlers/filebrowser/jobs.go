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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/config"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/services"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/indexer"
	bridgejobs "github.com/mordilloSan/LinuxIO/backend/bridge/jobs"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

const (
	JobTypeFileCompress = "file.compress"
	JobTypeFileExtract  = "file.extract"
	JobTypeFileCopy     = "file.copy"
	JobTypeFileMove     = "file.move"
	JobTypeFileIndexer  = "file.indexer"
	JobTypeFileUpload   = "file.upload"
	JobTypeFileDownload = "file.download"
	JobTypeFileArchive  = "file.archive"
	JobTypeFileChmod    = "file.chmod"
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

func newProgressLimiter(settings config.JobSettings, total int64) *progressLimiter {
	settings = config.EffectiveJobSettings(settings)
	minBytes := int64(settings.ProgressMinBytesMB) * 1024 * 1024
	if minBytes <= 0 {
		minBytes = progressReportIntervalBytes
	}
	minInterval := time.Duration(settings.ProgressMinIntervalMs) * time.Millisecond
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

func newCountProgressLimiter(settings config.JobSettings) *countProgressLimiter {
	settings = config.EffectiveJobSettings(settings)
	minInterval := time.Duration(settings.ProgressMinIntervalMs) * time.Millisecond
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

func jobSettingsForJob(job *bridgejobs.Job) config.JobSettings {
	if job == nil || strings.TrimSpace(job.Owner().Username) == "" {
		return config.DefaultJobSettings()
	}
	cfg, _, err := config.Load(job.Owner().Username)
	if err != nil || cfg == nil {
		return config.DefaultJobSettings()
	}
	return config.EffectiveJobSettings(cfg.Jobs)
}

func archiveCompressionWorkers(settings config.JobSettings) int {
	workers := settings.ArchiveCompressionWorkers
	if workers <= 0 {
		return runtime.GOMAXPROCS(0)
	}
	return workers
}

func archiveExtractWorkers(settings config.JobSettings) int {
	workers := settings.ArchiveExtractWorkers
	if workers <= 0 {
		return runtime.GOMAXPROCS(0)
	}
	return workers
}

type transferRequest struct {
	source      string
	destination string
	overwrite   bool
}

type ChmodProgress struct {
	Processed int64  `json:"processed"`
	Total     int64  `json:"total"`
	Pct       int    `json:"pct"`
	Phase     string `json:"phase,omitempty"`
}

func RegisterJobRunners() {
	bridgejobs.RegisterRunner(JobTypeFileCompress, runCompressJob)
	bridgejobs.RegisterRunner(JobTypeFileExtract, runExtractJob)
	bridgejobs.RegisterRunner(JobTypeFileCopy, runCopyJob)
	bridgejobs.RegisterRunner(JobTypeFileMove, runMoveJob)
	bridgejobs.RegisterRunner(JobTypeFileIndexer, runIndexerJob)
	bridgejobs.RegisterRunner(JobTypeFileUpload, runUploadJob)
	bridgejobs.RegisterRunner(JobTypeFileDownload, runDownloadJob)
	bridgejobs.RegisterRunner(JobTypeFileArchive, runArchiveJob)
	bridgejobs.RegisterRunner(JobTypeFileChmod, runChmodJob)
	bridgejobs.RegisterRecoverer(JobTypeFileIndexer, recoverIndexerJob)
	bridgejobs.RegisterDataAttacher(JobTypeFileUpload, attachFileTransferData)
	bridgejobs.RegisterDataAttacher(JobTypeFileDownload, attachFileTransferData)
	bridgejobs.RegisterDataAttacher(JobTypeFileArchive, attachFileTransferData)
}

func newJobPhaseCallbacks(ctx context.Context, job *bridgejobs.Job, totalSize int64, phase string) *ipc.OperationCallbacks {
	limiter := newProgressLimiter(jobSettingsForJob(job), totalSize)
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

func cleanupArchiveTarget(root *fsroot.FSRoot, targetRel, targetPath string) {
	removeWithDebug(root, targetRel, targetPath)
}

func removeWithDebug(root *fsroot.FSRoot, targetRel, targetPath string) {
	if err := root.Root.Remove(targetRel); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Debug("failed to remove failed archive", "path", targetPath, "error", err)
	}
}

func notifyCompressedArchive(targetPath string, info os.FileInfo) {
	go func(stat os.FileInfo) {
		if err := addToIndexer(targetPath, stat); err != nil {
			slog.Debug("failed to update indexer after archive creation", "path", targetPath, "error", err)
		}
	}(info)
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

func parseExtractArgs(args []string) (string, string, error) {
	if len(args) < 1 {
		return "", "", fmt.Errorf("missing archive path")
	}

	archivePath := filepath.Clean(args[0])
	destination := defaultExtractDestination(archivePath)
	if len(args) > 1 && args[1] != "" {
		destination = filepath.Clean(args[1])
	}
	return archivePath, destination, nil
}

func notifyExtractedFiles(destination string) {
	go func(destPath string) {
		walkRoot, err := fsroot.Open()
		if err != nil {
			slog.Debug("failed to open root for indexer walk", "path", destPath, "error", err)
			return
		}
		defer walkRoot.Close()

		if err := walkRoot.WalkDir(destPath, func(rel string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			info, infoErr := entry.Info()
			if infoErr != nil {
				return nil
			}
			absPath := filepath.Clean("/" + strings.TrimPrefix(rel, "/"))
			if err := addToIndexer(absPath, info); err != nil {
				slog.Debug("failed to update indexer for extracted path", "path", absPath, "error", err)
			}
			return nil
		}); err != nil {
			slog.Debug("failed to walk extracted destination", "path", destPath, "error", err)
		}
	}(destination)
}

func parseTransferRequest(args []string) (transferRequest, error) {
	if len(args) < 2 {
		return transferRequest{}, fmt.Errorf("missing source or destination")
	}

	return transferRequest{
		source:      filepath.Clean(args[0]),
		destination: filepath.Clean(args[1]),
		overwrite:   len(args) > 2 && args[2] == "true",
	}, nil
}

func parseChmodArgs(args []string) (path, modeStr, owner, group string, recursive bool, err error) {
	if len(args) < 2 {
		err = fmt.Errorf("missing path or mode")
		return
	}
	path = args[0]
	modeStr = args[1]
	switch len(args) {
	case 2:
	case 3:
		if args[2] == "true" || args[2] == "false" {
			recursive = args[2] == "true"
		} else {
			owner = args[2]
		}
	case 4:
		owner = args[2]
		group = args[3]
	default:
		owner = args[2]
		group = args[3]
		recursive = args[4] == "true"
	}
	return
}

func newChmodProgressReporter(job *bridgejobs.Job, settings config.JobSettings, phase string) func(processed, total int64) {
	limiter := newCountProgressLimiter(settings)
	return func(processed, total int64) {
		processed, pct, ok := limiter.Set(processed, total)
		if !ok {
			return
		}
		job.ReportProgress(ChmodProgress{
			Processed: processed,
			Total:     total,
			Pct:       pct,
			Phase:     phase,
		})
	}
}

func prepareTransfer(root *fsroot.FSRoot, req transferRequest) (transferRequest, error) {
	sourceInfo, err := root.Root.Stat(fsroot.ToRel(req.source))
	if err != nil {
		return req, fmt.Errorf("source not found: %w", err)
	}

	destInfo, destErr := root.Root.Stat(fsroot.ToRel(req.destination))
	if destErr == nil && destInfo.IsDir() {
		req.destination = filepath.Join(req.destination, filepath.Base(req.source))
		destInfo, destErr = root.Root.Stat(fsroot.ToRel(req.destination))
	}

	if destErr == nil {
		if !req.overwrite {
			return req, fmt.Errorf("destination exists")
		}
		if sourceInfo.IsDir() != destInfo.IsDir() {
			return req, fmt.Errorf("type mismatch")
		}
	}

	return req, nil
}

func runChmodJob(ctx context.Context, job *bridgejobs.Job, args []string) (any, error) {
	path, modeStr, owner, group, recursive, err := parseChmodArgs(args)
	if err != nil {
		return nil, bridgejobs.NewError(err.Error(), 400)
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, context.Canceled
	}

	mode, err := strconv.ParseInt(modeStr, 8, 32)
	if err != nil {
		return nil, bridgejobs.NewError(fmt.Sprintf("invalid mode: %v", err), 400)
	}

	realPath := filepath.Clean(path)
	settings := jobSettingsForJob(job)
	job.ReportProgress(ChmodProgress{Phase: "preparing"})

	if err := services.ChangePermissionsCtx(ctx, realPath, os.FileMode(mode), recursive, newChmodProgressReporter(job, settings, "chmod")); err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, context.Canceled
		}
		slog.Debug("error changing permissions", "path", realPath, "error", err)
		return nil, bridgejobs.NewError(err.Error(), 400)
	}

	if strings.TrimSpace(owner) != "" || strings.TrimSpace(group) != "" {
		if err := ctx.Err(); err != nil {
			return nil, context.Canceled
		}
		uid, err := resolveUserID(owner)
		if err != nil {
			slog.Debug("error resolving owner", "owner", owner, "error", err)
			return nil, bridgejobs.NewError(err.Error(), 400)
		}
		gid, err := resolveGroupID(group)
		if err != nil {
			slog.Debug("error resolving group", "group", group, "error", err)
			return nil, bridgejobs.NewError(err.Error(), 400)
		}
		if err := services.ChangeOwnershipCtx(ctx, realPath, uid, gid, recursive, newChmodProgressReporter(job, settings, "chown")); err != nil {
			if errors.Is(err, context.Canceled) {
				return nil, context.Canceled
			}
			slog.Debug("error changing ownership", "path", realPath, "owner", owner, "group", group, "error", err)
			return nil, bridgejobs.NewError(err.Error(), 400)
		}
	}

	return map[string]any{
		"message": "permissions changed",
		"path":    path,
		"mode":    fmt.Sprintf("%04o", mode),
		"owner":   owner,
		"group":   group,
	}, nil
}

func runCompressJob(ctx context.Context, job *bridgejobs.Job, args []string) (any, error) {
	if len(args) < 3 {
		return nil, bridgejobs.NewError("missing format, destination, or paths", 400)
	}

	format := args[0]
	destination := args[1]
	paths := args[2:]
	extension, err := archiveExtension(format)
	if err != nil {
		return nil, bridgejobs.NewError(fmt.Sprintf("unsupported format: %s", format), 400)
	}
	settings := jobSettingsForJob(job)
	release, err := heavyArchiveLimiter.acquire(ctx, settings.HeavyArchiveConcurrency)
	if err != nil {
		return nil, context.Canceled
	}
	defer release()

	targetPath := normalizeArchiveTargetPath(destination, extension)
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

	totalSize := computeArchiveSize(paths)
	writeJobPhaseProgress(job, totalSize, "preparing")
	opts := newJobPhaseCallbacks(ctx, job, totalSize, "compressing")
	err = createArchive(format, tempPath, opts, archiveCompressionWorkers(settings), paths)
	if err == ipc.ErrAborted {
		slog.Info("compress aborted, cleaning up", "path", targetPath)
		cleanupArchiveTarget(root, tempRel, tempPath)
		return nil, abortErr(ctx)
	}
	if err != nil {
		cleanupArchiveTarget(root, tempRel, tempPath)
		return nil, bridgejobs.NewError(fmt.Sprintf("compression failed: %v", err), 500)
	}
	if err := root.Root.Rename(tempRel, targetRel); err != nil {
		cleanupArchiveTarget(root, tempRel, tempPath)
		return nil, bridgejobs.NewError(fmt.Sprintf("finalize archive: %v", err), 500)
	}

	var archiveSize int64
	if info, err := root.Root.Stat(targetRel); err == nil {
		archiveSize = info.Size()
		notifyCompressedArchive(targetPath, info)
	}

	slog.Info("compress complete", "path", targetPath, "count", len(paths), "size", archiveSize, "format", format)
	return map[string]any{
		"path":   targetPath,
		"size":   archiveSize,
		"format": format,
	}, nil
}

func runExtractJob(ctx context.Context, job *bridgejobs.Job, args []string) (any, error) {
	archivePath, destination, err := parseExtractArgs(args)
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

	settings := jobSettingsForJob(job)
	release, err := heavyArchiveLimiter.acquire(ctx, settings.HeavyArchiveConcurrency)
	if err != nil {
		return nil, context.Canceled
	}
	defer release()

	totalSize := computeExtractSize(archivePath, archiveStat.Size())
	writeJobPhaseProgress(job, totalSize, "preparing")
	opts := newJobPhaseCallbacks(ctx, job, totalSize, "extracting")
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

func runCopyJob(ctx context.Context, job *bridgejobs.Job, args []string) (any, error) {
	if len(args) < 2 {
		return nil, bridgejobs.NewError("missing source or destination", 400)
	}

	source := filepath.Clean(args[0])
	destination := filepath.Clean(args[1])
	root, err := fsroot.Open()
	if err != nil {
		return nil, bridgejobs.NewError("failed to access filesystem", 500)
	}
	defer root.Close()
	overwrite := len(args) > 2 && args[2] == "true"

	sourceInfo, err := root.Root.Stat(fsroot.ToRel(source))
	if err != nil {
		return nil, bridgejobs.NewError(fmt.Sprintf("source not found: %v", err), 404)
	}

	destInfo, destErr := root.Root.Stat(fsroot.ToRel(destination))
	if destErr == nil && destInfo.IsDir() {
		destination = filepath.Join(destination, filepath.Base(source))
		destInfo, destErr = root.Root.Stat(fsroot.ToRel(destination))
	}

	if destErr == nil {
		if !overwrite {
			return nil, bridgejobs.NewError("destination already exists", 409)
		}
		if sourceInfo.IsDir() != destInfo.IsDir() {
			return nil, bridgejobs.NewError("source and destination types don't match", 400)
		}
	}

	totalSize, err := services.ComputeCopySize(source)
	if err != nil {
		slog.Debug("failed to compute copy size", "source", source, "error", err)
		totalSize = 0
	}
	writeJobPhaseProgress(job, totalSize, "preparing")

	opts := newJobPhaseCallbacks(ctx, job, totalSize, "copying")
	err = services.CopyFileWithCallbacks(source, destination, overwrite, opts)
	if err == ipc.ErrAborted {
		slog.Info("copy aborted", "source", source, "destination", destination)
		return nil, abortErr(ctx)
	}
	if err != nil {
		return nil, bridgejobs.NewError(fmt.Sprintf("copy failed: %v", err), 500)
	}

	if info, err := root.Root.Stat(fsroot.ToRel(destination)); err == nil {
		go func(stat os.FileInfo) {
			if err := addToIndexer(destination, stat); err != nil {
				slog.Debug("failed to update indexer after copy", "path", destination, "error", err)
			}
		}(info)
	}

	slog.Info("copy complete", "source", source, "destination", destination, "size", totalSize)
	return map[string]any{
		"source":      source,
		"destination": destination,
		"size":        totalSize,
	}, nil
}

func runMoveJob(ctx context.Context, job *bridgejobs.Job, args []string) (any, error) {
	req, err := parseTransferRequest(args)
	if err != nil {
		return nil, bridgejobs.NewError("missing source or destination", 400)
	}

	root, err := fsroot.Open()
	if err != nil {
		return nil, bridgejobs.NewError("failed to access filesystem", 500)
	}
	defer root.Close()

	req, err = prepareTransfer(root, req)
	if err != nil {
		code := 409
		message := err.Error()
		switch {
		case strings.Contains(message, "source not found"):
			code = 404
		case strings.Contains(message, "type mismatch"):
			code = 400
			message = "source and destination types don't match"
		case strings.Contains(message, "destination exists"):
			message = "destination already exists"
		}
		return nil, bridgejobs.NewError(message, code)
	}

	totalSize, err := services.ComputeCopySize(req.source)
	if err != nil {
		slog.Debug("failed to compute move size", "source", req.source, "error", err)
		totalSize = 0
	}
	writeJobPhaseProgress(job, totalSize, "preparing")

	opts := newJobPhaseCallbacks(ctx, job, totalSize, "moving")
	err = services.MoveFileWithCallbacks(req.source, req.destination, req.overwrite, opts)
	if err == ipc.ErrAborted {
		slog.Info("move aborted", "source", req.source, "destination", req.destination)
		return nil, abortErr(ctx)
	}
	if err != nil {
		return nil, bridgejobs.NewError(fmt.Sprintf("move failed: %v", err), 500)
	}

	destInfoAfterMove, statErr := root.Root.Stat(fsroot.ToRel(req.destination))
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		slog.Debug("failed to stat move destination", "destination", req.destination, "error", statErr)
	}
	go func(info os.FileInfo) {
		if err := deleteFromIndexer(req.source); err != nil {
			slog.Debug("failed to delete from indexer after move", "source", req.source, "error", err)
		}
		if info != nil {
			if err := addToIndexer(req.destination, info); err != nil {
				slog.Debug("failed to update indexer after move", "destination", req.destination, "error", err)
			}
		}
	}(destInfoAfterMove)

	slog.Info("move complete", "source", req.source, "destination", req.destination, "size", totalSize)
	return map[string]any{
		"source":      req.source,
		"destination": req.destination,
		"size":        totalSize,
	}, nil
}

func runIndexerJob(ctx context.Context, job *bridgejobs.Job, args []string) (any, error) {
	path := "/"
	if len(args) > 0 && args[0] != "" {
		path = filepath.Clean(args[0])
	}
	return runIndexerOperation(ctx, job, path, false)
}

func recoverIndexerJob(registry *bridgejobs.Registry, owner bridgejobs.Owner) (*bridgejobs.Job, error) {
	status, err := fetchIndexerStatusFromIndexer()
	if err != nil {
		if errors.Is(err, errIndexerUnavailable) {
			return nil, bridgejobs.NewError("no active indexer job", 404)
		}
		return nil, err
	}
	if !status.Running {
		return nil, bridgejobs.NewError("no active indexer job", 404)
	}
	return registry.StartWithRunnerForOwner(JobTypeFileIndexer, nil, owner, func(ctx context.Context, job *bridgejobs.Job, args []string) (any, error) {
		return runIndexerOperation(ctx, job, "", true)
	})
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
