package filebrowser

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

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
)

type transferRequest struct {
	source      string
	destination string
	overwrite   bool
}

func RegisterJobRunners() {
	bridgejobs.RegisterRunner(JobTypeFileCompress, runCompressJob)
	bridgejobs.RegisterRunner(JobTypeFileExtract, runExtractJob)
	bridgejobs.RegisterRunner(JobTypeFileCopy, runCopyJob)
	bridgejobs.RegisterRunner(JobTypeFileMove, runMoveJob)
	bridgejobs.RegisterRunner(JobTypeFileIndexer, runIndexerJob)
	bridgejobs.RegisterRecoverer(JobTypeFileIndexer, recoverIndexerJob)
}

func newJobPhaseCallbacks(ctx context.Context, job *bridgejobs.Job, totalSize int64, phase string) *ipc.OperationCallbacks {
	var processed int64
	var last int64
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
			processed += n
			if processed-last < progressReportIntervalBytes && processed < totalSize {
				return
			}
			last = processed
			job.ReportProgress(FileProgress{
				Bytes: processed,
				Total: totalSize,
				Pct:   min(int(processed*100/totalSize), 100),
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

func prepareArchiveTarget(root *fsroot.FSRoot, targetPath string) (string, error) {
	targetRel := fsroot.ToRel(targetPath)
	if info, err := root.Root.Stat(targetRel); err == nil {
		if info.IsDir() {
			return "", fmt.Errorf("destination is a directory")
		}
		if rmErr := root.Root.Remove(targetRel); rmErr != nil {
			return "", fmt.Errorf("remove existing file: %w", rmErr)
		}
	}

	if err := root.Root.MkdirAll(fsroot.ToRel(filepath.Dir(targetPath)), services.PermDir); err != nil {
		return "", fmt.Errorf("create parent dir: %w", err)
	}
	return targetRel, nil
}

func cleanupArchiveTarget(root *fsroot.FSRoot, targetRel, targetPath string) {
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

func computeExtractSize(archivePath string) int64 {
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

	targetPath := normalizeArchiveTargetPath(destination, extension)
	root, err := fsroot.Open()
	if err != nil {
		return nil, bridgejobs.NewError("failed to access filesystem", 500)
	}
	defer root.Close()
	targetRel, err := prepareArchiveTarget(root, targetPath)
	if err != nil {
		status := 500
		message := fmt.Sprintf("cannot create parent directory: %v", err)
		if strings.Contains(err.Error(), "destination is a directory") {
			status = 400
			message = "destination is a directory"
		} else if strings.Contains(err.Error(), "remove existing file") {
			message = fmt.Sprintf("cannot remove existing file: %v", err)
		}
		return nil, bridgejobs.NewError(message, status)
	}

	totalSize := computeArchiveSize(paths)
	writeJobPhaseProgress(job, totalSize, "preparing")
	opts := newJobPhaseCallbacks(ctx, job, totalSize, "compressing")
	err = createArchive(format, targetPath, opts, paths)
	if err == ipc.ErrAborted {
		slog.Info("compress aborted, cleaning up", "path", targetPath)
		cleanupArchiveTarget(root, targetRel, targetPath)
		return nil, abortErr(ctx)
	}
	if err != nil {
		cleanupArchiveTarget(root, targetRel, targetPath)
		return nil, bridgejobs.NewError(fmt.Sprintf("compression failed: %v", err), 500)
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

	totalSize := computeExtractSize(archivePath)
	writeJobPhaseProgress(job, totalSize, "preparing")
	opts := newJobPhaseCallbacks(ctx, job, totalSize, "extracting")
	err = services.ExtractArchive(archivePath, destination, opts)
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

func recoverIndexerJob(registry *bridgejobs.Registry) (*bridgejobs.Job, error) {
	status, err := fetchIndexerStatusFromIndexer()
	if err != nil {
		return nil, err
	}
	if !status.Running {
		return nil, bridgejobs.NewError("no active indexer job", 404)
	}
	return registry.StartWithRunner(JobTypeFileIndexer, nil, func(ctx context.Context, job *bridgejobs.Job, args []string) (any, error) {
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
