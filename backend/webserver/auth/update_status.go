package auth

import (
	"encoding/json"
	"net/http"
	"os"

	"github.com/mordilloSan/LinuxIO/backend/webserver/web"
)

const updateStatusPath = "/run/linuxio/update-status.json"

type updateStatusFile struct {
	ID         string `json:"id"`
	Status     string `json:"status"`
	ExitCode   *int   `json:"exit_code,omitempty"`
	StartedAt  *int64 `json:"started_at,omitempty"`
	FinishedAt *int64 `json:"finished_at,omitempty"`
}

type updateStatusResponse struct {
	Status     string `json:"status"`
	ID         string `json:"id,omitempty"`
	ExitCode   *int   `json:"exit_code,omitempty"`
	StartedAt  *int64 `json:"started_at,omitempty"`
	FinishedAt *int64 `json:"finished_at,omitempty"`
	Message    string `json:"message,omitempty"`
}

// UpdateStatus reports the last update status written by the update runner.
// Returns status=unknown if no update status file is present or if the run ID does not match.
func (h *Handlers) UpdateStatus(w http.ResponseWriter, r *http.Request) {
	data, err := os.ReadFile(updateStatusPath)
	if err != nil {
		if os.IsNotExist(err) {
			web.WriteJSON(w, http.StatusOK, updateStatusResponse{Status: "unknown"})
			return
		}
		web.WriteJSON(w, http.StatusInternalServerError, updateStatusResponse{
			Status:  "error",
			Message: err.Error(),
		})
		return
	}

	if len(data) == 0 {
		web.WriteJSON(w, http.StatusOK, updateStatusResponse{Status: "unknown"})
		return
	}

	var statusFile updateStatusFile
	if err := json.Unmarshal(data, &statusFile); err != nil {
		web.WriteJSON(w, http.StatusInternalServerError, updateStatusResponse{
			Status:  "error",
			Message: "invalid update status file",
		})
		return
	}

	if statusFile.Status == "" {
		web.WriteJSON(w, http.StatusOK, updateStatusResponse{Status: "unknown"})
		return
	}

	requestID := r.URL.Query().Get("id")
	if requestID != "" && statusFile.ID != requestID {
		web.WriteJSON(w, http.StatusOK, updateStatusResponse{Status: "unknown"})
		return
	}

	web.WriteJSON(w, http.StatusOK, updateStatusResponse{
		Status:     statusFile.Status,
		ID:         statusFile.ID,
		ExitCode:   statusFile.ExitCode,
		StartedAt:  statusFile.StartedAt,
		FinishedAt: statusFile.FinishedAt,
	})
}
