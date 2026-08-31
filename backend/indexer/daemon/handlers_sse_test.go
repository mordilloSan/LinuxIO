package daemon

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type testSSEWriter struct {
	mu      sync.Mutex
	header  http.Header
	body    bytes.Buffer
	status  int
	writeCh chan struct{}
}

func subscriberCount(b *workStreamBroadcaster) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subscribers)
}

func newTestSSEWriter() *testSSEWriter {
	return &testSSEWriter{
		header:  make(http.Header),
		writeCh: make(chan struct{}, 8),
	}
}

func (w *testSSEWriter) Header() http.Header {
	return w.header
}

func (w *testSSEWriter) WriteHeader(statusCode int) {
	w.mu.Lock()
	w.status = statusCode
	w.mu.Unlock()
}

func (w *testSSEWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.body.Write(p)
	w.mu.Unlock()

	select {
	case w.writeCh <- struct{}{}:
	default:
	}

	return n, err
}

func (w *testSSEWriter) Flush() {}

func (w *testSSEWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.body.String()
}

func mustReceiveWorkEvent(t *testing.T, ch <-chan workStreamEvent) workStreamEvent {
	t.Helper()

	select {
	case evt, ok := <-ch:
		if !ok {
			t.Fatal("expected event, got closed channel")
		}
		return evt
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for event")
	}
	return workStreamEvent{}
}

func waitForCondition(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal("condition not met before timeout")
}

//nolint:gocognit // Fanout behavior is clearest as one end-to-end concurrency scenario.
func TestWorkStreamBroadcasterFanout(t *testing.T) {
	t.Run("metadata", func(t *testing.T) {
		b := newWorkStreamBroadcaster("reindex", "/docs")
		defer b.close()

		if got := b.Operation(); got != "reindex" {
			t.Fatalf("operation = %q, want %q", got, "reindex")
		}
		if got := b.Path(); got != "/docs" {
			t.Fatalf("path = %q, want %q", got, "/docs")
		}
	})

	t.Run("fanout_to_subscribers", func(t *testing.T) {
		b := newWorkStreamBroadcaster("reindex", "/docs")
		defer b.close()

		_, ch1, err := b.subscribe()
		if err != nil {
			t.Fatalf("subscribe #1: %v", err)
		}
		_, ch2, err := b.subscribe()
		if err != nil {
			t.Fatalf("subscribe #2: %v", err)
		}
		if got := subscriberCount(b); got != 2 {
			t.Fatalf("subscriber count = %d, want 2", got)
		}

		if err := b.SendEvent("progress", WorkProgressEvent{Operation: "reindex", FilesIndexed: 10}); err != nil {
			t.Fatalf("broadcast progress: %v", err)
		}

		evt1 := mustReceiveWorkEvent(t, ch1)
		evt2 := mustReceiveWorkEvent(t, ch2)
		if evt1.event != "progress" || evt2.event != "progress" {
			t.Fatalf("unexpected event names: %q and %q", evt1.event, evt2.event)
		}

		p1, ok := evt1.data.(WorkProgressEvent)
		if !ok {
			t.Fatalf("event #1 data type = %T, want WorkProgressEvent", evt1.data)
		}
		if p1.FilesIndexed != 10 {
			t.Fatalf("files_indexed = %d, want 10", p1.FilesIndexed)
		}
	})

	t.Run("unsubscribe", func(t *testing.T) {
		b := newWorkStreamBroadcaster("reindex", "/docs")
		defer b.close()

		id1, ch1, err := b.subscribe()
		if err != nil {
			t.Fatalf("subscribe: %v", err)
		}
		if _, _, err := b.subscribe(); err != nil {
			t.Fatalf("subscribe #2: %v", err)
		}

		b.unsubscribe(id1)
		select {
		case _, ok := <-ch1:
			if ok {
				t.Fatal("channel should be closed after unsubscribe")
			}
		case <-time.After(1 * time.Second):
			t.Fatal("timed out waiting for channel close")
		}

		if got := subscriberCount(b); got != 1 {
			t.Fatalf("subscriber count after unsubscribe = %d, want 1", got)
		}
	})

	t.Run("send_error", func(t *testing.T) {
		b := newWorkStreamBroadcaster("reindex", "/docs")
		defer b.close()

		_, ch, err := b.subscribe()
		if err != nil {
			t.Fatalf("subscribe: %v", err)
		}

		if err := b.SendError("boom"); err != nil {
			t.Fatalf("broadcast error: %v", err)
		}

		evt := mustReceiveWorkEvent(t, ch)
		if evt.event != "error" {
			t.Fatalf("event name = %q, want %q", evt.event, "error")
		}
		errorData, ok := evt.data.(map[string]string)
		if !ok {
			t.Fatalf("error payload type = %T, want map[string]string", evt.data)
		}
		if got := errorData["message"]; got != "boom" {
			t.Fatalf("error message = %q, want %q", got, "boom")
		}
	})

	t.Run("close_broadcaster", func(t *testing.T) {
		b := newWorkStreamBroadcaster("reindex", "/docs")

		_, ch, err := b.subscribe()
		if err != nil {
			t.Fatalf("subscribe: %v", err)
		}

		b.close()
		select {
		case _, ok := <-ch:
			if ok {
				t.Fatal("channel should be closed after broadcaster close")
			}
		case <-time.After(1 * time.Second):
			t.Fatal("timed out waiting for channel close")
		}

		if got := subscriberCount(b); got != 0 {
			t.Fatalf("subscriber count after close = %d, want 0", got)
		}
		if err := b.SendEvent("progress", WorkProgressEvent{}); err == nil {
			t.Fatal("expected send error after broadcaster close")
		}
	})
}

