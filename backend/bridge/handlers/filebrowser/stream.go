package filebrowser

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/mordilloSan/go-logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/config"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/services"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/indexer"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// Stream types for filebrowser operations.
const (
	StreamTypeFBDownload      = "fb-download"       // Single file download
	StreamTypeFBUpload        = "fb-upload"         // Single file upload
	StreamTypeFBArchive       = "fb-archive"        // Multi-file archive download
	StreamTypeFBCompress      = "fb-compress"       // Create archive from paths
	StreamTypeFBExtract       = "fb-extract"        // Extract archive to destination
	StreamTypeFBReindex       = "fb-reindex"        // Reindex filesystem with progress
	StreamTypeFBIndexerAttach = "fb-indexer-attach" // Attach to running indexer operation
	StreamTypeFBCopy          = "fb-copy"           // Copy file or directory with progress
	StreamTypeFBMove          = "fb-move"           // Move file or directory with progress
)

const (
	// progressIntervalDownload is how often to send progress updates for downloads (2MB)
	progressIntervalDownload = 2 * 1024 * 1024
	// progressIntervalUpload is how often to send progress updates for uploads (512KB)
	// More frequent for flow control - acts as ACK for client-side window
	progressIntervalUpload = 512 * 1024
)

// chunkSizeFromSess returns the configured file-transfer chunk size in bytes.
// Falls back to 1 MiB when the config is unavailable or unset (ChunkSizeMB == 0).
func chunkSizeFromSess(sess *session.Session) int {
	const defaultChunkSize = 1 * 1024 * 1024
	cfg, _, err := config.Load(sess.User.Username)
	if err != nil || cfg.AppSettings.ChunkSizeMB <= 0 {
		return defaultChunkSize
	}
	return cfg.AppSettings.ChunkSizeMB * 1024 * 1024
}

// FileProgress represents progress for file transfer operations.
type FileProgress struct {
	Bytes int64  `json:"bytes"`           // Bytes transferred so far
	Total int64  `json:"total"`           // Total bytes (0 if unknown)
	Pct   int    `json:"pct"`             // Percentage (0-100)
	Phase string `json:"phase,omitempty"` // Optional phase description
}

// HandleDownloadStream handles a download stream for a single file.
func HandleDownloadStream(sess *session.Session, stream net.Conn, args []string) error {
	return handleDownload(stream, args, chunkSizeFromSess(sess))
}

// HandleUploadStream handles an upload stream for a single file.
func HandleUploadStream(sess *session.Session, stream net.Conn, args []string) error {
	return handleUpload(stream, args)
}

// HandleArchiveStream handles an archive download stream (multi-file).
func HandleArchiveStream(sess *session.Session, stream net.Conn, args []string) error {
	return handleArchiveDownload(stream, args, chunkSizeFromSess(sess))
}

// HandleCompressStream handles a compression stream.
func HandleCompressStream(sess *session.Session, stream net.Conn, args []string) error {
	return handleCompress(stream, args)
}

// HandleExtractStream handles an extraction stream.
func HandleExtractStream(sess *session.Session, stream net.Conn, args []string) error {
	return handleExtract(stream, args)
}

// HandleReindexStream handles a reindex stream with real-time progress.
func HandleReindexStream(sess *session.Session, stream net.Conn, args []string) error {
	return handleReindex(stream, args)
}

// HandleIndexerAttachStream attaches to an already-running indexer operation.
func HandleIndexerAttachStream(sess *session.Session, stream net.Conn, args []string) error {
	return handleIndexerAttach(stream)
}

// HandleCopyStream handles a copy stream with real-time progress.
func HandleCopyStream(sess *session.Session, stream net.Conn, args []string) error {
	return handleCopy(stream, args)
}

// HandleMoveStream handles a move stream with real-time progress.
func HandleMoveStream(sess *session.Session, stream net.Conn, args []string) error {
	return handleMove(stream, args)
}

