package web

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

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
)

// Stream flags for WebSocket binary protocol
const (
	FlagSYN  byte = 0x01 // Open new stream
	FlagDATA byte = 0x04 // Data frame
	FlagFIN  byte = 0x08 // Close stream
	FlagRST  byte = 0x10 // Abort stream
)

// streamRelay manages the mapping of streamID to yamux stream
type streamRelay struct {
	mu      sync.RWMutex
	streams map[uint32]*relayStream
	ws      *websocket.Conn
	wsMu    sync.Mutex
	closed  atomic.Uint32
	done    chan struct{} // Signal to stop ping goroutine
}

type relayStream struct {
	id     uint32
	stream io.ReadWriteCloser
	cancel chan struct{}
}

var upgrader = websocket.Upgrader{
	// Origin check is handled by the CORS middleware.
	CheckOrigin: func(*http.Request) bool { return true },
}

// wsConnsBySession tracks all active WebSocket connections for each session.
// Multiple tabs/windows can share the same session, each with their own WebSocket.
// map[sessionID]*sync.Map[*websocket.Conn]struct{}
var wsConnsBySession sync.Map

// addWebSocketForSession registers a WebSocket connection for a session.
func addWebSocketForSession(sessionID string, conn *websocket.Conn) {
	connsInterface, _ := wsConnsBySession.LoadOrStore(sessionID, &sync.Map{})
	connsMap, ok := connsInterface.(*sync.Map)
	if !ok {
		slog.Error("invalid WebSocket connection map type", "session_id", sessionID)
		return
	}
	connsMap.Store(conn, struct{}{})
}

// removeWebSocketForSession unregisters a WebSocket connection from a session.
func removeWebSocketForSession(sessionID string, conn *websocket.Conn) {
	if connsInterface, ok := wsConnsBySession.Load(sessionID); ok {
		connsMap, ok := connsInterface.(*sync.Map)
		if !ok {
			slog.Error("invalid WebSocket connection map type", "session_id", sessionID)
			return
		}
		connsMap.Delete(conn)

		// Clean up empty session entries
		isEmpty := true
		connsMap.Range(func(key, value any) bool {
			isEmpty = false
			return false // Stop iteration after first element
		})
		if isEmpty {
			wsConnsBySession.Delete(sessionID)
		}
	}
}

// CloseWebSocketForSession closes ALL WebSocket connections associated with a session.
// Called when a session expires to immediately disconnect all tabs/windows.
func CloseWebSocketForSession(sessionID string) {
	if connsInterface, ok := wsConnsBySession.Load(sessionID); ok {
		connsMap, ok := connsInterface.(*sync.Map)
		if !ok {
			slog.Error("invalid WebSocket connection map type", "session_id", sessionID)
			return
		}
		count := 0

		connsMap.Range(func(key, value any) bool {
			conn, ok := key.(*websocket.Conn)
			if !ok {
				slog.Error("invalid WebSocket entry type", "session_id", sessionID)
				return true // Continue to next connection
			}

			// Send close frame with code 1008 (Policy Violation) to indicate session expired
			// This allows the frontend to distinguish session expiry from network errors
			closeMsg := websocket.FormatCloseMessage(1008, "Session expired")
			if err := conn.WriteControl(websocket.CloseMessage, closeMsg, time.Now().Add(writeWait)); err != nil {
				slog.Debug("failed to write WebSocket close control frame",
					"session_id", sessionID,
					"error", err)
			}

			// Close the underlying connection
			if err := conn.Close(); err != nil {
				slog.Debug("failed to close WebSocket",
					"session_id", sessionID,
					"error", err)
			}
			count++
			return true // Continue iteration
		})

		wsConnsBySession.Delete(sessionID)
		slog.Debug("closed WebSockets for expired session",
			"session_id", sessionID,
			"count", count)
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

func refreshSessionActivity(sm *session.Manager, sessionID string) {
	if err := sm.Refresh(sessionID); err != nil {
		slog.Debug("failed to refresh WebSocket session",
			"session_id", sessionID,
			"error", err)
	}
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

		relay := &streamRelay{
			streams: make(map[uint32]*relayStream),
			ws:      conn,
			done:    make(chan struct{}),
		}

		// Track this WebSocket by session ID for session expiry handling
		// Multiple tabs/windows can share the same session
		addWebSocketForSession(sess.SessionID, conn)
		defer func() {
			removeWebSocketForSession(sess.SessionID, conn)
			relay.closeAll()
		}()
		slog.Info("WebSocket connected", "user", sess.User.Username, "session_id", sess.SessionID)

		// Count websocket liveness toward session activity so transport and
		// idle-session lifecycles stay aligned under the configured throttle.
		conn.SetPongHandler(func(string) error {
			slog.Debug("WebSocket pong received", "session_id", sess.SessionID, "deadline", pongWait)
			if err := conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
				slog.Debug("failed to set WebSocket read deadline in pong handler",
					"session_id", sess.SessionID,
					"error", err)
				return err
			}
			refreshSessionActivity(sm, sess.SessionID)
			return nil
		})
		// Set initial read deadline.
		slog.Debug("setting initial WebSocket read deadline",
			"session_id", sess.SessionID,
			"deadline", pongWait,
			"ping_interval", pingInterval)
		if err := conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
			slog.Warn("failed to set initial WebSocket read deadline",
				"session_id", sess.SessionID,
				"error", err)
			return
		}

		// Start ping goroutine to keep connection alive
		go relay.pingLoop()

		relay.readLoop(sm, sess)
		slog.Info("WebSocket disconnected", "user", sess.User.Username, "session_id", sess.SessionID)
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
		refreshSessionActivity(sm, sess.SessionID)

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

		switch {
		case flags&FlagSYN != 0:
			r.handleSYN(sess, streamID, payload)
		case flags&FlagDATA != 0:
			r.handleDATA(streamID, payload)
		case flags&FlagFIN != 0:
			r.handleFIN(streamID, payload)
		case flags&FlagRST != 0:
			r.handleRST(streamID)
		}
	}
}

