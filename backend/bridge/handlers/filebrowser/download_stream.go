package filebrowser

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
)

const routeDownloadStream = "filebrowser.download_stream"

type downloadStreamProgress struct {
	FileProgress
	FileName string `json:"fileName"`
}

// streamFileDownload is the bridge-side source for the native HTTP download
// route. A single file needs no background task: validation, size discovery,
// and streaming all belong to the lifetime of the HTTP request.
func streamFileDownload(parent context.Context, stream net.Conn, req apischema.PathRequest) error {
	ctx, cleanup := bridgeipc.ReceiveOnlyChannelContext(parent, stream)
	defer cleanup()

	file, size, err := openDownloadFile(req.Path)
	if err != nil {
		return writeFileDownloadError(stream, err)
	}
	defer file.Close()

	progress := downloadStreamProgress{
		Total: size, Phase: "streaming",
		FileName: filepath.Base(filepath.Clean(req.Path)),
	}
	if err := relay.WriteProgress(stream, 0, progress); err != nil {
		return err
	}
	if err := copyDownloadFile(ctx, stream, file); err != nil {
		if errors.Is(err, context.Canceled) || ctx.Err() != nil {
			return relay.WriteStreamClose(stream, 0)
		}
		return err
	}
	return relay.WriteResultOKAndClose(stream, 0, nil)
}

func openDownloadFile(requestPath string) (*os.File, int64, error) {
	if requestPath == "" {
		return nil, 0, bridgeipc.NewError("missing file path", 400)
	}

	root, err := fsroot.Open()
	if err != nil {
		return nil, 0, fmt.Errorf("failed to access filesystem: %w", err)
	}
	defer root.Close()

	cleanPath := filepath.Clean(requestPath)
	file, err := root.Root.Open(fsroot.ToRel(cleanPath))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, 0, bridgeipc.NewError("file not found", 404)
		}
		return nil, 0, fmt.Errorf("open file: %w", err)
	}
	stat, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, 0, fmt.Errorf("stat file: %w", err)
	}
	if stat.IsDir() {
		file.Close()
		return nil, 0, bridgeipc.NewError("path is a directory, use archive download instead", 400)
	}
	return file, stat.Size(), nil
}

func copyDownloadFile(ctx context.Context, stream net.Conn, file io.Reader) error {
	buf := make([]byte, progressReportIntervalBytes)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, readErr := file.Read(buf)
		if n > 0 {
			if err := relay.WriteRelayFrame(stream, &relay.StreamFrame{
				Opcode:  relay.OpStreamData,
				Payload: buf[:n],
			}); err != nil {
				return err
			}
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return fmt.Errorf("read file: %w", readErr)
		}
	}
}

func writeFileDownloadError(stream net.Conn, err error) error {
	code := 500
	var bridgeErr *bridgeipc.Error
	if errors.As(err, &bridgeErr) && bridgeErr.Code != 0 {
		code = bridgeErr.Code
	}
	return relay.WriteResultErrorAndClose(stream, 0, err.Error(), code)
}
