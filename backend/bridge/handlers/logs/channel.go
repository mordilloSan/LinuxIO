package logs

import (
	"context"
	"net"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/streamrelay"
)

func writeLogError(stream net.Conn, err error) error {
	return streamrelay.WriteError(stream, 0, err)
}

func writeLogErrorUnlessCanceled(ctx context.Context, stream net.Conn, err error) error {
	return streamrelay.WriteErrorUnlessCanceled(ctx, stream, 0, err)
}
