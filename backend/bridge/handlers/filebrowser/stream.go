package filebrowser

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/mordilloSan/go_logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/services"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// Stream types for filebrowser operations.
const (
	StreamTypeFBDownload = "fb-download" // Single file download
	StreamTypeFBUpload   = "fb-upload"   // Single file upload
	StreamTypeFBArchive  = "fb-archive"  // Multi-file archive download
	StreamTypeFBCompress = "fb-compress" // Create archive from paths
	StreamTypeFBExtract  = "fb-extract"  // Extract archive to destination
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

// RegisterStreamHandlers registers all filebrowser stream handlers.
func RegisterStreamHandlers(handlers map[string]func(*session.Session, net.Conn, []string) error) {
	handlers[StreamTypeFBDownload] = HandleDownloadStream
	handlers[StreamTypeFBUpload] = HandleUploadStream
	handlers[StreamTypeFBArchive] = HandleArchiveStream
	handlers[StreamTypeFBCompress] = HandleCompressStream
	handlers[StreamTypeFBExtract] = HandleExtractStream
}

// handleDownload streams a single file to the client.
// args: [path]
func handleDownload(stream net.Conn, args []string) error {
	if len(args) < 1 {
		_ = ipc.WriteResultError(stream, 0, "missing file path", 400)
		_ = ipc.WriteStreamClose(stream, 0)
		return fmt.Errorf("missing file path")
	}

	path := args[0]
	realPath := filepath.Clean(path)

	// Stat the file
	stat, err := os.Stat(realPath)
	if err != nil {
		_ = ipc.WriteResultError(stream, 0, fmt.Sprintf("file not found: %v", err), 404)
		_ = ipc.WriteStreamClose(stream, 0)
		return fmt.Errorf("file not found: %w", err)
	}

	if stat.IsDir() {
		_ = ipc.WriteResultError(stream, 0, "path is a directory, use fb-archive instead", 400)
		_ = ipc.WriteStreamClose(stream, 0)
		return ipc.ErrIsDirectory
	}

	totalSize := stat.Size()

	// Send initial progress with total size
	_ = ipc.WriteProgress(stream, 0, FileProgress{
		Total: totalSize,
		Phase: "starting",
	})

	// Open the file
	file, err := os.Open(realPath)
	if err != nil {
		_ = ipc.WriteResultError(stream, 0, fmt.Sprintf("cannot open file: %v", err), 500)
		_ = ipc.WriteStreamClose(stream, 0)
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
				_ = ipc.WriteProgress(stream, 0, FileProgress{
					Bytes: bytesRead,
					Total: totalSize,
					Pct:   pct,
				})
				lastProgress = bytesRead
			}
		}

		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			_ = ipc.WriteResultError(stream, 0, fmt.Sprintf("read error: %v", readErr), 500)
			_ = ipc.WriteStreamClose(stream, 0)
			return fmt.Errorf("read file: %w", readErr)
		}
	}

	// Send success result
	_ = ipc.WriteResultOK(stream, 0, map[string]any{
		"path":     path,
		"size":     totalSize,
		"fileName": filepath.Base(realPath),
	})

	// Close stream
	_ = ipc.WriteStreamClose(stream, 0)

	logger.Infof("[FBStream] Download complete: path=%s size=%d", path, totalSize)
	return nil
}

