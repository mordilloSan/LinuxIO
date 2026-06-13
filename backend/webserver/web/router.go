package web

import (
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"

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
	handler = http.NewCrossOriginProtection().Handler(handler)
	handler = LoggerMiddleware(handler)
	handler = RecoveryMiddleware(handler)

	// Register API routes (auth, etc.)
	if cfg.RegisterRoutes != nil {
		cfg.RegisterRoutes(mux)
	}

	// WebSocket relay — session validated inside wsAuthMiddleware so that auth
	// failures are sent as WS close code 1008 ("no-session") rather than HTTP
	// 401, which browsers cannot distinguish from a network error.
	mux.Handle("GET /ws", wsAuthMiddleware(sm, WebSocketRelayHandler(sm)))

	// Container reverse proxy — session-protected
	// Requests: /proxy/{container-name}/[...] → container's internal IP:port
	mux.Handle("/proxy/", sm.RequireSession(http.HandlerFunc(ContainerProxyHandler)))

	// Serve embedded SPA
	mountProductionSPA(mux, cfg.UI)

	return handler
}

// mountProductionSPA serves the embedded frontend with SPA fallback.
func mountProductionSPA(mux *http.ServeMux, ui fs.FS) {
	// Serve /assets/ with cache headers and precompressed asset negotiation.
	mux.HandleFunc("/assets/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if !strings.HasPrefix(name, "assets/") {
			http.NotFound(w, r)
			return
		}
		servePrecompressedAssetFS(w, r, ui, name)
	})

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

// servePrecompressedAssetFS serves an immutable frontend asset, preferring a
// Vite-generated compressed sidecar when the client supports it.
func servePrecompressedAssetFS(w http.ResponseWriter, r *http.Request, fsys fs.FS, name string) {
	type compressedVariant struct {
		encoding string
		name     string
		quality  float64
	}

	var variants []compressedVariant
	for _, variant := range []compressedVariant{
		{encoding: "br", name: name + ".br"},
		{encoding: "gzip", name: name + ".gz"},
	} {
		if _, err := fs.Stat(fsys, variant.name); err == nil {
			variant.quality = acceptedEncodingQuality(
				r.Header.Get("Accept-Encoding"),
				variant.encoding,
			)
			variants = append(variants, variant)
		}
	}

	if len(variants) > 0 {
		addVaryHeader(w, "Accept-Encoding")
		best := compressedVariant{}
		for _, variant := range variants {
			if variant.quality > best.quality {
				best = variant
			}
		}
		if best.quality > 0 {
			serveFileFSAs(w, r, fsys, best.name, name, best.encoding)
			return
		}
	}

	serveFileFS(w, r, fsys, name)
}

// serveFileFS serves a single uncompressed file from the fs.FS.
func serveFileFS(w http.ResponseWriter, r *http.Request, fsys fs.FS, name string) {
	serveFileFSAs(w, r, fsys, name, name, "")
}

func serveFileFSAs(w http.ResponseWriter, r *http.Request, fsys fs.FS, fileName, responseName, contentEncoding string) {
	f, err := fsys.Open(fileName)
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

	setContentType(w, responseName)
	setCacheHeaders(w, responseName)
	if contentEncoding != "" {
		w.Header().Set("Content-Encoding", contentEncoding)
	}

	// If it's a ReadSeeker, use http.ServeContent for proper caching
	if rs, ok := f.(io.ReadSeeker); ok {
		http.ServeContent(w, r, responseName, stat.ModTime(), rs)
		return
	}

	// Fallback: just copy the content
	if _, err := io.Copy(w, f); err != nil {
		slog.Warn("failed to copy file response", "path", responseName, "error", err)
	}
}

func acceptedEncodingQuality(header, encoding string) float64 {
	exactQ := -1.0
	wildcardQ := -1.0
	for part := range strings.SplitSeq(header, ",") {
		token, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		isEncoding := strings.EqualFold(token, encoding)
		isWildcard := token == "*"
		if !isEncoding && !isWildcard {
			continue
		}
		q := 1.0
		for param := range strings.SplitSeq(params, ";") {
			key, value, ok := strings.Cut(strings.TrimSpace(param), "=")
			if !ok || !strings.EqualFold(key, "q") {
				continue
			}
			parsed, err := strconv.ParseFloat(value, 64)
			if err == nil {
				q = parsed
			}
		}
		if isEncoding {
			exactQ = q
		}
		if isWildcard {
			wildcardQ = q
		}
	}
	if exactQ >= 0 {
		return exactQ
	}
	if wildcardQ >= 0 {
		return wildcardQ
	}
	return 0
}

func addVaryHeader(w http.ResponseWriter, value string) {
	current := w.Header().Values("Vary")
	for _, existing := range current {
		for part := range strings.SplitSeq(existing, ",") {
			if strings.EqualFold(strings.TrimSpace(part), value) {
				return
			}
		}
	}
	w.Header().Add("Vary", value)
}

func setCacheHeaders(w http.ResponseWriter, name string) {
	switch {
	case strings.HasPrefix(name, "assets/"):
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	case name == "index.html" || name == "manifest.json":
		w.Header().Set("Cache-Control", "no-cache")
	case strings.HasPrefix(name, "favicon-") || strings.HasSuffix(name, ".png"):
		w.Header().Set("Cache-Control", "public, max-age=86400")
	}
}

func setContentType(w http.ResponseWriter, name string) {
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
	default:
		if contentType := mime.TypeByExtension(ext); contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
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
	slog.Warn("http server", "message", msg)
	return len(p), nil
}
