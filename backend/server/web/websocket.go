package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/mordilloSan/go_logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/server/bridge"
)

var upgrader = websocket.Upgrader{
	// Origin check is handled by the CORS middleware.
	CheckOrigin: func(*http.Request) bool { return true },
}

type WSMessage struct {
	Type        string          `json:"type"`
	RequestID   string          `json:"requestId,omitempty"`
	Target      string          `json:"target,omitempty"`      // "main" or "container"
	ContainerID string          `json:"containerId,omitempty"` // if target == "container"
	Payload     json.RawMessage `json:"payload,omitempty"`
	Data        string          `json:"data,omitempty"` // for input
}

type WSResponse struct {
	Type        string      `json:"type"`
	RequestID   string      `json:"requestId,omitempty"`
	ContainerID string      `json:"containerId,omitempty"`
	Data        interface{} `json:"data,omitempty"`
	Error       string      `json:"error,omitempty"`
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
	logger.Debugf("[WebSocket] Connection details: user=%s remote=%s path=%s ua=%s",
		sess.User.Username, c.ClientIP(), c.Request.URL.Path, c.Request.UserAgent())

	// Initialize channel manager for route subscriptions
	channelMgr := NewChannelManager(ctx)
	defer channelMgr.CloseAll()

	// Get initial route from query params
	initialRoute := c.Query("route")
	if initialRoute == "" {
		initialRoute = "terminal" // default route
	}
	channelMgr.Subscribe(initialRoute)

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

	done := make(chan struct{})
	defer func() {
		close(done)
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
			newRoute := wsMsg.Data
			if newRoute == "" {
				logger.Warnf("[WebSocket] route_change with empty route")
				continue
			}
			logger.Debugf("[WebSocket] Route change: %s -> %s", channelMgr.GetActiveRoute(), newRoute)
			channelMgr.Subscribe(newRoute)
			_ = safeConn.WriteJSON(WSResponse{Type: "route_changed", Data: newRoute})

		case "terminal_start":
			if wsMsg.Target == "container" && wsMsg.ContainerID != "" {
				// Start container terminal via bridge
				shell := wsMsg.Data
				if shell == "" {
					shell = "bash"
				}
				if _, err := bridge.CallWithSession(sess, "terminal", "start_container", []string{wsMsg.ContainerID, shell}); err != nil {
					logger.Warnf("Could not start container terminal for %s: %v", wsMsg.ContainerID, err)
					_ = safeConn.WriteJSON(WSResponse{Type: "terminal_output", Data: "Failed to start container shell.\r\n"})
					continue
				}
				_ = safeConn.WriteJSON(WSResponse{Type: "terminal_output", Data: "Container shell started.\r\n"})

				// Subscribe to terminal route to get route context
				routeCtx := channelMgr.Subscribe("terminal")

				// Poll bridge for output and forward to WS
				go func(containerID string, routeCtx context.Context) {
					for {
						select {
						case <-done:
							logger.Debugf("[WebSocket] Container terminal polling stopped: connection closed")
							return
						case <-routeCtx.Done():
							logger.Debugf("[WebSocket] Container terminal polling stopped: route changed")
							return
						default:
						}
						data, closed, err := readFromBridgeContainer(sess, containerID, 1200)
						if err != nil {
							logger.Warnf("bridge read_container error: %v", err)
							return
						}
						if data != "" {
							_ = safeConn.WriteJSON(WSResponse{Type: "terminal_output", ContainerID: containerID, Data: data})
						}
						if closed {
							return
						}
						if data == "" {
							time.Sleep(60 * time.Millisecond)
						}
					}
				}(wsMsg.ContainerID, routeCtx)
			} else {
				// Start main terminal via bridge
				if _, err := bridge.CallWithSession(sess, "terminal", "start_main", nil); err != nil {
					logger.Warnf("Could not start terminal for %s: %v", sess.User.Username, err)
					_ = safeConn.WriteJSON(WSResponse{Type: "terminal_output", Data: "Failed to start shell.\r\n"})
					continue
				}
				_ = safeConn.WriteJSON(WSResponse{Type: "terminal_output", Data: "Shell started.\r\n"})

				// Send retained backlog once on (re)start to repopulate xterm
				if raw, err := bridge.CallWithSession(sess, "terminal", "read_main_backlog", nil); err == nil {
					var resp ipc.Response
					if json.Unmarshal(raw, &resp) == nil && strings.ToLower(resp.Status) == "ok" {
						if dataStr := extractDataString(resp.Output); dataStr != "" {
							_ = safeConn.WriteJSON(WSResponse{Type: "terminal_output", Data: dataStr})
						}
					}
				}

				// Subscribe to terminal route to get route context
				routeCtx := channelMgr.Subscribe("terminal")

				go func(routeCtx context.Context) {
					for {
						select {
						case <-done:
							logger.Debugf("[WebSocket] Main terminal polling stopped: connection closed")
							return
						case <-routeCtx.Done():
							logger.Debugf("[WebSocket] Main terminal polling stopped: route changed")
							return
						default:
						}
						data, closed, err := readFromBridgeMain(sess, 1200)
						if err != nil {
							logger.Warnf("bridge read_main error: %v", err)
							return
						}
						if data != "" {
							_ = safeConn.WriteJSON(WSResponse{Type: "terminal_output", Data: data})
						}
						if closed {
							return
						}
						if data == "" {
							time.Sleep(60 * time.Millisecond)
						}
					}
				}(routeCtx)
			}

		case "terminal_input":
			if wsMsg.Data == "" {
				continue
			}
			if wsMsg.Target == "container" && wsMsg.ContainerID != "" {
				_, err := bridge.CallWithSession(sess, "terminal", "input_container", []string{wsMsg.ContainerID, wsMsg.Data})
				if err != nil {
					logger.Warnf("bridge input_container error: %v", err)
				}
			} else {
				_, err := bridge.CallWithSession(sess, "terminal", "input_main", []string{wsMsg.Data})
				if err != nil {
					logger.Warnf("bridge input_main error: %v", err)
				}
			}

		case "terminal_resize":
			var size struct {
				Cols int `json:"cols"`
				Rows int `json:"rows"`
			}
			_ = json.Unmarshal(wsMsg.Payload, &size)
			if wsMsg.Target == "container" && wsMsg.ContainerID != "" {
				_, err := bridge.CallWithSession(sess, "terminal", "resize_container", []string{wsMsg.ContainerID, strconv.Itoa(size.Cols), strconv.Itoa(size.Rows)})
				if err != nil {
					logger.Warnf("bridge resize_container error: %v", err)
				}
			} else {
				_, err := bridge.CallWithSession(sess, "terminal", "resize_main", []string{strconv.Itoa(size.Cols), strconv.Itoa(size.Rows)})
				if err != nil {
					logger.Warnf("bridge resize_main error: %v", err)
				}
			}

		case "list_shells":
			if wsMsg.Target == "container" && wsMsg.ContainerID != "" {
				raw, err := bridge.CallWithSession(sess, "terminal", "list_shells", []string{wsMsg.ContainerID})
				if err != nil {
					_ = safeConn.WriteJSON(WSResponse{Type: "shell_list", ContainerID: wsMsg.ContainerID, Data: []string{""}, Error: err.Error()})
					continue
				}
				var resp ipc.Response
				_ = json.Unmarshal(raw, &resp)
				if strings.ToLower(resp.Status) != "ok" {
					_ = safeConn.WriteJSON(WSResponse{Type: "shell_list", ContainerID: wsMsg.ContainerID, Data: []string{""}, Error: resp.Error})
					continue
				}
				shells := extractStringSlice(resp.Output)
				_ = safeConn.WriteJSON(WSResponse{Type: "shell_list", ContainerID: wsMsg.ContainerID, Data: shells})
			}

		case "terminal_close":
			if wsMsg.Target == "container" && wsMsg.ContainerID != "" {
				_, err := bridge.CallWithSession(sess, "terminal", "close_container", []string{wsMsg.ContainerID})
				if err != nil {
					logger.Warnf("bridge close_container error: %v", err)
				}
				_ = safeConn.WriteJSON(WSResponse{Type: "terminal_closed", ContainerID: wsMsg.ContainerID, Data: "Container terminal closed."})
			} else {
				_, err := bridge.CallWithSession(sess, "terminal", "close_main", nil)
				if err != nil {
					logger.Warnf("bridge close_main error: %v", err)
				}
				_ = safeConn.WriteJSON(WSResponse{Type: "terminal_closed", Data: "Main terminal closed."})
			}

		case "subscribe_download_progress":
			reqId := wsMsg.Data
			if reqId == "" {
				logger.Warnf("[WebSocket] subscribe_download_progress with empty reqId")
				continue
			}
			key := sess.SessionID + ":" + reqId
			logger.Debugf("[WebSocket] Subscribing to download progress: %s", key)

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
				if ctx.Err() != nil {
					logger.Debugf("[WebSocket] Unsubscribing from download progress: %s", subscriptionKey)
					GlobalProgressBroadcaster.Unregister(subscriptionKey)
				}
			}(key, subCtx)

			_ = safeConn.WriteJSON(WSResponse{
				Type:      "download_subscribed",
				RequestID: reqId,
			})

		case "unsubscribe_download_progress":
			reqId := wsMsg.Data
			if reqId == "" {
				logger.Warnf("[WebSocket] unsubscribe_download_progress with empty reqId")
				continue
			}
			key := sess.SessionID + ":" + reqId
			logger.Debugf("[WebSocket] Unsubscribing from download progress: %s", key)
			if cancel := popSubscriptionCancel(key); cancel != nil {
				cancel()
			}
			GlobalProgressBroadcaster.Unregister(key)
			_ = safeConn.WriteJSON(WSResponse{
				Type:      "download_unsubscribed",
				RequestID: reqId,
			})

		case "subscribe_compression_progress":
			reqId := wsMsg.Data
			if reqId == "" {
				logger.Warnf("[WebSocket] subscribe_compression_progress with empty reqId")
				continue
			}
			key := sess.SessionID + ":" + reqId
			logger.Debugf("[WebSocket] Subscribing to compression progress: %s", key)

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
				if ctx.Err() != nil {
					logger.Debugf("[WebSocket] Unsubscribing from compression progress: %s", subscriptionKey)
					GlobalProgressBroadcaster.Unregister(subscriptionKey)
				}
			}(key, subCtx)

			_ = safeConn.WriteJSON(WSResponse{
				Type:      "compression_subscribed",
				RequestID: reqId,
			})

		case "unsubscribe_compression_progress":
			reqId := wsMsg.Data
			if reqId == "" {
				logger.Warnf("[WebSocket] unsubscribe_compression_progress with empty reqId")
				continue
			}
			key := sess.SessionID + ":" + reqId
			logger.Debugf("[WebSocket] Unsubscribing from compression progress: %s", key)
			if cancel := popSubscriptionCancel(key); cancel != nil {
				cancel()
			}
			GlobalProgressBroadcaster.Unregister(key)
			_ = safeConn.WriteJSON(WSResponse{
				Type:      "compression_unsubscribed",
				RequestID: reqId,
			})

		default:
			logger.Warnf("[WebSocket] Unknown message type: %s", wsMsg.Type)
		}
	}
}

