package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
)

// Atomic counter for stream IDs used by transport logs.
var streamCounter atomic.Uint64

// handleYamuxSession handles a yamux multiplexed connection.
// Each stream within the session is treated as an independent request.
func handleYamuxSession(ctx context.Context, rt runtime.Runtime, router *bridgeipc.Router, conn net.Conn) {
	ymuxSession, err := relay.NewYamuxServer(conn)
	if err != nil {
		slog.Error("failed to create yamux session", "session_id", sess.SessionID, "error", err)
		return
	}
	defer ymuxSession.Close()
	slog.Info("yamux session started", "session_id", sess.SessionID)

	// Track active streams for graceful shutdown.
	var streamWg sync.WaitGroup

	// Accept streams until session closes or bridge shuts down.
	// The loop exits when ymuxSession.Accept() returns an error
	// (e.g., the session is closed by the shutdown goroutine).
	for {
		stream, err := ymuxSession.Accept()
		if err != nil {
			if ymuxSession.IsClosed() {
				slog.Debug("yamux session closed", "session_id", sess.SessionID)
			} else {
				slog.Warn("yamux accept error", "session_id", sess.SessionID, "error", err)
			}
			break
		}

		streamID := strconv.FormatUint(streamCounter.Add(1), 10)
		s := stream
		sid := streamID
		streamWg.Go(func() {
			defer s.Close()

			handleYamuxStream(ctx, rt, router, s, sid)
		})
	}

	// Wait for all streams to complete.
	streamWg.Wait()
	slog.Info("yamux session ended", "session_id", sess.SessionID)
}

// handleYamuxStream handles a single stream within a yamux session.
// Reads the OpStreamOpen frame, looks up the registered handler, and executes it.
func handleYamuxStream(ctx context.Context, rt runtime.Runtime, router *bridgeipc.Router, stream net.Conn, streamID string) {
	sess := rt.Session
	// Read the first frame to determine stream type.
	frame, err := relay.ReadRelayFrame(stream)
	if err != nil {
		slog.Warn("failed to read stream open frame", "session_id", sess.SessionID, "stream_id", streamID, "error", err)
		return
	}

	if frame.Opcode != relay.OpStreamOpen {
		slog.Warn("expected OpStreamOpen frame", "session_id", sess.SessionID, "stream_id", streamID, "opcode", fmt.Sprintf("0x%02x", frame.Opcode))
		return
	}

	// Parse stream type and args from payload.
	route, args := relay.ParseStreamOpenPayload(frame.Payload)

	if err := router.Dispatch(ctx, stream, bridgeipc.Request{
		Route:   route,
		Args:    args,
		Session: sess,
	}); err != nil {
		return
	}
}
