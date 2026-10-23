package web

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gorilla/websocket"

	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/webserver/sessions"
)

// helper to perform a GET
func doGET(t *testing.T, r http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	return doGETWithHeaders(t, r, path, nil)
}

func doGETWithHeaders(t *testing.T, r http.Handler, path string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	r.ServeHTTP(w, req)
	return w
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

func TestMountProductionSPA_ServesPrecompressedAssetWhenAccepted(t *testing.T) {
	ui := fstest.MapFS{
		"index.html":              &fstest.MapFile{Data: []byte("<html>OK</html>")},
		"assets/app-abc123.js":    &fstest.MapFile{Data: []byte("console.log(1)")},
		"assets/app-abc123.js.gz": &fstest.MapFile{Data: []byte("gzipped-js")},
	}

	mux := http.NewServeMux()
	mountProductionSPA(mux, fs.FS(ui))

	w := doGETWithHeaders(t, mux, "/assets/app-abc123.js", map[string]string{
		"Accept-Encoding": "br, gzip",
	})

	if w.Code != http.StatusOK {
		t.Fatalf("expected status OK, got %d", w.Code)
	}
	if got := w.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("expected gzip content encoding, got %q", got)
	}
	if got := w.Header().Get("Content-Type"); !strings.Contains(got, "application/javascript") {
		t.Fatalf("expected JavaScript content type, got %q", got)
	}
	if got := w.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("expected immutable asset cache header, got %q", got)
	}
	if got := w.Header().Get("Vary"); got != "Accept-Encoding" {
		t.Fatalf("expected Vary: Accept-Encoding, got %q", got)
	}
	if got := w.Body.String(); got != "gzipped-js" {
		t.Fatalf("expected gzipped asset body, got %q", got)
	}
}

func TestMountProductionSPA_ServesBrotliAssetWhenPreferred(t *testing.T) {
	ui := fstest.MapFS{
		"index.html":              &fstest.MapFile{Data: []byte("<html>OK</html>")},
		"assets/app-abc123.js":    &fstest.MapFile{Data: []byte("console.log(1)")},
		"assets/app-abc123.js.br": &fstest.MapFile{Data: []byte("brotli-js")},
		"assets/app-abc123.js.gz": &fstest.MapFile{Data: []byte("gzipped-js")},
	}

	mux := http.NewServeMux()
	mountProductionSPA(mux, fs.FS(ui))

	w := doGETWithHeaders(t, mux, "/assets/app-abc123.js", map[string]string{
		"Accept-Encoding": "gzip, br",
	})

	if w.Code != http.StatusOK {
		t.Fatalf("expected status OK, got %d", w.Code)
	}
	if got := w.Header().Get("Content-Encoding"); got != "br" {
		t.Fatalf("expected br content encoding, got %q", got)
	}
	if got := w.Header().Get("Content-Type"); !strings.Contains(got, "application/javascript") {
		t.Fatalf("expected JavaScript content type, got %q", got)
	}
	if got := w.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("expected immutable asset cache header, got %q", got)
	}
	if got := w.Header().Get("Vary"); got != "Accept-Encoding" {
		t.Fatalf("expected Vary: Accept-Encoding, got %q", got)
	}
	if got := w.Body.String(); got != "brotli-js" {
		t.Fatalf("expected brotli asset body, got %q", got)
	}
}

func TestMountProductionSPA_RespectsEncodingQuality(t *testing.T) {
	ui := fstest.MapFS{
		"index.html":              &fstest.MapFile{Data: []byte("<html>OK</html>")},
		"assets/app-abc123.js":    &fstest.MapFile{Data: []byte("console.log(1)")},
		"assets/app-abc123.js.br": &fstest.MapFile{Data: []byte("brotli-js")},
		"assets/app-abc123.js.gz": &fstest.MapFile{Data: []byte("gzipped-js")},
	}

	mux := http.NewServeMux()
	mountProductionSPA(mux, fs.FS(ui))

	w := doGETWithHeaders(t, mux, "/assets/app-abc123.js", map[string]string{
		"Accept-Encoding": "br;q=0.5, gzip;q=1",
	})

	if w.Code != http.StatusOK {
		t.Fatalf("expected status OK, got %d", w.Code)
	}
	if got := w.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("expected gzip content encoding, got %q", got)
	}
	if got := w.Body.String(); got != "gzipped-js" {
		t.Fatalf("expected gzip asset body, got %q", got)
	}
}

