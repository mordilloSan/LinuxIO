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
	// chunkSize is the size of data chunks for file transfers
	chunkSize = 1 * 1024 * 1024
	// progressIntervalDownload is how often to send progress updates for downloads (2MB)
	progressIntervalDownload = 2 * 1024 * 1024
	// progressIntervalUpload is how often to send progress updates for uploads (512KB)
	// More frequent for flow control - acts as ACK for client-side window
	progressIntervalUpload = 512 * 1024
)

// FileProgress represents progress for file transfer operations.
type FileProgress struct {
	Bytes int64  `json:"bytes"`           // Bytes transferred so far
	Total int64  `json:"total"`           // Total bytes (0 if unknown)
	Pct   int    `json:"pct"`             // Percentage (0-100)
	Phase string `json:"phase,omitempty"` // Optional phase description
}

// HandleDownloadStream handles a download stream for a single file.
func HandleDownloadStream(sess *session.Session, stream net.Conn, args []string) error {
	return handleDownload(stream, args)
}

// HandleUploadStream handles an upload stream for a single file.
func HandleUploadStream(sess *session.Session, stream net.Conn, args []string) error {
	return handleUpload(stream, args)
}

// HandleArchiveStream handles an archive download stream (multi-file).
func HandleArchiveStream(sess *session.Session, stream net.Conn, args []string) error {
	return handleArchiveDownload(stream, args)
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

// handleDownload streams a single file to the client.
// args: [path]
func handleDownload(stream net.Conn, args []string) error {
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
	if len(args) < 2 {
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, "missing path or size", 400))
		return fmt.Errorf("missing path or size")
	}

	path := args[0]
	sizeStr := args[1]
	expectedSize, err := strconv.ParseInt(sizeStr, 10, 64)
	if err != nil {
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, "invalid size", 400))
		return fmt.Errorf("invalid size: %w", err)
	}

	realPath := filepath.Clean(path)
	root, err := fsroot.Open()
	if err != nil {
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, "failed to access filesystem", 500))
		return fmt.Errorf("failed to access filesystem: %w", err)
	}
	defer root.Close()
	realRel := fsroot.ToRel(realPath)

	// Check if file exists and save its attributes for later restoration
	var preserveMode os.FileMode
	var preserveUID, preserveGID int
	var hasExistingAttrs bool

	if existingStat, statErr := root.Root.Stat(realRel); statErr == nil {
		if existingStat.IsDir() {
			logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, "destination is a directory", 400))
			return fmt.Errorf("destination is a directory")
		}
		preserveMode = existingStat.Mode()
		if st, ok := existingStat.Sys().(*syscall.Stat_t); ok {
			preserveUID = int(st.Uid)
			preserveGID = int(st.Gid)
			hasExistingAttrs = true
		}
	}

	// Ensure parent directory exists
	if mkdirErr := root.Root.MkdirAll(fsroot.ToRel(filepath.Dir(realPath)), services.PermDir); mkdirErr != nil {
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("cannot create parent directory: %v", mkdirErr), 500))
		return fmt.Errorf("create parent dir: %w", mkdirErr)
	}

	// Create target file directly (delete partial on failure)
	file, err := root.Root.OpenFile(realRel, os.O_RDWR|os.O_CREATE|os.O_TRUNC, services.PermFile)
	if err != nil {
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("cannot create file: %v", err), 500))
		return fmt.Errorf("create file: %w", err)
	}

	// Track success to decide cleanup
	uploadSuccess := false
	defer func() {
		file.Close()
		if !uploadSuccess {
			if removeErr := root.Root.Remove(realRel); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				logger.Debugf(" failed to remove partial upload %s: %v", realPath, removeErr)
			}
		}
	}()

	// Send initial progress with total size
	logWriteErr("progress", ipc.WriteProgress(stream, 0, FileProgress{
		Total: expectedSize,
		Phase: "starting",
	}))

	// Read frames and write directly to file
	var bytesWritten int64
	var lastProgress int64