// RegisterStreamHandlers registers all filebrowser stream handlers.
func RegisterStreamHandlers(handlers map[string]func(*session.Session, net.Conn, []string) error) {
	handlers[StreamTypeFBDownload] = HandleDownloadStream
	handlers[StreamTypeFBUpload] = HandleUploadStream
	handlers[StreamTypeFBArchive] = HandleArchiveStream
	handlers[StreamTypeFBCompress] = HandleCompressStream
	handlers[StreamTypeFBExtract] = HandleExtractStream
	handlers[StreamTypeFBReindex] = HandleReindexStream
	handlers[StreamTypeFBIndexerAttach] = HandleIndexerAttachStream
	handlers[StreamTypeFBCopy] = HandleCopyStream
	handlers[StreamTypeFBMove] = HandleMoveStream
}

func logWriteErr(action string, err error) {
	if err != nil {
		logger.Debugf("[FBStream] failed to write %s frame: %v", action, err)
	}
}

type uploadAttributes struct {
	mode        os.FileMode
	uid         int
	gid         int
	hasExisting bool
}

type transferRequest struct {
	source      string
	destination string
	overwrite   bool
}

func parseUploadArgs(args []string) (string, int64, error) {
	if len(args) < 2 {
		return "", 0, fmt.Errorf("missing path or size")
	}

	expectedSize, err := strconv.ParseInt(args[1], 10, 64)
	if err != nil {
		return "", 0, fmt.Errorf("invalid size: %w", err)
	}

	return args[0], expectedSize, nil
}

func loadUploadAttributes(root *fsroot.FSRoot, realRel string) (uploadAttributes, error) {
	existingStat, err := root.Root.Stat(realRel)
	if err != nil {
		return uploadAttributes{}, nil
	}
	if existingStat.IsDir() {
		return uploadAttributes{}, fmt.Errorf("destination is a directory")
	}

	attrs := uploadAttributes{mode: existingStat.Mode()}
	if st, ok := existingStat.Sys().(*syscall.Stat_t); ok {
		attrs.uid = int(st.Uid)
		attrs.gid = int(st.Gid)
		attrs.hasExisting = true
	}
	return attrs, nil
}

func openUploadTarget(root *fsroot.FSRoot, realPath, realRel string) (*os.File, error) {
	if err := root.Root.MkdirAll(fsroot.ToRel(filepath.Dir(realPath)), services.PermDir); err != nil {
		return nil, fmt.Errorf("create parent dir: %w", err)
	}
	return root.Root.OpenFile(realRel, os.O_RDWR|os.O_CREATE|os.O_TRUNC, services.PermFile)
}

func receiveUploadFrames(stream net.Conn, file *os.File, expectedSize int64) (int64, error) {
	logWriteErr("progress", ipc.WriteProgress(stream, 0, FileProgress{
		Total: expectedSize,
		Phase: "starting",
	}))

	var bytesWritten int64
	var lastProgress int64
	for {
		frame, err := ipc.ReadRelayFrame(stream)
		if err != nil {
			if errors.Is(err, io.EOF) {
				if bytesWritten >= expectedSize {
					return bytesWritten, nil
				}
				return bytesWritten, ipc.ErrAborted
			}
			logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("read error: %v", err), 500))
			return bytesWritten, fmt.Errorf("read frame: %w", err)
		}

		done, err := handleUploadFrame(stream, file, frame, expectedSize, &bytesWritten, &lastProgress)
		if err != nil {
			return bytesWritten, err
		}
		if done {
			break
		}
	}

	if expectedSize > 0 && bytesWritten != expectedSize {
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("size mismatch: expected %d, got %d", expectedSize, bytesWritten), 400))
		return bytesWritten, fmt.Errorf("size mismatch")
	}

	return bytesWritten, nil
}

