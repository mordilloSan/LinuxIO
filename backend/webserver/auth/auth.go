package auth

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/webserver/bridge"
	"github.com/mordilloSan/LinuxIO/backend/webserver/web"
)

const maxConcurrentLogins = 8

// Handlers bundles dependencies (no global state).
type Handlers struct {
	SM      *session.Manager
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
	Success                bool        `json:"success"`
	Privileged             bool        `json:"privileged"`
	DockerAvailable        bool        `json:"docker_available"`
	IndexerAvailable       bool        `json:"indexer_available"`
	LMSensorsAvailable     bool        `json:"lm_sensors_available"`
	SmartmontoolsAvailable bool        `json:"smartmontools_available"`
	PackageKitAvailable    bool        `json:"packagekit_available"`
	Update                 *UpdateInfo `json:"update,omitempty"`
}

func writeLoginError(w http.ResponseWriter, status int, code, message string) {
	web.WriteJSON(w, status, loginErrorResponse{
		Error: message,
		Code:  code,
	})
}

func (h *Handlers) Login(w http.ResponseWriter, r *http.Request) {
	select {
	case h.authSem <- struct{}{}:
		defer func() { <-h.authSem }()
	default:
		writeLoginError(w, http.StatusServiceUnavailable, "too_many_requests", "too many login attempts, try again shortly")
		return
	}

	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeLoginError(w, http.StatusBadRequest, "invalid_request", "invalid request")
		return
	}

	sessionID, err := h.SM.NewSessionID()
	if err != nil {
		writeLoginError(w, http.StatusInternalServerError, "session_creation_failed", "session creation failed")
		return
	}

	sess, err := startBridge(h.SM, sessionID, req.Username, req.Password, h.Verbose)
	if err != nil {
		var authErr *bridge.AuthError
		if errors.As(err, &authErr) && authErr.IsUnauthorized() {
			slog.Warn("authentication failed",
				"component", "auth",
				"subsystem", "login",
				"user", req.Username,
				"error", err)
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
		slog.Error("failed to start bridge",
			"component", "auth",
			"subsystem", "login",
			"user", req.Username,
			"session_id", sessionID,
			"error", err)
		writeLoginError(w, http.StatusInternalServerError, "bridge_error", "failed to start bridge")
		return
	}

	h.SM.WriteCookie(w, sess.SessionID)

	response := loginSuccessResponse{
		Success:                true,
		Privileged:             sess.Privileged,
		DockerAvailable:        sess.Capabilities.DockerAvailable,
		IndexerAvailable:       sess.Capabilities.IndexerAvailable,
		LMSensorsAvailable:     sess.Capabilities.LMSensorsAvailable,
		SmartmontoolsAvailable: sess.Capabilities.SmartmontoolsAvailable,
		PackageKitAvailable:    sess.Capabilities.PackageKitAvailable,
	}

	// Only check for updates if user is privileged
	if sess.Privileged {
		if updateInfo := CheckForUpdate(); updateInfo != nil {
			response.Update = updateInfo
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
		slog.Error("session delete failed", "error", err)
	}
	slog.Info("session logout", "cookie_cleared", true)
	w.WriteHeader(http.StatusOK)
}

// Version returns installed component versions (public endpoint, no auth required).
// Used by frontend to detect when server is back up after updates.
func (h *Handlers) Version(w http.ResponseWriter, r *http.Request) {
	web.WriteJSON(w, http.StatusOK, getComponentVersions(r.Context()))
}