// handleUpload receives a file from the client.
// args: [path, size]
// If the file already exists, preserves its permissions and ownership.
func handleUpload(stream net.Conn, args []string) error {
	if len(args) < 2 {
		_ = ipc.WriteResultError(stream, 0, "missing path or size", 400)
		_ = ipc.WriteStreamClose(stream, 0)
		return fmt.Errorf("missing path or size")
	}

	path := args[0]
	sizeStr := args[1]
	expectedSize, err := strconv.ParseInt(sizeStr, 10, 64)
	if err != nil {
		_ = ipc.WriteResultError(stream, 0, "invalid size", 400)
		_ = ipc.WriteStreamClose(stream, 0)
		return fmt.Errorf("invalid size: %w", err)
	}

	realPath := filepath.Clean(path)

	// Check if file exists and save its attributes for later restoration
	var preserveMode os.FileMode
	var preserveUID, preserveGID int
	var hasExistingAttrs bool

	if existingStat, statErr := os.Stat(realPath); statErr == nil {
		if existingStat.IsDir() {
			_ = ipc.WriteResultError(stream, 0, "destination is a directory", 400)
			_ = ipc.WriteStreamClose(stream, 0)
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
	if mkdirErr := os.MkdirAll(filepath.Dir(realPath), services.PermDir); mkdirErr != nil {
		_ = ipc.WriteResultError(stream, 0, fmt.Sprintf("cannot create parent directory: %v", mkdirErr), 500)
		_ = ipc.WriteStreamClose(stream, 0)
		return fmt.Errorf("create parent dir: %w", mkdirErr)
	}

	// Create target file directly (delete partial on failure)
	file, err := os.Create(realPath)
	if err != nil {
		_ = ipc.WriteResultError(stream, 0, fmt.Sprintf("cannot create file: %v", err), 500)
		_ = ipc.WriteStreamClose(stream, 0)
		return fmt.Errorf("create file: %w", err)
	}

	// Track success to decide cleanup
	uploadSuccess := false
	defer func() {
		file.Close()
		if !uploadSuccess {
			os.Remove(realPath) // Clean up partial file on failure
		}
	}()

	// Send initial progress with total size
	_ = ipc.WriteProgress(stream, 0, FileProgress{
		Total: expectedSize,
		Phase: "starting",
	})

	// Read frames and write directly to file
	var bytesWritten int64
	var lastProgress int64

readLoop:
	for {
		frame, err := ipc.ReadRelayFrame(stream)
		if err != nil {
			if err == io.EOF {
				// Client closed connection - check if we got all data
				if bytesWritten >= expectedSize {
					break readLoop
				}
				_ = ipc.WriteResultError(stream, 0, fmt.Sprintf("connection closed early: got %d of %d bytes", bytesWritten, expectedSize), 500)
				_ = ipc.WriteStreamClose(stream, 0)
				return fmt.Errorf("connection closed early: got %d of %d bytes", bytesWritten, expectedSize)
			}
			_ = ipc.WriteResultError(stream, 0, fmt.Sprintf("read error: %v", err), 500)
			_ = ipc.WriteStreamClose(stream, 0)
			return fmt.Errorf("read frame: %w", err)
		}

		switch frame.Opcode {
		case ipc.OpStreamData:
			if len(frame.Payload) > 0 {
				n, werr := file.Write(frame.Payload)
				if werr != nil {
					_ = ipc.WriteResultError(stream, 0, fmt.Sprintf("write error: %v", werr), 500)
					_ = ipc.WriteStreamClose(stream, 0)
					return fmt.Errorf("write data: %w", werr)
				}
				bytesWritten += int64(n)

				// Send progress update periodically (frequent for flow control ACK)
				if bytesWritten-lastProgress >= progressIntervalUpload || bytesWritten == expectedSize {
					pct := 0
					if expectedSize > 0 {
						pct = int(bytesWritten * 100 / expectedSize)
					}
					_ = ipc.WriteProgress(stream, 0, FileProgress{
						Bytes: bytesWritten,
						Total: expectedSize,
						Pct:   pct,
						Phase: "uploading",
					})
					lastProgress = bytesWritten
				}
			}

		case ipc.OpStreamClose:
			// Client signaled done - break out of loop
			break readLoop

		default:
			logger.Debugf("[FBStream] Ignoring opcode: 0x%02x", frame.Opcode)
		}
	}

	// Verify size
	if expectedSize > 0 && bytesWritten != expectedSize {
		_ = ipc.WriteResultError(stream, 0, fmt.Sprintf("size mismatch: expected %d, got %d", expectedSize, bytesWritten), 400)
		_ = ipc.WriteStreamClose(stream, 0)
		return fmt.Errorf("size mismatch")
	}

	// Mark success before closing file
	uploadSuccess = true
	file.Close()

	// Set permissions: restore existing or use default
	if hasExistingAttrs {
		if err := os.Chmod(realPath, preserveMode); err != nil {
			logger.Debugf("[FBStream] Failed to restore permissions: %v", err)
		}
		if err := os.Chown(realPath, preserveUID, preserveGID); err != nil {
			logger.Debugf("[FBStream] Failed to restore ownership: %v", err)
		}
	} else {
		if err := os.Chmod(realPath, services.PermFile); err != nil {
			logger.Debugf("[FBStream] Failed to set permissions: %v", err)
		}
	}

	// Notify indexer about the new file (non-blocking)
	go func() {
		if finalInfo, err := os.Stat(realPath); err == nil {
			if err := addToIndexer(path, finalInfo); err != nil {
				logger.Debugf("[FBStream] Failed to update indexer: %v", err)
			}
		}
	}()

	// Send success result
	_ = ipc.WriteResultOK(stream, 0, map[string]any{
		"path": path,
		"size": bytesWritten,
	})

	// Close stream
	_ = ipc.WriteStreamClose(stream, 0)

	logger.Infof("[FBStream] Upload complete: path=%s size=%d", path, bytesWritten)
	return nil
}

// handleArchiveDownload creates and streams an archive of multiple files.
// args: [format, path1, path2, ...]
func handleArchiveDownload(stream net.Conn, args []string) error {
	if len(args) < 2 {
		_ = ipc.WriteResultError(stream, 0, "missing format or paths", 400)
		_ = ipc.WriteStreamClose(stream, 0)
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
		_ = ipc.WriteResultError(stream, 0, fmt.Sprintf("unsupported format: %s", format), 400)
		_ = ipc.WriteStreamClose(stream, 0)
		return ipc.ErrUnsupportedFormat
	}

	// Set up abort monitoring
	cancelFn, cleanup := ipc.AbortMonitor(stream)
	defer cleanup()

	// Compute total size for progress
	totalSize, err := services.ComputeArchiveSize(paths)
	if err != nil {
		logger.Debugf("[FBStream] Failed to compute archive size: %v", err)
		totalSize = 0
	}

	// Send initial progress
	_ = ipc.WriteProgress(stream, 0, FileProgress{
		Total: totalSize,
		Phase: "preparing",
	})

	// Create temp file for archive
	tempFile, err := os.CreateTemp("", "linuxio-stream-archive-*"+extension)
	if err != nil {
		_ = ipc.WriteResultError(stream, 0, fmt.Sprintf("cannot create temp file: %v", err), 500)
		_ = ipc.WriteStreamClose(stream, 0)
		return fmt.Errorf("create temp file: %w", err)
	}
	tempPath := tempFile.Name()
	tempFile.Close()
	defer os.Remove(tempPath)

	// Create archive with callbacks
	var bytesProcessed int64
	var lastProgress int64
	opts := &ipc.OperationCallbacks{
		Progress: func(n int64) {
			bytesProcessed += n
			if totalSize > 0 && (bytesProcessed-lastProgress >= progressIntervalDownload || bytesProcessed >= totalSize) {
				pct := int(bytesProcessed * 100 / totalSize)
				if pct > 100 {
					pct = 100
				}
				_ = ipc.WriteProgress(stream, 0, FileProgress{
					Bytes: bytesProcessed,
					Total: totalSize,
					Pct:   pct,
					Phase: "compressing",
				})
				lastProgress = bytesProcessed
			}
		},
		Cancel: cancelFn,
	}

	// Create archive
	switch format {
	case "zip":
		err = services.CreateZip(tempPath, opts, tempPath, paths...)
	case "tar.gz":
		err = services.CreateTarGz(tempPath, opts, tempPath, paths...)
	}
	if err == ipc.ErrAborted {
		logger.Infof("[FBStream] Archive download aborted")
		_ = ipc.WriteResultError(stream, 0, "operation aborted", 499)
		_ = ipc.WriteStreamClose(stream, 0)
		return fmt.Errorf("archive download aborted")
	}
	if err != nil {
		_ = ipc.WriteResultError(stream, 0, fmt.Sprintf("archive creation failed: %v", err), 500)
		_ = ipc.WriteStreamClose(stream, 0)
		return fmt.Errorf("create archive: %w", err)
	}

	// Open archive for streaming
	archiveFile, err := os.Open(tempPath)
	if err != nil {
		_ = ipc.WriteResultError(stream, 0, fmt.Sprintf("cannot open archive: %v", err), 500)
		_ = ipc.WriteStreamClose(stream, 0)
		return fmt.Errorf("open archive: %w", err)
	}
	defer archiveFile.Close()

	archiveStat, _ := archiveFile.Stat()
	archiveSize := archiveStat.Size()

	// Update progress for streaming phase
	_ = ipc.WriteProgress(stream, 0, FileProgress{
		Total: archiveSize,
		Phase: "streaming",
	})

	// Stream archive chunks
	buf := make([]byte, chunkSize)
	var bytesSent int64
	lastProgress = 0

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

			if bytesSent-lastProgress >= progressIntervalDownload || bytesSent == archiveSize {
				pct := 0
				if archiveSize > 0 {
					pct = int(bytesSent * 100 / archiveSize)
				}
				_ = ipc.WriteProgress(stream, 0, FileProgress{
					Bytes: bytesSent,
					Total: archiveSize,
					Pct:   pct,
					Phase: "streaming",
				})
				lastProgress = bytesSent
			}
		}

		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			_ = ipc.WriteResultError(stream, 0, fmt.Sprintf("read error: %v", readErr), 500)
			_ = ipc.WriteStreamClose(stream, 0)
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

	// Send success result
	_ = ipc.WriteResultOK(stream, 0, map[string]any{
		"archiveName": archiveName,
		"size":        archiveSize,
		"format":      format,
	})

	// Close stream
	_ = ipc.WriteStreamClose(stream, 0)

	logger.Infof("[FBStream] Archive download complete: files=%d size=%d", len(paths), archiveSize)
	return nil
}

// handleCompress creates an archive from provided paths and saves it to disk.
// args: [format, destination, path1, path2, ...]
func handleCompress(stream net.Conn, args []string) error {
	if len(args) < 3 {
		_ = ipc.WriteResultError(stream, 0, "missing format, destination, or paths", 400)
		_ = ipc.WriteStreamClose(stream, 0)
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
		_ = ipc.WriteResultError(stream, 0, fmt.Sprintf("unsupported format: %s", format), 400)
		_ = ipc.WriteStreamClose(stream, 0)
		return ipc.ErrUnsupportedFormat
	}

	// Ensure destination has correct extension
	targetPath := filepath.Clean(destination)
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

	// Check if destination exists
	if info, statErr := os.Stat(targetPath); statErr == nil {
		if info.IsDir() {
			_ = ipc.WriteResultError(stream, 0, "destination is a directory", 400)
			_ = ipc.WriteStreamClose(stream, 0)
			return fmt.Errorf("destination is a directory")
		}
		// Remove existing file (overwrite)
		if rmErr := os.Remove(targetPath); rmErr != nil {
			_ = ipc.WriteResultError(stream, 0, fmt.Sprintf("cannot remove existing file: %v", rmErr), 500)
			_ = ipc.WriteStreamClose(stream, 0)
			return fmt.Errorf("remove existing file: %w", rmErr)
		}
	}

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(targetPath), services.PermDir); err != nil {
		_ = ipc.WriteResultError(stream, 0, fmt.Sprintf("cannot create parent directory: %v", err), 500)
		_ = ipc.WriteStreamClose(stream, 0)
		return fmt.Errorf("create parent dir: %w", err)
	}

	// Set up abort monitoring
	cancelFn, cleanup := ipc.AbortMonitor(stream)
	defer cleanup()

	// Compute total size for progress
	totalSize, err := services.ComputeArchiveSize(paths)
	if err != nil {
		logger.Debugf("[FBStream] Failed to compute archive size: %v", err)
		totalSize = 0
	}

	// Send initial progress
	_ = ipc.WriteProgress(stream, 0, FileProgress{
		Total: totalSize,
		Phase: "preparing",
	})

	// Create callbacks
	var bytesProcessed int64
	var lastProgress int64
	opts := &ipc.OperationCallbacks{
		Progress: func(n int64) {
			bytesProcessed += n
			if totalSize > 0 && (bytesProcessed-lastProgress >= progressIntervalDownload || bytesProcessed >= totalSize) {
				pct := int(bytesProcessed * 100 / totalSize)
				if pct > 100 {
					pct = 100
				}
				_ = ipc.WriteProgress(stream, 0, FileProgress{
					Bytes: bytesProcessed,
					Total: totalSize,
					Pct:   pct,
					Phase: "compressing",
				})
				lastProgress = bytesProcessed
			}
		},
		Cancel: cancelFn,
	}

	// Create archive
	switch format {
	case "zip":
		err = services.CreateZip(targetPath, opts, targetPath, paths...)
	case "tar.gz":
		err = services.CreateTarGz(targetPath, opts, targetPath, paths...)
	}
	if err == ipc.ErrAborted {
		logger.Infof("[FBStream] Compress aborted, cleaning up: %s", targetPath)
		os.Remove(targetPath)
		_ = ipc.WriteResultError(stream, 0, "operation aborted", 499)
		_ = ipc.WriteStreamClose(stream, 0)
		return fmt.Errorf("compression aborted")
	}
	if err != nil {
		os.Remove(targetPath) // Clean up partial file on error
		_ = ipc.WriteResultError(stream, 0, fmt.Sprintf("compression failed: %v", err), 500)
		_ = ipc.WriteStreamClose(stream, 0)
		return fmt.Errorf("create archive: %w", err)
	}

	// Get final archive size
	var archiveSize int64
	if info, err := os.Stat(targetPath); err == nil {
		archiveSize = info.Size()
		// Notify indexer
		go func() {
			if err := addToIndexer(targetPath, info); err != nil {
				logger.Debugf("[FBStream] Failed to update indexer: %v", err)
			}
		}()
	}

	// Send success result
	_ = ipc.WriteResultOK(stream, 0, map[string]any{
		"path":   targetPath,
		"size":   archiveSize,
		"format": format,
	})

	// Close stream
	_ = ipc.WriteStreamClose(stream, 0)

	logger.Infof("[FBStream] Compress complete: path=%s files=%d size=%d", targetPath, len(paths), archiveSize)
	return nil
}