func handleUploadFrame(stream net.Conn, file *os.File, frame *ipc.StreamFrame, expectedSize int64, bytesWritten, lastProgress *int64) (bool, error) {
	switch frame.Opcode {
	case ipc.OpStreamData:
		if len(frame.Payload) == 0 {
			return false, nil
		}
		n, err := file.Write(frame.Payload)
		if err != nil {
			logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("write error: %v", err), 500))
			return false, fmt.Errorf("write data: %w", err)
		}
		*bytesWritten += int64(n)
		if *bytesWritten-*lastProgress >= progressIntervalUpload || *bytesWritten == expectedSize {
			writeUploadProgress(stream, *bytesWritten, expectedSize)
			*lastProgress = *bytesWritten
		}
		return false, nil
	case ipc.OpStreamClose:
		return true, nil
	case ipc.OpStreamAbort:
		return false, ipc.ErrAborted
	default:
		logger.Debugf(" Ignoring opcode: 0x%02x", frame.Opcode)
		return false, nil
	}
}

func writeUploadProgress(stream net.Conn, bytesWritten, expectedSize int64) {
	pct := 0
	if expectedSize > 0 {
		pct = int(bytesWritten * 100 / expectedSize)
	}
	logWriteErr("progress", ipc.WriteProgress(stream, 0, FileProgress{
		Bytes: bytesWritten,
		Total: expectedSize,
		Pct:   pct,
		Phase: "uploading",
	}))
}

func restoreUploadedFile(root *fsroot.FSRoot, realRel string, attrs uploadAttributes) {
	if attrs.hasExisting {
		if err := root.Root.Chmod(realRel, attrs.mode); err != nil {
			logger.Debugf(" Failed to restore permissions: %v", err)
		}
		if err := root.Root.Chown(realRel, attrs.uid, attrs.gid); err != nil {
			logger.Debugf(" Failed to restore ownership: %v", err)
		}
		return
	}
	if err := root.Root.Chmod(realRel, services.PermFile); err != nil {
		logger.Debugf(" Failed to set permissions: %v", err)
	}
}

func notifyUploadedFile(path string, info os.FileInfo) {
	go func(stat os.FileInfo) {
		if err := addToIndexer(path, stat); err != nil {
			logger.Debugf(" Failed to update indexer: %v", err)
		}
	}(info)
}

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
		logger.Debugf(" Failed to compute archive size: %v", err)
		return 0
	}
	return totalSize
}

func computeExtractSize(archivePath string) int64 {
	totalSize, err := services.ComputeExtractSize(archivePath)
	if err != nil {
		logger.Debugf(" Failed to compute extract size: %v", err)
		return 0
	}
	return totalSize
}

func writePhaseProgress(stream net.Conn, total int64, phase string) {
	logWriteErr("progress", ipc.WriteProgress(stream, 0, FileProgress{
		Total: total,
		Phase: phase,
	}))
}

func newPhaseCallbacks(stream net.Conn, cancelFn ipc.CancelFunc, totalSize int64, phase string) *ipc.OperationCallbacks {
	pt := ipc.NewProgressTracker(stream, 0, progressIntervalDownload)
	return pt.AsCallback(cancelFn, func(processed, total int64) any {
		return FileProgress{
			Bytes: processed,
			Total: total,
			Pct:   min(int(processed*100/total), 100),
			Phase: phase,
		}
	}, totalSize)
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

func streamArchiveFile(stream net.Conn, archiveFile *os.File, archiveSize int64, chunkSize int) error {
	buf := make([]byte, chunkSize)
	streamPT := ipc.NewProgressTracker(stream, 0, progressIntervalDownload)
	var bytesSent int64

	for {
		n, readErr := archiveFile.Read(buf)
		if n > 0 {
			if err := ipc.WriteRelayFrame(stream, &ipc.StreamFrame{
				Opcode:   ipc.OpStreamData,
				StreamID: 0,
				Payload:  buf[:n],
			}); err != nil {
				return fmt.Errorf("write archive chunk: %w", err)
			}

			bytesSent += int64(n)
			pct := 0
			if archiveSize > 0 {
				pct = int(bytesSent * 100 / archiveSize)
			}
			if err := streamPT.Report(bytesSent, archiveSize, FileProgress{
				Bytes: bytesSent,
				Total: archiveSize,
				Pct:   pct,
				Phase: "streaming",
			}); err != nil {
				logger.Debugf(" failed to write archive stream progress: %v", err)
			}
		}

		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("read error: %v", readErr), 500))
			return fmt.Errorf("read archive: %w", readErr)
		}
	}
}

