package cmd

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"

	"github.com/mordilloSan/LinuxIO/common/session"
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
	gin.SetMode(gin.TestMode)

	sm := session.NewManager(session.New(), session.SessionConfig{})
	cfg := Config{
		Env:      "development",
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

	// Arbitrary SPA path also redirects to Vite
	w = doGET(t, r, "/some/route?a=b")
	if w.Code != http.StatusTemporaryRedirect {
		t.Fatalf("expected 307, got %d", w.Code)
	}
	if !strings.Contains(w.Header().Get("Location"), "http://localhost:12345/some/route?a=b") {
		t.Fatalf("redirect Location unexpected: %q", w.Header().Get("Location"))
	}
}

func TestMountProductionSPA_ServesIndexAssetsAndFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Minimal virtual filesystem for the SPA
	ui := fstest.MapFS{
		"index.html":    &fstest.MapFile{Data: []byte("<html>OK</html>")},
		"assets/app.js": &fstest.MapFile{Data: []byte("console.log(1)")},
		"manifest.json": &fstest.MapFile{Data: []byte("{}")},
		"favicon.ico":   &fstest.MapFile{Data: []byte("ico")},
		"favicon-1.png": &fstest.MapFile{Data: []byte("p1")},
	}

	r := gin.New()
	mountProductionSPA(r, fs.FS(ui))

	// index
	w := doGET(t, r, "/")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "OK") {
		t.Fatalf("expected index.html, got code=%d body=%q", w.Code, w.Body.String())
	}

	// asset
	w = doGET(t, r, "/assets/app.js")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "console.log(1)") {
		t.Fatalf("expected asset content, got code=%d body=%q", w.Code, w.Body.String())
	}

	// manifest
	w = doGET(t, r, "/manifest.json")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "{}") {
		t.Fatalf("expected manifest.json, got code=%d body=%q", w.Code, w.Body.String())
	}

	// fallback (NoRoute -> index.html)
	w = doGET(t, r, "/some/unknown/route")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "OK") {
		t.Fatalf("expected SPA fallback to index.html, got code=%d body=%q", w.Code, w.Body.String())
	}
}