// handleExtract extracts an archive to a destination directory.
// args: [archivePath, destination?]
func handleExtract(stream net.Conn, args []string) error {
	if len(args) < 1 {
		_ = ipc.WriteResultError(stream, 0, "missing archive path", 400)
		_ = ipc.WriteStreamClose(stream, 0)
		return fmt.Errorf("missing archive path")
	}

	archivePath := filepath.Clean(args[0])

	// Determine destination
	var destination string
	if len(args) > 1 && args[1] != "" {
		destination = filepath.Clean(args[1])
	} else {
		destination = defaultExtractDestination(archivePath)
	}

	// Check archive exists
	archiveStat, err := os.Stat(archivePath)
	if err != nil {
		_ = ipc.WriteResultError(stream, 0, fmt.Sprintf("archive not found: %v", err), 404)
		_ = ipc.WriteStreamClose(stream, 0)
		return fmt.Errorf("archive not found: %w", err)
	}
	if archiveStat.IsDir() {
		_ = ipc.WriteResultError(stream, 0, "path is a directory, not an archive", 400)
		_ = ipc.WriteStreamClose(stream, 0)
		return ipc.ErrIsDirectory
	}

	// Set up abort monitoring
	cancelFn, cleanup := ipc.AbortMonitor(stream)
	defer cleanup()

	// Compute total size for progress (uncompressed size)
	totalSize, err := services.ComputeExtractSize(archivePath)
	if err != nil {
		logger.Debugf("[FBStream] Failed to compute extract size: %v", err)
		totalSize = 0
	}

	// Send initial progress
	_ = ipc.WriteProgress(stream, 0, FileProgress{
		Total: totalSize,
		Phase: "preparing",
	})

	// Create callbacks
	var bytesProcessed int64
	var lastProgress int64
	opts := &ipc.OperationCallbacks{
		Progress: func(n int64) {
			bytesProcessed += n
			if totalSize > 0 && (bytesProcessed-lastProgress >= progressIntervalDownload || bytesProcessed >= totalSize) {
				pct := int(bytesProcessed * 100 / totalSize)
				if pct > 100 {
					pct = 100
				}
				_ = ipc.WriteProgress(stream, 0, FileProgress{
					Bytes: bytesProcessed,
					Total: totalSize,
					Pct:   pct,
					Phase: "extracting",
				})
				lastProgress = bytesProcessed
			}
		},
		Cancel: cancelFn,
	}

	// Extract archive
	err = services.ExtractArchive(archivePath, destination, opts)
	if err == ipc.ErrAborted {
		logger.Infof("[FBStream] Extract aborted")
		_ = ipc.WriteResultError(stream, 0, "operation aborted", 499)
		_ = ipc.WriteStreamClose(stream, 0)
		return fmt.Errorf("extraction aborted")
	}
	if err != nil {
		_ = ipc.WriteResultError(stream, 0, fmt.Sprintf("extraction failed: %v", err), 500)
		_ = ipc.WriteStreamClose(stream, 0)
		return fmt.Errorf("extract archive: %w", err)
	}

	// Notify indexer about extracted files (non-blocking)
	go func() {
		_ = filepath.Walk(destination, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if err := addToIndexer(path, info); err != nil {
				logger.Debugf("[FBStream] Failed to update indexer for %s: %v", path, err)
			}
			return nil
		})
	}()

	// Send success result
	_ = ipc.WriteResultOK(stream, 0, map[string]any{
		"destination": destination,
	})

	// Close stream
	_ = ipc.WriteStreamClose(stream, 0)

	logger.Infof("[FBStream] Extract complete: archive=%s destination=%s", archivePath, destination)
	return nil
}