func archiveNameForPaths(paths []string, extension string) string {
	if len(paths) == 1 {
		base := filepath.Base(paths[0])
		if base != "" && base != "." && base != "/" {
			return base + extension
		}
	}
	return "download" + extension
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
		logger.Debugf(" failed to remove failed archive %s: %v", targetPath, err)
	}
}

func notifyCompressedArchive(targetPath string, info os.FileInfo) {
	go func(stat os.FileInfo) {
		if err := addToIndexer(targetPath, stat); err != nil {
			logger.Debugf(" Failed to update indexer: %v", err)
		}
	}(info)
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
			logger.Debugf(" Failed to open root for indexer walk: %v", err)
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
				logger.Debugf(" Failed to update indexer for %s: %v", absPath, err)
			}
			return nil
		}); err != nil {
			logger.Debugf(" failed to walk extracted destination %s: %v", destPath, err)
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

// handleDownload streams a single file to the client.
// args: [path]
func handleDownload(stream net.Conn, args []string, chunkSize int) error {
	if len(args) < 1 {
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, "missing file path", 400))
		return fmt.Errorf("missing file path")
	}

	path := args[0]
	realPath := filepath.Clean(path)
	root, err := fsroot.Open()
	if err != nil {
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, "failed to access filesystem", 500))
		return fmt.Errorf("failed to access filesystem: %w", err)
	}
	defer root.Close()
	realRel := fsroot.ToRel(realPath)

	// Stat the file
	stat, err := root.Root.Stat(realRel)
	if err != nil {
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("file not found: %v", err), 404))
		return fmt.Errorf("file not found: %w", err)
	}

	if stat.IsDir() {
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, "path is a directory, use fb-archive instead", 400))
		return ipc.ErrIsDirectory
	}

	totalSize := stat.Size()

	// Send initial progress with total size
	logWriteErr("progress", ipc.WriteProgress(stream, 0, FileProgress{
		Total: totalSize,
		Phase: "starting",
	}))

	// Open the file
	file, err := root.Root.Open(realRel)
	if err != nil {
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("cannot open file: %v", err), 500))
		return fmt.Errorf("cannot open file: %w", err)
	}
	defer file.Close()

	// Stream file chunks
	buf := make([]byte, chunkSize)
	var bytesRead int64
	var lastProgress int64

	for {
		n, readErr := file.Read(buf)
		if n > 0 {
			// Send data chunk
			if err := ipc.WriteRelayFrame(stream, &ipc.StreamFrame{
				Opcode:   ipc.OpStreamData,
				StreamID: 0,
				Payload:  buf[:n],
			}); err != nil {
				return fmt.Errorf("write data chunk: %w", err)
			}

			bytesRead += int64(n)

			// Send progress update periodically
			if bytesRead-lastProgress >= progressIntervalDownload || bytesRead == totalSize {
				pct := 0
				if totalSize > 0 {
					pct = int(bytesRead * 100 / totalSize)
				}
				logWriteErr("progress", ipc.WriteProgress(stream, 0, FileProgress{
					Bytes: bytesRead,
					Total: totalSize,
					Pct:   pct,
				}))
				lastProgress = bytesRead
			}
		}

		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("read error: %v", readErr), 500))
			return fmt.Errorf("read file: %w", readErr)
		}
	}

	// Send success result and close stream.
	logWriteErr("ok+close", ipc.WriteResultOKAndClose(stream, 0, map[string]any{
		"path":     path,
		"size":     totalSize,
		"fileName": filepath.Base(realPath),
	}))

	logger.Infof(" Download complete: path=%s size=%d", path, totalSize)
	return nil
}

