package auth

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/msteinert/pam"

	"github.com/mordilloSan/LinuxIO/internal/logger"
	"github.com/mordilloSan/LinuxIO/internal/session"
	"github.com/mordilloSan/LinuxIO/server/bridge"
	"github.com/mordilloSan/LinuxIO/server/filebrowser"
	"github.com/mordilloSan/LinuxIO/server/terminal"
)

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
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

	secure := (cfg.Env == "production") && (c.Request.TLS != nil)
	session.SetCookie(c, sess.SessionID, secure)

	if err := filebrowser.ApplyNavigatorDefaults(c, sess); err != nil {
		logger.Warnf("[auth.login] navigator defaults failed for user=%s: %v", sess.User.Username, err)
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "privileged": sess.Privileged})
}

func logoutHandler(c *gin.Context) {
	sessionID, err := c.Cookie(session.CookieName)
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
	secure := (cfg.Env == "production") && (c.Request.TLS != nil)
	session.DeleteCookie(c, secure)

	if sess == nil {
		logger.Debugf("No session found for ID: %s (already expired?)", sessionID)
		c.Status(http.StatusOK)
		return
	}

	// 1) Close all terminals (main + containers)
	terminal.CloseAllForSession(sess.SessionID)

	// 2) Ask bridge to shutdown
	if sess.User.Username != "" {
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
	sysu, err := user.Lookup(req.Username)
	if err != nil {
		return nil, fmt.Errorf("lookup user: %w", err)
	}
	u := session.User{
		Username: req.Username,
		UID:      sysu.Uid,
		GID:      sysu.Gid,
	}

	sess, err := session.CreateSession("", u, privileged)
	if err != nil {
		logger.Errorf("Failed to create session: %v", err)
		return nil, err
	}
	return sess, nil
}

func startBridgeSession(sess *session.Session, password string) error {
	bridgeBinary := bridge.GetBridgeBinaryPath(cfg.BridgeBinaryOverride, cfg.Env)
	if err := bridge.StartBridge(sess, password, cfg.Env, cfg.Verbose, bridgeBinary); err != nil {
		if sess.Privileged {
			logger.Warnf("Privileged bridge failed, retrying unprivileged: %v", err)
			_ = session.SetPrivileged(sess.SessionID, false) // persist in store
			sess.Privileged = false                          // keep local copy in sync
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

// pamAuth authenticates a user via PAM ("login" service) and runs AcctMgmt.
func pamAuth(username, password string) error {
	conv := func(style pam.Style, msg string) (string, error) {
		switch style {
		case pam.PromptEchoOff:
			return password, nil
		case pam.PromptEchoOn:
			return username, nil
		case pam.ErrorMsg, pam.TextInfo:
			return "", nil
		default:
			return "", fmt.Errorf("unsupported PAM style: %v (msg=%q)", style, msg)
		}
	}

	t, err := pam.StartFunc("linuxio", username, conv)
	if err != nil {
		return fmt.Errorf("pam start: %w", err)
	}

	if host, _ := os.Hostname(); host != "" {
		_ = t.SetItem(pam.Rhost, host)
	}

	if err := t.Authenticate(0); err != nil {
		return fmt.Errorf("pam authenticate: %w", err)
	}
	if err := t.AcctMgmt(0); err != nil {
		return fmt.Errorf("pam account check: %w", err)
	}
	return nil
}

// trySudo silently validates whether the given password unlocks sudo.
func trySudo(password string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_ = exec.Command("sudo", "-k").Run() // invalidate timestamp

	cmd := exec.CommandContext(ctx, "sudo", "-S", "-p", "", "-v")
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	cmd.Stdin = strings.NewReader(password + "\n")
	cmd.Env = append(os.Environ(), "LANG=C")

	if err := cmd.Run(); err != nil {
		return false
	}
	return true
}
