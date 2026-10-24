package daemon

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/indexer/api"
)

// sseWriter wraps an http.ResponseWriter for Server-Sent Events.
type sseWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
	rc      *http.ResponseController
}

// newSSEWriter creates a new SSE writer and sets appropriate headers.
func newSSEWriter(w http.ResponseWriter) (*sseWriter, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("streaming unsupported")
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	return &sseWriter{w: w, flusher: flusher, rc: http.NewResponseController(w)}, nil
}

// SendEvent sends an SSE event with the given event type and payload.
func (s *sseWriter) SendEvent(event string, data any) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}

	if err := s.rc.SetWriteDeadline(time.Now().Add(sseWriteDeadline)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return err
	}
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", event, jsonData); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

type sseEventSender interface {
	SendEvent(event string, data any) error
	SendError(msg string) error
}

type sseEventWriter interface {
	SendEvent(event string, data any) error
}

type workStreamEvent struct {
	event string
	data  any
}

type workStreamBroadcaster struct {
	operation        string
	operationID      string
	path             string
	mu               sync.Mutex
	subscribers      map[int]chan workStreamEvent
	nextSubscriberID int
	closed           bool
}

func newWorkStreamBroadcaster(operation, operationID, path string) *workStreamBroadcaster {
	return &workStreamBroadcaster{
		operation:   operation,
		operationID: operationID,
		path:        path,
		subscribers: make(map[int]chan workStreamEvent),
	}
}

func (b *workStreamBroadcaster) OperationID() string { return b.operationID }

func (b *workStreamBroadcaster) Operation() string {
	return b.operation
}

func (b *workStreamBroadcaster) Path() string {
	return b.path
}

func (b *workStreamBroadcaster) SendEvent(event string, data any) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return fmt.Errorf("work stream closed")
	}

	evt := workStreamEvent{event: event, data: data}
	for id, ch := range b.subscribers {
		select {
		case ch <- evt:
		default:
			close(ch)
			delete(b.subscribers, id)
			slog.Warn("dropping slow work stream subscriber", "id", id)
		}
	}

	return nil
}

func (b *workStreamBroadcaster) SendError(msg string) error {
	return b.SendEvent(api.EventError, api.WorkErrorEvent{
		Status:      "error",
		Operation:   b.operation,
		OperationID: b.operationID,
		Path:        b.path,
		Message:     msg,
	})
}

func (b *workStreamBroadcaster) subscribe() (int, <-chan workStreamEvent, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return 0, nil, fmt.Errorf("work stream closed")
	}

	id := b.nextSubscriberID
	b.nextSubscriberID++
	ch := make(chan workStreamEvent, workStreamSubscriberBuffer)
	b.subscribers[id] = ch

	return id, ch, nil
}

func (b *workStreamBroadcaster) unsubscribe(id int) {
	b.mu.Lock()
	ch, ok := b.subscribers[id]
	if ok {
		delete(b.subscribers, id)
		close(ch)
	}
	b.mu.Unlock()
}

func (b *workStreamBroadcaster) close() {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	for id, ch := range b.subscribers {
		delete(b.subscribers, id)
		close(ch)
	}
	b.mu.Unlock()
}

func sendSSEEvent(s sseEventWriter, event string, data any) bool {
	if err := s.SendEvent(event, data); err != nil {
		slog.Warn("SSE send failed", "event", event, "error", err)
		return false
	}
	return true
}

func sendSSEError(s sseEventSender, msg string) {
	if err := s.SendError(msg); err != nil {
		slog.Warn("SSE send error event failed", "error", err)
	}
}

func wantsStatusSSE(r *http.Request) bool {
	streamParam := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("stream")))
	if streamParam == "1" || streamParam == "true" || streamParam == "yes" {
		return true
	}
	return strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/event-stream")
}

func (d *daemon) attachStatusSSEIfActive(w http.ResponseWriter, r *http.Request) bool {
	broadcaster := d.getWorkStreamBroadcaster()
	if broadcaster == nil {
		return false
	}
	expectedID := strings.TrimSpace(r.URL.Query().Get("operation_id"))
	expectedOperation := strings.TrimSpace(r.URL.Query().Get("operation"))
	_, pathProvided := r.URL.Query()["path"]
	expectedPath := api.NormalizePath(r.URL.Query().Get("path"))
	if expectedID == "" || expectedID != broadcaster.OperationID() ||
		expectedOperation != broadcaster.Operation() || !pathProvided || expectedPath != api.NormalizePath(broadcaster.Path()) {
		return false
	}

	sse, err := newSSEWriter(w)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return true
	}

	subscriberID, events, err := broadcaster.subscribe()
	if err != nil {
		// Race with operation completion; fallback to JSON status response.
		return false
	}
	defer broadcaster.unsubscribe(subscriberID)

	if !sendSSEEvent(sse, api.EventStarted, api.WorkStartedEvent{
		Status:      "running",
		Operation:   broadcaster.Operation(),
		OperationID: broadcaster.OperationID(),
		Path:        broadcaster.Path(),
	}) {
		return true
	}

	d.streamWorkEvents(r.Context(), sse, events)
	return true
}

func (d *daemon) streamWorkEvents(ctx context.Context, sse *sseWriter, events <-chan workStreamEvent) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-events:
			if !ok {
				return
			}
			if !sendSSEEvent(sse, evt.event, evt.data) {
				return
			}
		}
	}
}

func (d *daemon) beginWorkStream(operation, operationID, path string) *workStreamBroadcaster {
	b := newWorkStreamBroadcaster(operation, operationID, path)
	d.setWorkStreamBroadcaster(b)
	return b
}

func (d *daemon) endWorkStream(b *workStreamBroadcaster) {
	d.clearWorkStreamBroadcaster(b)
	b.close()
}

func (d *daemon) setWorkStreamBroadcaster(b *workStreamBroadcaster) {
	d.workStreamMu.Lock()
	d.workStream = b
	d.workStreamMu.Unlock()
}

func (d *daemon) getWorkStreamBroadcaster() *workStreamBroadcaster {
	d.workStreamMu.RLock()
	b := d.workStream
	d.workStreamMu.RUnlock()
	return b
}

func (d *daemon) clearWorkStreamBroadcaster(b *workStreamBroadcaster) {
	d.workStreamMu.Lock()
	if d.workStream == b {
		d.workStream = nil
	}
	d.workStreamMu.Unlock()
}
