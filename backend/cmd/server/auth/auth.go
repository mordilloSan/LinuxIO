package auth

import (
	"bytes"
	"fmt"

	"io"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/mordilloSan/LinuxIO/cmd/server/config"
	"github.com/mordilloSan/LinuxIO/internal/bridge"
	"github.com/mordilloSan/LinuxIO/internal/logger"
	"github.com/mordilloSan/LinuxIO/internal/session"
	"github.com/mordilloSan/LinuxIO/internal/terminal"
	"github.com/mordilloSan/LinuxIO/internal/utils"
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

func loginHandler(c *gin.Context) {
	var req LoginRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	if err := authenticateUser(req); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication failed"})
		return
	}

	privileged := checkSudoAccess(req.Password)

	sess, err := createUserSession(req, privileged)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "session creation failed"})
		return
	}

	prepareUserEnvironment()

	if err := startBridgeSession(sess, req.Password); err != nil {
		_ = session.DeleteSession(sess.SessionID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to start bridge"})
		return
	}

	startUserTerminal(sess)

	setSessionCookie(c, sess.SessionID)

	c.JSON(http.StatusOK, gin.H{"success": true, "privileged": privileged})
}

func logoutHandler(c *gin.Context) {
	sessionID, err := c.Cookie("session_id")
	if err != nil {
		c.Status(http.StatusOK)
		return
	}

	sess, err := session.GetSession(sessionID)
	if err != nil {
		logger.Errorf("Failed to get session (id=%s): %v", sessionID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "session fetch failed"})
		return
	}

	if sess == nil {
		logger.Debugf("No session found for ID: %s (already expired?)", sessionID)
		c.SetCookie("session_id", "", -1, "/", "", false, true)
		c.Status(http.StatusOK)
		return
	}

	if sess.User.ID != "" {
		_, err := bridge.CallWithSession(sess, "control", "shutdown", []string{"logout"})
		if err != nil {
			logger.Warnf("CallWithSession for shutdown failed: %v", err)
		}
	}

	if err := session.DeleteSession(sessionID); err != nil {
		logger.Errorf("Failed to delete session %q: %v", sessionID, err)
	}
	c.SetCookie("session_id", "", -1, "/", "", false, true)
	logger.Infof("👋 Logged out session: %s", sessionID)
	c.Status(http.StatusOK)
}

func meHandler(c *gin.Context) {
	sess := c.MustGet("session").(*session.Session)
	c.JSON(http.StatusOK, gin.H{"user": sess.User})
}

func authenticateUser(req LoginRequest) error {
	if err := pamAuth(req.Username, req.Password); err != nil {
		logger.Warnf("❌ Authentication failed for user: %s", req.Username)
		return err
	}
	return nil
}

func checkSudoAccess(password string) bool {
	return trySudo(password)
}

func createUserSession(req LoginRequest, privileged bool) (*session.Session, error) {
	sessionID := uuid.New().String()
	user := utils.User{ID: req.Username, Name: req.Username}

	if err := session.CreateSession(sessionID, user, sessionDuration, privileged); err != nil {
		logger.Errorf("Failed to create session: %v", err)
		return nil, err
	}

	sess, err := session.GetSession(sessionID)
	if err != nil || sess == nil {
		logger.Errorf("Failed to get session after creation (id=%s): %v", sessionID, err)
		return nil, fmt.Errorf("session retrieval failed")
	}

	return sess, nil
}

func prepareUserEnvironment() {
	logger.Infof("📦 Loading docker configuration...")
	if err := config.LoadDockerConfig(); err != nil {
		logger.Errorf("❌ Failed to load config: %v", err)
	}
	if err := config.EnsureDockerAppsDirExists(); err != nil {
		logger.Errorf("❌ Failed to create docker apps directory: %v", err)
	}
}

func startBridgeSession(sess *session.Session, password string) error {
	if err := bridge.StartBridge(sess, password); err != nil {
		if sess.Privileged {
			logger.Warnf("Privileged bridge failed, retrying unprivileged: %v", err)
			if err2 := bridge.StartBridge(sess, password); err2 != nil {
				logger.Errorf("Unprivileged bridge also failed: %v", err2)
				return err2
			}
		} else {
			logger.Errorf("Bridge failed to start: %v", err)
			return err
		}
	}
	return nil
}

func startUserTerminal(sess *session.Session) {
	if err := terminal.StartTerminal(sess); err != nil {
		logger.Errorf("[WebSocket] Shell failed: %v", err)
	}
}

func setSessionCookie(c *gin.Context, sessionID string) {
	env := os.Getenv("GO_ENV")
	isHTTPS := c.Request.TLS != nil
	secureCookie := env == "production" && isHTTPS
	c.SetCookie("session_id", sessionID, int(sessionDuration.Seconds()), "/", "", secureCookie, true)
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
	cmd := exec.Command("sudo", "-S", "-n", "-l")
	cmd.Env = append(cmd.Env, "LANG=C")
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return false
	}
	go func() {
		defer func() {
			if cerr := stdin.Close(); cerr != nil {
				logger.Warnf("failed to close stdin: %v", cerr)
			}
		}()
		if _, err := io.WriteString(stdin, password+"\n"); err != nil {
			logger.Warnf("failed to write password to stdin: %v", err)
		}
	}()

	err = cmd.Run()
	return err == nil && (bytes.Contains(out.Bytes(), []byte("may run")) || bytes.Contains(stderr.Bytes(), []byte("may run")))
}
