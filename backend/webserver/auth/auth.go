package auth

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/mordilloSan/go_logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/webserver/web"
)

// Handlers bundles dependencies (no global state).
type Handlers struct {
	SM      *session.Manager
	Verbose bool
}

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (h *Handlers) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		web.WriteError(w, http.StatusBadRequest, "invalid request")
		return
	}

	// Create session without deciding privilege; helper will decide.
	sess, err := h.createUserSession(req)
	if err != nil {
		web.WriteError(w, http.StatusInternalServerError, "session creation failed")
		return
	}

	privileged, err := startBridge(sess, req.Password, h.Verbose)
	if err != nil {
		_ = h.SM.DeleteSession(sess.SessionID, session.ReasonManual)

		// Classify auth failures to 401; others 500.
		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "authentication failure") ||
			strings.Contains(msg, "authentication failed") ||
			strings.Contains(msg, "invalid credentials") ||
			strings.Contains(msg, "pam_") || strings.Contains(msg, "pam ") {
			logger.Warnf("[auth.login] authentication failed for user %s: %v", req.Username, err)
			web.WriteError(w, http.StatusUnauthorized, "authentication failed")
			return
		}

		logger.Errorf("[auth.login] failed to start bridge: %v", err)
		web.WriteError(w, http.StatusInternalServerError, "failed to start bridge")
		return
	}

	// Persist actual mode (informational)
	_ = h.SM.SetPrivileged(sess.SessionID, privileged)

	h.SM.WriteCookie(w, sess.SessionID)

	response := map[string]any{
		"success":    true,
		"privileged": privileged,
	}

	// Only check for updates if user is privileged
	if privileged {
		if updateInfo := CheckForUpdate(); updateInfo != nil {
			response["update"] = updateInfo
		}
	}

	web.WriteJSON(w, http.StatusOK, response)
}

func (h *Handlers) Logout(w http.ResponseWriter, r *http.Request) {
	ck, err := r.Cookie(h.SM.CookieName())
	if err != nil {
		w.WriteHeader(http.StatusOK)
		return
	}

	h.SM.DeleteCookie(w)
	if err := h.SM.DeleteSession(ck.Value, session.ReasonLogout); err != nil {
		logger.ErrorKV("session delete failed", "error", err)
	}
	logger.InfoKV("session logout", "cookie_cleared", true)
	w.WriteHeader(http.StatusOK)
}

func (h *Handlers) Me(w http.ResponseWriter, r *http.Request) {
	sess := session.SessionFromContext(r.Context())
	if sess == nil {
		web.WriteError(w, http.StatusUnauthorized, "no active session")
		return
	}
	web.WriteJSON(w, http.StatusOK, map[string]any{
		"user":       sess.User,
		"privileged": sess.Privileged,
	})
}

// ---- internals ----

func (h *Handlers) createUserSession(req LoginRequest) (*session.Session, error) {
	u, err := lookupUser(req.Username)
	if err != nil {
		return nil, err
	}

	// Always create as non-privileged; helper decides real mode.
	sess, err := h.SM.CreateSession(u, false)
	if err != nil {
		logger.Errorf("Failed to create session: %v", err)
		return nil, err
	}
	return sess, nil
}