// handleUpload receives a file from the client.
// args: [path, size]
// If the file already exists, preserves its permissions and ownership.
func handleUpload(stream net.Conn, args []string) error {
	path, expectedSize, err := parseUploadArgs(args)
	if err != nil {
		status := 400
		message := "missing path or size"
		if len(args) >= 2 {
			message = "invalid size"
		}
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, message, status))
		return err
	}

	realPath := filepath.Clean(path)
	root, err := fsroot.Open()
	if err != nil {
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, "failed to access filesystem", 500))
		return fmt.Errorf("failed to access filesystem: %w", err)
	}
	defer root.Close()
	realRel := fsroot.ToRel(realPath)

	attrs, err := loadUploadAttributes(root, realRel)
	if err != nil {
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, err.Error(), 400))
		return err
	}

	file, err := openUploadTarget(root, realPath, realRel)
	if err != nil {
		message := fmt.Sprintf("cannot create file: %v", err)
		if strings.Contains(err.Error(), "create parent dir") {
			message = fmt.Sprintf("cannot create parent directory: %v", err)
		}
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, message, 500))
		return err
	}

	uploadSuccess := false
	defer func() {
		file.Close()
		if !uploadSuccess {
			if removeErr := root.Root.Remove(realRel); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				logger.Debugf(" failed to remove partial upload %s: %v", realPath, removeErr)
			}
		}
	}()

	bytesWritten, err := receiveUploadFrames(stream, file, expectedSize)
	if err != nil {
		return err
	}

	uploadSuccess = true
	file.Close()
	restoreUploadedFile(root, realRel, attrs)
	if finalInfo, err := root.Root.Stat(realRel); err == nil {
		notifyUploadedFile(path, finalInfo)
	}

	// Send success result and close stream.
	logWriteErr("ok+close", ipc.WriteResultOKAndClose(stream, 0, map[string]any{
		"path": path,
		"size": bytesWritten,
	}))

	logger.Infof(" Upload complete: path=%s size=%d", path, bytesWritten)
	return nil
}

// handleArchiveDownload creates and streams an archive of multiple files.
// args: [format, path1, path2, ...]
func handleArchiveDownload(stream net.Conn, args []string, chunkSize int) error {
	if len(args) < 2 {
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, "missing format or paths", 400))
		return fmt.Errorf("missing format or paths")
	}

	format := args[0]
	paths := args[1:]
	extension, err := archiveExtension(format)
	if err != nil {
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("unsupported format: %s", format), 400))
		return ipc.ErrUnsupportedFormat
	}

	cancelFn, cleanup := ipc.AbortMonitor(stream)
	defer cleanup()

	totalSize := computeArchiveSize(paths)
	writePhaseProgress(stream, totalSize, "preparing")

	tempFile, err := os.CreateTemp("", "linuxio-stream-archive-*"+extension)
	if err != nil {
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("cannot create temp file: %v", err), 500))
		return fmt.Errorf("create temp file: %w", err)
	}
	tempPath := tempFile.Name()
	tempFile.Close()
	defer os.Remove(tempPath)

	opts := newPhaseCallbacks(stream, cancelFn, totalSize, "compressing")
	err = createArchive(format, tempPath, opts, paths)
	if err == ipc.ErrAborted {
		logger.Infof(" Archive download aborted")
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, "operation aborted", 499))
		return ipc.ErrAborted
	}
	if err != nil {
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("archive creation failed: %v", err), 500))
		return fmt.Errorf("create archive: %w", err)
	}

	// Open archive for streaming
	archiveFile, err := os.Open(tempPath)
	if err != nil {
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("cannot open archive: %v", err), 500))
		return fmt.Errorf("open archive: %w", err)
	}
	defer archiveFile.Close()

	archiveStat, err := archiveFile.Stat()
	if err != nil {
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("cannot stat archive: %v", err), 500))
		return fmt.Errorf("stat archive: %w", err)
	}
	archiveSize := archiveStat.Size()

	writePhaseProgress(stream, archiveSize, "streaming")
	if err := streamArchiveFile(stream, archiveFile, archiveSize, chunkSize); err != nil {
		return err
	}

	// Send success result and close stream.
	logWriteErr("ok+close", ipc.WriteResultOKAndClose(stream, 0, map[string]any{
		"archiveName": archiveNameForPaths(paths, extension),
		"size":        archiveSize,
		"format":      format,
	}))

	logger.Infof(" Archive download complete: files=%d size=%d", len(paths), archiveSize)
	return nil
}

