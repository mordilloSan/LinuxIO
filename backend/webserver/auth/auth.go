package auth

import (
	"encoding/json/v2"
	"errors"
	"log/slog"
	"net/http"
	"time"

	authipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/auth"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/webserver/authclient"
	"github.com/mordilloSan/LinuxIO/backend/webserver/sessions"
	"github.com/mordilloSan/LinuxIO/backend/webserver/web"
)

const (
	maxConcurrentLogins = 8
	// maxLoginBodyBytes bounds the credentials JSON; the auth protocol itself
	// caps usernames at 255 and passwords at 2047 bytes.
	maxLoginBodyBytes = 8 << 10
	loginBodyTimeout  = 10 * time.Second
)

// Handlers bundles dependencies (no global state).
type Handlers struct {
	SM      *sessions.Manager
	authSem chan struct{}
}

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginErrorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code,omitempty"`
}

type loginSuccessResponse struct {
	Success    bool `json:"success"`
	Privileged bool `json:"privileged"`
}

func writeLoginError(w http.ResponseWriter, status int, code, message string) {
	web.WriteJSON(w, status, loginErrorResponse{
		Error: message,
		Code:  code,
	})
}

func (h *Handlers) Login(w http.ResponseWriter, r *http.Request) {
	// Read the body before taking a slot, bounded in size and time, so a slow
	// or oversized client cannot hold one of the few login slots. The deadline
	// is best effort: test recorders do not support it.
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(loginBodyTimeout))
	var req LoginRequest
	if err := json.UnmarshalRead(http.MaxBytesReader(w, r.Body, maxLoginBodyBytes), &req, json.RejectUnknownMembers(true)); err != nil {
		writeLoginError(w, http.StatusBadRequest, "invalid_request", "invalid request")
		return
	}

	select {
	case h.authSem <- struct{}{}:
		defer func() { <-h.authSem }()
	default:
		writeLoginError(w, http.StatusServiceUnavailable, "too_many_requests", "too many login attempts, try again shortly")
		return
	}

	sessionID, err := h.SM.NewSessionID()
	if err != nil {
		writeLoginError(w, http.StatusInternalServerError, "session_creation_failed", "session creation failed")
		return
	}

	remoteHost := web.ClientRemoteHost(r)
	sess, err := startBridge(r.Context(), h.SM, sessionID, req.Username, req.Password, remoteHost)
	if err != nil {
		writeStartBridgeError(w, req.Username, remoteHost, sessionID, err)
		return
	}
	if r.Context().Err() != nil {
		// The client is gone, so the cookie can never be delivered.
		h.SM.DeleteSession(sess.SessionID, sessions.ReasonManual)
		return
	}

	h.SM.WriteCookie(w, sess.SessionID)
	slog.Info("authentication succeeded",
		"component", "auth",
		"subsystem", "login",
		"user", sess.User.Username,
		"session_ref", session.DiagnosticRef(sess.SessionID),
		"privileged", sess.Privileged,
		"remote_host", remoteHost)

	response := loginSuccessResponse{
		Success:    true,
		Privileged: sess.Privileged,
	}

	web.WriteJSON(w, http.StatusOK, response)
}

// writeStartBridgeError maps a startBridge failure to the login response.
func writeStartBridgeError(w http.ResponseWriter, username, remoteHost, sessionID string, err error) {
	var authErr *authclient.AuthError
	switch {
	case errors.Is(err, authipc.ErrInvalidField) || (errors.As(err, &authErr) && authErr.Code == authipc.ResultBadRequest):
		slog.Warn("login rejected: invalid credentials format",
			"component", "auth",
			"subsystem", "login",
			"remote_host", remoteHost,
			"error", err)
		writeLoginError(w, http.StatusBadRequest, "invalid_request", "invalid request")
	case errors.As(err, &authErr) && authErr.IsUnauthorized():
		slog.Warn("authentication failed",
			"component", "auth",
			"subsystem", "login",
			"user", username,
			"remote_host", remoteHost,
			"error", err)
		switch authErr.Code {
		case authipc.ResultPasswordExpired, authipc.ResultAccessDenied:
			msg := authErr.Message
			if msg == "" {
				msg = authErr.Code.DefaultMessage()
			}
			writeLoginError(w, http.StatusForbidden, authErr.Code.APIName(), msg)
		default:
			writeLoginError(w, http.StatusUnauthorized, authErr.Code.APIName(), "authentication failed")
		}
	default:
		slog.Error("failed to start bridge",
			"component", "auth",
			"subsystem", "login",
			"user", username,
			"remote_host", remoteHost,
			"session_ref", session.DiagnosticRef(sessionID),
			"error", err)
		writeLoginError(w, http.StatusInternalServerError, "bridge_error", "failed to start bridge")
	}
}

func (h *Handlers) Logout(w http.ResponseWriter, r *http.Request) {
	ck, err := r.Cookie(h.SM.CookieName())
	if err != nil {
		w.WriteHeader(http.StatusOK)
		return
	}

	h.SM.DeleteCookie(w)
	h.SM.DeleteSession(ck.Value, sessions.ReasonLogout)
	slog.Debug("session logout", "cookie_cleared", true)
	w.WriteHeader(http.StatusOK)
}
