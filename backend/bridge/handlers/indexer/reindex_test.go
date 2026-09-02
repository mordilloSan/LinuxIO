package indexer

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	ipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
	indexerapi "github.com/mordilloSan/LinuxIO/backend/indexer/api"
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
	orig := Client
	Client = &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("tcp", srv.Listener.Addr().String())
			},
		},
	}
	t.Cleanup(func() { Client = orig })
}

// newTwoStepServer creates a test server that handles both indexer trigger
// endpoints and GET /status?stream=true (SSE).
func newTwoStepServer(t *testing.T, triggerStatus int, sseHandler func(http.ResponseWriter, *http.Request)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && (r.URL.Path == "/index" || r.URL.Path == "/reindex") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(triggerStatus)
			if triggerStatus == http.StatusAccepted || triggerStatus == http.StatusOK {
				mustWrite(t, w, `{"status":"running","operation_id":"test-operation"}`)
			}
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/status" && r.URL.Query().Get("stream") == "true" {
			sseHandler(w, r)
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/status" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.String())
		w.WriteHeader(http.StatusNotFound)
	}))
}

func TestStreamIndexer_CompleteFlow(t *testing.T) {
	srv := newTwoStepServer(t, http.StatusAccepted, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := requireFlusher(t, w)

		mustWrite(t, w, "event:started\ndata:{\"status\":\"running\",\"operation\":\"index\",\"operation_id\":\"test-operation\",\"path\":\"/\"}\n\n")
		flusher.Flush()

		mustWrite(t, w, "event:progress\ndata:{\"operation\":\"index\",\"operation_id\":\"test-operation\",\"path\":\"/\",\"message\":\"Scanning filesystem\",\"current_path\":\"Scanning filesystem...\"}\n\n")
		flusher.Flush()

		mustWrite(t, w, "event:progress\ndata:{\"operation\":\"index\",\"operation_id\":\"test-operation\",\"path\":\"/\",\"phase\":\"scan\",\"files_indexed\":10,\"dirs_indexed\":2,\"bytes_indexed\":512}\n\n")
		flusher.Flush()

		mustWrite(t, w, "event:complete\ndata:{\"status\":\"complete\",\"operation\":\"index\",\"operation_id\":\"test-operation\",\"path\":\"/\",\"files_indexed\":100,\"dirs_indexed\":20,\"total_size\":5000,\"duration_ms\":150,\"deleted_entries\":3}\n\n")
		flusher.Flush()
	})
	defer srv.Close()
	overrideClient(t, srv)

	var progresses []IndexerProgress
	var gotResult IndexerResult

	cb := IndexerCallbacks{
		OnProgress: func(p IndexerProgress) error {
			progresses = append(progresses, p)
			return nil
		},
		OnResult: func(r IndexerResult) error {
			gotResult = r
			return nil
		},
		OnError: func(msg string, code int) error {
			t.Errorf("unexpected error: %s (code %d)", msg, code)
			return nil
		},
	}

	err := StreamIndexer(context.Background(), "/", cb)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// connecting + started + a phase message + scan counters.
	if len(progresses) != 4 {
		t.Errorf("expected 4 progress calls, got %d", len(progresses))
	}
	if len(progresses) == 4 {
		if got := progresses[2]; got.Message != "Scanning filesystem" || got.CurrentPath != "Scanning filesystem..." || got.Phase != "indexing" {
			t.Errorf("step progress = %+v", got)
		}
		if got := progresses[3]; got.Phase != "scan" || got.BytesIndexed != 512 || got.FilesIndexed != 10 || got.DirsIndexed != 2 {
			t.Errorf("scan progress = %+v", got)
		}
	}
	if gotResult.FilesIndexed != 100 || gotResult.DurationMs != 150 || gotResult.Operation != "index" || gotResult.DeletedEntries != 3 {
		t.Errorf("unexpected result: %+v", gotResult)
	}
}