// handleCompress creates an archive from provided paths and saves it to disk.
// args: [format, destination, path1, path2, ...]
func handleCompress(stream net.Conn, args []string) error {
	if len(args) < 3 {
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, "missing format, destination, or paths", 400))
		return fmt.Errorf("missing format, destination, or paths")
	}

	format := args[0]
	destination := args[1]
	paths := args[2:]
	extension, err := archiveExtension(format)
	if err != nil {
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("unsupported format: %s", format), 400))
		return ipc.ErrUnsupportedFormat
	}

	targetPath := normalizeArchiveTargetPath(destination, extension)
	root, err := fsroot.Open()
	if err != nil {
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, "failed to access filesystem", 500))
		return fmt.Errorf("failed to access filesystem: %w", err)
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
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, message, status))
		return err
	}

	cancelFn, cleanup := ipc.AbortMonitor(stream)
	defer cleanup()

	totalSize := computeArchiveSize(paths)
	writePhaseProgress(stream, totalSize, "preparing")
	opts := newPhaseCallbacks(stream, cancelFn, totalSize, "compressing")
	err = createArchive(format, targetPath, opts, paths)
	if err == ipc.ErrAborted {
		logger.Infof(" Compress aborted, cleaning up: %s", targetPath)
		cleanupArchiveTarget(root, targetRel, targetPath)
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, "operation aborted", 499))
		return ipc.ErrAborted
	}
	if err != nil {
		cleanupArchiveTarget(root, targetRel, targetPath)
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("compression failed: %v", err), 500))
		return fmt.Errorf("create archive: %w", err)
	}

	// Get final archive size
	var archiveSize int64
	if info, err := root.Root.Stat(targetRel); err == nil {
		archiveSize = info.Size()
		notifyCompressedArchive(targetPath, info)
	}

	// Send success result and close stream.
	logWriteErr("ok+close", ipc.WriteResultOKAndClose(stream, 0, map[string]any{
		"path":   targetPath,
		"size":   archiveSize,
		"format": format,
	}))

	logger.Infof(" Compress complete: path=%s files=%d size=%d", targetPath, len(paths), archiveSize)
	return nil
}

// handleExtract extracts an archive to a destination directory.
// args: [archivePath, destination?]
func handleExtract(stream net.Conn, args []string) error {
	archivePath, destination, err := parseExtractArgs(args)
	if err != nil {
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, "missing archive path", 400))
		return err
	}

	root, err := fsroot.Open()
	if err != nil {
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, "failed to access filesystem", 500))
		return fmt.Errorf("failed to access filesystem: %w", err)
	}
	defer root.Close()

	_, statErr := root.Root.Stat(fsroot.ToRel(destination))
	destExistedBefore := statErr == nil

	archiveStat, err := root.Root.Stat(fsroot.ToRel(archivePath))
	if err != nil {
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("archive not found: %v", err), 404))
		return fmt.Errorf("archive not found: %w", err)
	}
	if archiveStat.IsDir() {
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, "path is a directory, not an archive", 400))
		return ipc.ErrIsDirectory
	}

	cancelFn, cleanup := ipc.AbortMonitor(stream)
	defer cleanup()

	totalSize := computeExtractSize(archivePath)
	writePhaseProgress(stream, totalSize, "preparing")
	opts := newPhaseCallbacks(stream, cancelFn, totalSize, "extracting")
	err = services.ExtractArchive(archivePath, destination, opts)
	if err == ipc.ErrAborted {
		logger.Infof(" Extract aborted, cleaning up: %s", destination)
		if !destExistedBefore {
			if removeErr := root.Root.RemoveAll(fsroot.ToRel(destination)); removeErr != nil {
				logger.Debugf(" Failed to clean up extraction directory: %v", removeErr)
			}
		}
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, "operation aborted", 499))
		return ipc.ErrAborted
	}
	if err != nil {
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("extraction failed: %v", err), 500))
		return fmt.Errorf("extract archive: %w", err)
	}

	notifyExtractedFiles(destination)

	// Send success result and close stream.
	logWriteErr("ok+close", ipc.WriteResultOKAndClose(stream, 0, map[string]any{
		"destination": destination,
	}))

	logger.Infof(" Extract complete: archive=%s destination=%s", archivePath, destination)
	return nil
}

