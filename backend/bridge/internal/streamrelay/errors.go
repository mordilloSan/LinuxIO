package streamrelay

import (
	"context"
	"errors"
	"io"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	ipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
)

// WriteError writes err as an error result, closes the stream, and returns
// err so a duplex handler can hand the failure to the router, which logs it.
func WriteError(w io.Writer, streamID uint32, err error) error {
	if writeErr := ipc.WriteResultErrorAndClose(w, streamID, err.Error(), bridgeipc.StatusCode(err)); writeErr != nil {
		return errors.Join(err, writeErr)
	}
	return err
}

// Canceled reports whether err is the result of the caller going away.
func Canceled(ctx context.Context, err error) bool {
	return errors.Is(err, context.Canceled) || ctx.Err() != nil
}

// WriteErrorUnlessCanceled is WriteError, except that a stream the client
// already abandoned is just closed: the route ended normally, so nothing is
// returned for the router to log.
func WriteErrorUnlessCanceled(ctx context.Context, w io.Writer, streamID uint32, err error) error {
	if Canceled(ctx, err) {
		return ipc.WriteStreamClose(w, streamID)
	}
	return WriteError(w, streamID, err)
}
