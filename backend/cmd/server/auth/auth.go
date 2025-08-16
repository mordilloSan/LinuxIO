package auth

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"net/http"
	"os/exec"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/mordilloSan/LinuxIO/backend/cmd/server/config"
	"github.com/mordilloSan/LinuxIO/cmd/server/terminal"
	"github.com/mordilloSan/LinuxIO/internal/bridge"
	"github.com/mordilloSan/LinuxIO/internal/logger"
	"github.com/mordilloSan/LinuxIO/internal/session"
	"github.com/mordilloSan/LinuxIO/internal/utils"
	"github.com/msteinert/pam"
)

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

const sessionDuration = 6 * time.Hour

// Config is injected by main() so this package doesn't read env.
type Config struct {
	Env                  string
	Verbose              bool
	BridgeBinaryOverride string
}

var cfg Config

func RegisterAuthRoutes(router *gin.Engine, c Config) {
	cfg = c
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

	// Ensure per-user config exists & is valid (repair if needed).
	if err := config.Initialize(req.Username); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "unable to prepare user config"})
		return
	}

	privileged := trySudo(req.Password)

	sess, err := createUserSession(req, privileged)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "session creation failed"})
		return
	}

	if err := startBridgeSession(sess, req.Password); err != nil {
		_ = session.DeleteSession(sess.SessionID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to start bridge"})
		return
	}

	setSessionCookie(c, sess.SessionID)
	c.JSON(http.StatusOK, gin.H{"success": true, "privileged": sess.Privileged})
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
	// Clear cookie early to prevent new activity during teardown
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie("session_id", "", -1, "/", "", false, true)

	if sess == nil {
		logger.Debugf("No session found for ID: %s (already expired?)", sessionID)
		c.Status(http.StatusOK)
		return
	}

	// 1) Close all terminals (main + containers) to stop PTY readers cleanly
	terminal.CloseAllForSession(sess.SessionID)

	// 2) Tell bridge to shutdown (logout). Ignore minor errors.
	if sess.User.ID != "" {
		if _, err := bridge.CallWithSession(sess, "control", "shutdown", []string{"logout"}); err != nil {
			logger.Warnf("CallWithSession for shutdown failed: %v", err)
		}
	}

	// 3) Delete session
	if err := session.DeleteSession(sessionID); err != nil {
		logger.Errorf("Failed to delete session %q: %v", sessionID, err)
	}

	logger.Infof("👋 Logged out session: %s", sessionID)
	c.Status(http.StatusOK)
}

func meHandler(c *gin.Context) {
	sessVal, ok := c.Get("session")
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "no active session"})
		return
	}

	sess, ok := sessVal.(*session.Session)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid session type"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"user": sess.User})
}

func authenticateUser(req LoginRequest) error {
	if err := pamAuth(req.Username, req.Password); err != nil {
		logger.Warnf("❌ Authentication failed for user: %s", req.Username)
		return err
	}
	return nil
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

func startBridgeSession(sess *session.Session, password string) error {
	bridgeBinary := bridge.GetBridgeBinaryPath(cfg.BridgeBinaryOverride, cfg.Env)
	if err := bridge.StartBridge(sess, password, cfg.Env, cfg.Verbose, bridgeBinary); err != nil {
		if sess.Privileged {
			logger.Warnf("Privileged bridge failed, retrying unprivileged: %v", err)
			sess.Privileged = false
			if err2 := bridge.StartBridge(sess, password, cfg.Env, cfg.Verbose, bridgeBinary); err2 != nil {
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

func setSessionCookie(c *gin.Context, sessionID string) {
	secureCookie := (cfg.Env == "production") && (c.Request.TLS != nil)
	c.SetSameSite(http.SameSiteStrictMode)
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

// trySudo silently validates whether the given password unlocks sudo.
func trySudo(password string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// invalidate timestamp
	_ = exec.Command("sudo", "-k").Run()

	// Run sudo with stdin password, no prompt, validate privileges
	cmd := exec.CommandContext(ctx, "sudo", "-S", "-p", "", "-v")

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	cmd.Stdin = strings.NewReader(password + "\n")

	// Keep env, force consistent behavior
	cmd.Env = append(os.Environ(), "LANG=C")

	// Run — returns nil if sudo accepts the password
	if err := cmd.Run(); err != nil {
		return false
	}
	return true
}
