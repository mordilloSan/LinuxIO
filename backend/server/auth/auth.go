package auth

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/server/bridge/handlers/control"
	"github.com/mordilloSan/go_logger/logger"
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
	pingResp, err := callBridgeWithSess(sess, "control", "ping", nil)
	if err != nil {
		logger.Errorf("[auth.login] bridge socket not ready after start: %v", err)
		_ = h.SM.DeleteSession(sess.SessionID, session.ReasonManual)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "bridge initialization failed"})
		return
	}

	// Parse and validate ping response
	var pingResult map[string]interface{}
	if err := json.Unmarshal(pingResp, &pingResult); err != nil {
		logger.Errorf("[auth.login] invalid ping response format: %v", err)
		_ = h.SM.DeleteSession(sess.SessionID, session.ReasonManual)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "bridge initialization failed"})
		return
	}

	// Check the wrapper status first
	if status, ok := pingResult["status"].(string); !ok || status != "ok" {
		logger.Errorf("[auth.login] ping failed: %s", string(pingResp))
		_ = h.SM.DeleteSession(sess.SessionID, session.ReasonManual)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "bridge initialization failed"})
		return
	}

	// Extract the nested output and verify it's a pong
	output, ok := pingResult["output"].(map[string]interface{})
	if !ok || output["type"] != "pong" {
		logger.Errorf("[auth.login] unexpected ping response: %s", string(pingResp))
		_ = h.SM.DeleteSession(sess.SessionID, session.ReasonManual)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "bridge initialization failed"})
		return
	}

	logger.Debugf("[auth.login] bridge confirmed ready (pong received)")

	// Persist actual mode (informational)
	_ = h.SM.SetPrivileged(sess.SessionID, privileged)

	secure := (h.Env == "production") && (c.Request.TLS != nil)
	if !secure && h.Env == "production" {
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
