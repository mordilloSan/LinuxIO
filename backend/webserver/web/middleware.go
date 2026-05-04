package web

import (
	"bufio"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"
)

// RecoveryMiddleware returns middleware that recovers from panics.
func RecoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				slog.Error("panic recovered", "error", err, "stack", string(debug.Stack()))
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// LoggerMiddleware returns middleware that logs requests.
func LoggerMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapped := &responseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(wrapped, r)

		kind, label, displayPath := requestLogValues(r.URL.Path)
		outcome, verb := "success", "succeeded"
		if wrapped.status >= http.StatusBadRequest {
			outcome, verb = "failure", "failed"
		}

		slog.Debug(label+" "+verb+": "+r.Method+" "+displayPath,
			"request_kind", kind,
			"method", r.Method,
			"path", displayPath,
			"status", wrapped.status,
			"outcome", outcome,
			"duration", time.Since(start))
	})
}

func requestLogValues(requestPath string) (kind string, label string, displayPath string) {
	switch {
	case requestPath == "/ws":
		return "websocket", "websocket upgrade", requestPath
	case strings.HasPrefix(requestPath, "/auth/"), strings.HasPrefix(requestPath, "/api/"):
		return "api_http", "api http", requestPath
	case strings.HasPrefix(requestPath, "/proxy/"):
		return "proxy", "proxy request", summarizeProxyPath(requestPath)
	case strings.HasPrefix(requestPath, "/assets/"):
		return "asset", "asset request", "/assets/*"
	case isStaticAsset(requestPath):
		return "asset", "asset request", requestPath
	default:
		return "spa", "spa request", requestPath
	}
}

func isStaticAsset(requestPath string) bool {
	switch requestPath {
	case "/favicon.ico", "/manifest.json", "/robots.txt":
		return true
	}
	return strings.HasPrefix(requestPath, "/favicon-") ||
		strings.HasPrefix(requestPath, "/apple-touch-icon")
}

func summarizeProxyPath(requestPath string) string {
	trimmed := strings.TrimPrefix(requestPath, "/proxy/")
	if trimmed == "" {
		return "/proxy"
	}

	if strings.Contains(trimmed, "/") {
		return "/proxy/<container>/..."
	}
	return "/proxy/<container>"
}

// responseWriter wraps http.ResponseWriter to capture status code.
// It also implements http.Hijacker to support WebSocket upgrades.
type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

// Hijack implements http.Hijacker for WebSocket upgrades.
func (rw *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := rw.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return hijacker.Hijack()
}
