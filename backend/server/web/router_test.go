package web

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/mordilloSan/LinuxIO/backend/common/config"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// helper to perform a GET
func doGET(t *testing.T, r http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	r.ServeHTTP(w, req)
	return w
}

func TestBuildRouter_DevRedirectsToVite(t *testing.T) {
	sm := session.NewManager(session.New(), session.SessionConfig{})
	cfg := Config{
		Env:      config.EnvDevelopment,
		Verbose:  false,
		VitePort: 12345,
		UI:       fstest.MapFS{},
	}

	r := BuildRouter(cfg, sm)

	// Root redirect to Vite
	w := doGET(t, r, "/")
	if w.Code != http.StatusTemporaryRedirect {
		t.Fatalf("expected 307, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if loc != "http://localhost:12345/" {
		t.Fatalf("expected vite redirect, got %q", loc)
	}
}

func TestMountProductionSPA_ServesIndexAssetsAndFallback(t *testing.T) {
	// Minimal virtual filesystem for the SPA
	ui := fstest.MapFS{
		"index.html":    &fstest.MapFile{Data: []byte("<html>OK</html>")},
		"assets/app.js": &fstest.MapFile{Data: []byte("console.log(1)")},
		"manifest.json": &fstest.MapFile{Data: []byte("{}")},
		"favicon.ico":   &fstest.MapFile{Data: []byte("ico")},
		"favicon-1.png": &fstest.MapFile{Data: []byte("p1")},
	}

	mux := http.NewServeMux()
	mountProductionSPA(mux, fs.FS(ui))

	// index
	w := doGET(t, mux, "/")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "OK") {
		t.Fatalf("expected index.html, got code=%d body=%q", w.Code, w.Body.String())
	}

	// asset
	w = doGET(t, mux, "/assets/app.js")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "console.log(1)") {
		t.Fatalf("expected asset content, got code=%d body=%q", w.Code, w.Body.String())
	}

	// manifest
	w = doGET(t, mux, "/manifest.json")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "{}") {
		t.Fatalf("expected manifest.json, got code=%d body=%q", w.Code, w.Body.String())
	}

	// fallback (unknown routes -> index.html via "/" catch-all)
	w = doGET(t, mux, "/some/unknown/route")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "OK") {
		t.Fatalf("expected SPA fallback to index.html, got code=%d body=%q", w.Code, w.Body.String())
	}
}