readLoop:
	for {
		frame, err := ipc.ReadRelayFrame(stream)
		if err != nil {
			if errors.Is(err, io.EOF) {
				// Client closed connection - check if we got all data
				if bytesWritten >= expectedSize {
					break readLoop
				}
				return ipc.ErrAborted
			}
			logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("read error: %v", err), 500))
			return fmt.Errorf("read frame: %w", err)
		}

		switch frame.Opcode {
		case ipc.OpStreamData:
			if len(frame.Payload) > 0 {
				n, werr := file.Write(frame.Payload)
				if werr != nil {
					logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("write error: %v", werr), 500))
					return fmt.Errorf("write data: %w", werr)
				}
				bytesWritten += int64(n)

				// Send progress update periodically (frequent for flow control ACK)
				if bytesWritten-lastProgress >= progressIntervalUpload || bytesWritten == expectedSize {
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
					lastProgress = bytesWritten
				}
			}

		case ipc.OpStreamClose:
			// Client signaled done - break out of loop
			break readLoop
		case ipc.OpStreamAbort:
			// Client explicitly canceled upload.
			return ipc.ErrAborted

		default:
			logger.Debugf(" Ignoring opcode: 0x%02x", frame.Opcode)
		}
	}

	// Verify size
	if expectedSize > 0 && bytesWritten != expectedSize {
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("size mismatch: expected %d, got %d", expectedSize, bytesWritten), 400))
		return fmt.Errorf("size mismatch")
	}

	// Mark success before closing file
	uploadSuccess = true
	file.Close()

	// Set permissions: restore existing or use default
	if hasExistingAttrs {
		if err := root.Root.Chmod(realRel, preserveMode); err != nil {
			logger.Debugf(" Failed to restore permissions: %v", err)
		}
		if err := root.Root.Chown(realRel, preserveUID, preserveGID); err != nil {
			logger.Debugf(" Failed to restore ownership: %v", err)
		}
	} else {
		if err := root.Root.Chmod(realRel, services.PermFile); err != nil {
			logger.Debugf(" Failed to set permissions: %v", err)
		}
	}

	// Notify indexer about the new file (non-blocking)
	if finalInfo, err := root.Root.Stat(realRel); err == nil {
		go func(info os.FileInfo) {
			if err := addToIndexer(path, info); err != nil {
				logger.Debugf(" Failed to update indexer: %v", err)
			}
		}(finalInfo)
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
func handleArchiveDownload(stream net.Conn, args []string) error {
	if len(args) < 2 {
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, "missing format or paths", 400))
		return fmt.Errorf("missing format or paths")
	}

	format := args[0]
	paths := args[1:]

	// Validate format
	var extension string
	switch format {
	case "zip":
		extension = ".zip"
	case "tar.gz":
		extension = ".tar.gz"
	default:
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("unsupported format: %s", format), 400))
		return ipc.ErrUnsupportedFormat
	}

	// Set up abort monitoring
	cancelFn, cleanup := ipc.AbortMonitor(stream)
	defer cleanup()

	// Compute total size for progress
	totalSize, err := services.ComputeArchiveSize(paths)
	if err != nil {
		logger.Debugf(" Failed to compute archive size: %v", err)
		totalSize = 0
	}

	// Send initial progress
	logWriteErr("progress", ipc.WriteProgress(stream, 0, FileProgress{
		Total: totalSize,
		Phase: "preparing",
	}))

	// Create temp file for archive
	tempFile, err := os.CreateTemp("", "linuxio-stream-archive-*"+extension)
	if err != nil {
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("cannot create temp file: %v", err), 500))
		return fmt.Errorf("create temp file: %w", err)
	}
	tempPath := tempFile.Name()
	tempFile.Close()
	defer os.Remove(tempPath)

	// Create archive with callbacks
	pt := ipc.NewProgressTracker(stream, 0, progressIntervalDownload)
	opts := pt.AsCallback(cancelFn, func(processed, total int64) any {
		return FileProgress{
			Bytes: processed,
			Total: total,
			Pct:   min(int(processed*100/total), 100),
			Phase: "compressing",
		}
	}, totalSize)

	// Create archive
	switch format {
	case "zip":
		err = services.CreateZip(tempPath, opts, tempPath, paths...)
	case "tar.gz":
		err = services.CreateTarGz(tempPath, opts, tempPath, paths...)
	}
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

	// Update progress for streaming phase
	logWriteErr("progress", ipc.WriteProgress(stream, 0, FileProgress{
		Total: archiveSize,
		Phase: "streaming",
	}))

	// Stream archive chunks
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
			if progressErr := streamPT.Report(bytesSent, archiveSize, FileProgress{
				Bytes: bytesSent,
				Total: archiveSize,
				Pct:   pct,
				Phase: "streaming",
			}); progressErr != nil {
				logger.Debugf(" failed to write archive stream progress: %v", progressErr)
			}
		}

		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("read error: %v", readErr), 500))
			return fmt.Errorf("read archive: %w", readErr)
		}
	}

	// Determine archive name
	archiveName := "download" + extension
	if len(paths) == 1 {
		base := filepath.Base(paths[0])
		if base != "" && base != "." && base != "/" {
			archiveName = base + extension
		}
	}

	// Send success result and close stream.
	logWriteErr("ok+close", ipc.WriteResultOKAndClose(stream, 0, map[string]any{
		"archiveName": archiveName,
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

	// Validate format and determine extension
	var extension string
	switch format {
	case "zip":
		extension = ".zip"
	case "tar.gz":
		extension = ".tar.gz"
	default:
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("unsupported format: %s", format), 400))
		return ipc.ErrUnsupportedFormat
	}

	// Ensure destination has correct extension
	targetPath := filepath.Clean(destination)
	root, err := fsroot.Open()
	if err != nil {
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, "failed to access filesystem", 500))
		return fmt.Errorf("failed to access filesystem: %w", err)
	}
	defer root.Close()
	lowerTarget := strings.ToLower(targetPath)
	switch extension {
	case ".zip":
		if !strings.HasSuffix(lowerTarget, ".zip") {
			targetPath = targetPath + ".zip"
		}
	case ".tar.gz":
		if !(strings.HasSuffix(lowerTarget, ".tar.gz") || strings.HasSuffix(lowerTarget, ".tgz")) {
			targetPath = targetPath + ".tar.gz"
		}
	}
	targetRel := fsroot.ToRel(targetPath)

	// Check if destination exists
	if info, statErr := root.Root.Stat(targetRel); statErr == nil {
		if info.IsDir() {
			logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, "destination is a directory", 400))
			return fmt.Errorf("destination is a directory")
		}
		// Remove existing file (overwrite)
		if rmErr := root.Root.Remove(targetRel); rmErr != nil {
			logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("cannot remove existing file: %v", rmErr), 500))
			return fmt.Errorf("remove existing file: %w", rmErr)
		}
	}

	// Ensure parent directory exists
	if mkdirErr := root.Root.MkdirAll(fsroot.ToRel(filepath.Dir(targetPath)), services.PermDir); mkdirErr != nil {
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("cannot create parent directory: %v", mkdirErr), 500))
		return fmt.Errorf("create parent dir: %w", mkdirErr)
	}

	// Set up abort monitoring
	cancelFn, cleanup := ipc.AbortMonitor(stream)
	defer cleanup()

	// Compute total size for progress
	totalSize, err := services.ComputeArchiveSize(paths)
	if err != nil {
		logger.Debugf(" Failed to compute archive size: %v", err)
		totalSize = 0
	}

	// Send initial progress
	logWriteErr("progress", ipc.WriteProgress(stream, 0, FileProgress{
		Total: totalSize,
		Phase: "preparing",
	}))

	// Create callbacks
	pt := ipc.NewProgressTracker(stream, 0, progressIntervalDownload)
	opts := pt.AsCallback(cancelFn, func(processed, total int64) any {
		return FileProgress{
			Bytes: processed,
			Total: total,
			Pct:   min(int(processed*100/total), 100),
			Phase: "compressing",
		}
	}, totalSize)

	// Create archive
	switch format {
	case "zip":
		err = services.CreateZip(targetPath, opts, targetPath, paths...)
	case "tar.gz":
		err = services.CreateTarGz(targetPath, opts, targetPath, paths...)
	}
	if err == ipc.ErrAborted {
		logger.Infof(" Compress aborted, cleaning up: %s", targetPath)
		if removeErr := root.Root.Remove(targetRel); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			logger.Debugf(" failed to remove partial archive %s: %v", targetPath, removeErr)
		}
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, "operation aborted", 499))
		return ipc.ErrAborted
	}
	if err != nil {
		if removeErr := root.Root.Remove(targetRel); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			logger.Debugf(" failed to remove failed archive %s: %v", targetPath, removeErr)
		}
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("compression failed: %v", err), 500))
		return fmt.Errorf("create archive: %w", err)
	}

	// Get final archive size
	var archiveSize int64
	if info, err := root.Root.Stat(targetRel); err == nil {
		archiveSize = info.Size()
		// Notify indexer
		go func(stat os.FileInfo) {
			if err := addToIndexer(targetPath, stat); err != nil {
				logger.Debugf(" Failed to update indexer: %v", err)
			}
		}(info)
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
	if len(args) < 1 {
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, "missing archive path", 400))
		return fmt.Errorf("missing archive path")
	}

	archivePath := filepath.Clean(args[0])
	root, err := fsroot.Open()
	if err != nil {
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, "failed to access filesystem", 500))
		return fmt.Errorf("failed to access filesystem: %w", err)
	}
	defer root.Close()

	// Determine destination
	var destination string
	if len(args) > 1 && args[1] != "" {
		destination = filepath.Clean(args[1])
	} else {
		destination = defaultExtractDestination(archivePath)
	}

	// Check if destination already exists (for cleanup decision on abort)
	_, statErr := root.Root.Stat(fsroot.ToRel(destination))
	destExistedBefore := statErr == nil

	// Check archive exists
	archiveStat, err := root.Root.Stat(fsroot.ToRel(archivePath))
	if err != nil {
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("archive not found: %v", err), 404))
		return fmt.Errorf("archive not found: %w", err)
	}
	if archiveStat.IsDir() {
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, "path is a directory, not an archive", 400))
		return ipc.ErrIsDirectory
	}

	// Set up abort monitoring
	cancelFn, cleanup := ipc.AbortMonitor(stream)
	defer cleanup()

	// Compute total size for progress (uncompressed size)
	totalSize, err := services.ComputeExtractSize(archivePath)
	if err != nil {
		logger.Debugf(" Failed to compute extract size: %v", err)
		totalSize = 0
	}

	// Send initial progress
	logWriteErr("progress", ipc.WriteProgress(stream, 0, FileProgress{
		Total: totalSize,
		Phase: "preparing",
	}))

	// Create callbacks
	pt := ipc.NewProgressTracker(stream, 0, progressIntervalDownload)
	opts := pt.AsCallback(cancelFn, func(processed, total int64) any {
		return FileProgress{
			Bytes: processed,
			Total: total,
			Pct:   min(int(processed*100/total), 100),
			Phase: "extracting",
		}
	}, totalSize)

	// Extract archive
	err = services.ExtractArchive(archivePath, destination, opts)
	if err == ipc.ErrAborted {
		logger.Infof(" Extract aborted, cleaning up: %s", destination)
		// Clean up extracted files on abort
		// Only remove the destination if we created it (didn't exist before)
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

	// Notify indexer about extracted files (non-blocking)
	go func(destPath string) {
		walkRoot, openErr := fsroot.Open()
		if openErr != nil {
			logger.Debugf(" Failed to open root for indexer walk: %v", openErr)
			return
		}
		defer walkRoot.Close()

		if walkErr := walkRoot.WalkDir(destPath, func(rel string, entry fs.DirEntry, walkErr error) error {
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
		}); walkErr != nil {
			logger.Debugf(" failed to walk extracted destination %s: %v", destPath, walkErr)
		}
	}(destination)

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

	// Compute total size for progress (in case we need to copy)
	totalSize, err := services.ComputeCopySize(source)
	if err != nil {
		logger.Debugf(" Failed to compute move size: %v", err)
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
			Phase: "moving",
		}
	}, totalSize)

	// Perform the move operation
	err = services.MoveFileWithCallbacks(source, destination, overwrite, opts)
	if err == ipc.ErrAborted {
		logger.Infof(" Move aborted: %s -> %s", source, destination)
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, "operation aborted", 499))
		return ipc.ErrAborted
	}
	if err != nil {
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("move failed: %v", err), 500))
		return fmt.Errorf("move failed: %w", err)
	}

	destInfoAfterMove, statErr := root.Root.Stat(fsroot.ToRel(destination))
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		logger.Debugf(" failed to stat move destination %s: %v", destination, statErr)
	}
	// Notify indexer about the move (non-blocking)
	go func(info os.FileInfo) {
		// Delete source from indexer
		if err := deleteFromIndexer(source); err != nil {
			logger.Debugf(" Failed to delete from indexer: %v", err)
		}
		// Add destination to indexer
		if info != nil {
			if err := addToIndexer(destination, info); err != nil {
				logger.Debugf(" Failed to update indexer: %v", err)
			}
		}
	}(destInfoAfterMove)

	// Send success result and close stream.
	logWriteErr("ok+close", ipc.WriteResultOKAndClose(stream, 0, map[string]any{
		"source":      source,
		"destination": destination,
		"size":        totalSize,
	}))

	logger.Infof(" Move complete: %s -> %s size=%d", source, destination, totalSize)
	return nil
}
