package web

import (
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

	"github.com/mordilloSan/LinuxIO/common/ipc"
	"github.com/mordilloSan/LinuxIO/common/logger"
	"github.com/mordilloSan/LinuxIO/common/session"
	"github.com/mordilloSan/LinuxIO/server/bridge"
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
	logger.Debugf("[WebSocket] Connection details: user=%s session=%s remote=%s path=%s ua=%s",
		sess.User.Username, sess.SessionID, c.ClientIP(), c.Request.URL.Path, c.Request.UserAgent())

	done := make(chan struct{})
	defer func() {
		close(done)
		logger.Infof("[WebSocket] Disconnected: user=%s session=%s", sess.User.Username, sess.SessionID)
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

				// Poll bridge for output and forward to WS
				go func(containerID string) {
					for {
						select {
						case <-done:
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
				}(wsMsg.ContainerID)
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

				go func() {
					for {
						select {
						case <-done:
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
				}()
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

	// resp.Output is `any`, so type assert to map
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

	// resp.Output is `any`, so type assert to map
	data, closed := extractTerminalOutput(resp.Output)
	return data, closed, nil
}

// extractTerminalOutput extracts data and closed from resp.Output (which is type `any`)
func extractTerminalOutput(output any) (data string, closed bool) {
	if output == nil {
		return "", false
	}

	// When JSON unmarshals into `any`, objects become map[string]interface{}
	if m, ok := output.(map[string]interface{}); ok {
		if d, ok := m["data"].(string); ok {
			data = d
		}
		if c, ok := m["closed"].(bool); ok {
			closed = c
		}
	}
	return data, closed
}

// extractDataString extracts the "data" field from resp.Output (which is type `any`)
func extractDataString(output any) string {
	if output == nil {
		return ""
	}

	if m, ok := output.(map[string]interface{}); ok {
		if d, ok := m["data"].(string); ok {
			return d
		}
	}
	return ""
}

// extractStringSlice extracts a []string from resp.Output (which is type `any`)
func extractStringSlice(output any) []string {
	if output == nil {
		return []string{}
	}

	// Could be []interface{} when unmarshaled
	if arr, ok := output.([]interface{}); ok {
		result := make([]string, 0, len(arr))
		for _, v := range arr {
			if s, ok := v.(string); ok {
				result = append(result, s)
			}
		}
		return result
	}

	return []string{}
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

// PTY reading now occurs inside the bridge process.
