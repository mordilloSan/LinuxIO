package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/mordilloSan/go_logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

var upgrader = websocket.Upgrader{
	// Origin check is handled by the CORS middleware.
	CheckOrigin: func(*http.Request) bool { return true },
}

type WSMessage struct {
	Type      string `json:"type"`
	RequestID string `json:"requestId,omitempty"`
	Data      string `json:"data,omitempty"`
}

type WSResponse struct {
	Type      string      `json:"type"`
	RequestID string      `json:"requestId,omitempty"`
	Data      interface{} `json:"data,omitempty"`
	Error     string      `json:"error,omitempty"`
}

type wsSafeConn struct {
	Conn      *websocket.Conn
	Mu        sync.Mutex
	closeOnce sync.Once
	closed    uint32 // 0 open, 1 closed
}

func (sc *wsSafeConn) WriteJSON(v interface{}) error {
	sc.Mu.Lock()
	defer sc.Mu.Unlock()
	return sc.Conn.WriteJSON(v)
}

func (sc *wsSafeConn) Close() error {
	var err error
	sc.closeOnce.Do(func() {
		_ = sc.Conn.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
			time.Now().Add(1*time.Second),
		)
		err = sc.Conn.Close()
		atomic.StoreUint32(&sc.closed, 1)
	})
	return err
}

func (sc *wsSafeConn) IsClosed() bool {
	return atomic.LoadUint32(&sc.closed) == 1
}

// WebSocketHandler handles legacy WebSocket connections.
// Used for: progress subscriptions for folder uploads.
// Terminal streaming now uses yamux via /ws/relay.
func WebSocketHandler(c *gin.Context) {
	sess := session.SessionFromContext(c)
	if sess == nil {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		logger.Errorf("[WebSocket] WS upgrade failed: %v", err)
		return
	}
	safeConn := &wsSafeConn{Conn: conn}

	conn.SetCloseHandler(func(code int, text string) error {
		switch code {
		case websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseNoStatusReceived:
			logger.Debugf("[WebSocket] Close from client (code=%d): %s", code, text)
		default:
			logger.Warnf("[WebSocket] Close from client (code=%d): %s", code, text)
		}
		return nil
	})

	ctx := c.Request.Context()
	go func() {
		<-ctx.Done()
		if !safeConn.IsClosed() {
			logger.Infof("[WebSocket] HTTP context cancelled; closing WS...")
		}
		_ = safeConn.Close()
	}()

	defer func() { _ = safeConn.Close() }()
	logger.Infof("[WebSocket] Connected: user=%s", sess.User.Username)

	subscriptionCancels := struct {
		mu      sync.Mutex
		cancels map[string]context.CancelFunc
	}{
		cancels: make(map[string]context.CancelFunc),
	}

	addSubscriptionCancel := func(key string, cancel context.CancelFunc) {
		subscriptionCancels.mu.Lock()
		subscriptionCancels.cancels[key] = cancel
		subscriptionCancels.mu.Unlock()
	}

	popSubscriptionCancel := func(key string) context.CancelFunc {
		subscriptionCancels.mu.Lock()
		cancel := subscriptionCancels.cancels[key]
		delete(subscriptionCancels.cancels, key)
		subscriptionCancels.mu.Unlock()
		return cancel
	}

	defer func() {
		logger.Infof("[WebSocket] Disconnected: user=%s", sess.User.Username)
	}()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			if isExpectedWSClose(err) {
				logger.Debugf("[WebSocket] WS disconnect: %v", err)
			} else {
				logger.Warnf("[WebSocket] WS disconnect: %v", err)
			}
			break
		}

		var wsMsg WSMessage
		if err := json.Unmarshal(msg, &wsMsg); err != nil {
			logger.Warnf("[WebSocket] Invalid JSON: %v", err)
			_ = safeConn.WriteJSON(WSResponse{Type: "error", Error: "Invalid JSON"})
			continue
		}
		logger.Debugf("[WebSocket] Message: %+v", wsMsg)

		switch wsMsg.Type {
		case "route_change":
			// Accept route_change for frontend compatibility, but no action needed
			// Terminal routing now handled by yamux streams
			_ = safeConn.WriteJSON(WSResponse{Type: "route_changed", Data: wsMsg.Data})

		case "subscribe_operation_progress":
			reqId := wsMsg.Data
			if reqId == "" {
				logger.Warnf("[WebSocket] subscribe_operation_progress with empty reqId")
				continue
			}
			key := sess.SessionID + ":" + reqId
			logger.Debugf("[WebSocket] Subscribing to operation progress: %s", key)

			subCtx, cancel := context.WithCancel(ctx)
			addSubscriptionCancel(key, cancel)

			GlobalProgressBroadcaster.Register(key, func(update ProgressUpdate) {
				_ = safeConn.WriteJSON(WSResponse{
					Type:      update.Type,
					RequestID: reqId,
					Data:      update,
				})
			})

			go func(subscriptionKey string, childCtx context.Context) {
				<-childCtx.Done()
				subscriptionCancels.mu.Lock()
				delete(subscriptionCancels.cancels, subscriptionKey)
				subscriptionCancels.mu.Unlock()
				logger.Debugf("[WebSocket] Unsubscribing from operation progress: %s", subscriptionKey)
				GlobalProgressBroadcaster.Unregister(subscriptionKey)
			}(key, subCtx)

			_ = safeConn.WriteJSON(WSResponse{
				Type:      "operation_subscribed",
				RequestID: reqId,
			})

		case "unsubscribe_operation_progress":
			reqId := wsMsg.Data
			if reqId == "" {
				logger.Warnf("[WebSocket] unsubscribe_operation_progress with empty reqId")
				continue
			}
			key := sess.SessionID + ":" + reqId
			logger.Debugf("[WebSocket] Unsubscribing from operation progress: %s", key)
			if cancel := popSubscriptionCancel(key); cancel != nil {
				cancel()
			}
			GlobalProgressBroadcaster.Unregister(key)
			GlobalOperationCanceller.Cancel(key)
			_ = safeConn.WriteJSON(WSResponse{
				Type:      "operation_unsubscribed",
				RequestID: reqId,
			})

		default:
			logger.Warnf("[WebSocket] Unknown message type: %s", wsMsg.Type)
		}
	}
}

func isExpectedWSClose(err error) bool {
	var ce *websocket.CloseError
	if errors.As(err, &ce) {
		switch ce.Code {
		case websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseNoStatusReceived:
			return true
		}
	}
	return strings.Contains(strings.ToLower(err.Error()), "use of closed network connection")
}