// handleSYN opens a new yamux stream and starts relaying
func (r *streamRelay) handleSYN(sess *session.Session, streamID uint32, payload []byte) {
	r.mu.Lock()
	if _, exists := r.streams[streamID]; exists {
		r.mu.Unlock()
		slog.Warn("stream already exists", "stream_id", streamID)
		return
	}
	r.mu.Unlock()

	// Get yamux session for this user (created by StartBridge during login)
	yamuxSession, err := bridge.GetYamuxSession(sess.SessionID)
	if err != nil {
		slog.Error("failed to get yamux session",
			"session_id", sess.SessionID,
			"stream_id", streamID,
			"error", err)
		r.sendFrame(streamID, FlagRST, nil)
		// Bridge is gone (likely session expired) - close the WebSocket entirely
		// This signals to the frontend that reconnection/re-auth is needed
		go r.closeAll()
		return
	}

	// Open new yamux stream
	stream, err := yamuxSession.Open(context.Background())
	if err != nil {
		slog.Error("failed to open yamux stream",
			"session_id", sess.SessionID,
			"stream_id", streamID,
			"error", err)
		r.sendFrame(streamID, FlagRST, nil)
		return
	}

	rs := &relayStream{
		id:     streamID,
		stream: stream,
		cancel: make(chan struct{}),
	}

	// Re-check under lock to prevent TOCTOU race (another goroutine may have added same streamID)
	r.mu.Lock()
	if _, exists := r.streams[streamID]; exists {
		r.mu.Unlock()
		// Another goroutine won the race - close our stream and return
		stream.Close()
		slog.Warn("stream race detected, closing duplicate", "stream_id", streamID)
		return
	}
	r.streams[streamID] = rs
	r.mu.Unlock()

	// Write payload directly - frontend sends StreamFrame-formatted bytes
	if len(payload) > 0 {
		if _, err := stream.Write(payload); err != nil {
			slog.Warn("failed to write SYN payload", "stream_id", streamID, "error", err)
			r.closeStream(streamID)
			return
		}
	}

	// Start reading from yamux stream and relaying to WebSocket
	go r.relayFromBridge(rs)
	slog.Debug("stream opened", "stream_id", streamID)
}

// handleDATA writes payload to the yamux stream
func (r *streamRelay) handleDATA(streamID uint32, payload []byte) {
	r.mu.RLock()
	rs, exists := r.streams[streamID]
	r.mu.RUnlock()

	if !exists {
		slog.Debug("data received for unknown stream", "stream_id", streamID)
		return
	}

	if len(payload) > 0 {
		if _, err := rs.stream.Write(payload); err != nil {
			slog.Debug("failed to write stream payload", "stream_id", streamID, "error", err)
			r.closeStream(streamID)
		}
	}
}