// handleReindex triggers a reindex operation and streams progress to the client.
// args: [path?] - optional path, defaults to "/" for full filesystem reindex
func handleReindex(stream net.Conn, args []string) error {
	path := "/"
	if len(args) > 0 && args[0] != "" {
		path = filepath.Clean(args[0])
	}

	ctx, _, cleanup := ipc.AbortContext(context.Background(), stream)
	defer cleanup()

	cb := indexer.IndexerCallbacks{
		OnProgress: func(p indexer.IndexerProgress) error {
			return ipc.WriteProgress(stream, 0, p)
		},
		OnResult: func(r indexer.IndexerResult) error {
			logWriteErr("ok+close", ipc.WriteResultOKAndClose(stream, 0, r))
			logger.Infof(" Reindex complete: path=%s files=%d dirs=%d duration=%dms",
				r.Path, r.FilesIndexed, r.DirsIndexed, r.DurationMs)
			return nil
		},
		OnError: func(msg string, code int) error {
			logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, msg, code))
			return nil
		},
	}

	return indexer.StreamIndexer(ctx, path, cb)
}

// handleIndexerAttach attaches to an already-running indexer operation and streams progress.
func handleIndexerAttach(stream net.Conn) error {
	ctx, _, cleanup := ipc.AbortContext(context.Background(), stream)
	defer cleanup()

	cb := indexer.IndexerCallbacks{
		OnProgress: func(p indexer.IndexerProgress) error {
			return ipc.WriteProgress(stream, 0, p)
		},
		OnResult: func(r indexer.IndexerResult) error {
			logWriteErr("ok+close", ipc.WriteResultOKAndClose(stream, 0, r))
			logger.Infof(" Indexer attach complete: files=%d dirs=%d duration=%dms",
				r.FilesIndexed, r.DirsIndexed, r.DurationMs)
			return nil
		},
		OnError: func(msg string, code int) error {
			logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, msg, code))
			return nil
		},
	}

	return indexer.StreamIndexerAttach(ctx, cb)
}

// handleCopy copies a file or directory with progress feedback.
// args: [source, destination, overwrite?]
func handleCopy(stream net.Conn, args []string) error {
	if len(args) < 2 {
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, "missing source or destination", 400))
		return fmt.Errorf("missing source or destination")
	}

	source := filepath.Clean(args[0])
	destination := filepath.Clean(args[1])
	root, err := fsroot.Open()
	if err != nil {
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, "failed to access filesystem", 500))
		return fmt.Errorf("failed to access filesystem: %w", err)
	}
	defer root.Close()
	overwrite := false
	if len(args) > 2 {
		overwrite = args[2] == "true"
	}

	// Validate source exists
	sourceInfo, err := root.Root.Stat(fsroot.ToRel(source))
	if err != nil {
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("source not found: %v", err), 404))
		return fmt.Errorf("source not found: %w", err)
	}

	// Check if destination is a directory - if so, append source filename
	destInfo, destErr := root.Root.Stat(fsroot.ToRel(destination))
	if destErr == nil && destInfo.IsDir() {
		destination = filepath.Join(destination, filepath.Base(source))
		// Re-check the new destination path
		destInfo, destErr = root.Root.Stat(fsroot.ToRel(destination))
	}

	// Check destination conflicts
	if destErr == nil {
		if !overwrite {
			logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, "destination already exists", 409))
			return fmt.Errorf("destination exists")
		}
		if sourceInfo.IsDir() != destInfo.IsDir() {
			logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, "source and destination types don't match", 400))
			return fmt.Errorf("type mismatch")
		}
	}

	// Set up abort monitoring
	cancelFn, cleanup := ipc.AbortMonitor(stream)
	defer cleanup()

	// Compute total size for progress
	totalSize, err := services.ComputeCopySize(source)
	if err != nil {
		logger.Debugf(" Failed to compute copy size: %v", err)
		totalSize = 0
	}

	// Send initial progress
	logWriteErr("progress", ipc.WriteProgress(stream, 0, FileProgress{
		Total: totalSize,
		Phase: "preparing",
	}))

	// Create callbacks for progress tracking
	pt := ipc.NewProgressTracker(stream, 0, progressIntervalDownload)
	opts := pt.AsCallback(cancelFn, func(processed, total int64) any {
		return FileProgress{
			Bytes: processed,
			Total: total,
			Pct:   min(int(processed*100/total), 100),
			Phase: "copying",
		}
	}, totalSize)

	// Perform the copy operation
	err = services.CopyFileWithCallbacks(source, destination, overwrite, opts)
	if err == ipc.ErrAborted {
		logger.Infof(" Copy aborted: %s -> %s", source, destination)
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, "operation aborted", 499))
		return ipc.ErrAborted
	}
	if err != nil {
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("copy failed: %v", err), 500))
		return fmt.Errorf("copy failed: %w", err)
	}

	// Notify indexer about the copied file/directory (non-blocking)
	if info, err := root.Root.Stat(fsroot.ToRel(destination)); err == nil {
		go func(stat os.FileInfo) {
			if err := addToIndexer(destination, stat); err != nil {
				logger.Debugf(" Failed to update indexer: %v", err)
			}
		}(info)
	}

	// Send success result and close stream.
	logWriteErr("ok+close", ipc.WriteResultOKAndClose(stream, 0, map[string]any{
		"source":      source,
		"destination": destination,
		"size":        totalSize,
	}))

	logger.Infof(" Copy complete: %s -> %s size=%d", source, destination, totalSize)
	return nil
}

