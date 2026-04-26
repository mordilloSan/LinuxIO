package filebrowser

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/services"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

type uploadAttributes struct {
	mode        os.FileMode
	uid         int
	gid         int
	hasExisting bool
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
		if *bytesWritten-*lastProgress >= uploadProgressAckIntervalBytes || *bytesWritten == expectedSize {
			writeUploadProgress(stream, *bytesWritten, expectedSize)
			*lastProgress = *bytesWritten
		}
		return false, nil
	case ipc.OpStreamClose:
		return true, nil
	case ipc.OpStreamAbort:
		return false, ipc.ErrAborted
	default:
		slog.Debug("ignoring filebrowser stream opcode", "opcode", fmt.Sprintf("0x%02x", frame.Opcode))
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
			slog.Debug("failed to restore uploaded file permissions", "path", realRel, "error", err)
		}
		if err := root.Root.Chown(realRel, attrs.uid, attrs.gid); err != nil {
			slog.Debug("failed to restore uploaded file ownership", "path", realRel, "error", err)
		}
		return
	}
	if err := root.Root.Chmod(realRel, services.PermFile); err != nil {
		slog.Debug("failed to set uploaded file permissions", "path", realRel, "error", err)
	}
}

func notifyUploadedFile(path string, info os.FileInfo) {
	go func(stat os.FileInfo) {
		if err := addToIndexer(path, stat); err != nil {
			slog.Debug("failed to update indexer after upload", "path", path, "error", err)
		}
	}(info)
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
				slog.Debug("failed to remove partial upload", "path", realPath, "error", removeErr)
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

	logWriteErr("ok+close", ipc.WriteResultOKAndClose(stream, 0, map[string]any{
		"path": path,
		"size": bytesWritten,
	}))
	slog.Info("upload complete", "path", path, "size", bytesWritten)
	return nil
}