func TestStreamIndexerSelectsFullOrPathEndpoint(t *testing.T) {
	tests := []struct {
		name          string
		path          string
		wantEndpoint  string
		wantQueryPath string
		operation     string
	}{
		{name: "root uses full index", path: "/", wantEndpoint: "/index", operation: "index"},
		{name: "subpath uses reindex", path: "/data/photos", wantEndpoint: "/reindex", wantQueryPath: "/data/photos", operation: "reindex"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodPost:
					if r.URL.Path != tt.wantEndpoint {
						t.Fatalf("trigger endpoint = %q, want %q", r.URL.Path, tt.wantEndpoint)
					}
					if got := r.URL.Query().Get("path"); got != tt.wantQueryPath {
						t.Fatalf("trigger path = %q, want %q", got, tt.wantQueryPath)
					}
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusAccepted)
					mustWrite(t, w, `{"status":"running","operation_id":"select-operation"}`)
				case r.Method == http.MethodGet && r.URL.Path == "/status":
					w.Header().Set("Content-Type", "text/event-stream")
					mustWrite(t, w, "event:complete\ndata:{\"operation\":\""+tt.operation+"\",\"operation_id\":\"select-operation\",\"path\":\""+tt.path+"\"}\n\n")
				default:
					t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
				}
			}))
			defer srv.Close()
			overrideClient(t, srv)

			if err := StreamIndexer(context.Background(), tt.path, IndexerCallbacks{}); err != nil {
				t.Fatalf("StreamIndexer: %v", err)
			}
		})
	}
}

func TestStreamIndexerRejectsMissingTriggerOperationID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != indexerapi.RouteIndex {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		w.WriteHeader(http.StatusAccepted)
		mustWrite(t, w, `{"status":"running"}`)
	}))
	defer srv.Close()
	overrideClient(t, srv)

	if err := StreamIndexer(context.Background(), "/", IndexerCallbacks{}); err == nil || !strings.Contains(err.Error(), "missing operation_id") {
		t.Fatalf("error = %v, want missing operation_id", err)
	}
}

func TestHandleIndexerSSEStateEventPreservesState(t *testing.T) {
	var got IndexerProgress
	expected := streamExpectation("/", "state-operation")
	_, err := handleIndexerSSEEvent(IndexerCallbacks{
		OnProgress: func(progress IndexerProgress) error {
			got = progress
			return nil
		},
	}, SSEEvent{
		Type: "state",
		Data: `{"operation":"index","operation_id":"state-operation","path":"/","state":"checkpoint","message":"Checkpointing database"}`,
	}, &expected)
	if err != nil {
		t.Fatalf("handleIndexerSSEEvent: %v", err)
	}
	if got.State != "checkpoint" || got.Phase != "checkpoint" || got.Message != "Checkpointing database" {
		t.Fatalf("progress = %+v", got)
	}
}

func TestHandleIndexerSSEEventRejectsNonIndexOperation(t *testing.T) {
	var gotCode int
	expected := streamExpectation("/", "operation-1")
	_, err := handleIndexerSSEEvent(IndexerCallbacks{
		OnError: func(_ string, code int) error {
			gotCode = code
			return nil
		},
	}, SSEEvent{Type: "started", Data: `{"operation":"vacuum","operation_id":"operation-1","path":"/"}`}, &expected)
	if err == nil {
		t.Fatal("expected non-index operation error")
	}
	if gotCode != http.StatusConflict {
		t.Fatalf("error code = %d, want %d", gotCode, http.StatusConflict)
	}
}

func TestHandleIndexerSSEEventValidatesOperationIdentity(t *testing.T) {
	expected := streamExpectation("/docs", "operation-1")
	var gotCode int
	_, err := handleIndexerSSEEvent(IndexerCallbacks{
		OnError: func(_ string, code int) error {
			gotCode = code
			return nil
		},
	}, SSEEvent{
		Type: indexerapi.EventError,
		Data: `{"message":"failed","operation":"reindex","path":"/docs"}`,
	}, &expected)
	if err == nil || !strings.Contains(err.Error(), "operation identity is required") {
		t.Fatalf("error = %v, want required operation identity", err)
	}
	if gotCode != http.StatusConflict {
		t.Fatalf("error code = %d, want %d", gotCode, http.StatusConflict)
	}
}

