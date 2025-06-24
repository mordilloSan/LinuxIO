package websocket

import (
	"encoding/json"
	"fmt"
	"go-backend/internal/auth"
	"go-backend/internal/bridge"
	"go-backend/internal/logger"
	"net/http"
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// WebSocket message/request/response structs

type WSMessage struct {
	Type      string          `json:"type"`
	RequestID string          `json:"requestId,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

type WSResponse struct {
	Type      string      `json:"type"`
	RequestID string      `json:"requestId,omitempty"`
	Data      interface{} `json:"data,omitempty"`
	Error     string      `json:"error,omitempty"`
}

// --- CHANNEL SUBSCRIPTION INFRASTRUCTURE ---
var (
	channelsMu         sync.Mutex
	channelSubscribers = make(map[string]map[*websocket.Conn]struct{})
)

func subscribe(conn *websocket.Conn, channel string) {
	channelsMu.Lock()
	defer channelsMu.Unlock()
	if channelSubscribers[channel] == nil {
		channelSubscribers[channel] = make(map[*websocket.Conn]struct{})
	}
	channelSubscribers[channel][conn] = struct{}{}
	logger.Infof("WebSocket subscribed to channel: %s", channel)
}

func unsubscribe(conn *websocket.Conn, channel string) {
	channelsMu.Lock()
	defer channelsMu.Unlock()
	if subs, exists := channelSubscribers[channel]; exists && subs != nil {
		delete(subs, conn)
		if len(subs) == 0 {
			delete(channelSubscribers, channel)
		}
	}
	logger.Infof("WebSocket unsubscribed from channel: %s", channel)
}

func removeConnFromAllChannels(conn *websocket.Conn) {
	channelsMu.Lock()
	defer channelsMu.Unlock()
	for channel, subs := range channelSubscribers {
		delete(subs, conn)
		if len(subs) == 0 {
			delete(channelSubscribers, channel)
		}
	}
}

type TerminalSession struct {
	Cols      int
	Rows      int
	PtyFile   *os.File
	ShellCmd  *exec.Cmd
	ShellDone chan struct{}
	Active    bool
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func WebSocketHandler(c *gin.Context) {
	sess := auth.GetSessionOrAbort(c)
	if sess == nil {
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		logger.Errorf("WS upgrade failed: %v", err)
		return
	}
	defer func() {
		conn.Close()
	}()

	logger.Infof("WebSocket connected for user: %s (session: %s, privileged: %v)", sess.User.Name, sess.SessionID, sess.Privileged)

	var (
		termSession = &TerminalSession{}
	)

	// Cleanup on disconnect
	defer func() {
		if termSession.Active && termSession.ShellCmd != nil {
			close(termSession.ShellDone)
			termSession.ShellCmd.Process.Kill()
			termSession.ShellCmd.Wait()
			termSession.PtyFile.Close()
			termSession.Active = false
		}
	}()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			logger.Warnf("WS disconnect: %v", err)
			break
		}
		var wsMsg WSMessage
		if err := json.Unmarshal(msg, &wsMsg); err != nil {
			_ = conn.WriteJSON(WSResponse{Type: "error", Error: "Invalid JSON"})
			continue
		}

		switch wsMsg.Type {

		case "subscribe":
			var payload struct {
				Channel string `json:"channel"`
			}
			if err := json.Unmarshal(wsMsg.Payload, &payload); err != nil || payload.Channel == "" {
				_ = conn.WriteJSON(WSResponse{Type: "error", Error: "Missing channel"})
				continue
			}
			subscribe(conn, payload.Channel)
			_ = conn.WriteJSON(WSResponse{Type: "subscribed", Data: payload.Channel})

		case "unsubscribe":
			var payload struct {
				Channel string `json:"channel"`
			}
			if err := json.Unmarshal(wsMsg.Payload, &payload); err != nil || payload.Channel == "" {
				_ = conn.WriteJSON(WSResponse{Type: "error", Error: "Missing channel"})
				continue
			}
			unsubscribe(conn, payload.Channel)
			_ = conn.WriteJSON(WSResponse{Type: "unsubscribed", Data: payload.Channel})

		case "getUserInfo":
			_ = conn.WriteJSON(WSResponse{
				Type:      "getUserInfo_response",
				RequestID: wsMsg.RequestID,
				Data:      sess.User,
			})

		case "bridgeCall":
			var payload struct {
				ReqType string   `json:"reqType"`
				Command string   `json:"command"`
				Args    []string `json:"args"`
			}
			if err := json.Unmarshal(wsMsg.Payload, &payload); err != nil {
				_ = conn.WriteJSON(WSResponse{Type: "error", Error: "Invalid bridgeCall payload"})
				continue
			}
			output, err := bridge.CallWithSession(sess, payload.ReqType, payload.Command, payload.Args)
			if err != nil {
				_ = conn.WriteJSON(WSResponse{
					Type:      wsMsg.Type + "_response",
					RequestID: wsMsg.RequestID,
					Error:     err.Error(),
					Data:      output,
				})
				continue
			}
			_ = conn.WriteJSON(WSResponse{
				Type:      wsMsg.Type + "_response",
				RequestID: wsMsg.RequestID,
				Data:      output,
			})
		case "terminal_resize":
			var payload struct {
				Cols int `json:"cols"`
				Rows int `json:"rows"`
			}
			if err := json.Unmarshal(wsMsg.Payload, &payload); err == nil {
				logger.Infof("Terminal resize cols=%d rows=%d", payload.Cols, payload.Rows)
				termSession.Cols = payload.Cols
				termSession.Rows = payload.Rows
				if termSession.PtyFile != nil {
					pty.Setsize(termSession.PtyFile, &pty.Winsize{
						Cols: uint16(payload.Cols),
						Rows: uint16(payload.Rows),
					})
				}
			}

		case "terminal_start":
			if termSession.Active {
				_ = conn.WriteJSON(WSResponse{Type: "terminal_output", Data: "Session already started.\r\n"})
				continue
			}
			cmd := exec.Command("bash", "-i", "-l")
			cmd.Env = append(os.Environ(),
				"TERM=xterm-256color",
				fmt.Sprintf("HOME=%s", os.Getenv("HOME")),
			)
			var ptmx *os.File
			var err error
			// Use known cols/rows if available
			if termSession.Cols > 0 && termSession.Rows > 0 {
				ptmx, err = pty.StartWithSize(cmd, &pty.Winsize{
					Cols: uint16(termSession.Cols),
					Rows: uint16(termSession.Rows),
				})
			} else {
				ptmx, err = pty.Start(cmd)
			}
			if err != nil {
				_ = conn.WriteJSON(WSResponse{Type: "terminal_output", Data: "Shell failed: " + err.Error() + "\r\n"})
				continue
			}
			termSession.ShellCmd = cmd
			termSession.PtyFile = ptmx
			termSession.Active = true
			termSession.ShellDone = make(chan struct{})

			go func() {
				buf := make([]byte, 4096)
				for {
					n, err := ptmx.Read(buf)
					if n > 0 {
						if werr := conn.WriteJSON(WSResponse{Type: "terminal_output", Data: string(buf[:n])}); werr != nil {
							return
						}
					}
					if err != nil {
						return
					}
				}
			}()
			_ = conn.WriteJSON(WSResponse{Type: "terminal_output", Data: "Shell started!\r\n"})

		case "terminal_input":
			if !termSession.Active || termSession.PtyFile == nil {
				_ = conn.WriteJSON(WSResponse{Type: "terminal_output", Data: "Session not started.\r\n"})
				continue
			}
			var input struct {
				Data string `json:"data"`
			}
			ok := false
			if wsMsg.Payload != nil {
				if err := json.Unmarshal(wsMsg.Payload, &input); err == nil && input.Data != "" {
					ok = true
				}
			}
			if !ok {
				_ = json.Unmarshal([]byte(msg), &input)
			}
			if input.Data != "" {
				_, _ = termSession.PtyFile.Write([]byte(input.Data))
			}

		default:
			_ = conn.WriteJSON(WSResponse{Type: "error", Error: "Unknown message type"})
		}
	}
}
