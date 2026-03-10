package filebrowser

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/mordilloSan/go-logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/services"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

type filebrowserRegistration struct {
	command string
	handler ipc.HandlerFunc
}

type uploadContext struct {
	path         string
	realPath     string
	tempPath     string
	realRel      string
	tempRel      string
	expectedSize int64
	override     bool
}

type uploadPreserveState struct {
	mode os.FileMode
	uid  int
	gid  int
	ok   bool
}

// RegisterHandlers registers all filebrowser handlers with the global registry
func RegisterHandlers(sess *session.Session) {
	registerFilebrowserHandlers([]filebrowserRegistration{
		{command: "resource_get", handler: emitFilebrowserArgsResult(resourceGet)},
		{command: "resource_stat", handler: emitFilebrowserArgsResult(resourceStat)},
		{command: "resource_delete", handler: emitFilebrowserLoggedArgsResult("resource_delete requested", resourceDelete)},
		{command: "resource_post", handler: emitFilebrowserLoggedArgsResult("resource_post requested", resourcePost)},
		{command: "resource_patch", handler: handleResourcePatch},
		{command: "dir_size", handler: emitFilebrowserArgsResult(dirSize)},
		{command: "indexer_status", handler: emitFilebrowserArgsResult(indexerStatus)},
		{command: "subfolders", handler: emitFilebrowserArgsResult(subfolders)},
		{command: "search", handler: emitFilebrowserArgsResult(searchFiles)},
		{command: "chmod", handler: emitFilebrowserLoggedArgsResult("chmod requested", resourceChmod)},
		{command: "users_groups", handler: handleUsersGroups},
		{command: "file_update_from_temp", handler: emitFilebrowserLoggedArgsResult("file_update_from_temp requested", fileUpdateFromTemp)},
		{command: "download", handler: downloadHandler(sess)},
		{command: "archive", handler: handleArchiveDownloadNotMigrated},
		{command: "compress", handler: handleCompressNotMigrated},
		{command: "extract", handler: handleExtractNotMigrated},
	})

	// Upload - bidirectional handler (receives data from client)
	ipc.Register("filebrowser", "upload", &uploadHandler{})
}

func registerFilebrowserHandlers(registrations []filebrowserRegistration) {
	for _, registration := range registrations {
		ipc.RegisterFunc("filebrowser", registration.command, registration.handler)
	}
}

func emitFilebrowserArgsResult(fn func([]string) (any, error)) ipc.HandlerFunc {
	return func(ctx context.Context, args []string, emit ipc.Events) error {
		result, err := fn(args)
		if err != nil {
			return err
		}
		return emit.Result(result)
	}
}

func emitFilebrowserLoggedArgsResult(message string, fn func([]string) (any, error)) ipc.HandlerFunc {
	return func(ctx context.Context, args []string, emit ipc.Events) error {
		logger.Infof("%s", message)
		result, err := fn(args)
		if err != nil {
			return err
		}
		return emit.Result(result)
	}
}

func handleResourcePatch(ctx context.Context, args []string, emit ipc.Events) error {
	logger.Infof("resource_patch requested")
	result, err := resourcePatchWithProgress(ctx, args, emit)
	if err != nil {
		return err
	}
	return emit.Result(result)
}

func handleUsersGroups(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := usersGroups()
	if err != nil {
		return err
	}
	return emit.Result(result)
}

func downloadHandler(sess *session.Session) ipc.HandlerFunc {
	return func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 1 {
			return ipc.ErrInvalidArgs
		}
		return streamFileDownload(ctx, args[0], emit, chunkSizeFromSess(sess))
	}
}

func streamFileDownload(ctx context.Context, path string, emit ipc.Events, chunkSize int) error {
	realPath := filepath.Clean(path)
	root, file, totalSize, err := openDownloadTarget(realPath)
	if err != nil {
		return err
	}
	defer root.Close()
	defer file.Close()

	if progressErr := emit.Progress(FileProgress{Total: totalSize, Phase: "starting"}); progressErr != nil {
		return fmt.Errorf("write progress: %w", progressErr)
	}

	bytesRead, err := relayDownloadChunks(ctx, file, emit, chunkSize, totalSize)
	if err != nil {
		return err
	}

	logger.Infof("Download complete: path=%s size=%d", path, bytesRead)
	return emit.Result(map[string]any{
		"path":     path,
		"size":     totalSize,
		"fileName": filepath.Base(realPath),
	})
}

