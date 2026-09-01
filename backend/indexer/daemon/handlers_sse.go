package daemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/indexer/api"
	"github.com/mordilloSan/LinuxIO/backend/indexer/indexing"
	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/configfile"
	"github.com/mordilloSan/LinuxIO/backend/indexer/storage"
)

// SSEWriter wraps an http.ResponseWriter for Server-Sent Events.
type SSEWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
	rc      *http.ResponseController
}

// sseWriteDeadline bounds each individual event write. Setting it per write
// replaces the server's absolute WriteTimeout for this connection, which
// would otherwise cut every stream 60s after the request started — long
// before a full index or vacuum finishes — while still timing out writes to
// dead clients.
const sseWriteDeadline = 30 * time.Second

// NewSSEWriter creates a new SSE writer and sets appropriate headers.
func NewSSEWriter(w http.ResponseWriter) (*SSEWriter, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("streaming unsupported")
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	return &SSEWriter{w: w, flusher: flusher, rc: http.NewResponseController(w)}, nil
}

// SendEvent sends an SSE event with the given event type and payload.
func (s *SSEWriter) SendEvent(event string, data any) error {
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

const workStreamSubscriberBuffer = 64

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
	return b.SendEvent("error", WorkErrorEvent{
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
		slog.Warn("SSE send failed", "event", event, "err", err)
		return false
	}
	return true
}

func sendSSEError(s sseEventSender, msg string) {
	if err := s.SendError(msg); err != nil {
		slog.Warn("SSE send error event failed", "err", err)
	}
}

type WorkStartedEvent = api.WorkStartedEvent
type WorkProgressEvent = api.WorkProgressEvent
type WorkCompleteEvent = api.WorkCompleteEvent
type WorkErrorEvent = api.WorkErrorEvent

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
	expectedPath := indexing.NormalizeIndexPath(r.URL.Query().Get("path"))
	if expectedID == "" || expectedID != broadcaster.OperationID() ||
		expectedOperation != broadcaster.Operation() || !pathProvided || expectedPath != indexing.NormalizeIndexPath(broadcaster.Path()) {
		return false
	}

	sse, err := NewSSEWriter(w)
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

	if !sendSSEEvent(sse, "started", WorkStartedEvent{
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

func (d *daemon) streamWorkEvents(ctx context.Context, sse *SSEWriter, events <-chan workStreamEvent) {
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

// reindexPathWithProgress reindexes a path and emits progress events.
func (d *daemon) reindexPathWithProgress(ctx context.Context, operationID, relativePath string, sender sseEventSender) {
	start := time.Now()
	slog.Info("starting reindex", "path", relativePath)

	indexID, err := d.store.LatestIndexID(ctx)
	if err != nil {
		sendSSEError(sender, fmt.Sprintf("no index present: %v", err))
		return
	}

	cfg := d.configSnapshot()
	fi, statErr := os.Lstat(relativePath)
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		sendSSEError(sender, fmt.Sprintf("cannot access %s: %v", relativePath, statErr))
		return
	}
	if statErr == nil && !fi.IsDir() {
		sendSSEError(sender, fmt.Sprintf("%s is not a directory; reindex its parent directory instead", relativePath))
		return
	}

	if statErr != nil {
		if deleteErr := storage.DeletePathRecursive(ctx, d.db, indexID, relativePath); deleteErr != nil {
			sendSSEError(sender, fmt.Sprintf("failed to delete stale entries: %v", deleteErr))
			return
		}
		sendSSEEvent(sender, "complete", WorkCompleteEvent{Status: "complete", Operation: "reindex", OperationID: operationID, Path: relativePath, DurationMs: time.Since(start).Milliseconds()})
		return
	}
	d.reindexExistingPath(ctx, operationID, relativePath, sender, start, cfg, indexID)
}

func (d *daemon) reindexExistingPath(ctx context.Context, operationID, relativePath string, sender sseEventSender, start time.Time, cfg DaemonConfig, indexID int64) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		sendSSEError(sender, fmt.Sprintf("begin reindex transaction: %v", err))
		return
	}
	defer func() {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			slog.Warn("reindex transaction rollback failed", "err", rollbackErr)
		}
	}()

	var oldSize int64
	if err := tx.QueryRowContext(ctx, `SELECT size FROM entries WHERE index_id = ? AND relative_path = ?`, indexID, relativePath).Scan(&oldSize); err != nil && !errors.Is(err, sql.ErrNoRows) {
		sendSSEError(sender, fmt.Sprintf("read prior directory size: %v", err))
		return
	}

	index := indexing.Initialize(
		"/",
		indexing.WithNetworkMounts(cfg.IncludeNetworkMounts),
		indexing.WithExcludePaths(configfile.EffectiveExcludePaths(configfile.Config{ExcludePaths: cfg.ExcludePaths})),
	)
	writer := storage.NewTransactionalStreamingWriter(ctx, tx, indexID, 1000, reindexProgressCallback(operationID, relativePath, sender))
	index.EnableStreaming(writer)

	if indexErr := index.StartIndexingFromPath(ctx, relativePath); indexErr != nil {
		if closeErr := writer.Close(); closeErr != nil {
			slog.Warn("failed to close streaming writer after reindex stream error", "err", closeErr)
		}
		sendSSEError(sender, fmt.Sprintf("indexing failed: %v", indexErr))
		return
	}

	if closeErr := writer.Close(); closeErr != nil {
		sendSSEError(sender, fmt.Sprintf("streaming writer close: %v", closeErr))
		return
	}

	newSize, finishErr := d.finishPathReindex(ctx, tx, indexID, relativePath, writer.ScanTime(), oldSize)
	if finishErr != nil {
		sendSSEError(sender, finishErr.Error())
		return
	}
	if commitErr := tx.Commit(); commitErr != nil {
		sendSSEError(sender, fmt.Sprintf("commit reindex: %v", commitErr))
		return
	}

	if stats, checkpointErr := storage.WALCheckpointTruncate(ctx, d.db); checkpointErr != nil {
		slog.Warn("WAL checkpoint failed after reindex", "err", checkpointErr)
	} else {
		slog.Info("WAL checkpoint complete after reindex", "duration", stats.Duration)
	}
	if releaseErr := storage.ReleaseSQLiteMemory(ctx, d.db); releaseErr != nil {
		slog.Warn("failed to release SQLite memory after reindex", "err", releaseErr)
	}

	duration := time.Since(start)
	slog.Info("reindex complete", "path", relativePath, "duration", duration)

	sendSSEEvent(sender, "complete", WorkCompleteEvent{
		Status:       "complete",
		Operation:    "reindex",
		OperationID:  operationID,
		Path:         relativePath,
		FilesIndexed: int64(index.NumFiles),
		DirsIndexed:  int64(index.NumDirs),
		TotalSize:    newSize,
		DurationMs:   duration.Milliseconds(),
	})
}

