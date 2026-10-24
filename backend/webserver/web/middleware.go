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

		kind, displayPath := requestLogValues(r.URL.Path)
		msg, level := "http request completed", slog.LevelDebug
		switch {
		case wrapped.status >= http.StatusInternalServerError:
			msg, level = "http request failed", slog.LevelError
		case wrapped.status >= http.StatusBadRequest:
			msg, level = "http request failed", slog.LevelWarn
		}

		slog.Log(r.Context(), level, msg,
			"request_kind", kind,
			"method", r.Method,
			"path", displayPath,
			"status", wrapped.status,
			"duration", time.Since(start))
	})
}

func requestLogValues(requestPath string) (kind string, displayPath string) {
	switch {
	case requestPath == "/ws":
		return "websocket", requestPath
	case strings.HasPrefix(requestPath, "/auth/"), strings.HasPrefix(requestPath, "/api/"):
		return "api_http", requestPath
	case strings.HasPrefix(requestPath, "/assets/"):
		return "asset", "/assets/*"
	case isStaticAsset(requestPath):
		return "asset", requestPath
	default:
		return "spa", requestPath
	}
}

func isStaticAsset(requestPath string) bool {
	return requestPath == "/manifest.json" || strings.HasPrefix(requestPath, "/favicon-")
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

// Unwrap lets http.ResponseController reach the server's writer for
// deadlines and flushing.
func (rw *responseWriter) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}

// Hijack implements http.Hijacker for WebSocket upgrades.
func (rw *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := rw.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return hijacker.Hijack()
}
