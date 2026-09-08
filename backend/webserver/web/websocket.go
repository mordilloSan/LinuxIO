package web

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	yamux "github.com/libp2p/go-yamux/v5"

	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/webserver/bridge"
)

// WebSocket keepalive configuration
const (
	// How often to send ping frames to the client
	pingInterval = 25 * time.Second

	// How long to wait for a pong response before considering connection dead
	// This is the read deadline - must be longer than pingInterval to allow
	// the ping/pong cycle to complete even when no data is being sent
	pongWait = 35 * time.Second // pingInterval + 10 seconds buffer

	// Maximum time allowed to write a message (ping or data)
	writeWait = 10 * time.Second

	bridgeWriteWait = 20 * time.Second
	// Queue messages independently so a slow bridge consumer cannot hold up
	// other streams or WebSocket control frames. Message sizes remain unrestricted.
	streamWriteQueueSize = 16
	relayReadBufferSize  = 32 * 1024
)

// Stream flags for WebSocket binary protocol
const (
	FlagSYN  byte = 0x01 // Open new stream
	FlagDATA byte = 0x04 // Data frame
	FlagFIN  byte = 0x08 // Close stream
	FlagRST  byte = 0x10 // Abort stream
	// FlagActivity is outer relay metadata. It is observed and stripped by the relay; payload bytes are never inspected.
	FlagActivity byte = 0x20
)

// streamRelay manages the mapping of streamID to yamux stream
type streamRelay struct {
	mu      sync.RWMutex
	streams map[uint32]*relayStream
	ws      *websocket.Conn
	ctx     context.Context
	cancel  context.CancelFunc
	wsMu    sync.Mutex
	closed  atomic.Bool
	wg      sync.WaitGroup
}

type relayStream struct {
	id      uint32
	stream  net.Conn // Attached under streamRelay.mu before its reader starts.
	pending chan relayWrite
	cancel  context.CancelFunc
}

type relayWrite struct {
	flags   byte
	payload []byte
}

// Gorilla's default checks Origin against Host, including the port. HTTP's
// CrossOriginProtection allows GET and therefore does not protect upgrades.
var upgrader = websocket.Upgrader{}

// wsConnsBySession tracks all active WebSocket connections for each session.
// Multiple tabs/windows can share the same session, each with their own WebSocket.
var wsConnsBySession = struct {
	sync.Mutex
	conns map[string]map[*websocket.Conn]struct{}
}{conns: make(map[string]map[*websocket.Conn]struct{})}

// addWebSocketForSession registers a WebSocket connection for a session.
func addWebSocketForSession(sessionID string, conn *websocket.Conn) {
	wsConnsBySession.Lock()
	defer wsConnsBySession.Unlock()
	if wsConnsBySession.conns[sessionID] == nil {
		wsConnsBySession.conns[sessionID] = make(map[*websocket.Conn]struct{})
	}
	wsConnsBySession.conns[sessionID][conn] = struct{}{}
}

// removeWebSocketForSession unregisters a WebSocket connection from a session.
func removeWebSocketForSession(sessionID string, conn *websocket.Conn) {
	wsConnsBySession.Lock()
	defer wsConnsBySession.Unlock()
	conns := wsConnsBySession.conns[sessionID]
	delete(conns, conn)
	if len(conns) == 0 {
		delete(wsConnsBySession.conns, sessionID)
	}
}

// CloseWebSocketForSession closes ALL WebSocket connections associated with a session.
// Called when a session expires to immediately disconnect all tabs/windows.
func CloseWebSocketForSession(sessionID string) {
	wsConnsBySession.Lock()
	conns := wsConnsBySession.conns[sessionID]
	delete(wsConnsBySession.conns, sessionID)
	wsConnsBySession.Unlock()

	// Detach atomically, then do network I/O outside the registry lock. A
	// concurrent registration must not be lost when this batch finishes.
	closeMsg := websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "Session expired")
	deadline := time.Now().Add(writeWait)
	for conn := range conns {
		if err := conn.WriteControl(websocket.CloseMessage, closeMsg, deadline); err != nil {
			slog.Debug("failed to write WebSocket close control frame",
				"session_ref", session.DiagnosticRef(sessionID), "error", err)
		}
		if err := conn.Close(); err != nil {
			slog.Debug("failed to close WebSocket",
				"session_ref", session.DiagnosticRef(sessionID), "error", err)
		}
	}
	slog.Debug("closed WebSockets for expired session",
		"session_ref", session.DiagnosticRef(sessionID), "count", len(conns))
}

func newStreamRelay(ctx context.Context, ws *websocket.Conn) *streamRelay {
	ctx, cancel := context.WithCancel(ctx)
	return &streamRelay{
		streams: make(map[uint32]*relayStream), ws: ws, ctx: ctx, cancel: cancel,
	}
}

