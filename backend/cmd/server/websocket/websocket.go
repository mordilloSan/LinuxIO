package websocket

import (
	"encoding/json"

	"net/http"
	"sync"

	"github.com/creack/pty"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/mordilloSan/LinuxIO/internal/logger"
	"github.com/mordilloSan/LinuxIO/internal/session"
	"github.com/mordilloSan/LinuxIO/internal/terminal"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
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
	Conn *websocket.Conn
	Mu   sync.Mutex
}

// Helper: safe WriteJSON
func (sc *wsSafeConn) WriteJSON(v interface{}) error {
	sc.Mu.Lock()
	defer sc.Mu.Unlock()
	return sc.Conn.WriteJSON(v)
}

func WebSocketHandler(c *gin.Context) {
	sess := session.GetSessionOrAbort(c)
	if sess == nil {
		return
	}
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		logger.Errorf("[WebSocket] WS upgrade failed: %v", err)
		return
	}
	safeConn := &wsSafeConn{Conn: conn}

	// Listen for shutdown
	ctx := c.Request.Context()
	go func() {
		<-ctx.Done()
		logger.Infof("[WebSocket] HTTP context cancelled, closing WS connection (server shutdown)...")
		if err := conn.Close(); err != nil {
			logger.Warnf("[WebSocket] Error closing WS connection: %v", err)
		}
	}()

	defer func() {
		if cerr := conn.Close(); cerr != nil {
			logger.Warnf("[WebSocket] failed to close WS connection: %v", cerr)
		}
	}()
	logger.Infof("[WebSocket] Connected: user=%s session=%s", sess.User.Name, sess.SessionID)

	done := make(chan struct{})
	defer func() {
		close(done)
		logger.Infof("[WebSocket] Disconnected: user=%s session=%s", sess.User.Name, sess.SessionID)
	}()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			logger.Warnf("[WebSocket] WS disconnect: %v", err)
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
								logger.Warnf("[WebSocket] pty read error (container): %v", err)
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
						logger.Warnf("Could not start terminal for %s: %v", sess.User.Name, err)
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
								logger.Warnf("[WebSocket] pty read error: %v", err)
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
