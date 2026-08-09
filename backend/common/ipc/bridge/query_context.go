package bridge

import (
	"context"
	"net"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
)

// requestAbortContext derives the context used by a request-response handler.
// Call handlers never read from the request stream themselves, so the router
// can centrally reserve its read side for explicit cancellation frames.
//
// A transport close is deliberately not cancellation. Calls and Tasks both
// survive an ordinary client disconnect; only OpStreamAbort represents the
// caller explicitly abandoning a Call.
func requestAbortContext(parent context.Context, stream net.Conn) (context.Context, func()) {
	return readOwnedStreamContext(parent, stream, false)
}

// ReceiveOnlyChannelContext derives a context for a server-producing channel.
// The monitor owns the connection's read side and turns client abort/close or
// disconnect into cancellation.
func ReceiveOnlyChannelContext(parent context.Context, stream net.Conn) (context.Context, func()) {
	return readOwnedStreamContext(parent, stream, true)
}

func readOwnedStreamContext(parent context.Context, stream net.Conn, cancelOnClose bool) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)
	monitorDone := make(chan struct{})

	go func() {
		defer close(monitorDone)
		for {
			frame, err := relay.ReadRelayFrame(stream)
			if err != nil {
				if cancelOnClose {
					cancel()
				}
				return
			}
			switch frame.Opcode {
			case relay.OpStreamAbort:
				cancel()
				return
			case relay.OpStreamClose:
				if cancelOnClose {
					cancel()
				}
				return
			}
		}
	}()

	cleanup := func() {
		cancel()

		// Interrupt the monitor's read without owning or closing the stream.
		// The yamux caller remains responsible for the connection lifecycle.
		if err := stream.SetReadDeadline(time.Now()); err != nil {
			return
		}
		<-monitorDone
		_ = stream.SetReadDeadline(time.Time{})
	}
	return ctx, cleanup
}