func isExpectedWSClose(err error) bool {
	if ce, ok := errors.AsType[*websocket.CloseError](err); ok {
		switch ce.Code {
		case websocket.CloseNormalClosure, websocket.CloseGoingAway,
			websocket.CloseNoStatusReceived, websocket.CloseAbnormalClosure:
			return true
		}
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "use of closed network connection") ||
		strings.Contains(errStr, "i/o timeout")
}

func isExpectedStreamReadClose(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, yamux.ErrStreamReset) || errors.Is(err, yamux.ErrStreamClosed) {
		return true
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "use of closed network connection") ||
		strings.Contains(errStr, "i/o timeout") ||
		strings.Contains(errStr, "stream reset") ||
		strings.Contains(errStr, "stream closed")
}

// wsAuthMiddleware validates the session for WebSocket connections.
// Unlike RequireSession, it upgrades the WebSocket before rejecting invalid
// sessions, so auth failures are communicated as close code 1008 ("no-session")
// rather than HTTP 401 — which browsers cannot distinguish from network errors.
func wsAuthMiddleware(sm *session.Manager, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, err := sm.ValidateFromRequest(r)
		if err != nil {
			conn, upgradeErr := upgrader.Upgrade(w, r, nil)
			if upgradeErr != nil {
				slog.Debug("failed to upgrade unauthenticated WebSocket", "error", upgradeErr)
				return
			}
			closeMsg := websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "no-session")
			if writeErr := conn.WriteControl(websocket.CloseMessage, closeMsg, time.Now().Add(writeWait)); writeErr != nil {
				slog.Debug("failed to send no-session close", "error", writeErr)
			}
			// Wait briefly for the client to receive the close frame before
			// tearing down the TCP connection.  Without this, conn.Close()
			// can race the close frame and the browser sees code 1006
			// (abnormal closure) instead of 1008 (policy violation).
			_ = conn.SetReadDeadline(time.Now().Add(time.Second))
			for {
				if _, _, readErr := conn.NextReader(); readErr != nil {
					break
				}
			}
			conn.Close()
			return
		}
		next.ServeHTTP(w, r.WithContext(session.WithSession(r.Context(), sess)))
	})
}

func refreshSessionActivity(sm *session.Manager, sessionID string) error {
	if err := sm.Refresh(sessionID); err != nil {
		slog.Debug("failed to refresh WebSocket session",
			"session_ref", session.DiagnosticRef(sessionID),
			"error", err)
		return err
	}
	return nil
}

func validateSessionActivity(sm *session.Manager, sessionID string) error {
	_, err := sm.ValidateSession(sessionID)
	return err
}

// WebSocketRelayHandler handles binary WebSocket connections as a pure byte relay.
// The server never parses payloads - just routes bytes between WebSocket and yamux streams.
func WebSocketRelayHandler(sm *session.Manager) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess := session.SessionFromContext(r.Context())
		if sess == nil {
			slog.
				// Should not happen — wsAuthMiddleware guarantees session in context.
				Error("WebSocketRelayHandler: missing session in context")
			return
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			slog.Error("WebSocket upgrade failed", "error", err)
			return
		}

		relay := newStreamRelay(r.Context(), conn)

		// Track this WebSocket by session ID for session expiry handling
		// Multiple tabs/windows can share the same session
		addWebSocketForSession(sess.SessionID, conn)
		defer func() {
			removeWebSocketForSession(sess.SessionID, conn)
			relay.closeAll()
			relay.wg.Wait()
		}()
		// Session deletion can race the upgrade and registration. Either its
		// callback sees this connection, or this recheck closes it here.
		if err := validateSessionActivity(sm, sess.SessionID); err != nil {
			CloseWebSocketForSession(sess.SessionID)
			return
		}
		slog.Info("WebSocket connected", "user", sess.User.Username, "session_ref", session.DiagnosticRef(sess.SessionID))

		conn.SetPongHandler(func(string) error {
			slog.Debug("WebSocket pong received", "session_ref", session.DiagnosticRef(sess.SessionID), "deadline", pongWait)
			if err := conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
				slog.Debug("failed to set WebSocket read deadline in pong handler",
					"session_ref", session.DiagnosticRef(sess.SessionID),
					"error", err)
				return err
			}
			if err := validateSessionActivity(sm, sess.SessionID); err != nil {
				slog.Debug("WebSocket pong rejected for expired session", "session_ref", session.DiagnosticRef(sess.SessionID), "error", err)
				return err
			}
			return nil
		})
		// Set initial read deadline.
		slog.Debug("setting initial WebSocket read deadline",
			"session_ref", session.DiagnosticRef(sess.SessionID),
			"deadline", pongWait,
			"ping_interval", pingInterval)
		if err := conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
			slog.Warn("failed to set initial WebSocket read deadline",
				"session_ref", session.DiagnosticRef(sess.SessionID),
				"error", err)
			return
		}

		// Start ping goroutine to keep connection alive
		relay.wg.Go(relay.pingLoop)

		relay.readLoop(sm, sess)
		slog.Info("WebSocket disconnected", "user", sess.User.Username, "session_ref", session.DiagnosticRef(sess.SessionID))
	})
}

