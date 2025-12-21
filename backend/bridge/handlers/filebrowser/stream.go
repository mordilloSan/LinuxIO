package filebrowser

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"

	"github.com/mordilloSan/go_logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/services"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

const (
	// chunkSize is the size of data chunks for file transfers (512KB for high throughput)
	chunkSize = 512 * 1024
	// progressInterval is how often to send progress updates (every 2MB to reduce overhead)
	progressInterval = 2 * 1024 * 1024
)

// HandleFilebrowserStream handles a yamux stream for filebrowser operations.
// streamType is one of: fb-download, fb-upload, fb-archive
// args contains operation-specific parameters
func HandleFilebrowserStream(sess *session.Session, stream net.Conn, streamType string, args []string) error {
	logger.Debugf("[FBStream] Starting type=%s args=%v", streamType, args)

	switch streamType {
	case ipc.StreamTypeFBDownload:
		return handleDownload(stream, args)
	case ipc.StreamTypeFBUpload:
		return handleUpload(stream, args)
	case ipc.StreamTypeFBArchive:
		return handleArchiveDownload(stream, args)
	default:
		logger.Warnf("[FBStream] Unknown stream type: %s", streamType)
		_ = ipc.WriteResultError(stream, 0, fmt.Sprintf("unknown stream type: %s", streamType), 400)
		_ = ipc.WriteStreamClose(stream, 0)
		return fmt.Errorf("unknown stream type: %s", streamType)
	}
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
		return fmt.Errorf("path is a directory")
	}

	totalSize := stat.Size()

	// Send initial progress with total size
	_ = ipc.WriteProgressFrame(stream, 0, &ipc.ProgressFrame{
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
			if bytesRead-lastProgress >= progressInterval || bytesRead == totalSize {
				pct := 0
				if totalSize > 0 {
					pct = int(bytesRead * 100 / totalSize)
				}
				_ = ipc.WriteProgressFrame(stream, 0, &ipc.ProgressFrame{
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
	_ = ipc.WriteProgressFrame(stream, 0, &ipc.ProgressFrame{
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

				// Send progress update periodically
				if bytesWritten-lastProgress >= progressInterval || bytesWritten == expectedSize {
					pct := 0
					if expectedSize > 0 {
						pct = int(bytesWritten * 100 / expectedSize)
					}
					_ = ipc.WriteProgressFrame(stream, 0, &ipc.ProgressFrame{
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

	// Set permissions
	if err := os.Chmod(realPath, services.PermFile); err != nil {
		logger.Debugf("[FBStream] Failed to set permissions: %v", err)
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
		return fmt.Errorf("unsupported format: %s", format)
	}

	// Compute total size for progress
	totalSize, err := services.ComputeArchiveSize(paths)
	if err != nil {
		logger.Debugf("[FBStream] Failed to compute archive size: %v", err)
		totalSize = 0
	}

	// Send initial progress
	_ = ipc.WriteProgressFrame(stream, 0, &ipc.ProgressFrame{
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

	// Create archive with progress callback
	var bytesProcessed int64
	var lastProgress int64
	progressCb := func(n int64) {
		bytesProcessed += n
		if totalSize > 0 && (bytesProcessed-lastProgress >= progressInterval || bytesProcessed >= totalSize) {
			pct := int(bytesProcessed * 100 / totalSize)
			if pct > 100 {
				pct = 100
			}
			_ = ipc.WriteProgressFrame(stream, 0, &ipc.ProgressFrame{
				Bytes: bytesProcessed,
				Total: totalSize,
				Pct:   pct,
				Phase: "compressing",
			})
			lastProgress = bytesProcessed
		}
	}

	// Create archive
	switch format {
	case "zip":
		err = services.CreateZip(tempPath, progressCb, tempPath, paths...)
	case "tar.gz":
		err = services.CreateTarGz(tempPath, progressCb, tempPath, paths...)
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
	_ = ipc.WriteProgressFrame(stream, 0, &ipc.ProgressFrame{
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

			if bytesSent-lastProgress >= progressInterval || bytesSent == archiveSize {
				pct := 0
				if archiveSize > 0 {
					pct = int(bytesSent * 100 / archiveSize)
				}
				_ = ipc.WriteProgressFrame(stream, 0, &ipc.ProgressFrame{
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