func TestHandleStatusStreamFallsBackToJSONWhenIdle(t *testing.T) {
	d, _ := newDaemonWithDB(t)

	req := httptest.NewRequest(http.MethodGet, "/status?stream=true", nil)
	req.Header.Set("Accept", "text/event-stream")
	rr := httptest.NewRecorder()
	d.handleStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if got := rr.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("content-type = %q, want application/json", got)
	}
	if !strings.Contains(rr.Body.String(), `"status":"idle"`) {
		t.Fatalf("body = %q, expected idle json status", rr.Body.String())
	}
}

func TestHandleStatusStreamAttachReceivesEventAndCleansSubscriber(t *testing.T) {
	d := &daemon{}
	broadcaster := newWorkStreamBroadcaster("reindex", "/docs")
	d.setWorkStreamBroadcaster(broadcaster)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/status?stream=true", nil).WithContext(ctx)
	req.Header.Set("Accept", "text/event-stream")
	writer := newTestSSEWriter()

	done := make(chan struct{})
	go func() {
		d.handleStatus(writer, req)
		close(done)
	}()

	waitForCondition(t, 1*time.Second, func() bool {
		return subscriberCount(broadcaster) == 1
	})

	if err := broadcaster.SendEvent("progress", WorkProgressEvent{
		Operation:   "reindex",
		CurrentPath: "/docs",
	}); err != nil {
		t.Fatalf("send progress: %v", err)
	}

	select {
	case <-writer.writeCh:
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for SSE write")
	}

	cancel()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("status stream handler did not return after cancel")
	}

	waitForCondition(t, 1*time.Second, func() bool {
		return subscriberCount(broadcaster) == 0
	})

	body := writer.String()
	if !strings.Contains(body, "event: started") {
		t.Fatalf("body = %q, expected started event", body)
	}
	if !strings.Contains(body, "\"operation\":\"reindex\"") {
		t.Fatalf("body = %q, expected operation payload", body)
	}
	if !strings.Contains(body, "event: progress") {
		t.Fatalf("body = %q, expected progress event", body)
	}
}
