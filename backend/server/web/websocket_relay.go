package web

import (
	"encoding/binary"
	"io"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/mordilloSan/go_logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/server/bridge"
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
}

type relayStream struct {
	id     uint32
	stream io.ReadWriteCloser
	cancel chan struct{}
}

// WebSocketRelayHandler handles binary WebSocket connections as a pure byte relay.
// The server never parses payloads - just routes bytes between WebSocket and yamux streams.
func WebSocketRelayHandler(c *gin.Context) {
	sess := session.SessionFromContext(c)
	if sess == nil {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		logger.Errorf("[WSRelay] upgrade failed: %v", err)
		return
	}

	relay := &streamRelay{
		streams: make(map[uint32]*relayStream),
		ws:      conn,
	}

	defer relay.closeAll()
	logger.Infof("[WSRelay] Connected: user=%s", sess.User.Username)

	// Read binary messages from WebSocket
	for {
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			if !isExpectedWSClose(err) {
				logger.Warnf("[WSRelay] read error: %v", err)
			}
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

	// Get yamux session for this user
	yamuxSession, err := bridge.GetOrCreateYamuxSession(sess.SocketPath)
	if err != nil {
		logger.Errorf("[WSRelay] failed to get yamux session: %v", err)
		r.sendFrame(streamID, FlagRST, nil)
		return
	}

	// Open new yamux stream
	stream, err := yamuxSession.Open()
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

	r.mu.Lock()
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
	err := r.ws.WriteMessage(websocket.BinaryMessage, frame)
	r.wsMu.Unlock()

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
	}
}

// closeAll closes all streams and the WebSocket
func (r *streamRelay) closeAll() {
	atomic.StoreUint32(&r.closed, 1)

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
