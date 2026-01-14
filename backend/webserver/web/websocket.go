package web

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/mordilloSan/go-logger/logger"

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
	closed  uint32
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
		logger.Errorf("[WSRelay] Invalid type in wsConnsBySession for session: %s", sessionID)
		return
	}
	connsMap.Store(conn, struct{}{})
}

// removeWebSocketForSession unregisters a WebSocket connection from a session.
func removeWebSocketForSession(sessionID string, conn *websocket.Conn) {
	if connsInterface, ok := wsConnsBySession.Load(sessionID); ok {
		connsMap, ok := connsInterface.(*sync.Map)
		if !ok {
			logger.Errorf("[WSRelay] Invalid type in wsConnsBySession for session: %s", sessionID)
			return
		}
		connsMap.Delete(conn)

		// Clean up empty session entries
		isEmpty := true
		connsMap.Range(func(key, value interface{}) bool {
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
			logger.Errorf("[WSRelay] Invalid type in wsConnsBySession for session: %s", sessionID)
			return
		}
		count := 0

		connsMap.Range(func(key, value interface{}) bool {
			conn, ok := key.(*websocket.Conn)
			if !ok {
				logger.Errorf("[WSRelay] Invalid WebSocket type in connection map for session: %s", sessionID)
				return true // Continue to next connection
			}

			// Send close frame with code 1008 (Policy Violation) to indicate session expired
			// This allows the frontend to distinguish session expiry from network errors
			closeMsg := websocket.FormatCloseMessage(1008, "Session expired")
			_ = conn.WriteControl(websocket.CloseMessage, closeMsg, time.Now().Add(writeWait))

			// Close the underlying connection
			_ = conn.Close()
			count++
			return true // Continue iteration
		})

		wsConnsBySession.Delete(sessionID)
		logger.Debugf("[WSRelay] Closed %d WebSocket(s) for expired session: %s", count, sessionID)
	}
}

func isExpectedWSClose(err error) bool {
	var ce *websocket.CloseError
	if errors.As(err, &ce) {
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

// WebSocketRelayHandler handles binary WebSocket connections as a pure byte relay.
// The server never parses payloads - just routes bytes between WebSocket and yamux streams.
func WebSocketRelayHandler(w http.ResponseWriter, r *http.Request) {
	sess := session.SessionFromContext(r.Context())
	if sess == nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Errorf("[WSRelay] upgrade failed: %v", err)
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

	logger.Infof("[WSRelay] Connected: user=%s", sess.User.Username)

	// Set up pong handler - this resets the read deadline when pong is received
	conn.SetPongHandler(func(string) error {
		logger.Debugf("[WSRelay] pong received, resetting deadline to %v", pongWait)
		if err := conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
			logger.Debugf("[WSRelay] failed to set read deadline in pong handler: %v", err)
			return err
		}
		return nil
	})

	// Set initial read deadline
	logger.Debugf("[WSRelay] setting initial read deadline: %v (pingInterval=%v, pongWait=%v)", pongWait, pingInterval, pongWait)
	if err := conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
		logger.Warnf("[WSRelay] failed to set initial read deadline: %v", err)
		return
	}

	// Start ping goroutine to keep connection alive
	go relay.pingLoop()

	// Read binary messages from WebSocket
	for {
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			if !isExpectedWSClose(err) {
				logger.Warnf("[WSRelay] read error: %v", err)
			}
			break
		}

		// Reset read deadline on any successful read (data keeps connection alive too)
		if err := conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
			logger.Debugf("[WSRelay] failed to reset read deadline: %v", err)
			break
		}

		// Only handle binary messages
		if messageType != websocket.BinaryMessage {
			logger.Debugf("[WSRelay] ignoring non-binary message type=%d", messageType)
			continue
		}

		// Parse frame header: [streamID:4][flags:1][payload:N]
		if len(data) < 5 {
			logger.Warnf("[WSRelay] frame too short: %d bytes", len(data))
			continue
		}

		streamID := binary.BigEndian.Uint32(data[0:4])
		flags := data[4]
		payload := data[5:]

		if flags&FlagSYN != 0 {
			// Open new stream
			relay.handleSYN(sess, streamID, payload)
		} else if flags&FlagDATA != 0 {
			// Write data to stream
			relay.handleDATA(streamID, payload)
		} else if flags&FlagFIN != 0 {
			// Close stream (forward payload first if present)
			relay.handleFIN(streamID, payload)
		} else if flags&FlagRST != 0 {
			// Abort stream
			relay.handleRST(streamID)
		}
	}

	logger.Infof("[WSRelay] Disconnected: user=%s", sess.User.Username)
}

