package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
)

const streamOpenReadTimeout = 5 * time.Second

// handleYamuxSession handles a yamux multiplexed connection.
// Each stream within the session is treated as an independent request.
func handleYamuxSession(ctx context.Context, rt runtime.Runtime, router *bridgeipc.Router, conn net.Conn) {
	ymuxSession, err := relay.NewYamuxServer(conn)
	if err != nil {
		slog.Error("failed to create yamux session", "session_id", rt.Session.SessionID, "error", err)
		return
	}
	defer ymuxSession.Close()
	slog.Info("yamux session started", "session_id", rt.Session.SessionID)

	// Track active streams for graceful shutdown.
	var streamWg sync.WaitGroup
	var streamCounter uint64

	// Accept streams until session closes or bridge shuts down.
	// The loop exits when ymuxSession.Accept() returns an error
	// (e.g., the session is closed by the shutdown goroutine).
	for {
		stream, err := ymuxSession.Accept()
		if err != nil {
			if ymuxSession.IsClosed() {
				slog.Debug("yamux session closed", "session_id", rt.Session.SessionID)
			} else {
				slog.Warn("yamux accept error", "session_id", rt.Session.SessionID, "error", err)
			}
			break
		}

		streamCounter++
		streamID := strconv.FormatUint(streamCounter, 10)
		streamWg.Go(func() {
			defer stream.Close()

			handleYamuxStream(ctx, rt, router, stream, streamID)
		})
	}

	// Wait for all streams to complete.
	streamWg.Wait()
	slog.Info("yamux session ended", "session_id", rt.Session.SessionID)
}

// handleYamuxStream handles a single stream within a yamux session.
// Reads the OpStreamOpen frame, looks up the registered handler, and executes it.
func handleYamuxStream(ctx context.Context, rt runtime.Runtime, router *bridgeipc.Router, stream net.Conn, streamID string) {
	sess := rt.Session
	// Read the first frame to determine stream type.
	_ = stream.SetReadDeadline(time.Now().Add(streamOpenReadTimeout))
	frame, err := relay.ReadRelayFrameProgressive(stream)
	// The deadline is only for the initial frame; handlers may legitimately run
	// for much longer and control subsequent stream I/O themselves.
	_ = stream.SetReadDeadline(time.Time{})
	if err != nil {
		slog.Warn("failed to read stream open frame", "session_id", sess.SessionID, "stream_id", streamID, "error", err)
		return
	}

	if frame.Opcode != relay.OpStreamOpen {
		slog.Warn("expected OpStreamOpen frame", "session_id", sess.SessionID, "stream_id", streamID, "opcode", fmt.Sprintf("0x%02x", frame.Opcode))
		_ = relay.WriteResultErrorAndClose(stream, 0, "expected stream open frame", 400)
		return
	}

	envelope, err := relay.ParseStreamOpenPayload(frame.Payload)
	if err != nil {
		slog.Warn("failed to parse stream open payload", "session_id", sess.SessionID, "stream_id", streamID, "error", err)
		_ = relay.WriteResultErrorAndClose(stream, 0, err.Error(), 400)
		return
	}

	if err := router.Dispatch(ctx, stream, bridgeipc.Request{
		Route:      envelope.Route,
		RawRequest: envelope.Request,
		Session:    sess,
	}); err != nil {
		return
	}
}
