package auth

import (
	jsonv2 "encoding/json/v2"
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
	Verbose bool
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
	if err := jsonv2.UnmarshalRead(http.MaxBytesReader(w, r.Body, maxLoginBodyBytes), &req, jsonv2.RejectUnknownMembers(true)); err != nil {
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
	sess, err := startBridge(h.SM, sessionID, req.Username, req.Password, remoteHost, h.Verbose)
	if err != nil {
		var authErr *authclient.AuthError
		if errors.As(err, &authErr) && authErr.IsUnauthorized() {
			slog.Warn("authentication failed",
				"component", "auth",
				"subsystem", "login",
				"user", req.Username,
				"remote_host", remoteHost,
				"error", err)
			switch authErr.Code {
			case authipc.ResultPasswordExpired, authipc.ResultAccessDenied:
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
		slog.Error("failed to start bridge",
			"component", "auth",
			"subsystem", "login",
			"user", req.Username,
			"remote_host", remoteHost,
			"session_ref", session.DiagnosticRef(sessionID),
			"error", err)
		writeLoginError(w, http.StatusInternalServerError, "bridge_error", "failed to start bridge")
		return
	}

	h.SM.WriteCookie(w, sess.SessionID)
	slog.Info("authentication succeeded",
		"component", "auth",
		"subsystem", "login",
		"user", sess.User.Username,
		"remote_host", remoteHost)

	response := loginSuccessResponse{
		Success:    true,
		Privileged: sess.Privileged,
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
	h.SM.DeleteSession(ck.Value, sessions.ReasonLogout)
	slog.Info("session logout", "cookie_cleared", true)
	w.WriteHeader(http.StatusOK)
}
