package web

import (
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/mordilloSan/go_logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// Config holds router configuration.
type Config struct {
	Env            string
	Verbose        bool
	UI             fs.FS
	RegisterRoutes func(mux *http.ServeMux) // Called to register API routes
}

// BuildRouter constructs and returns the main HTTP handler.
func BuildRouter(cfg Config, sm *session.Manager) http.Handler {
	mux := http.NewServeMux()

	// Apply middleware chain
	var handler http.Handler = mux
	handler = LoggerMiddleware(handler)
	handler = RecoveryMiddleware(handler)

	// Register API routes (auth, etc.)
	if cfg.RegisterRoutes != nil {
		cfg.RegisterRoutes(mux)
	}

	// WebSocket relay (protected)
	mux.Handle("GET /ws", sm.RequireSession(http.HandlerFunc(WebSocketRelayHandler)))

	// Serve embedded SPA
	mountProductionSPA(mux, cfg.UI)

	return handler
}

// mountProductionSPA serves the embedded frontend with SPA fallback.
func mountProductionSPA(mux *http.ServeMux, ui fs.FS) {
	// Serve /assets/ directly
	mux.Handle("/assets/", http.FileServer(http.FS(ui)))

	// Serve specific root files
	rootFiles := []string{"manifest.json", "favicon.ico", "favicon-1.png", "favicon-2.png"}
	for _, f := range rootFiles {
		fileName := f
		mux.HandleFunc("/"+fileName, func(w http.ResponseWriter, r *http.Request) {
			serveFileFS(w, r, ui, fileName)
		})
	}

	// Catch-all: serve index.html for SPA routing
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// If it's a real file in assets, let it through (already handled above)
		// Otherwise serve index.html
		serveFileFS(w, r, ui, "index.html")
	})
}

// serveFileFS serves a single file from the fs.FS.
func serveFileFS(w http.ResponseWriter, r *http.Request, fsys fs.FS, name string) {
	f, err := fsys.Open(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Set content type based on extension
	ext := path.Ext(name)
	switch ext {
	case ".html":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	case ".js":
		w.Header().Set("Content-Type", "application/javascript")
	case ".css":
		w.Header().Set("Content-Type", "text/css")
	case ".json":
		w.Header().Set("Content-Type", "application/json")
	case ".png":
		w.Header().Set("Content-Type", "image/png")
	case ".ico":
		w.Header().Set("Content-Type", "image/x-icon")
	}

	// If it's a ReadSeeker, use http.ServeContent for proper caching
	if rs, ok := f.(io.ReadSeeker); ok {
		http.ServeContent(w, r, name, stat.ModTime(), rs)
		return
	}

	// Fallback: just copy the content
	_, _ = io.Copy(w, f)
}

// HTTPErrorLogAdapter adapts logger.Warnf to the log.Logger interface for http.Server.ErrorLog.
type HTTPErrorLogAdapter struct{}

func (HTTPErrorLogAdapter) Write(p []byte) (n int, err error) {
	msg := strings.TrimSpace(string(p))
	// Filter out noisy "TLS handshake error" messages from scanners
	if strings.Contains(msg, "TLS handshake error") {
		return len(p), nil
	}
	logger.Warnf("[http.Server] %s", msg)
	return len(p), nil
}