func (r *streamRelay) readLoop(sm *session.Manager, sess *session.Session) {
	for {
		messageType, data, err := r.ws.ReadMessage()
		if err != nil {
			if !isExpectedWSClose(err) {
				slog.Warn("WebSocket read error", "error", err)
			}
			return
		}

		if err := r.ws.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
			slog.Debug("failed to reset WebSocket read deadline", "error", err)
			return
		}
		if err := validateSessionActivity(sm, sess.SessionID); err != nil {
			slog.Debug("WebSocket frame rejected for expired session", "session_ref", session.DiagnosticRef(sess.SessionID), "error", err)
			r.closeAll()
			return
		}

		if messageType != websocket.BinaryMessage {
			slog.Debug("ignoring non-binary WebSocket message", "type", messageType)
			continue
		}

		if len(data) < 5 {
			slog.Warn("WebSocket frame too short", "size", len(data))
			continue
		}

		streamID := binary.BigEndian.Uint32(data[0:4])
		flags := data[4]
		payload := data[5:]
		flags, ok := r.consumeActivityFlag(sm, sess.SessionID, flags)
		if !ok {
			return
		}

		switch {
		case flags&FlagSYN != 0:
			r.handleSYN(sess, streamID, payload)
		case flags&FlagDATA != 0:
			r.enqueueFrame(streamID, FlagDATA, payload)
		case flags&FlagFIN != 0:
			r.enqueueFrame(streamID, FlagFIN, payload)
		case flags&FlagRST != 0:
			r.enqueueFrame(streamID, FlagRST, nil)
		}
	}
}

func (r *streamRelay) consumeActivityFlag(sm *session.Manager, sessionID string, flags byte) (byte, bool) {
	if flags&FlagActivity == 0 {
		return flags, true
	}
	if err := refreshSessionActivity(sm, sessionID); err != nil {
		r.closeAll()
		return 0, false
	}
	return flags &^ FlagActivity, true
}

// handleSYN registers the stream before opening it asynchronously. The reader
// can immediately enqueue subsequent DATA, FIN and RST in wire order.
func (r *streamRelay) handleSYN(sess *session.Session, streamID uint32, payload []byte) {
	r.mu.Lock()
	if r.closed.Load() || r.streams[streamID] != nil {
		r.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(r.ctx)
	rs := &relayStream{id: streamID, pending: make(chan relayWrite, streamWriteQueueSize), cancel: cancel}
	r.streams[streamID] = rs
	r.mu.Unlock()

	r.wg.Go(func() {
		yamuxSession, err := bridge.GetYamuxSession(sess.SessionID)
		if err != nil {
			slog.Debug("bridge unavailable", "session_ref", session.DiagnosticRef(sess.SessionID), "error", err)
			r.closeAll()
			return
		}
		openCtx, stopOpen := context.WithTimeout(ctx, 10*time.Second)
		stream, err := yamuxSession.Open(openCtx)
		stopOpen()
		if err != nil {
			slog.Debug("failed to open yamux stream", "stream_id", streamID, "error", err)
			r.finishStream(rs, FlagRST)
			return
		}
		r.relayToBridge(ctx, rs, stream, payload)
	})
}

// enqueueFrame never waits on bridge I/O. Overflow cancels only this stream;
// its owner interrupts the blocked write and notifies the browser of closure.
func (r *streamRelay) enqueueFrame(streamID uint32, flags byte, payload []byte) {
	r.mu.RLock()
	rs := r.streams[streamID]
	r.mu.RUnlock()
	if rs == nil {
		return
	}
	select {
	case rs.pending <- relayWrite{flags: flags, payload: payload}:
	default:
		slog.Debug("bridge stream write queue full", "stream_id", streamID)
		rs.cancel()
	}
}

func (r *streamRelay) relayToBridge(ctx context.Context, rs *relayStream, stream net.Conn, payload []byte) {
	defer r.finishStream(rs, FlagRST)
	r.mu.Lock()
	if r.closed.Load() || r.streams[rs.id] != rs || ctx.Err() != nil {
		r.mu.Unlock()
		_ = stream.Close()
		return
	}
	rs.stream = stream
	r.mu.Unlock()

	// Cancellation must interrupt a Write waiting for yamux window credit.
	// Closing the stream also wakes its reader. Join the callback on exit.
	closeDone := make(chan struct{})
	stopClose := context.AfterFunc(ctx, func() {
		_ = stream.Close()
		close(closeDone)
	})
	defer func() {
		if !stopClose() {
			<-closeDone
		}
	}()
	r.wg.Go(func() { r.relayFromBridge(rs) })

	if err := writeBridgePayload(stream, payload); err != nil {
		slog.Debug("failed to write SYN payload", "stream_id", rs.id, "error", err)
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case frame := <-rs.pending:
			// Preserve earlier DATA (including OpStreamAbort) before closing the
			// transport. FIN forwards its payload and waits for the bridge reply.
			if frame.flags == FlagRST {
				return
			}
			if err := writeBridgePayload(stream, frame.payload); err != nil {
				slog.Debug("failed to write stream payload", "stream_id", rs.id, "error", err)
				return
			}
		}
	}
}

