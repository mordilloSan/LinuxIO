package auth

import (
	"bytes"
	"context"
	"fmt"
	"io"
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

	if err := authenticateUser(req); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication failed"})
		return
	}

	privileged := trySudo(req.Username, req.Password)

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

	// Write cookie via Manager
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

// ---- auth primitives ----

func authenticateUser(req LoginRequest) error {
	if err := pamAuth(req.Username, req.Password); err != nil {
		logger.Warnf("uthentication failed for user %s: %v", req.Username, err)
		return err
	}
	return nil
}

const pamHelperDefault = "/usr/local/bin/linuxio-auth-helper"

func pamAuth(username, password string) error {
	helper := os.Getenv("LINUXIO_PAM_HELPER")
	if helper == "" {
		helper = pamHelperDefault
	}
	logger.Debugf("Invoking PAM helper %s for user %s", helper, username)

	cmd := exec.Command(helper, username)
	cmd.Env = append(os.Environ(), "LANG=C")
	cmd.Stdout = io.Discard
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("pam helper stdin: %w", err)
	}

	pwBytes := []byte(password + "\n")
	go func() {
		defer func() {
			zeroBytes(pwBytes)
			if cerr := stdin.Close(); cerr != nil {
				logger.Warnf("failed to close pam helper stdin: %v", cerr)
			}
		}()
		if _, werr := stdin.Write(pwBytes); werr != nil {
			logger.Warnf("failed to write password to pam helper: %v", werr)
		}
	}()

	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = err.Error()
		}
		return fmt.Errorf("pam helper failed: %s", errMsg)
	}
	return nil
}

func zeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// trySudo silently validates whether the given password unlocks sudo.
func trySudo(username, password string) bool {
	if username == "" || password == "" {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_ = exec.Command("sudo", "-k").Run() // invalidate timestamp for current user

	// If the server runs as root or as the same user, use sudo directly.

	var cmd *exec.Cmd
	var stdinData string

	// Switch to the login user first, then validate sudo inside that context.
	cmd = exec.CommandContext(ctx, "su", "--preserve-environment", username, "-c", "sudo -k; sudo -S -p '' -v")
	stdinData = password + "\n" + password + "\n"

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
