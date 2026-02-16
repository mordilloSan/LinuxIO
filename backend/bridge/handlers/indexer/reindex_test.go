package indexer

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

func mustWrite(t *testing.T, w http.ResponseWriter, s string) {
	t.Helper()
	if _, err := io.WriteString(w, s); err != nil {
		t.Fatalf("failed to write SSE payload: %v", err)
	}
}

func requireFlusher(t *testing.T, w http.ResponseWriter) http.Flusher {
	t.Helper()
	flusher, ok := w.(http.Flusher)
	if !ok {
		t.Fatal("response writer does not implement http.Flusher")
	}
	return flusher
}

// overrideClient temporarily replaces the indexer HTTP client with one that
// dials the given test server, restoring the original on cleanup.
func overrideClient(t *testing.T, srv *httptest.Server) {
	t.Helper()
	orig := indexerClient
	indexerClient = &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("tcp", srv.Listener.Addr().String())
			},
		},
	}
	t.Cleanup(func() { indexerClient = orig })
}

func TestStreamReindex_CompleteFlow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusAccepted)
		flusher := requireFlusher(t, w)

		mustWrite(t, w, "event:started\ndata:{}\n\n")
		flusher.Flush()

		mustWrite(t, w, "event:progress\ndata:{\"files_indexed\":10,\"dirs_indexed\":2}\n\n")
		flusher.Flush()

		mustWrite(t, w, "event:complete\ndata:{\"path\":\"/\",\"files_indexed\":100,\"dirs_indexed\":20,\"total_size\":5000,\"duration_ms\":150}\n\n")
		flusher.Flush()
	}))
	defer srv.Close()
	overrideClient(t, srv)

	var progressCount int
	var gotResult ReindexResult

	cb := ReindexCallbacks{
		OnProgress: func(p ReindexProgress) error {
			progressCount++
			return nil
		},
		OnResult: func(r ReindexResult) error {
			gotResult = r
			return nil
		},
		OnError: func(msg string, code int) error {
			t.Errorf("unexpected error: %s (code %d)", msg, code)
			return nil
		},
	}

	err := StreamReindex(context.Background(), "/", cb)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// "connecting" + "started/indexing" + "progress/indexing" = 3 progress calls
	if progressCount != 3 {
		t.Errorf("expected 3 progress calls, got %d", progressCount)
	}
	if gotResult.FilesIndexed != 100 || gotResult.DurationMs != 150 {
		t.Errorf("unexpected result: %+v", gotResult)
	}
}

func TestStreamReindex_ErrorEvent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusAccepted)
		mustWrite(t, w, "event:error\ndata:{\"message\":\"disk full\"}\n\n")
	}))
	defer srv.Close()
	overrideClient(t, srv)

	var gotErrMsg string
	var gotErrCode int

	cb := ReindexCallbacks{
		OnProgress: func(p ReindexProgress) error { return nil },
		OnError: func(msg string, code int) error {
			gotErrMsg = msg
			gotErrCode = code
			return nil
		},
	}

	err := StreamReindex(context.Background(), "/tmp", cb)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if gotErrMsg != "disk full" {
		t.Errorf("expected 'disk full', got %q", gotErrMsg)
	}
	if gotErrCode != 500 {
		t.Errorf("expected code 500, got %d", gotErrCode)
	}
}

func TestStreamReindex_ConflictStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
	}))
	defer srv.Close()
	overrideClient(t, srv)

	var gotErrCode int
	cb := ReindexCallbacks{
		OnProgress: func(p ReindexProgress) error { return nil },
		OnError: func(msg string, code int) error {
			gotErrCode = code
			return nil
		},
	}

	err := StreamReindex(context.Background(), "/", cb)
	if err == nil {
		t.Fatal("expected error for conflict")
	}
	if gotErrCode != 409 {
		t.Errorf("expected code 409, got %d", gotErrCode)
	}
}

func TestStreamReindex_ContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusAccepted)
		flusher := requireFlusher(t, w)

		mustWrite(t, w, "event:started\ndata:{}\n\n")
		flusher.Flush()

		// Block until client disconnects
		<-r.Context().Done()
	}))
	defer srv.Close()
	overrideClient(t, srv)

	ctx, cancel := context.WithCancel(context.Background())

	var gotAbortError bool
	cb := ReindexCallbacks{
		OnProgress: func(p ReindexProgress) error {
			// Cancel after first progress
			cancel()
			return nil
		},
		OnError: func(msg string, code int) error {
			if code == 499 {
				gotAbortError = true
			}
			return nil
		},
	}

	err := StreamReindex(ctx, "/", cb)
	if err == nil {
		t.Fatal("expected error after cancellation")
	}
	if !errors.Is(err, ipc.ErrAborted) {
		t.Fatalf("expected ipc.ErrAborted, got %v", err)
	}
	if !gotAbortError {
		t.Error("expected abort error (code 499)")
	}
}

func TestStreamReindex_PathWithSpecialChars(t *testing.T) {
	specialPath := "/tmp/space dir/a&b#frag?x=1"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath := r.URL.Query().Get("path")
		if gotPath != specialPath {
			t.Errorf("expected path %q, got %q", specialPath, gotPath)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusAccepted)
		mustWrite(t, w, "event:complete\ndata:{\"path\":\"/\"}\n\n")
	}))
	defer srv.Close()
	overrideClient(t, srv)

	if err := StreamReindex(context.Background(), specialPath, ReindexCallbacks{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStreamReindex_NilCallbacks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusAccepted)
		mustWrite(t, w, "event:started\ndata:{}\n\nevent:complete\ndata:{\"path\":\"/\"}\n\n")
	}))
	defer srv.Close()
	overrideClient(t, srv)

	// All nil callbacks — should not panic
	err := StreamReindex(context.Background(), "/", ReindexCallbacks{})
	if err != nil {
		t.Fatalf("unexpected error with nil callbacks: %v", err)
	}
}

func TestStreamReindex_UnexpectedEOF(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusAccepted)
		flusher := requireFlusher(t, w)
		// Send progress then close without "complete"
		mustWrite(t, w, "event:started\ndata:{}\n\n")
		flusher.Flush()
		// Server closes connection
	}))
	defer srv.Close()
	overrideClient(t, srv)

	var gotErrCode int
	cb := ReindexCallbacks{
		OnProgress: func(p ReindexProgress) error { return nil },
		OnError: func(msg string, code int) error {
			gotErrCode = code
			return nil
		},
	}

	done := make(chan error, 1)
	go func() {
		done <- StreamReindex(context.Background(), "/", cb)
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected error for unexpected EOF")
		}
		if gotErrCode != 500 {
			t.Errorf("expected code 500, got %d", gotErrCode)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("test timed out")
	}
}