func openDownloadTarget(realPath string) (*fsroot.FSRoot, io.ReadCloser, int64, error) {
	root, err := fsroot.Open()
	if err != nil {
		return nil, nil, 0, fmt.Errorf("failed to access filesystem: %w", err)
	}
	realRel := fsroot.ToRel(realPath)

	stat, err := root.Root.Stat(realRel)
	if err != nil {
		root.Close()
		return nil, nil, 0, fmt.Errorf("file not found: %w", err)
	}
	if stat.IsDir() {
		root.Close()
		return nil, nil, 0, fmt.Errorf("path is a directory, use archive instead")
	}

	file, err := root.Root.Open(realRel)
	if err != nil {
		root.Close()
		return nil, nil, 0, fmt.Errorf("cannot open file: %w", err)
	}
	return root, file, stat.Size(), nil
}

func relayDownloadChunks(
	ctx context.Context,
	file io.Reader,
	emit ipc.Events,
	chunkSize int,
	totalSize int64,
) (int64, error) {
	buf := make([]byte, chunkSize)
	var bytesRead int64
	var lastProgress int64

	for {
		n, readErr := file.Read(buf)
		if n > 0 {
			if err := emit.Data(buf[:n]); err != nil {
				return bytesRead, fmt.Errorf("write data chunk: %w", err)
			}
			bytesRead += int64(n)
			if shouldReportDownloadProgress(bytesRead, lastProgress, totalSize) {
				if err := emit.Progress(FileProgress{
					Bytes: bytesRead,
					Total: totalSize,
					Pct:   percentComplete(bytesRead, totalSize),
				}); err != nil {
					return bytesRead, fmt.Errorf("write progress: %w", err)
				}
				lastProgress = bytesRead
			}
		}

		if readErr == io.EOF {
			return bytesRead, nil
		}
		if readErr != nil {
			return bytesRead, fmt.Errorf("read file: %w", readErr)
		}
		if err := checkFilebrowserContext(ctx); err != nil {
			return bytesRead, err
		}
	}
}

func shouldReportDownloadProgress(bytesRead, lastProgress, totalSize int64) bool {
	return bytesRead-lastProgress >= progressIntervalDownload || bytesRead == totalSize
}

func percentComplete(current, total int64) int {
	if total <= 0 {
		return 0
	}
	return int(current * 100 / total)
}

func checkFilebrowserContext(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func handleArchiveDownloadNotMigrated(ctx context.Context, args []string, emit ipc.Events) error {
	return fmt.Errorf("archive download not yet migrated")
}

func handleCompressNotMigrated(ctx context.Context, args []string, emit ipc.Events) error {
	return fmt.Errorf("compress not yet migrated")
}

func handleExtractNotMigrated(ctx context.Context, args []string, emit ipc.Events) error {
	return fmt.Errorf("extract not yet migrated")
}

// uploadHandler implements BidirectionalHandler for file uploads
type uploadHandler struct{}

func (h *uploadHandler) Execute(ctx context.Context, args []string, emit ipc.Events) error {
	return fmt.Errorf("upload requires bidirectional stream")
}

func (h *uploadHandler) ExecuteWithInput(ctx context.Context, args []string, emit ipc.Events, input <-chan []byte) error {
	upload, err := parseUploadContext(args)
	if err != nil {
		return err
	}
	logger.Infof("upload requested: path=%s size=%d", upload.path, upload.expectedSize)

	root, preserveState, err := openUploadRoot(upload)
	if err != nil {
		return err
	}
	defer root.Close()

	file, err := root.Root.OpenFile(upload.tempRel, os.O_RDWR|os.O_CREATE|os.O_TRUNC, services.PermFile)
	if err != nil {
		return fmt.Errorf("cannot create temp file: %w", err)
	}
	defer cleanupUploadTempFile(root, file, upload.tempRel, upload.tempPath)

	if progressErr := emit.Progress(FileProgress{Total: upload.expectedSize, Phase: "receiving"}); progressErr != nil {
		return fmt.Errorf("write progress: %w", progressErr)
	}

	bytesWritten, err := receiveUploadChunks(ctx, input, file, emit, upload.expectedSize)
	if err != nil {
		return err
	}

	if err := finalizeUpload(root, file, upload, bytesWritten, preserveState); err != nil {
		return err
	}

	logger.Infof("Upload complete: path=%s size=%d", upload.path, bytesWritten)
	return emit.Result(map[string]any{
		"path": upload.path,
		"size": bytesWritten,
	})
}

func parseUploadContext(args []string) (*uploadContext, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("missing path or size")
	}
	expectedSize, err := strconv.ParseInt(args[1], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid size: %w", err)
	}
	realPath := filepath.Clean(args[0])
	return &uploadContext{
		path:         args[0],
		realPath:     realPath,
		tempPath:     realPath + ".upload.tmp",
		realRel:      fsroot.ToRel(realPath),
		tempRel:      fsroot.ToRel(realPath + ".upload.tmp"),
		expectedSize: expectedSize,
		override:     len(args) >= 3 && args[2] == "true",
	}, nil
}

