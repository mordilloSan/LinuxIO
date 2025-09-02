package web

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"net/http"
	"sync"
	"sync/atomic"

	"github.com/creack/pty"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/mordilloSan/LinuxIO/internal/logger"
	"github.com/mordilloSan/LinuxIO/internal/session"
	"github.com/mordilloSan/LinuxIO/server/terminal"
)

var upgrader = websocket.Upgrader{
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

// Helper: safe WriteJSON
func (sc *wsSafeConn) WriteJSON(v interface{}) error {
	sc.Mu.Lock()
	defer sc.Mu.Unlock()
	return sc.Conn.WriteJSON(v)
}

// Close politely, only once
func (sc *wsSafeConn) Close() error {
	var err error
	sc.closeOnce.Do(func() {
		_ = sc.Conn.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
			time.Now().Add(200*time.Millisecond),
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
		// Fallback attempt (optional)
		if s, err := session.ValidateSessionFromRequest(c.Request); err == nil {
			sess = s
			c.Set("session", s)
		} else {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
	}
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		logger.Errorf("[WebSocket] WS upgrade failed: %v", err)
		return
	}
	safeConn := &wsSafeConn{Conn: conn}

	// Close handler — expected closes as Debug
	conn.SetCloseHandler(func(code int, text string) error {
		switch code {
		case websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseNoStatusReceived:
			logger.Debugf("[WebSocket] Close from client (code=%d): %s", code, text)
		default:
			logger.Warnf("[WebSocket] Close from client (code=%d): %s", code, text)
		}
		return nil
	})

	// Close on request context cancel — log only if we’re actually closing it here
	ctx := c.Request.Context()
	go func() {
		<-ctx.Done()
		if !safeConn.IsClosed() {
			logger.Infof("[WebSocket] HTTP context cancelled; closing WS...")
		}
		_ = safeConn.Close()
	}()

	defer func() { _ = safeConn.Close() }()

	logger.Infof("[WebSocket] Connected: user=%s session=%s", sess.User.Username, sess.SessionID)

	done := make(chan struct{})
	defer func() {
		close(done)
		logger.Infof("[WebSocket] Disconnected: user=%s session=%s", sess.User.Username, sess.SessionID)
	}()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			// Expected closes are Debug; warnings only for unexpected errors
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
				// ---- CONTAINER TERMINAL START ----
				ts := terminal.GetContainerTerminal(sess.SessionID, wsMsg.ContainerID)
				if ts == nil || ts.PTY == nil {
					shell := wsMsg.Data
					if shell == "" {
						shell = "bash"
					}
					if err := terminal.StartContainerTerminal(sess, wsMsg.ContainerID, shell); err != nil {
						logger.Warnf("Could not start container terminal for %s: %v", wsMsg.ContainerID, err)
						_ = safeConn.WriteJSON(WSResponse{Type: "terminal_output", Data: "Failed to start container shell.\r\n"})
						continue
					}
					ts = terminal.GetContainerTerminal(sess.SessionID, wsMsg.ContainerID)
					_ = safeConn.WriteJSON(WSResponse{Type: "terminal_output", Data: "Container shell started.\r\n"})
				} else {
					ts.Mu.Lock()
					if len(ts.Buffer) > 0 {
						_ = safeConn.WriteJSON(WSResponse{Type: "terminal_output", Data: string(ts.Buffer)})
					}
					ts.Mu.Unlock()
				}
				// Reader goroutine (per container terminal)
				go func(ts *terminal.TerminalSession) {
					buf := make([]byte, 4096)
					for {
						select {
						case <-done:
							return
						default:
							n, err := ts.PTY.Read(buf)
							if n > 0 {
								ts.Mu.Lock()
								if len(ts.Buffer)+n > 8192*2 {
									ts.Buffer = ts.Buffer[(len(ts.Buffer)+n)-8192*2:]
								}
								ts.Buffer = append(ts.Buffer, buf[:n]...)
								ts.Mu.Unlock()
								_ = safeConn.WriteJSON(WSResponse{
									Type:        "terminal_output",
									ContainerID: wsMsg.ContainerID,
									Data:        string(buf[:n]),
								})
							}
							if err != nil {
								if isExpectedPTYRead(err) {
									logger.Debugf("[WebSocket] pty closed (container, normal): %v", err)
								} else {
									logger.Warnf("[WebSocket] pty read error (container): %v", err)
								}
								return
							}
						}
					}
				}(ts)

			} else {
				// ---- MAIN TERMINAL ----
				ts := terminal.Get(sess.SessionID)
				if ts == nil || ts.PTY == nil {
					if err := terminal.StartTerminal(sess); err != nil {
						logger.Warnf("Could not start terminal for %s: %v", sess.User.Username, err)
						_ = safeConn.WriteJSON(WSResponse{Type: "terminal_output", Data: "Failed to start shell.\r\n"})
						continue
					}
					ts = terminal.Get(sess.SessionID)
					_ = safeConn.WriteJSON(WSResponse{Type: "terminal_output", Data: "Shell started.\r\n"})
				} else {
					ts.Mu.Lock()
					if len(ts.Buffer) > 0 {
						_ = safeConn.WriteJSON(WSResponse{Type: "terminal_output", Data: string(ts.Buffer)})
					}
					ts.Mu.Unlock()
				}
				// Reader goroutine (main shell)
				go func(ts *terminal.TerminalSession) {
					buf := make([]byte, 4096)
					for {
						select {
						case <-done:
							return
						default:
							n, err := ts.PTY.Read(buf)
							if n > 0 {
								ts.Mu.Lock()
								if len(ts.Buffer)+n > 8192*2 {
									ts.Buffer = ts.Buffer[(len(ts.Buffer)+n)-8192*2:]
								}
								ts.Buffer = append(ts.Buffer, buf[:n]...)
								ts.Mu.Unlock()
								_ = safeConn.WriteJSON(WSResponse{
									Type: "terminal_output",
									Data: string(buf[:n]),
								})
							}
							if err != nil {
								if isExpectedPTYRead(err) {
									logger.Debugf("[WebSocket] pty closed (normal): %v", err)
								} else {
									logger.Warnf("[WebSocket] pty read error: %v", err)
								}
								return
							}
						}
					}
				}(ts)
			}
		case "terminal_input":
			if wsMsg.Target == "container" && wsMsg.ContainerID != "" {
				ts := terminal.GetContainerTerminal(sess.SessionID, wsMsg.ContainerID)
				if ts != nil && ts.PTY != nil && wsMsg.Data != "" {
					_, _ = ts.PTY.Write([]byte(wsMsg.Data))
				}
			} else {
				ts := terminal.Get(sess.SessionID)
				if ts != nil && ts.PTY != nil && wsMsg.Data != "" {
					_, _ = ts.PTY.Write([]byte(wsMsg.Data))
				}
			}
		case "terminal_resize":
			var size struct {
				Cols int `json:"cols"`
				Rows int `json:"rows"`
			}
			_ = json.Unmarshal(wsMsg.Payload, &size)
			if wsMsg.Target == "container" && wsMsg.ContainerID != "" {
				ts := terminal.GetContainerTerminal(sess.SessionID, wsMsg.ContainerID)
				if ts != nil && ts.PTY != nil {
					if err := pty.Setsize(ts.PTY, &pty.Winsize{Cols: uint16(size.Cols), Rows: uint16(size.Rows)}); err != nil {
						logger.Warnf("failed to set PTY size (container): %v", err)
					}
				}
			} else {
				ts := terminal.Get(sess.SessionID)
				if ts != nil && ts.PTY != nil {
					if err := pty.Setsize(ts.PTY, &pty.Winsize{Cols: uint16(size.Cols), Rows: uint16(size.Rows)}); err != nil {
						logger.Warnf("failed to set PTY size: %v", err)
					}
				}
			}
		case "list_shells":
			if wsMsg.Target == "container" && wsMsg.ContainerID != "" {
				shells, err := terminal.ListContainerShells(wsMsg.ContainerID)
				if err != nil {
					_ = safeConn.WriteJSON(WSResponse{
						Type:        "shell_list",
						ContainerID: wsMsg.ContainerID,
						Data:        []string{""},
						Error:       err.Error(),
					})
					continue
				}
				_ = safeConn.WriteJSON(WSResponse{
					Type:        "shell_list",
					ContainerID: wsMsg.ContainerID,
					Data:        shells,
				})
			}
		case "terminal_close":
			if wsMsg.Target == "container" && wsMsg.ContainerID != "" {
				if err := terminal.CloseContainerTerminal(sess.SessionID, wsMsg.ContainerID); err != nil {
					logger.Warnf("Failed to close container terminal %s: %v", wsMsg.ContainerID, err)
				} else {
					logger.Infof("Closed terminal for container %s", wsMsg.ContainerID)
				}
				_ = safeConn.WriteJSON(WSResponse{
					Type:        "terminal_closed",
					ContainerID: wsMsg.ContainerID,
					Data:        "Container terminal closed.",
				})
			} else {
				if err := terminal.Close(sess.SessionID); err != nil {
					logger.Warnf("Failed to close main terminal for session %s: %v", sess.SessionID, err)
				} else {
					logger.Infof("Closed main terminal for session %s", sess.SessionID)
				}
				_ = safeConn.WriteJSON(WSResponse{
					Type: "terminal_closed",
					Data: "Main terminal closed.",
				})
			}

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
	// “use of closed network connection” after deferred Close is fine
	return strings.Contains(strings.ToLower(err.Error()), "use of closed network connection")
}

func isExpectedPTYRead(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "input/output error") || // EIO after PTY closed
		strings.Contains(s, "bad file descriptor") ||
		strings.Contains(s, "file already closed")
}