func TestHandleIndexerSSEEventRejectsMalformedPayloads(t *testing.T) {
	for _, event := range []string{"started", "progress", "complete", "error"} {
		t.Run(event, func(t *testing.T) {
			expected := streamExpectation("/", "operation-1")
			_, err := handleIndexerSSEEvent(IndexerCallbacks{}, SSEEvent{Type: event, Data: "{"}, &expected)
			if err == nil {
				t.Fatal("expected malformed payload error")
			}
			if !strings.Contains(err.Error(), event) {
				t.Fatalf("error = %v, want event name", err)
			}
		})
	}

	expected := streamExpectation("/", "operation-1")
	_, err := handleIndexerSSEEvent(IndexerCallbacks{}, SSEEvent{Type: "error", Data: `{}`}, &expected)
	if err == nil || !strings.Contains(err.Error(), "missing message") {
		t.Fatalf("error = %v, want missing message", err)
	}
}

func TestStreamIndexer_ErrorEvent(t *testing.T) {
	srv := newTwoStepServer(t, http.StatusAccepted, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		mustWrite(t, w, "event:error\ndata:{\"message\":\"disk full\",\"operation\":\"reindex\",\"operation_id\":\"test-operation\",\"path\":\"/tmp\"}\n\n")
	})
	defer srv.Close()
	overrideClient(t, srv)

	var gotErrMsg string
	var gotErrCode int

	cb := IndexerCallbacks{
		OnProgress: func(p IndexerProgress) error { return nil },
		OnError: func(msg string, code int) error {
			gotErrMsg = msg
			gotErrCode = code
			return nil
		},
	}

	err := StreamIndexer(context.Background(), "/tmp", cb)
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

func TestStreamIndexer_ConflictStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The indexer trigger returns 409.
		w.WriteHeader(http.StatusConflict)
	}))
	defer srv.Close()
	overrideClient(t, srv)

	var gotErrCode int
	cb := IndexerCallbacks{
		OnProgress: func(p IndexerProgress) error { return nil },
		OnError: func(msg string, code int) error {
			gotErrCode = code
			return nil
		},
	}

	err := StreamIndexer(context.Background(), "/", cb)
	if err == nil {
		t.Fatal("expected error for conflict")
	}
	if gotErrCode != 409 {
		t.Errorf("expected code 409, got %d", gotErrCode)
	}
}

func TestStreamIndexer_ContextCancellation(t *testing.T) {
	srv := newTwoStepServer(t, http.StatusAccepted, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := requireFlusher(t, w)

		mustWrite(t, w, "event:started\ndata:{\"operation\":\"index\",\"operation_id\":\"test-operation\",\"path\":\"/\"}\n\n")
		flusher.Flush()

		// Block until client disconnects
		<-r.Context().Done()
	})
	defer srv.Close()
	overrideClient(t, srv)

	ctx, cancel := context.WithCancel(context.Background())

	var gotAbortError bool
	cb := IndexerCallbacks{
		OnProgress: func(p IndexerProgress) error {
			// Cancel after second progress ("indexing" from started event)
			if p.Phase == "indexing" {
				cancel()
			}
			return nil
		},
		OnError: func(msg string, code int) error {
			if code == 499 {
				gotAbortError = true
			}
			return nil
		},
	}

	err := StreamIndexer(ctx, "/", cb)
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

