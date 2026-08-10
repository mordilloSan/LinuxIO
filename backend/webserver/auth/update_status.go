package auth

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/mordilloSan/LinuxIO/backend/common/durabletask"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/webserver/web"
)

var updateStatusStoreRoot = durabletask.DefaultRoot

const appUpdateRoute = "control.app_update"

type updateStatusResponse struct {
	Status     string `json:"status"`
	ID         string `json:"id,omitempty"`
	ExitCode   *int   `json:"exit_code,omitempty"`
	StartedAt  *int64 `json:"started_at,omitempty"`
	FinishedAt *int64 `json:"finished_at,omitempty"`
	Message    string `json:"message,omitempty"`
}

// UpdateStatus reports one durable app update owned by the authenticated UID.
// A different UID receives the same unknown response as a missing operation.
func (h *Handlers) UpdateStatus(w http.ResponseWriter, r *http.Request) {
	sess := session.SessionFromContext(r.Context())
	if sess == nil {
		web.WriteJSON(w, http.StatusUnauthorized, updateStatusResponse{Status: "unknown"})
		return
	}
	id := r.URL.Query().Get("id")
	if durabletask.ValidateID(id) != nil {
		web.WriteJSON(w, http.StatusOK, updateStatusResponse{Status: "unknown"})
		return
	}

	store := durabletask.NewStore(updateStatusStoreRoot)
	record, err := store.Get(r.Context(), id, sess.User.UID)
	if err != nil {
		if errors.Is(err, durabletask.ErrNotFound) {
			web.WriteJSON(w, http.StatusOK, updateStatusResponse{Status: "unknown"})
			return
		}
		web.WriteJSON(w, http.StatusInternalServerError, updateStatusResponse{Status: "error", Message: "failed to read update status"})
		return
	}
	if record.Route != appUpdateRoute {
		web.WriteJSON(w, http.StatusOK, updateStatusResponse{Status: "unknown"})
		return
	}

	if !record.Terminal() {
		if result, resultErr := store.ReadExecutorResult(record.ID); resultErr == nil {
			if terminal, applyErr := store.ApplyExecutorResult(r.Context(), record.UID, result); applyErr == nil {
				record = terminal
			}
		}
	}
	web.WriteJSON(w, http.StatusOK, updateStatusFromRecord(record))
}

func updateStatusFromRecord(record durabletask.Record) updateStatusResponse {
	response := updateStatusResponse{ID: record.ID}
	if record.StartedAt != nil {
		value := record.StartedAt.Unix()
		response.StartedAt = &value
	}
	if record.FinishedAt != nil {
		value := record.FinishedAt.Unix()
		response.FinishedAt = &value
	}
	if len(record.Result) > 0 {
		var result struct {
			ExitCode int `json:"exit_code"`
		}
		if json.Unmarshal(record.Result, &result) == nil {
			response.ExitCode = &result.ExitCode
		}
	}
	if record.Error != nil {
		response.Message = record.Error.Message
		if response.ExitCode == nil && record.Error.Code != 0 {
			response.ExitCode = &record.Error.Code
		}
	}

	switch record.State {
	case durabletask.StateCompleted:
		response.Status = "ok"
	case durabletask.StateFailed, durabletask.StateCanceled, durabletask.StateUnknown:
		response.Status = "error"
	default:
		response.Status = "running"
	}
	return response
}
