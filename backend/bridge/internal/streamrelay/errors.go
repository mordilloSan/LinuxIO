package streamrelay

import (
	"context"
	"errors"
	"io"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	ipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
)

// ErrorCode is the bridgeipc error code of err, or 500.
func ErrorCode(err error) int {
	var bridgeErr *bridgeipc.Error
	if errors.As(err, &bridgeErr) && bridgeErr.Code != 0 {
		return bridgeErr.Code
	}
	return 500
}

// WriteError writes err as an error result and closes the stream.
func WriteError(w io.Writer, streamID uint32, err error) error {
	return ipc.WriteResultErrorAndClose(w, streamID, err.Error(), ErrorCode(err))
}

// Canceled reports whether err is the result of the caller going away.
func Canceled(ctx context.Context, err error) bool {
	return errors.Is(err, context.Canceled) || ctx.Err() != nil
}

// WriteErrorUnlessCanceled is WriteError, except that a stream the client
// already abandoned is just closed.
func WriteErrorUnlessCanceled(ctx context.Context, w io.Writer, streamID uint32, err error) error {
	if Canceled(ctx, err) {
		return ipc.WriteStreamClose(w, streamID)
	}
	return WriteError(w, streamID, err)
}
