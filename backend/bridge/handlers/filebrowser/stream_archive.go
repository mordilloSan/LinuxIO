package filebrowser

import (
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

func writePhaseProgress(stream net.Conn, total int64, phase string) {
	logWriteErr("progress", ipc.WriteProgress(stream, 0, FileProgress{
		Total: total,
		Phase: phase,
	}))
}

func newPhaseCallbacks(stream net.Conn, cancelFn ipc.CancelFunc, totalSize int64, phase string) *ipc.OperationCallbacks {
	pt := ipc.NewProgressTracker(stream, 0, progressReportIntervalBytes)
	return pt.AsCallback(cancelFn, func(processed, total int64) any {
		return FileProgress{
			Bytes: processed,
			Total: total,
			Pct:   min(int(processed*100/total), 100),
			Phase: phase,
		}
	}, totalSize)
}

func streamArchiveFile(stream net.Conn, archiveFile *os.File, archiveSize int64, chunkSize int) error {
	buf := make([]byte, chunkSize)
	streamPT := ipc.NewProgressTracker(stream, 0, progressReportIntervalBytes)
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
				slog.Debug("failed to write archive stream progress", "error", err)
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
		slog.Info("Archive download aborted")
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, "operation aborted", 499))
		return ipc.ErrAborted
	}
	if err != nil {
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("archive creation failed: %v", err), 500))
		return fmt.Errorf("create archive: %w", err)
	}

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

	logWriteErr("ok+close", ipc.WriteResultOKAndClose(stream, 0, map[string]any{
		"archiveName": archiveNameForPaths(paths, extension),
		"size":        archiveSize,
		"format":      format,
	}))
	slog.Info("archive download complete", "count", len(paths), "size", archiveSize, "format", format)
	return nil
}
