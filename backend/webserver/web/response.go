package web

import (
	"encoding/json/v2"
	"log/slog"
	"net/http"
)

// WriteJSON writes a JSON response with the given status code.
func WriteJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.MarshalWrite(w, data); err != nil {
		slog.Warn("failed to encode JSON response", "error", err)
	}
}
