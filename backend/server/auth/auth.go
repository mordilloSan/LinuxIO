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

	"github.com/mordilloSan/LinuxIO/common/logger"
	"github.com/mordilloSan/LinuxIO/common/session"
	"github.com/mordilloSan/LinuxIO/server/bridge"
	"github.com/mordilloSan/LinuxIO/server/filebrowser"
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

	if err := authenticateUser(req); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication failed"})
		return
	}

	privileged := trySudo(req.Password)

	sess, err := h.createUserSession(req, privileged)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "session creation failed"})
		return
	}

	if err := h.startBridgeSession(sess, req.Password); err != nil {
		_ = h.SM.DeleteSession(sess.SessionID, session.ReasonManual)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to start bridge"})
		return
	}

	// Write cookie via Manager (mirrors SCS style).
	secure := (h.Env == "production") && (c.Request.TLS != nil)
	if !secure && h.Env == "production" {
		// In case you terminate TLS upstream and want Secure anyway, set Cookie.Secure in Manager config.
		logger.Warnf("[auth.login] insecure cookie write under production env (no TLS detected)")
	}
	h.SM.WriteCookie(c.Writer, sess.SessionID)

	// Navigator defaults (best-effort).
	if err := filebrowser.ApplyNavigatorDefaults(c, sess); err != nil {
		logger.Warnf("[auth.login] navigator defaults failed for user=%s: %v", sess.User.Username, err)
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "privileged": sess.Privileged})
}

func (h *Handlers) Logout(c *gin.Context) {
	// Read cookie by name from Manager config
	ck, err := c.Request.Cookie(h.SMCookieName())
	if err != nil {
		c.Status(http.StatusOK)
		return
	}

	// Clear cookie first
	h.SM.DeleteCookie(c.Writer)

	// Delete session (hooks will do cleanup)
	if err := h.SM.DeleteSession(ck.Value, session.ReasonLogout); err != nil {
		logger.Errorf("Failed to delete session %q: %v", ck.Value, err)
	}

	logger.Infof("👋 Logged out session: %s", ck.Value)
	c.Status(http.StatusOK)
}

func (h *Handlers) Me(c *gin.Context) {
	// This relies on sm.RequireSession() having run earlier.
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
		return nil, fmt.Errorf("lookup user: %w", err)
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
	if err := bridge.StartBridge(sess, password, h.Env, h.Verbose, bridgeBinary); err != nil {
		if sess.Privileged {
			logger.Warnf("Privileged bridge failed, retrying unprivileged: %v", err)
			_ = h.SM.SetPrivileged(sess.SessionID, false) // persist
			sess.Privileged = false                       // keep local copy in sync
			if err2 := bridge.StartBridge(sess, password, h.Env, h.Verbose, bridgeBinary); err2 != nil {
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

func (h *Handlers) SMCookieName() string              { return h.SMCookieCfg().Name }
func (h *Handlers) SMCookieCfg() session.CookieConfig { return h.SMConfig().Cookie }
func (h *Handlers) SMConfig() session.SessionConfig   { return h.SMConfigUnsafe() }

// SMConfigUnsafe: quick helper to get the effective config (unexported in Manager).
// If you prefer to avoid this helper, just hardcode cookie name from your config
// at wire-up time and pass it into Handlers as a string.
func (h *Handlers) SMConfigUnsafe() session.SessionConfig {
	// NOTE: Manager doesn't expose cfg, so either:
	// 1) store cookie name in Handlers at construction, or
	// 2) add an exported getter on Manager.
	// For now, assume cookie name = "session_id" (your default).
	return session.SessionConfig{Cookie: session.CookieConfig{Name: "session_id"}}
}

// ---- auth primitives ----

func authenticateUser(req LoginRequest) error {
	if err := pamAuth(req.Username, req.Password); err != nil {
		logger.Warnf("❌ Authentication failed for user: %s", req.Username)
		return err
	}
	return nil
}

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
