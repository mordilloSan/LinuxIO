package auth

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/mordilloSan/go_logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/common/config"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/server/bridge/handlers/control"
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

	// Create session without deciding privilege; helper will decide.
	sess, err := h.createUserSession(req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "session creation failed"})
		return
	}

	bridgeBinary := getBridgeBinary(h.BridgeBinaryOverride)
	privileged, err := startBridge(sess, req.Password, h.Env, h.Verbose, bridgeBinary)
	if err != nil {
		_ = h.SM.DeleteSession(sess.SessionID, session.ReasonManual)

		// Classify auth failures to 401; others 500.
		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "authentication failure") ||
			strings.Contains(msg, "authentication failed") ||
			strings.Contains(msg, "invalid credentials") ||
			strings.Contains(msg, "pam_") || strings.Contains(msg, "pam ") {
			logger.Warnf("[auth.login] authentication failed for user %s: %v", req.Username, err)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication failed"})
			return
		}

		logger.Errorf("[auth.login] failed to start bridge: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to start bridge"})
		return
	}

	// Verify bridge socket is ready with ping/pong
	var pingResult struct {
		Type string `json:"type"`
	}
	if err := callBridgeWithSess(sess, "control", "ping", nil, &pingResult); err != nil {
		logger.Errorf("[auth.login] bridge socket not ready after start: %v", err)
		_ = h.SM.DeleteSession(sess.SessionID, session.ReasonManual)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "bridge initialization failed"})
		return
	}

	// Ensure the response is a pong
	if pingResult.Type != "pong" {
		logger.Errorf("[auth.login] unexpected ping response type: %s", pingResult.Type)
		_ = h.SM.DeleteSession(sess.SessionID, session.ReasonManual)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "bridge initialization failed"})
		return
	}

	logger.Debugf("[auth.login] bridge confirmed ready (pong received)")

	// Persist actual mode (informational)
	_ = h.SM.SetPrivileged(sess.SessionID, privileged)

	secure := (h.Env == config.EnvProduction) && (c.Request.TLS != nil)
	if !secure && h.Env == config.EnvProduction {
		logger.Warnf("[auth.login] insecure cookie write under production env (no TLS detected)")
	}
	h.SM.WriteCookie(c.Writer, sess.SessionID)

	response := gin.H{
		"success":    true,
		"privileged": privileged,
	}

	// Only check for updates if user is privileged
	if privileged {
		if updateInfo := control.CheckForUpdate(); updateInfo != nil {
			response["update"] = updateInfo
		}
	}

	// Check if indexer daemon is available
	var indexerStatusResult struct {
		Available bool   `json:"available"`
		Error     string `json:"error,omitempty"`
	}
	if err := callBridgeWithSess(sess, "filebrowser", "indexer_status", nil, &indexerStatusResult); err != nil {
		logger.Debugf("[auth.login] failed to check indexer status: %v", err)
		// Don't fail login if indexer check fails, just log it
	} else {
		response["indexer_available"] = indexerStatusResult.Available
		if indexerStatusResult.Error != "" {
			logger.Debugf("[auth.login] indexer check returned error: %s", indexerStatusResult.Error)
		}
	}

	c.JSON(http.StatusOK, response)
}

func (h *Handlers) Logout(c *gin.Context) {
	ck, err := c.Request.Cookie(h.SM.CookieName())
	if err != nil {
		c.Status(http.StatusOK)
		return
	}

	h.SM.DeleteCookie(c.Writer)
	if err := h.SM.DeleteSession(ck.Value, session.ReasonLogout); err != nil {
		logger.ErrorKV("session delete failed", "error", err)
	}
	logger.InfoKV("session logout", "cookie_cleared", true)
	c.Status(http.StatusOK)
}

func (h *Handlers) Me(c *gin.Context) {
	sess := session.SessionFromContext(c)
	if sess == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "no active session"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"user":       sess.User,
		"privileged": sess.Privileged,
	})
}

// ---- internals ----

func (h *Handlers) createUserSession(req LoginRequest) (*session.Session, error) {
	sysu, err := lookupUser(req.Username)
	if err != nil {
		return nil, err
	}
	u := session.User{Username: req.Username, UID: sysu.Uid, GID: sysu.Gid}

	// Always create as non-privileged; helper decides real mode.
	sess, err := h.SM.CreateSession(u, false)
	if err != nil {
		logger.Errorf("Failed to create session: %v", err)
		return nil, err
	}
	return sess, nil
}
