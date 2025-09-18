package auth

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mordilloSan/LinuxIO/common/logger"
	"github.com/mordilloSan/LinuxIO/common/session"
	"github.com/mordilloSan/LinuxIO/server/bridge"
)

// Handlers bundles dependencies (no global state).
type Handlers struct {
	SM                   *session.Manager
	Env                  string
	Verbose              bool
	BridgeBinaryOverride string
}

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (h *Handlers) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	// Decide privileged mode (no preflight PAM; helper will auth)
	privileged := trySudo(req.Username, req.Password)

	sess, err := h.createUserSession(req, privileged)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "session creation failed"})
		return
	}

	if err := h.startBridgeSession(sess, req.Password); err != nil {
		_ = h.SM.DeleteSession(sess.SessionID, session.ReasonManual)

		// Map helper-auth errors to 401; infra issues to 500
		if isAuthError(err) {
			logger.Warnf("[auth.login] authentication failed for user %s: %v", req.Username, err)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication failed"})
			return
		}
		logger.Errorf("[auth.login] failed to start bridge: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to start bridge"})
		return
	}

	secure := (h.Env == "production") && (c.Request.TLS != nil)
	if !secure && h.Env == "production" {
		logger.Warnf("[auth.login] insecure cookie write under production env (no TLS detected)")
	}
	h.SM.WriteCookie(c.Writer, sess.SessionID)

	c.JSON(http.StatusOK, gin.H{"success": true, "privileged": sess.Privileged})
}

func (h *Handlers) Logout(c *gin.Context) {
	ck, err := c.Request.Cookie(h.SM.CookieName())
	if err != nil {
		c.Status(http.StatusOK)
		return
	}

	h.SM.DeleteCookie(c.Writer)
	if err := h.SM.DeleteSession(ck.Value, session.ReasonLogout); err != nil {
		logger.Errorf("Failed to delete session %q: %v", ck.Value, err)
	}
	logger.Infof("Logged out session: %s", ck.Value)
	c.Status(http.StatusOK)
}

func (h *Handlers) Me(c *gin.Context) {
	sess := session.SessionFromContext(c)
	if sess == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "no active session"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": sess.User})
}

// ---- internals ----

func (h *Handlers) createUserSession(req LoginRequest, privileged bool) (*session.Session, error) {
	sysu, err := user.Lookup(req.Username)
	if err != nil {
		return nil, err
	}
	u := session.User{
		Username: req.Username,
		UID:      sysu.Uid,
		GID:      sysu.Gid,
	}

	sess, err := h.SM.CreateSession(u, privileged)
	if err != nil {
		logger.Errorf("Failed to create session: %v", err)
		return nil, err
	}
	return sess, nil
}

func (h *Handlers) startBridgeSession(sess *session.Session, password string) error {
	bridgeBinary := bridge.GetBridgeBinaryPath(h.BridgeBinaryOverride, h.Env)
	err := bridge.StartBridge(sess, password, h.Env, h.Verbose, bridgeBinary)
	if err == nil {
		return nil
	}

	// If we tried privileged and it failed for a non-auth reason, fall back to unprivileged.
	// Avoid retry on obvious auth failures to save a second PAM round-trip.
	if sess.Privileged && !isAuthError(err) {
		logger.Warnf("Privileged bridge failed, retrying unprivileged: %v", err)
		_ = h.SM.SetPrivileged(sess.SessionID, false)
		sess.Privileged = false
		if err2 := bridge.StartBridge(sess, password, h.Env, h.Verbose, bridgeBinary); err2 != nil {
			logger.Errorf("Unprivileged bridge also failed: %v", err2)
			return err2
		}
		return nil
	}
	return err
}

// ---- heuristics ----

// isAuthError best-effort classification of helper/bridge auth failures.
func isAuthError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "authentication failure"),
		strings.Contains(msg, "authentication failed"),
		strings.Contains(msg, "auth failure"),
		strings.Contains(msg, "invalid credentials"),
		strings.Contains(msg, "pam_"),
		strings.Contains(msg, "pam "):
		return true
	}
	return false
}

// trySudo validates whether the given password unlocks sudo for the user.
func trySudo(username, password string) bool {
	if username == "" || password == "" {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_ = exec.Command("sudo", "-k").Run() // invalidate timestamp for current user

	cmd := exec.CommandContext(ctx, "su", "--preserve-environment", username, "-c", "sudo -k; sudo -S -p '' -v")
	stdinData := password + "\n" + password + "\n"

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	cmd.Stdin = strings.NewReader(stdinData)
	cmd.Env = append(os.Environ(), "LANG=C")

	if err := cmd.Run(); err != nil {
		return false
	}
	return true
}
