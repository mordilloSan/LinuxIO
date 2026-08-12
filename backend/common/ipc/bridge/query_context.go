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

	go monitorReadOwnedStream(stream, cancel, cancelOnClose, monitorDone)

	cleanup := func() {
		cancel()

		// Interrupt the monitor's read without owning or closing the stream.
		// The yamux caller remains responsible for the connection lifecycle.
		if err := stream.SetReadDeadline(time.Now()); err != nil {
			return
		}
		<-monitorDone
		_ = stream.SetReadDeadline(time.Time{})
		if cancelOnClose {
			_ = stream.SetWriteDeadline(time.Time{})
		}
	}
	return ctx, cleanup
}

func monitorReadOwnedStream(stream net.Conn, cancel context.CancelFunc, cancelOnClose bool, done chan<- struct{}) {
	defer close(done)
	for {
		frame, err := relay.ReadRelayFrame(stream)
		if err != nil {
			if cancelOnClose {
				cancelForClientEnd(stream, cancel, true)
			}
			return
		}
		switch frame.Opcode {
		case relay.OpStreamAbort:
			cancelForClientEnd(stream, cancel, cancelOnClose)
			return
		case relay.OpStreamClose:
			if cancelOnClose {
				cancelForClientEnd(stream, cancel, true)
			}
			return
		}
	}
}

func cancelForClientEnd(stream net.Conn, cancel context.CancelFunc, interruptWrite bool) {
	if interruptWrite {
		// A server-producing Channel may be blocked by backpressure on its
		// write side. Wake that write so cancellation cannot leak the producer.
		_ = stream.SetWriteDeadline(time.Now())
	}
	cancel()
}