func writeBridgePayload(stream net.Conn, payload []byte) error {
	if len(payload) == 0 {
		return nil
	}
	if err := stream.SetWriteDeadline(time.Now().Add(bridgeWriteWait)); err != nil {
		return err
	}
	n, err := stream.Write(payload)
	if err == nil && n != len(payload) {
		return io.ErrShortWrite
	}
	return err
}

// relayFromBridge reuses a buffer for the outer header and bridge bytes.
// Grow only for bulk output, keeping small JSON responses inexpensive.
func (r *streamRelay) relayFromBridge(rs *relayStream) {
	defer r.finishStream(rs, FlagFIN)
	frame := make([]byte, 5+4096)
	binary.BigEndian.PutUint32(frame[:4], rs.id)
	frame[4] = FlagDATA
	for {
		n, err := rs.stream.Read(frame[5:])
		if n > 0 && !r.writeMessage(frame[:5+n]) {
			return
		}
		if err != nil {
			if !isExpectedStreamReadClose(err) {
				slog.Debug("stream read error", "stream_id", rs.id, "error", err)
			}
			return
		}
		if n == len(frame)-5 && n < relayReadBufferSize {
			larger := make([]byte, 5+relayReadBufferSize)
			copy(larger, frame[:5])
			frame = larger
		}
	}
}

func (r *streamRelay) sendFrame(streamID uint32, flags byte) bool {
	var frame [5]byte
	binary.BigEndian.PutUint32(frame[:4], streamID)
	frame[4] = flags
	return r.writeMessage(frame[:])
}

func (r *streamRelay) writeMessage(frame []byte) bool {
	r.wsMu.Lock()
	if r.closed.Load() {
		r.wsMu.Unlock()
		return false
	}
	err := r.ws.SetWriteDeadline(time.Now().Add(writeWait))
	if err == nil {
		err = r.ws.WriteMessage(websocket.BinaryMessage, frame)
	}
	r.wsMu.Unlock()
	if err != nil {
		slog.Debug("failed to send WebSocket frame", "error", err)
		// Gorilla write failures are terminal. Stop consuming bridge output
		// and close the connection so the browser can recover.
		r.closeAll()
		return false
	}
	return true
}

func (r *streamRelay) finishStream(rs *relayStream, flags byte) {
	r.mu.Lock()
	if r.streams[rs.id] != rs {
		r.mu.Unlock()
		return
	}
	delete(r.streams, rs.id)
	stream := rs.stream
	r.mu.Unlock()

	rs.cancel()
	if stream != nil {
		_ = stream.Close()
	}
	r.sendFrame(rs.id, flags)
}

// closeAll signals shutdown; the HTTP handler joins all relay workers after
// its reader exits. It may also be called by any worker after a write failure.
func (r *streamRelay) closeAll() {
	if !r.closed.CompareAndSwap(false, true) {
		return
	}
	r.cancel()
	// Close the socket first to release any worker holding the writer mutex.
	_ = r.ws.Close()
	r.mu.Lock()
	streams := r.streams
	r.streams = nil
	r.mu.Unlock()
	for _, rs := range streams {
		rs.cancel()
		if rs.stream != nil {
			_ = rs.stream.Close()
		}
	}
}

func (r *streamRelay) ping() error {
	// WriteControl is safe alongside the data writer and owns its deadline.
	err := r.ws.WriteControl(websocket.PingMessage, nil, time.Now().Add(writeWait))
	if err != nil {
		r.closeAll()
	}
	return err
}

func (r *streamRelay) pingLoop() {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-r.ctx.Done():
			r.closeAll()
			return
		case <-ticker.C:
			if err := r.ping(); err != nil {
				slog.Debug("WebSocket ping failed", "error", err)
				return
			}
		}
	}
}