func openUploadRoot(upload *uploadContext) (*fsroot.FSRoot, uploadPreserveState, error) {
	root, err := fsroot.Open()
	if err != nil {
		return nil, uploadPreserveState{}, fmt.Errorf("failed to access filesystem: %w", err)
	}
	preserveState, err := loadUploadPreserveState(root, upload)
	if err != nil {
		root.Close()
		return nil, uploadPreserveState{}, err
	}
	return root, preserveState, nil
}

func loadUploadPreserveState(root *fsroot.FSRoot, upload *uploadContext) (uploadPreserveState, error) {
	existingStat, err := root.Root.Stat(upload.realRel)
	if err != nil {
		if os.IsNotExist(err) {
			return uploadPreserveState{}, nil
		}
		return uploadPreserveState{}, nil
	}
	if !upload.override {
		return uploadPreserveState{}, fmt.Errorf("bad_request:file already exists. Set override=true to overwrite")
	}
	preserveState := uploadPreserveState{mode: existingStat.Mode(), ok: true}
	if sysStat, ok := existingStat.Sys().(*syscall.Stat_t); ok {
		preserveState.uid = int(sysStat.Uid)
		preserveState.gid = int(sysStat.Gid)
	}
	return preserveState, nil
}

func cleanupUploadTempFile(root *fsroot.FSRoot, file *os.File, tempRel, tempPath string) {
	if closeErr := file.Close(); closeErr != nil {
		logger.Debugf("failed to close temp upload file: %v", closeErr)
	}
	if removeErr := root.Root.Remove(tempRel); removeErr != nil && !os.IsNotExist(removeErr) {
		logger.Debugf("failed to remove temp upload file %s: %v", tempPath, removeErr)
	}
}

func receiveUploadChunks(
	ctx context.Context,
	input <-chan []byte,
	file *os.File,
	emit ipc.Events,
	expectedSize int64,
) (int64, error) {
	var bytesWritten int64
	var lastProgress int64

	for chunk := range input {
		n, err := file.Write(chunk)
		if err != nil {
			return bytesWritten, fmt.Errorf("write error: %w", err)
		}
		bytesWritten += int64(n)
		if shouldReportUploadProgress(bytesWritten, lastProgress, expectedSize) {
			if err := emit.Progress(FileProgress{
				Bytes: bytesWritten,
				Total: expectedSize,
				Pct:   percentComplete(bytesWritten, expectedSize),
			}); err != nil {
				return bytesWritten, fmt.Errorf("write progress: %w", err)
			}
			lastProgress = bytesWritten
		}
		if err := checkFilebrowserContext(ctx); err != nil {
			return bytesWritten, err
		}
	}
	return bytesWritten, nil
}

func shouldReportUploadProgress(bytesWritten, lastProgress, expectedSize int64) bool {
	return bytesWritten-lastProgress >= progressIntervalUpload || bytesWritten == expectedSize
}

func finalizeUpload(
	root *fsroot.FSRoot,
	file *os.File,
	upload *uploadContext,
	bytesWritten int64,
	preserveState uploadPreserveState,
) error {
	if err := file.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if bytesWritten != upload.expectedSize {
		return fmt.Errorf("size mismatch: expected %d, got %d", upload.expectedSize, bytesWritten)
	}
	if err := root.Root.Rename(upload.tempRel, upload.realRel); err != nil {
		return fmt.Errorf("rename failed: %w", err)
	}
	restoreUploadMetadata(root, upload.realRel, upload.realPath, preserveState)
	return nil
}

func restoreUploadMetadata(root *fsroot.FSRoot, realRel, realPath string, preserveState uploadPreserveState) {
	if !preserveState.ok {
		return
	}
	if err := root.Root.Chmod(realRel, preserveState.mode); err != nil {
		logger.Debugf("failed to restore mode on %s: %v", realPath, err)
	}
	if err := root.Root.Chown(realRel, preserveState.uid, preserveState.gid); err != nil {
		logger.Debugf("failed to restore ownership on %s: %v", realPath, err)
	}
}