func reindexProgressCallback(operationID, operationPath string, sender sseEventSender) func(int64, int64, string) {
	return func(filesWritten, dirsWritten int64, lastPath string) {
		if (filesWritten+dirsWritten)%100 == 0 {
			sendSSEEvent(sender, "progress", WorkProgressEvent{
				Operation:    "reindex",
				OperationID:  operationID,
				Path:         operationPath,
				FilesIndexed: filesWritten,
				DirsIndexed:  dirsWritten,
				CurrentPath:  lastPath,
			})
		}
	}
}

func (d *daemon) finishPathReindex(ctx context.Context, tx *sql.Tx, indexID int64, relativePath string, scanTime, oldSize int64) (int64, error) {
	deleted, err := storage.CleanupDeletedEntriesUnderPath(ctx, tx, indexID, relativePath, scanTime)
	if err != nil {
		return 0, fmt.Errorf("cleanup deleted entries: %w", err)
	}
	if deleted > 0 {
		slog.Info("cleaned up deleted entries under path", "deleted", deleted, "path", relativePath)
	}

	// Directory rows already aggregate descendants, so read the reindexed
	// path's own row instead of summing and double-counting its subtree.
	var newSizeRow sql.NullInt64
	err = tx.QueryRowContext(ctx, `
		SELECT size FROM entries
		WHERE index_id = ? AND relative_path = ?;
	`, indexID, relativePath).Scan(&newSizeRow)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("query new size: %w", err)
	}

	var newSize int64
	if newSizeRow.Valid {
		newSize = newSizeRow.Int64
	}
	// The root has no parent; propagating its size would double its own row.
	if relativePath != "/" && newSize != oldSize {
		if updateErr := storage.UpdateParentDirectorySizes(ctx, tx, indexID, relativePath, newSize-oldSize); updateErr != nil {
			return 0, fmt.Errorf("update parent sizes: %w", updateErr)
		}
	}
	return newSize, nil
}
