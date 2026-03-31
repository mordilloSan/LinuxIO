package auth

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/mordilloSan/go-logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/webserver/bridge"
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

type loginErrorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code,omitempty"`
}

func writeLoginError(w http.ResponseWriter, status int, code, message string) {
	web.WriteJSON(w, status, loginErrorResponse{
		Error: message,
		Code:  code,
	})
}

func (h *Handlers) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeLoginError(w, http.StatusBadRequest, "invalid_request", "invalid request")
		return
	}

	// Create session without deciding privilege; helper will decide.
	sess, err := h.createUserSession(req)
	if err != nil {
		writeLoginError(w, http.StatusInternalServerError, "session_creation_failed", "session creation failed")
		return
	}

	privileged, err := startBridge(sess, req.Password, h.Verbose)
	if err != nil {
		if delErr := h.SM.DeleteSession(sess.SessionID, session.ReasonManual); delErr != nil {
			logger.Warnf("[auth.login] failed to cleanup session after bridge error: %v", delErr)
		}

		var authErr *bridge.AuthError
		if errors.As(err, &authErr) && authErr.IsUnauthorized() {
			logger.Warnf("[auth.login] authentication failed for user %s: %v", req.Username, err)
			switch authErr.Code {
			case ipc.ResultPasswordExpired, ipc.ResultAccessDenied:
				msg := authErr.Message
				if msg == "" {
					msg = authErr.Code.DefaultMessage()
				}
				writeLoginError(w, http.StatusForbidden, authErr.Code.APIName(), msg)
				return
			default:
				writeLoginError(w, http.StatusUnauthorized, authErr.Code.APIName(), "authentication failed")
				return
			}
		}

		logger.Errorf("[auth.login] failed to start bridge: %v", err)
		writeLoginError(w, http.StatusInternalServerError, "bridge_error", "failed to start bridge")
		return
	}

	// Persist actual mode (informational)
	if setErr := h.SM.SetPrivileged(sess.SessionID, privileged); setErr != nil {
		logger.Warnf("[auth.login] failed to persist privilege mode: %v", setErr)
	}

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

// Version returns installed component versions (public endpoint, no auth required).
// Used by frontend to detect when server is back up after updates.
func (h *Handlers) Version(w http.ResponseWriter, r *http.Request) {
	// Get component versions from CLI command
	versions := getComponentVersions()
	if versions == nil {
		versions = make(map[string]string)
	}

	web.WriteJSON(w, http.StatusOK, versions)
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
