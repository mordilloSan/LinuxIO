package auth

import (
	"bytes"
	"go-backend/internal/bridge"
	"go-backend/internal/config"
	"go-backend/internal/logger"
	"go-backend/internal/session"
	"go-backend/internal/terminal"
	"go-backend/internal/utils"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/msteinert/pam"
)

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

const sessionDuration = 6 * time.Hour

func RegisterAuthRoutes(router *gin.Engine) {
	auth := router.Group("/auth")
	{
		auth.POST("/login", loginHandler)
		auth.GET("/me", AuthMiddleware(), meHandler)
		auth.GET("/logout", AuthMiddleware(), logoutHandler)
	}
}

func pamAuth(username, password string) error {
	t, err := pam.StartFunc("login", username, func(s pam.Style, msg string) (string, error) {
		return password, nil
	})
	if err != nil {
		return err
	}
	return t.Authenticate(0)
}

func trySudo(password string) bool {
	cmd := exec.Command("sudo", "-S", "-l")
	cmd.Env = append(cmd.Env, "LANG=C")
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return false
	}
	go func() {
		defer stdin.Close()
		io.WriteString(stdin, password+"\n")
	}()
	err = cmd.Run()
	return err == nil && (bytes.Contains(out.Bytes(), []byte("may run")) || bytes.Contains(stderr.Bytes(), []byte("may run")))
}

func loginHandler(c *gin.Context) {
	var req LoginRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	// 1. Authenticate with PAM
	if err := pamAuth(req.Username, req.Password); err != nil {
		logger.Warnf("❌ Authentication failed for user: %s", req.Username)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication failed"})
		return
	}

	// 2. Check if user has sudo rights
	privileged := trySudo(req.Password)

	// 3. Create session (with privilege info)
	sessionID := uuid.New().String()
	user := utils.User{ID: req.Username, Name: req.Username}
	session.CreateSession(sessionID, user, sessionDuration, privileged)
	sess := session.Get(sessionID)

	if sess == nil {
		logger.Errorf("Failed to get session after creation (id=%s)", sessionID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "session creation failed"})
		return
	}

	// 4. Creating user specific config files
	logger.Infof("📦 Loading docker configuration...")
	if err := config.LoadDockerConfig(); err != nil {
		logger.Errorf("❌ Failed to load config: %v", err)
	}
	if err := config.EnsureDockerAppsDirExists(); err != nil {
		logger.Errorf("❌ Failed to create docker apps directory: %v", err)
	}

	// 4. Start main socket for this session
	if err := bridge.StartBridgeSocket(sess); err != nil {
		logger.Errorf("Failed to start main socket: %v", err)
		session.DeleteSession(sessionID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to start session socket"})
		return
	}

	// 5. Start the bridge process for this session
	if err := bridge.StartBridge(sess, req.Password); err != nil {
		if privileged {
			logger.Warnf("Privileged bridge failed, falling back to unprivileged: %v", err)
			privileged = false
			if err2 := bridge.StartBridge(sess, req.Password); err2 != nil {
				logger.Errorf("Unprivileged bridge also failed: %v", err2)
				session.DeleteSession(sessionID)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to start bridge"})
				return
			}
		} else {
			logger.Errorf("Bridge failed to start: %v", err)
			session.DeleteSession(sessionID)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to start bridge"})
			return
		}
	}

	// 6. Start the terminal PTY
	if err := terminal.StartTerminal(sess); err != nil {
		logger.Errorf("[WebSocket] Shell failed: %v", err)
	}

	// 7. Set session cookie
	env := os.Getenv("GO_ENV")
	isHTTPS := c.Request.TLS != nil
	secureCookie := env == "production" && isHTTPS
	c.SetCookie("session_id", sessionID, int(sessionDuration.Seconds()), "/", "", secureCookie, true)

	// 8. Pre-create FileBrowser user (background)
	go createFilebrowserUser(sessionID)

	// 9. Send response
	c.JSON(http.StatusOK, gin.H{"success": true, "privileged": privileged})

}

func logoutHandler(c *gin.Context) {
	sessionID, err := c.Cookie("session_id")
	if err != nil {
		c.Status(http.StatusOK)
		return
	}

	s := session.Get(sessionID)
	if s == nil {
		logger.Debugf("[auth] No session found for ID: %s (already expired?)", sessionID)
		c.SetCookie("session_id", "", -1, "/", "", false, true)
		c.Status(http.StatusOK)
		return
	}
	terminal.Close(sessionID)
	session.DeleteSession(sessionID)
	if s.User.ID != "" {
		bridge.CallWithSession(s, "control", "shutdown", []string{"logout"})
	}
	c.SetCookie("session_id", "", -1, "/", "", false, true)
	logger.Infof("👋 Logged out session: %s", sessionID)
	c.Status(http.StatusOK)
}

func meHandler(c *gin.Context) {
	sess := c.MustGet("session").(*session.Session)
	c.JSON(http.StatusOK, gin.H{"user": sess.User})
}

func createFilebrowserUser(sessionID string) {
	jar, _ := cookiejar.New(nil)
	client := &http.Client{
		Jar:     jar,
		Timeout: 5 * time.Second,
	}
	req, _ := http.NewRequest("GET", "http://127.0.0.1:8080/navigator/", nil)
	req.AddCookie(&http.Cookie{
		Name:  "session_id",
		Value: sessionID,
		Path:  "/",
	})
	resp, err := client.Do(req)
	if err != nil {
		logger.Warnf("[login] Could not pre-create FileBrowser user: %v", err)
		return
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	logger.Infof("[login] Pre-created FileBrowser user for session %s (status %d)", sessionID, resp.StatusCode)
}
