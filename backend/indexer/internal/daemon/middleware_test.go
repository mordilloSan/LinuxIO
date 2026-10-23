package daemon

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/peercred"
)

type optionalResponseWriter struct {
	header       http.Header
	body         bytes.Buffer
	status       int
	flushed      bool
	pushedTarget string
	readFrom     bool
	peer         net.Conn
}

func (w *optionalResponseWriter) Header() http.Header {
	return w.header
}

func (w *optionalResponseWriter) WriteHeader(status int) {
	w.status = status
}

func (w *optionalResponseWriter) Write(p []byte) (int, error) {
	return w.body.Write(p)
}

func (w *optionalResponseWriter) Flush() {
	w.flushed = true
}

func (w *optionalResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	server, peer := net.Pipe()
	w.peer = peer
	rw := bufio.NewReadWriter(bufio.NewReader(server), bufio.NewWriter(server))
	return server, rw, nil
}

func (w *optionalResponseWriter) Push(target string, _ *http.PushOptions) error {
	w.pushedTarget = target
	return nil
}

func (w *optionalResponseWriter) ReadFrom(src io.Reader) (int64, error) {
	w.readFrom = true
	return w.body.ReadFrom(src)
}

func TestResponseRecorderForwardsOptionalCapabilities(t *testing.T) {
	underlying := &optionalResponseWriter{header: make(http.Header)}
	recorder := &responseRecorder{ResponseWriter: underlying, status: http.StatusOK}

	if err := http.NewResponseController(recorder).Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if !underlying.flushed || !recorder.wrote || underlying.status != http.StatusOK {
		t.Fatalf("flush state = flushed:%t wrote:%t status:%d", underlying.flushed, recorder.wrote, underlying.status)
	}

	conn, _, err := recorder.Hijack()
	if err != nil {
		t.Fatalf("Hijack: %v", err)
	}
	if closeErr := conn.Close(); closeErr != nil {
		t.Errorf("close hijacked connection: %v", closeErr)
	}
	if underlying.peer != nil {
		if closeErr := underlying.peer.Close(); closeErr != nil {
			t.Errorf("close hijacked peer: %v", closeErr)
		}
	}

	if pushErr := recorder.Push("/asset", nil); pushErr != nil {
		t.Fatalf("Push: %v", pushErr)
	}
	if underlying.pushedTarget != "/asset" {
		t.Fatalf("pushed target = %q, want /asset", underlying.pushedTarget)
	}

	n, err := io.Copy(recorder, io.LimitReader(strings.NewReader("payload"), 7))
	if err != nil {
		t.Fatalf("ReadFrom: %v", err)
	}
	if n != 7 || recorder.bytes != 7 || underlying.body.String() != "payload" || !underlying.readFrom {
		t.Fatalf("copy state = n:%d bytes:%d body:%q read_from:%t", n, recorder.bytes, underlying.body.String(), underlying.readFrom)
	}
}

func TestResponseRecorderReportsUnsupportedCapabilities(t *testing.T) {
	recorder := &responseRecorder{ResponseWriter: httptest.NewRecorder(), status: http.StatusOK}

	if _, _, err := recorder.Hijack(); !errors.Is(err, http.ErrNotSupported) {
		t.Fatalf("Hijack error = %v, want ErrNotSupported", err)
	}
	if err := recorder.Push("/asset", nil); !errors.Is(err, http.ErrNotSupported) {
		t.Fatalf("Push error = %v, want ErrNotSupported", err)
	}
}

func TestAuthorizeTransportMiddlewareRequiresRootUnix(t *testing.T) {
	withUnixConnection := func(ctx context.Context) context.Context {
		return context.WithValue(ctx, connectionKindContextKey{}, true)
	}
	for _, test := range []struct {
		name    string
		context context.Context
		want    int
	}{
		{name: "root Unix", context: peercred.WithCredForTest(withUnixConnection(context.Background()), 0), want: http.StatusNoContent},
		{name: "non-root Unix", context: peercred.WithCredForTest(withUnixConnection(context.Background()), 1000), want: http.StatusForbidden},
		{name: "missing transport", context: context.Background(), want: http.StatusForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := authorizeTransportMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}))
			req := httptest.NewRequest(http.MethodGet, "/status", nil).WithContext(test.context)
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != test.want {
				t.Fatalf("status = %d, want %d", rr.Code, test.want)
			}
		})
	}
}

func TestRequestCompletionPublishesActivityBeforeRelease(t *testing.T) {
	d := &daemon{}
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	handler := d.activityMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(entered)
		<-release
	}))
	go func() {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
		close(done)
	}()
	<-entered

	d.activityMu.Lock()
	close(release)
	time.Sleep(10 * time.Millisecond)
	heldUntilActivityPublished := d.activeRequests.Load()
	d.activityMu.Unlock()
	<-done

	if heldUntilActivityPublished != 1 {
		t.Fatalf("active requests while publishing completion = %d, want 1", heldUntilActivityPublished)
	}
	if d.activeRequests.Load() != 0 {
		t.Fatal("request pin remained active")
	}
	if idleFor := time.Since(d.activityTime()); idleFor > 100*time.Millisecond {
		t.Fatalf("request completion did not start a fresh grace: idle for %v", idleFor)
	}
}
