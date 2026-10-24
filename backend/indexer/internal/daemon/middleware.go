package daemon

import (
	"bufio"
	"io"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/peercred"
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
		if !requestFromUnixSocket(r) {
			http.Error(w, "indexer connection transport unavailable", http.StatusForbidden)
			return
		}
		uid, ok := peercred.RequestUID(r)
		if !ok || uid != 0 {
			http.Error(w, "this operation requires root privileges; run with sudo", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// activityMiddleware keeps the idle watcher informed: an in-flight request
// pins the daemon, and its completion starts a fresh idle grace.
func (d *daemon) activityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.activeRequests.Add(1)
		d.markActivity()
		defer func() {
			d.markActivity()
			d.activeRequests.Add(-1)
		}()
		next.ServeHTTP(w, r)
	})
}