func TestMountProductionSPA_ServesRawAssetWhenGzipRejected(t *testing.T) {
	ui := fstest.MapFS{
		"index.html":              &fstest.MapFile{Data: []byte("<html>OK</html>")},
		"assets/app-abc123.js":    &fstest.MapFile{Data: []byte("console.log(1)")},
		"assets/app-abc123.js.gz": &fstest.MapFile{Data: []byte("gzipped-js")},
	}

	mux := http.NewServeMux()
	mountProductionSPA(mux, fs.FS(ui))

	w := doGETWithHeaders(t, mux, "/assets/app-abc123.js", map[string]string{
		"Accept-Encoding": "gzip;q=0, *;q=1",
	})

	if w.Code != http.StatusOK {
		t.Fatalf("expected status OK, got %d", w.Code)
	}
	if got := w.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("expected raw response without content encoding, got %q", got)
	}
	if got := w.Header().Get("Vary"); got != "Accept-Encoding" {
		t.Fatalf("expected Vary: Accept-Encoding, got %q", got)
	}
	if got := w.Body.String(); got != "console.log(1)" {
		t.Fatalf("expected raw asset body, got %q", got)
	}
}

func TestMountProductionSPA_ServesRawIndexWhenPrecompressedSidecarExists(t *testing.T) {
	ui := fstest.MapFS{
		"index.html":    &fstest.MapFile{Data: []byte("<html>OK</html>")},
		"index.html.gz": &fstest.MapFile{Data: []byte("gzipped-index")},
	}

	mux := http.NewServeMux()
	mountProductionSPA(mux, fs.FS(ui))

	w := doGETWithHeaders(t, mux, "/", map[string]string{
		"Accept-Encoding": "gzip",
	})

	if w.Code != http.StatusOK {
		t.Fatalf("expected status OK, got %d", w.Code)
	}
	if got := w.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("expected raw response without content encoding, got %q", got)
	}
	if got := w.Header().Get("Content-Type"); !strings.Contains(got, "text/html") {
		t.Fatalf("expected HTML content type, got %q", got)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("expected no-cache index header, got %q", got)
	}
	if got := w.Body.String(); got != "<html>OK</html>" {
		t.Fatalf("expected raw index body, got %q", got)
	}
}

func TestMountProductionSPA_ServesRawManifestWhenPrecompressedSidecarExists(t *testing.T) {
	ui := fstest.MapFS{
		"index.html":       &fstest.MapFile{Data: []byte("<html>OK</html>")},
		"manifest.json":    &fstest.MapFile{Data: []byte(`{"name":"LinuxIO"}`)},
		"manifest.json.gz": &fstest.MapFile{Data: []byte("gzipped-manifest")},
	}

	mux := http.NewServeMux()
	mountProductionSPA(mux, fs.FS(ui))

	w := doGETWithHeaders(t, mux, "/manifest.json", map[string]string{
		"Accept-Encoding": "gzip",
	})

	if w.Code != http.StatusOK {
		t.Fatalf("expected status OK, got %d", w.Code)
	}
	if got := w.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("expected raw response without content encoding, got %q", got)
	}
	if got := w.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("expected JSON content type, got %q", got)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("expected no-cache manifest header, got %q", got)
	}
	if got := w.Body.String(); got != `{"name":"LinuxIO"}` {
		t.Fatalf("expected raw manifest body, got %q", got)
	}
}

func TestWebSocketOriginValidation(t *testing.T) {
	for _, authenticated := range []bool{false, true} {
		for _, origin := range []string{"", "same-origin", "http://untrusted.test", "http://127.0.0.1:1", "null"} {
			t.Run(origin+map[bool]string{false: "/anonymous", true: "/authenticated"}[authenticated], func(t *testing.T) {
				checkWebSocketOrigin(t, authenticated, origin)
			})
		}
	}
}

func checkWebSocketOrigin(t *testing.T, authenticated bool, origin string) {
	t.Helper()
	cfg := sessions.DefaultConfig
	cfg.Cookie.Secure = false
	cfg.GCInterval = 0
	sm := sessions.NewManager(cfg)
	t.Cleanup(sm.Close)
	server := httptest.NewServer(BuildRouter(Config{}, sm))
	t.Cleanup(server.Close)
	header := http.Header{}
	if origin == "same-origin" {
		header.Set("Origin", server.URL)
	} else if origin != "" {
		header.Set("Origin", origin)
	}
	if authenticated {
		sess, err := sm.CreateSession("origin-test", session.User{Username: "review", UID: 1000, GID: 1000}, false)
		if err != nil {
			t.Fatal(err)
		}
		header.Add("Cookie", (&http.Cookie{Name: sm.CookieName(), Value: sess.SessionID}).String())
	}
	conn, response, err := websocket.DefaultDialer.Dial(wsURL(server.URL, "/ws"), header)
	if origin != "" && origin != "same-origin" {
		if err == nil {
			conn.Close()
			t.Fatal("accepted an untrusted origin")
		}
		if response == nil || response.StatusCode != http.StatusForbidden {
			t.Fatalf("expected HTTP 403, got response=%v error=%v", response, err)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
}

func wsURL(httpURL string, path string) string {
	return "ws" + strings.TrimPrefix(httpURL, "http") + path
}