func readFromBridgeMain(sess *session.Session, waitMs int) (string, bool, error) {
	if waitMs <= 0 {
		waitMs = 750
	}
	raw, err := bridge.CallWithSession(sess, "terminal", "read_main", []string{strconv.Itoa(waitMs)})
	if err != nil {
		return "", false, err
	}
	var resp ipc.Response
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", false, err
	}
	if strings.ToLower(resp.Status) != "ok" {
		return "", false, errors.New(resp.Error)
	}

	data, closed := extractTerminalOutput(resp.Output)
	return data, closed, nil
}

func readFromBridgeContainer(sess *session.Session, containerID string, waitMs int) (string, bool, error) {
	if waitMs <= 0 {
		waitMs = 750
	}
	raw, err := bridge.CallWithSession(sess, "terminal", "read_container", []string{containerID, strconv.Itoa(waitMs)})
	if err != nil {
		return "", false, err
	}
	var resp ipc.Response
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", false, err
	}
	if strings.ToLower(resp.Status) != "ok" {
		return "", false, errors.New(resp.Error)
	}

	data, closed := extractTerminalOutput(resp.Output)
	return data, closed, nil
}

// extractTerminalOutput decodes the terminal bridge response payload.
func extractTerminalOutput(output json.RawMessage) (data string, closed bool) {
	if len(output) == 0 {
		return "", false
	}

	var payload struct {
		Data   string `json:"data"`
		Closed bool   `json:"closed"`
	}
	if err := json.Unmarshal(output, &payload); err != nil {
		return "", false
	}
	return payload.Data, payload.Closed
}

// extractDataString extracts the "data" field from resp.Output.
func extractDataString(output json.RawMessage) string {
	if len(output) == 0 {
		return ""
	}

	var payload struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(output, &payload); err != nil {
		return ""
	}
	return payload.Data
}

// extractStringSlice extracts a []string from resp.Output.
func extractStringSlice(output json.RawMessage) []string {
	if len(output) == 0 {
		return []string{}
	}

	var arr []string
	if err := json.Unmarshal(output, &arr); err != nil {
		return []string{}
	}
	return arr
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