// handleFIN forwards the close frame to bridge but doesn't close the stream yet.
// The stream will be closed by relayFromBridge when the bridge sends its response and closes.
func (r *streamRelay) handleFIN(streamID uint32, payload []byte) {
	r.mu.RLock()
	rs, exists := r.streams[streamID]
	r.mu.RUnlock()

	if !exists {
		slog.Debug("FIN received for unknown stream", "stream_id", streamID)
		return
	}

	// Forward the payload (e.g., OpStreamClose frame) to bridge
	// Don't close the stream - let relayFromBridge handle that when bridge responds
	if len(payload) > 0 {
		if _, err := rs.stream.Write(payload); err != nil {
			slog.Debug("failed to write FIN payload", "stream_id", streamID, "error", err)
			r.closeStream(streamID)
			return
		}
	}
	slog.Debug("stream FIN forwarded", "stream_id", streamID)
}

// handleRST aborts the stream
func (r *streamRelay) handleRST(streamID uint32) {
	r.closeStream(streamID)
	slog.Debug("stream aborted", "stream_id", streamID)
}

// relayFromBridge reads from yamux stream and sends to WebSocket
func (r *streamRelay) relayFromBridge(rs *relayStream) {
	buf := make([]byte, 4096)
	for {
		select {
		case <-rs.cancel:
			return
		default:
		}

		n, err := rs.stream.Read(buf)
		if n > 0 {
			// Send DATA frame to WebSocket
			r.sendFrame(rs.id, FlagDATA, buf[:n])
		}
		if err != nil {
			if err != io.EOF {
				slog.Debug("stream read error", "stream_id", rs.id, "error", err)
			}
			// Send FIN to WebSocket
			r.sendFrame(rs.id, FlagFIN, nil)
			r.closeStream(rs.id)
			return
		}
	}
}

// sendFrame sends a binary frame to WebSocket
func (r *streamRelay) sendFrame(streamID uint32, flags byte, payload []byte) {
	if r.closed.Load() == 1 {
		return
	}

	frame := make([]byte, 5+len(payload))
	binary.BigEndian.PutUint32(frame[0:4], streamID)
	frame[4] = flags
	if len(payload) > 0 {
		copy(frame[5:], payload)
	}

	r.wsMu.Lock()
	defer r.wsMu.Unlock()

	if err := r.ws.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
		slog.Debug("failed to set WebSocket write deadline", "error", err)
		return
	}

	err := r.ws.WriteMessage(websocket.BinaryMessage, frame)

	// Always clear deadline after write attempt
	if clearErr := r.ws.SetWriteDeadline(time.Time{}); clearErr != nil {
		slog.Debug("failed to clear WebSocket write deadline", "error", clearErr)
	}

	if err != nil {
		slog.Debug("failed to send WebSocket frame", "stream_id", streamID, "error", err)
	}
}

// closeStream closes and removes a stream
func (r *streamRelay) closeStream(streamID uint32) {
	r.mu.Lock()
	rs, exists := r.streams[streamID]
	if exists {
		delete(r.streams, streamID)
	}
	r.mu.Unlock()

	if exists {
		close(rs.cancel)
		rs.stream.Close()
		slog.Debug("stream closed", "stream_id", streamID)
	}
}

// closeAll closes all streams and the WebSocket
func (r *streamRelay) closeAll() {
	if !r.closed.CompareAndSwap(0, 1) {
		return // Already closed
	}

	// Stop the ping goroutine
	close(r.done)

	r.mu.Lock()
	streams := r.streams
	r.streams = make(map[uint32]*relayStream)
	r.mu.Unlock()

	for _, rs := range streams {
		close(rs.cancel)
		rs.stream.Close()
	}

	r.ws.Close()
}

// pingLoop sends periodic ping frames to keep the connection alive.
// Runs in a separate goroutine and exits when done channel is closed.
func (r *streamRelay) pingLoop() {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.done:
			return
		case <-ticker.C:
			if r.closed.Load() == 1 {
				return
			}

			r.wsMu.Lock()
			// Set write deadline, write ping, then clear deadline
			if err := r.ws.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				r.wsMu.Unlock()
				slog.Debug("failed to set ping write deadline", "error", err)
				return
			}

			err := r.ws.WriteMessage(websocket.PingMessage, nil)

			// Always clear deadline after write attempt
			if clearErr := r.ws.SetWriteDeadline(time.Time{}); clearErr != nil {
				slog.Debug("failed to clear ping write deadline", "error", clearErr)
			}
			r.wsMu.Unlock()

			if err != nil {
				slog.Debug("WebSocket ping failed", "error", err)
				return
			}
			slog.Debug("ping sent")
		}
	}
}
