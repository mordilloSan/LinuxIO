package appupdate

import (
	"errors"
	"net/http"

	"github.com/mordilloSan/LinuxIO/backend/common/updatestatus"
	"github.com/mordilloSan/LinuxIO/backend/webserver/sessions"
	"github.com/mordilloSan/LinuxIO/backend/webserver/web"
)

type updateStatusResponse struct {
	Status     string `json:"status"`
	ID         string `json:"id,omitempty"`
	ExitCode   *int   `json:"exit_code,omitempty"`
	StartedAt  *int64 `json:"started_at,omitempty"`
	FinishedAt *int64 `json:"finished_at,omitempty"`
	Message    string `json:"message,omitempty"`
}

// handleUpdateStatus reports the last update status written by the update runner.
// Returns status=unknown if no update status file is present or if the run ID does not match.
func handleUpdateStatus(w http.ResponseWriter, r *http.Request) {
	sess := sessions.SessionFromContext(r.Context())
	if sess == nil {
		web.WriteJSON(w, http.StatusUnauthorized, updateStatusResponse{Status: "unknown"})
		return
	}
	statusFile, err := updatestatus.Read()
	if err != nil {
		if errors.Is(err, updatestatus.ErrInvalid) {
			web.WriteJSON(w, http.StatusInternalServerError, updateStatusResponse{
				Status:  "error",
				Message: "invalid update status file",
			})
			return
		}
		web.WriteJSON(w, http.StatusInternalServerError, updateStatusResponse{Status: "error", Message: "failed to read update status"})
		return
	}

	if statusFile == nil || statusFile.OwnerUID != sess.User.UID || !statusFile.Valid() {
		web.WriteJSON(w, http.StatusOK, updateStatusResponse{Status: "unknown"})
		return
	}

	requestID := r.URL.Query().Get("id")
	if requestID == "" || statusFile.ID != requestID {
		web.WriteJSON(w, http.StatusOK, updateStatusResponse{Status: "unknown"})
		return
	}

	web.WriteJSON(w, http.StatusOK, updateStatusResponse{
		Status:     statusFile.Status,
		ID:         statusFile.ID,
		ExitCode:   statusFile.ExitCode,
		StartedAt:  statusFile.StartedAt,
		FinishedAt: statusFile.FinishedAt,
		Message:    statusFile.Error,
	})
}
