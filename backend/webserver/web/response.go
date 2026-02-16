package web

import (
	"encoding/json"
	"net/http"

	"github.com/mordilloSan/go-logger/logger"
)

// WriteJSON writes a JSON response with the given status code.
func WriteJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		logger.Warnf("failed to encode JSON response: %v", err)
	}
}

// WriteError writes a JSON error response with the given status code.
func WriteError(w http.ResponseWriter, status int, message string) {
	WriteJSON(w, status, map[string]string{"error": message})
}
