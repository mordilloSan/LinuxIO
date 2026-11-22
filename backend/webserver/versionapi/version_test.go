package versionapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

// The endpoint answers without a session, with this build's version alone.
func TestVersionAnswersWithTheBuild(t *testing.T) {
	orig := version.Version
	t.Cleanup(func() { version.Version = orig })
	version.Version = "v9.9.9"

	mux := http.NewServeMux()
	RegisterRoutes(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/version", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got, want := rec.Body.String(), `{"version":"v9.9.9"}`; got != want {
		t.Fatalf("body = %s, want %s", got, want)
	}
}