// handleSYN opens a new yamux stream and starts relaying
func (r *streamRelay) handleSYN(sess *session.Session, streamID uint32, payload []byte) {
	r.mu.Lock()
	if _, exists := r.streams[streamID]; exists {
		r.mu.Unlock()
		logger.Warnf("[WSRelay] stream %d already exists", streamID)
		return
	}
	r.mu.Unlock()

	// Get yamux session for this user (created by StartBridge during login)
	yamuxSession, err := bridge.GetYamuxSession(sess.SessionID)
	if err != nil {
		logger.Errorf("[WSRelay] failed to get yamux session: %v", err)
		r.sendFrame(streamID, FlagRST, nil)
		// Bridge is gone (likely session expired) - close the WebSocket entirely
		// This signals to the frontend that reconnection/re-auth is needed
		go r.closeAll()
		return
	}

	// Open new yamux stream
	stream, err := yamuxSession.Open(context.Background())
	if err != nil {
		logger.Errorf("[WSRelay] failed to open stream: %v", err)
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
		logger.Warnf("[WSRelay] stream %d race detected, closing duplicate", streamID)
		return
	}
	r.streams[streamID] = rs
	r.mu.Unlock()

	// Write payload directly - frontend sends StreamFrame-formatted bytes
	if len(payload) > 0 {
		if _, err := stream.Write(payload); err != nil {
			logger.Warnf("[WSRelay] failed to write SYN payload: %v", err)
			r.closeStream(streamID)
			return
		}
	}

	// Start reading from yamux stream and relaying to WebSocket
	go r.relayFromBridge(rs)

	logger.Debugf("[WSRelay] stream %d opened", streamID)
}

// handleDATA writes payload to the yamux stream
func (r *streamRelay) handleDATA(streamID uint32, payload []byte) {
	r.mu.RLock()
	rs, exists := r.streams[streamID]
	r.mu.RUnlock()

	if !exists {
		logger.Debugf("[WSRelay] DATA for unknown stream %d", streamID)
		return
	}

	if len(payload) > 0 {
		if _, err := rs.stream.Write(payload); err != nil {
			logger.Debugf("[WSRelay] write to stream %d failed: %v", streamID, err)
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
		logger.Debugf("[WSRelay] FIN for unknown stream %d", streamID)
		return
	}

	// Forward the payload (e.g., OpStreamClose frame) to bridge
	// Don't close the stream - let relayFromBridge handle that when bridge responds
	if len(payload) > 0 {
		if _, err := rs.stream.Write(payload); err != nil {
			logger.Debugf("[WSRelay] write FIN payload to stream %d failed: %v", streamID, err)
			r.closeStream(streamID)
			return
		}
	}

	logger.Debugf("[WSRelay] stream %d FIN forwarded, waiting for bridge response", streamID)
}

// handleRST aborts the stream
func (r *streamRelay) handleRST(streamID uint32) {
	r.closeStream(streamID)
	logger.Debugf("[WSRelay] stream %d aborted (RST)", streamID)
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
				logger.Debugf("[WSRelay] stream %d read error: %v", rs.id, err)
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
	if atomic.LoadUint32(&r.closed) == 1 {
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
		logger.Debugf("[WSRelay] failed to set write deadline: %v", err)
		return
	}

	err := r.ws.WriteMessage(websocket.BinaryMessage, frame)

	// Always clear deadline after write attempt
	if clearErr := r.ws.SetWriteDeadline(time.Time{}); clearErr != nil {
		logger.Debugf("[WSRelay] failed to clear write deadline: %v", clearErr)
	}

	if err != nil {
		logger.Debugf("[WSRelay] failed to send frame: %v", err)
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
		logger.Debugf("[WSRelay] stream %d closed", streamID)
	}
}

// closeAll closes all streams and the WebSocket
func (r *streamRelay) closeAll() {
	if !atomic.CompareAndSwapUint32(&r.closed, 0, 1) {
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
			if atomic.LoadUint32(&r.closed) == 1 {
				return
			}

			r.wsMu.Lock()
			// Set write deadline, write ping, then clear deadline
			if err := r.ws.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				r.wsMu.Unlock()
				logger.Debugf("[WSRelay] ping: failed to set write deadline: %v", err)
				return
			}

			err := r.ws.WriteMessage(websocket.PingMessage, nil)

			// Always clear deadline after write attempt
			if clearErr := r.ws.SetWriteDeadline(time.Time{}); clearErr != nil {
				logger.Debugf("[WSRelay] ping: failed to clear write deadline: %v", clearErr)
			}
			r.wsMu.Unlock()

			if err != nil {
				logger.Debugf("[WSRelay] ping failed: %v", err)
				return
			}
			logger.Debugf("[WSRelay] ping sent")
		}
	}
}
