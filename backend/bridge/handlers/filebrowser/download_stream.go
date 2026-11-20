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
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/services"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/streamrelay"
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

	file, size, err := openDownloadFile(ctx, req.Path)
	if err != nil {
		return streamrelay.WriteError(stream, 0, err)
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

func openDownloadFile(ctx context.Context, requestPath string) (*os.File, int64, error) {
	if requestPath == "" {
		return nil, 0, fmt.Errorf("%w: missing file path", bridgeipc.ErrInvalidArgs)
	}
	// A download reads, so every link on the way is followed, an absolute
	// target included, and only a regular file is opened at all (see
	// services.OpenRegularForRead).
	file, info, err := services.OpenRegularForRead(ctx, requestPath)
	if errors.Is(err, services.ErrNotRegularFile) {
		return nil, 0, downloadable(info)
	}
	if err != nil {
		return nil, 0, fsError("open file", err)
	}
	return file, info.Size(), nil
}

// downloadable refuses what is not a regular file.
func downloadable(info os.FileInfo) error {
	if info.IsDir() {
		return fmt.Errorf("%w: path is a directory, use archive download instead", bridgeipc.ErrInvalidArgs)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%w: path is not a regular file", bridgeipc.ErrInvalidArgs)
	}
	return nil
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
		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if readErr != nil {
			return fmt.Errorf("read file: %w", readErr)
		}
	}
}
