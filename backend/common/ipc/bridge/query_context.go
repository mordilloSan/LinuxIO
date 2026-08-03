package bridge

import (
	"context"
	"net"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
)

// queryAbortContext derives the context used by a request-response handler.
// Query handlers never read from the request stream themselves, so the router
// can centrally reserve its read side for explicit cancellation frames.
//
// A transport close is deliberately not cancellation. Queries and jobs both
// survive an ordinary client disconnect; only OpStreamAbort represents the
// caller explicitly abandoning a Query.
func queryAbortContext(parent context.Context, stream net.Conn) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)
	monitorDone := make(chan struct{})

	go func() {
		defer close(monitorDone)
		for {
			frame, err := relay.ReadRelayFrame(stream)
			if err != nil {
				return
			}
			switch frame.Opcode {
			case relay.OpStreamAbort:
				cancel()
				return
			case relay.OpStreamClose:
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
