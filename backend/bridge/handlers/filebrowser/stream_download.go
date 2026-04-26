package filebrowser

import (
	"fmt"
	"io"
	"log/slog"
	"net"
	"path/filepath"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

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
	logWriteErr("progress", ipc.WriteProgress(stream, 0, FileProgress{
		Total: totalSize,
		Phase: "starting",
	}))

	file, err := root.Root.Open(realRel)
	if err != nil {
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("cannot open file: %v", err), 500))
		return fmt.Errorf("cannot open file: %w", err)
	}
	defer file.Close()

	if err := streamFileChunks(stream, file, totalSize, chunkSize); err != nil {
		return err
	}

	logWriteErr("ok+close", ipc.WriteResultOKAndClose(stream, 0, map[string]any{
		"path":     path,
		"size":     totalSize,
		"fileName": filepath.Base(realPath),
	}))
	slog.Info("download complete", "path", path, "size", totalSize)
	return nil
}

func streamFileChunks(stream net.Conn, file io.Reader, totalSize int64, chunkSize int) error {
	buf := make([]byte, chunkSize)
	var bytesRead int64
	var lastProgress int64

	for {
		n, readErr := file.Read(buf)
		if n > 0 {
			if err := ipc.WriteRelayFrame(stream, &ipc.StreamFrame{
				Opcode:   ipc.OpStreamData,
				StreamID: 0,
				Payload:  buf[:n],
			}); err != nil {
				return fmt.Errorf("write data chunk: %w", err)
			}

			bytesRead += int64(n)

			if bytesRead-lastProgress >= progressReportIntervalBytes || bytesRead == totalSize {
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
	return nil
}
