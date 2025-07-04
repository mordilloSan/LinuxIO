package websocket

import (
	"encoding/json"
	"go-backend/internal/auth"
	"go-backend/internal/logger"
	"go-backend/internal/terminal"
	"net/http"

	"github.com/creack/pty"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type WSMessage struct {
	Type      string          `json:"type"`
	RequestID string          `json:"requestId,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	Data      string          `json:"data,omitempty"` // for input
}

type WSResponse struct {
	Type      string      `json:"type"`
	RequestID string      `json:"requestId,omitempty"`
	Data      interface{} `json:"data,omitempty"`
	Error     string      `json:"error,omitempty"`
}

func WebSocketHandler(c *gin.Context) {
	sess := auth.GetSessionOrAbort(c)
	if sess == nil {
		return
	}
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		logger.Errorf("[WebSocket] WS upgrade failed: %v", err)
		return
	}
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

	var ptyStarted bool

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			logger.Warnf("[WebSocket] WS disconnect: %v", err)
			break
		}
		var wsMsg WSMessage
		if err := json.Unmarshal(msg, &wsMsg); err != nil {
			logger.Warnf("[WebSocket] Invalid JSON: %v", err)
			_ = conn.WriteJSON(WSResponse{Type: "error", Error: "Invalid JSON"})
			continue
		}
		logger.Debugf("[WebSocket] Message: %+v", wsMsg)
		switch wsMsg.Type {
		case "terminal_start":
			ts := terminal.Get(sess.SessionID)
			if ts == nil || ts.PTY == nil {
				if err := terminal.StartTerminal(sess); err != nil {
					logger.Warnf("Could not start terminal for %s: %v", sess.User.Name, err)
					_ = conn.WriteJSON(WSResponse{Type: "terminal_output", Data: "Failed to start shell.\r\n"})
					continue
				}
				ts = terminal.Get(sess.SessionID)
				_ = conn.WriteJSON(WSResponse{Type: "terminal_output", Data: "Shell started.\r\n"})
			} else {
				ts.Mu.Lock()
				if len(ts.Buffer) > 0 {
					_ = conn.WriteJSON(WSResponse{Type: "terminal_output", Data: string(ts.Buffer)})
				}
				ts.Mu.Unlock()
			}

			if !ptyStarted && ts != nil && ts.PTY != nil {
				go func() {
					buf := make([]byte, 4096)
					for {
						select {
						case <-done:
							return
						default:
							n, err := ts.PTY.Read(buf)
							if n > 0 {
								// Append to Buffer, rotating if needed (e.g. max 16KB)
								ts.Mu.Lock()
								if len(ts.Buffer)+n > 8192*2 {
									// Drop oldest data to keep max length (keep newest 8192-n bytes, then append n new bytes)
									ts.Buffer = ts.Buffer[(len(ts.Buffer)+n)-8192*2:]
								}
								ts.Buffer = append(ts.Buffer, buf[:n]...)
								ts.Mu.Unlock()
								_ = conn.WriteJSON(WSResponse{Type: "terminal_output", Data: string(buf[:n])})
							}
							if err != nil {
								logger.Warnf("[WebSocket] pty read error: %v", err)
								return
							}
						}
					}
				}()
				ptyStarted = true
			}
		case "terminal_input":
			ts := terminal.Get(sess.SessionID)
			if ts != nil && ts.PTY != nil {
				if wsMsg.Data != "" {
					_, _ = ts.PTY.Write([]byte(wsMsg.Data))
				}
			}
		case "terminal_resize":
			ts := terminal.Get(sess.SessionID)
			if ts != nil && ts.PTY != nil {
				var size struct {
					Cols int `json:"cols"`
					Rows int `json:"rows"`
				}
				_ = json.Unmarshal(wsMsg.Payload, &size)
				if err := pty.Setsize(ts.PTY, &pty.Winsize{Cols: uint16(size.Cols), Rows: uint16(size.Rows)}); err != nil {
					logger.Warnf("failed to set PTY size: %v", err)
				}

			}
		default:
			logger.Warnf("[WebSocket] Unknown message type: %s", wsMsg.Type)
		}
	}
}