// handleMove moves a file or directory with progress feedback.
// args: [source, destination, overwrite?]
func handleMove(stream net.Conn, args []string) error {
	req, err := parseTransferRequest(args)
	if err != nil {
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, "missing source or destination", 400))
		return err
	}

	root, err := fsroot.Open()
	if err != nil {
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, "failed to access filesystem", 500))
		return fmt.Errorf("failed to access filesystem: %w", err)
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
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, message, code))
		return err
	}

	cancelFn, cleanup := ipc.AbortMonitor(stream)
	defer cleanup()

	totalSize, err := services.ComputeCopySize(req.source)
	if err != nil {
		logger.Debugf(" Failed to compute move size: %v", err)
		totalSize = 0
	}

	// Send initial progress
	logWriteErr("progress", ipc.WriteProgress(stream, 0, FileProgress{
		Total: totalSize,
		Phase: "preparing",
	}))

	opts := newPhaseCallbacks(stream, cancelFn, totalSize, "moving")
	err = services.MoveFileWithCallbacks(req.source, req.destination, req.overwrite, opts)
	if err == ipc.ErrAborted {
		logger.Infof(" Move aborted: %s -> %s", req.source, req.destination)
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, "operation aborted", 499))
		return ipc.ErrAborted
	}
	if err != nil {
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("move failed: %v", err), 500))
		return fmt.Errorf("move failed: %w", err)
	}

	destInfoAfterMove, statErr := root.Root.Stat(fsroot.ToRel(req.destination))
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		logger.Debugf(" failed to stat move destination %s: %v", req.destination, statErr)
	}
	go func(info os.FileInfo) {
		if err := deleteFromIndexer(req.source); err != nil {
			logger.Debugf(" Failed to delete from indexer: %v", err)
		}
		if info != nil {
			if err := addToIndexer(req.destination, info); err != nil {
				logger.Debugf(" Failed to update indexer: %v", err)
			}
		}
	}(destInfoAfterMove)

	// Send success result and close stream.
	logWriteErr("ok+close", ipc.WriteResultOKAndClose(stream, 0, map[string]any{
		"source":      req.source,
		"destination": req.destination,
		"size":        totalSize,
	}))

	logger.Infof(" Move complete: %s -> %s size=%d", req.source, req.destination, totalSize)
	return nil
}
