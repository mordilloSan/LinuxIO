package web

import (
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/mordilloSan/go_logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// Config holds router configuration.
type Config struct {
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

	// Serve module static files
	// SPA routes (no file extension) are public - React handles auth
	// File routes (.js, .css, etc.) require session
	mux.Handle("/modules/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		urlPath := strings.TrimPrefix(r.URL.Path, "/modules/")
		ext := filepath.Ext(urlPath)

		// SPA route - serve index.html, let React handle auth
		if urlPath == "" || ext == "" {
			serveFileFS(w, r, cfg.UI, "index.html")
			return
		}

		// File route - require session
		sm.RequireSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ServeModuleFiles(w, r, cfg.UI)
		})).ServeHTTP(w, r)
	}))

	// Serve embedded SPA
	mountProductionSPA(mux, cfg.UI)

	return handler
}

// mountProductionSPA serves the embedded frontend with SPA fallback.
func mountProductionSPA(mux *http.ServeMux, ui fs.FS) {
	// Serve /assets/ directly
	mux.Handle("/assets/", http.FileServer(http.FS(ui)))

	// Serve specific root files
	rootFiles := []string{"manifest.json", "favicon-1.png", "favicon-2.png", "favicon-3.png", "favicon-4.png", "favicon-5.png", "favicon-6.png"}
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

// ServeModuleFiles serves static files for modules from their directories
// Note: SPA routes are handled by the router before this is called
func ServeModuleFiles(w http.ResponseWriter, r *http.Request, ui fs.FS) {
	// Extract path like: /modules/example-module/component.js
	urlPath := strings.TrimPrefix(r.URL.Path, "/modules/")
	ext := filepath.Ext(urlPath)

	// Security: prevent directory traversal
	if strings.Contains(urlPath, "..") {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	// Try user modules first, then system modules
	userHome := os.Getenv("HOME")
	if userHome == "" {
		userHome = "/root"
	}

	paths := []string{
		filepath.Join(userHome, ".config/linuxio/modules", urlPath),
		filepath.Join("/etc/linuxio/modules", urlPath),
	}

	var filePath string
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			filePath = p
			break
		}
	}

	if filePath == "" {
		http.NotFound(w, r)
		return
	}

	// Read and serve the file
	content, err := os.ReadFile(filePath)
	if err != nil {
		http.Error(w, "Failed to read file", http.StatusInternalServerError)
		return
	}

	// Set content type based on extension (ext already computed above)
	switch ext {
	case ".js":
		w.Header().Set("Content-Type", "application/javascript")
	case ".css":
		w.Header().Set("Content-Type", "text/css")
	case ".json":
		w.Header().Set("Content-Type", "application/json")
	default:
		w.Header().Set("Content-Type", "text/plain")
	}

	// Add CORS headers for module loading
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-cache")

	if _, err := w.Write(content); err != nil {
		logger.Warnf("Failed to write module file response: %v", err)
	}
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
