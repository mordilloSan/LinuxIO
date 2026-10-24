package daemon

import (
	"encoding/json/v2"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/indexer/api"
)

func writeJSON(w http.ResponseWriter, v any) {
	writeJSONStatus(w, http.StatusOK, v)
}

func writeJSONStatus(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.MarshalWrite(w, v); err != nil {
		slog.Warn("failed to encode JSON response", "err", err)
	}
}

// validateRelativePath checks if a path is safe (doesn't escape the root via traversal).
// Only ".." path segments are rejected; names that merely contain consecutive
// dots (e.g. "notes..txt") are valid.
func validateRelativePath(p string) bool {
	return !slices.Contains(strings.Split(p, "/"), "..")
}

// queryPathOrRoot returns the path query parameter or "/" if empty.
func queryPathOrRoot(path string) (string, bool) {
	if path == "" {
		return "/", true
	}
	// Validate path to prevent traversal attempts
	if !validateRelativePath(path) {
		return "", false
	}
	return api.NormalizePath(path), true
}