func TestStreamIndexer_PathWithSpecialChars(t *testing.T) {
	specialPath := "/tmp/space dir/a&b#frag?x=1"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/reindex" {
			gotPath := r.URL.Query().Get("path")
			if gotPath != specialPath {
				t.Errorf("expected path %q, got %q", specialPath, gotPath)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			mustWrite(t, w, `{"status":"running","operation_id":"special-operation"}`)
			return
		}
		// GET /status?stream=true
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		mustWrite(t, w, "event:complete\ndata:{\"operation\":\"reindex\",\"operation_id\":\"special-operation\",\"path\":\""+specialPath+"\"}\n\n")
	}))
	defer srv.Close()
	overrideClient(t, srv)

	if err := StreamIndexer(context.Background(), specialPath, IndexerCallbacks{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStreamIndexer_NilCallbacks(t *testing.T) {
	srv := newTwoStepServer(t, http.StatusAccepted, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		mustWrite(t, w, "event:started\ndata:{\"operation\":\"index\",\"operation_id\":\"test-operation\",\"path\":\"/\"}\n\nevent:complete\ndata:{\"operation\":\"index\",\"operation_id\":\"test-operation\",\"path\":\"/\"}\n\n")
	})
	defer srv.Close()
	overrideClient(t, srv)

	// All nil callbacks — should not panic
	err := StreamIndexer(context.Background(), "/", IndexerCallbacks{})
	if err != nil {
		t.Fatalf("unexpected error with nil callbacks: %v", err)
	}
}

func TestStreamIndexer_UnexpectedEOF(t *testing.T) {
	srv := newTwoStepServer(t, http.StatusAccepted, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := requireFlusher(t, w)
		// Send progress then close without "complete"
		mustWrite(t, w, "event:started\ndata:{\"operation\":\"index\",\"operation_id\":\"test-operation\",\"path\":\"/\"}\n\n")
		flusher.Flush()
		// Server closes connection
	})
	defer srv.Close()
	overrideClient(t, srv)

	var gotErrCode int
	cb := IndexerCallbacks{
		OnProgress: func(p IndexerProgress) error { return nil },
		OnError: func(msg string, code int) error {
			gotErrCode = code
			return nil
		},
	}

	done := make(chan error, 1)
	go func() {
		done <- StreamIndexer(context.Background(), "/", cb)
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

func newScopedReattachServer(t *testing.T, streamRequests *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/reindex":
			if got := r.URL.Query().Get("path"); got != "/docs" {
				t.Errorf("reindex path = %q, want /docs", got)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			mustWrite(t, w, `{"status":"running","operation_id":"reindex-1","path":"/docs"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/status" && r.URL.Query().Get("stream") == "true":
			if got := r.URL.Query().Get("operation_id"); got != "reindex-1" {
				t.Errorf("stream operation_id = %q, want reindex-1", got)
			}
			if got := r.URL.Query().Get("operation"); got != "reindex" {
				t.Errorf("stream operation = %q, want reindex", got)
			}
			if got := r.URL.Query().Get("path"); got != "/docs" {
				t.Errorf("stream path = %q, want /docs", got)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			if streamRequests.Add(1) == 1 {
				return
			}
			mustWrite(t, w, "event:complete\ndata:{\"status\":\"complete\",\"operation\":\"reindex\",\"operation_id\":\"reindex-1\",\"path\":\"/docs\",\"files_indexed\":3}\n\n")
		case r.Method == http.MethodGet && r.URL.Path == "/status":
			w.Header().Set("Content-Type", "application/json")
			mustWrite(t, w, `{"status":"indexing","active_operation":"reindex","active_operation_id":"reindex-1","active_path":"/docs","num_files":2}`)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.String())
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestStreamIndexerReattachesScopedOperationAfterStreamEnds(t *testing.T) {
	var streamRequests atomic.Int32
	srv := newScopedReattachServer(t, &streamRequests)
	defer srv.Close()
	overrideClient(t, srv)

	var recovered IndexerProgress
	var result IndexerResult
	err := StreamIndexer(context.Background(), "/docs", IndexerCallbacks{
		OnProgress: func(progress IndexerProgress) error {
			if progress.Operation == "reindex" && progress.CurrentPath == "/docs" {
				recovered = progress
			}
			return nil
		},
		OnResult: func(got IndexerResult) error {
			result = got
			return nil
		},
	})
	if err != nil {
		t.Fatalf("StreamIndexer: %v", err)
	}
	if streamRequests.Load() != 2 {
		t.Fatalf("stream requests = %d, want 2", streamRequests.Load())
	}
	if recovered.FilesIndexed != 2 || recovered.Operation != "reindex" || recovered.OperationID != "reindex-1" || recovered.CurrentPath != "/docs" {
		t.Fatalf("recovered progress = %#v", recovered)
	}
	if result.Path != "/docs" || result.Operation != "reindex" || result.OperationID != "reindex-1" || result.FilesIndexed != 3 {
		t.Fatalf("result = %#v", result)
	}
}

func TestStreamIndexerIdleRecoveryUsesExpectedIdentity(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/index":
			w.WriteHeader(http.StatusAccepted)
			mustWrite(t, w, `{"status":"running","operation_id":"idle-operation"}`)
		case r.Method == http.MethodGet && r.URL.Query().Get("stream") == "true":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && r.URL.Path == "/status":
			w.Header().Set("Content-Type", "application/json")
			mustWrite(t, w, `{"status":"idle","num_files":4,"num_dirs":2,"total_size":128}`)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer srv.Close()
	overrideClient(t, srv)

	var got IndexerResult
	err := StreamIndexer(context.Background(), "/", IndexerCallbacks{
		OnResult: func(result IndexerResult) error {
			got = result
			return nil
		},
	})
	if err != nil {
		t.Fatalf("StreamIndexer: %v", err)
	}
	if got.Operation != "index" || got.OperationID != "idle-operation" || got.Path != "/" || got.FilesIndexed != 4 {
		t.Fatalf("recovered result = %#v", got)
	}
}

func TestStreamIndexerAttach_CompleteFlow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("stream") != "true" {
			w.Header().Set("Content-Type", "application/json")
			mustWrite(t, w, `{"status":"indexing","active_operation":"index","active_operation_id":"attach-1","active_path":"/"}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := requireFlusher(t, w)

		mustWrite(t, w, "event:progress\ndata:{\"operation\":\"index\",\"operation_id\":\"attach-1\",\"path\":\"/\",\"files_indexed\":50,\"dirs_indexed\":10}\n\n")
		flusher.Flush()

		mustWrite(t, w, "event:complete\ndata:{\"status\":\"complete\",\"operation\":\"index\",\"operation_id\":\"attach-1\",\"path\":\"/\",\"files_indexed\":200,\"dirs_indexed\":40,\"total_size\":10000,\"duration_ms\":300}\n\n")
		flusher.Flush()
	}))
	defer srv.Close()
	overrideClient(t, srv)

	var progressCount int
	var gotResult IndexerResult

	cb := IndexerCallbacks{
		OnProgress: func(p IndexerProgress) error {
			progressCount++
			return nil
		},
		OnResult: func(r IndexerResult) error {
			gotResult = r
			return nil
		},
		OnError: func(msg string, code int) error {
			t.Errorf("unexpected error: %s (code %d)", msg, code)
			return nil
		},
	}

	err := StreamIndexerAttach(context.Background(), "/", cb)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// "connecting" + "progress/indexing" = 2 progress calls
	if progressCount != 2 {
		t.Errorf("expected 2 progress calls, got %d", progressCount)
	}
	if gotResult.FilesIndexed != 200 || gotResult.DurationMs != 300 {
		t.Errorf("unexpected result: %+v", gotResult)
	}
}

func TestStreamIndexerAttach_NoActiveOperation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	overrideClient(t, srv)

	var gotErrCode int
	cb := IndexerCallbacks{
		OnProgress: func(p IndexerProgress) error { return nil },
		OnError: func(msg string, code int) error {
			gotErrCode = code
			return nil
		},
	}

	err := StreamIndexerAttach(context.Background(), "/", cb)
	if err == nil {
		t.Fatal("expected error when no active operation")
	}
	if gotErrCode != 404 {
		t.Errorf("expected code 404, got %d", gotErrCode)
	}
}
