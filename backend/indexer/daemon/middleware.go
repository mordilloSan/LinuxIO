package daemon

import (
	"bufio"
	"io"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/indexer/api"
)

type responseRecorder struct {
	http.ResponseWriter
	status int
	bytes  int64
	wrote  bool
}

func (r *responseRecorder) WriteHeader(code int) {
	if r.wrote {
		return
	}
	r.status = code
	r.wrote = true
	r.ResponseWriter.WriteHeader(code)
}

func (r *responseRecorder) Write(p []byte) (int, error) {
	if !r.wrote {
		r.status = http.StatusOK
		r.wrote = true
	}
	n, err := r.ResponseWriter.Write(p)
	r.bytes += int64(n)
	return n, err
}

// Unwrap lets http.ResponseController reach optional capabilities exposed by
// the underlying server ResponseWriter.
func (r *responseRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

func (r *responseRecorder) Flush() {
	_ = r.FlushError()
}

func (r *responseRecorder) FlushError() error {
	if !r.wrote {
		r.WriteHeader(http.StatusOK)
	}
	if f, ok := r.ResponseWriter.(interface{ FlushError() error }); ok {
		return f.FlushError()
	}
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
		return nil
	}
	return http.ErrNotSupported
}

func (r *responseRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return h.Hijack()
}

func (r *responseRecorder) Push(target string, opts *http.PushOptions) error {
	p, ok := r.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}
	return p.Push(target, opts)
}

func (r *responseRecorder) ReadFrom(src io.Reader) (int64, error) {
	if readerFrom, ok := r.ResponseWriter.(io.ReaderFrom); ok {
		if !r.wrote {
			r.status = http.StatusOK
			r.wrote = true
		}
		n, err := readerFrom.ReadFrom(src)
		r.bytes += n
		return n, err
	}

	// Hide ReadFrom from io.Copy so it uses Write and keeps byte accounting in
	// one place instead of recursively calling this method.
	return io.Copy(struct{ io.Writer }{r}, src)
}

func loggerMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &responseRecorder{ResponseWriter: w, status: http.StatusOK}
		defer func() {
			slog.Debug("http request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"bytes", rec.bytes,
				"duration", time.Since(start).Truncate(time.Microsecond),
			)
		}()
		next.ServeHTTP(rec, r)
	})
}

func recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("http handler panic",
					"method", r.Method,
					"path", r.URL.Path,
					"panic", rec,
					"stack", string(debug.Stack()),
				)
				if rw, ok := w.(*responseRecorder); !ok || !rw.wrote {
					http.Error(w, "internal server error", http.StatusInternalServerError)
				}
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func authorizeTransportMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		kind, _ := r.Context().Value(connectionKindContextKey{}).(connectionKind)
		if kind == connectionKindTCP {
			if !isRemoteReadOnlyRequest(r) {
				http.Error(w, "the TCP listener is read-only; use the local root Unix socket for mutations", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		if kind != connectionKindUnix {
			http.Error(w, "indexer connection transport unavailable", http.StatusForbidden)
			return
		}
		uid, ok := peerUIDFromRequest(r)
		if !ok || uid != 0 {
			http.Error(w, "this operation requires root privileges; run with sudo", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isRemoteReadOnlyRequest(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	switch r.URL.Path {
	case api.RouteOpenAPI,
		api.RouteStatus,
		api.RouteSearch,
		api.RouteDirSize,
		api.RouteEntryCount,
		api.RouteSubfolders,
		api.RouteEntries,
		api.RouteConfig:
		return true
	default:
		return false
	}
}

// httpErrorLogAdapter routes http.Server.ErrorLog output through slog.
type httpErrorLogAdapter struct{}

func (httpErrorLogAdapter) Write(p []byte) (int, error) {
	msg := strings.TrimSpace(string(p))
	if msg == "" {
		return len(p), nil
	}
	slog.Warn("http server", "message", msg)
	return len(p), nil
}
