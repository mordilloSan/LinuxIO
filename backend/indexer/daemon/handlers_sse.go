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

// SendError sends an SSE error event.
func (s *SSEWriter) SendError(msg string) error {
	return s.SendEvent("error", map[string]string{"message": msg})
}

type sseEventSender interface {
	SendEvent(event string, data any) error
	SendError(msg string) error
}

type workStreamEvent struct {
	event string
	data  any
}

type workStreamBroadcaster struct {
	operation        string
	path             string
	mu               sync.Mutex
	subscribers      map[int]chan workStreamEvent
	nextSubscriberID int
	closed           bool
}

const workStreamSubscriberBuffer = 64

func newWorkStreamBroadcaster(operation, path string) *workStreamBroadcaster {
	return &workStreamBroadcaster{
		operation:   operation,
		path:        path,
		subscribers: make(map[int]chan workStreamEvent),
	}
}

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
	return b.SendEvent("error", map[string]string{"message": msg})
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

func sendSSEEvent(s sseEventSender, event string, data any) bool {
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
		Status:    "running",
		Operation: broadcaster.Operation(),
		Path:      broadcaster.Path(),
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

func (d *daemon) beginWorkStream(operation, path string) *workStreamBroadcaster {
	b := newWorkStreamBroadcaster(operation, path)
	d.setWorkStreamBroadcaster(b)
	return b
}

func (d *daemon) endWorkStream(b *workStreamBroadcaster) {
	d.clearWorkStreamBroadcaster(b)
	b.close()
}

// reindexPathWithProgress reindexes a path and emits progress events.
func (d *daemon) reindexPathWithProgress(ctx context.Context, relativePath string, sender sseEventSender) {
	start := time.Now()
	slog.Info("starting reindex", "path", relativePath)

	indexID, err := d.store.LatestIndexID(ctx)
	if err != nil {
		sendSSEError(sender, fmt.Sprintf("no index present: %v", err))
		return
	}

	// Refuse to proceed when the path can't even be stat'ed (e.g. permissions):
	// entries are deleted before rescanning, so failing later would leave the
	// index missing the whole subtree. A path that no longer exists is fine —
	// removing its stale entries is the point of reindexing it. Files are
	// rejected for the same reason: traversal only handles directories, so a
	// file's row would be deleted and then never rewritten.
	cfg := d.configSnapshot()
	realPath := strings.TrimRight(cfg.IndexPath, "/") + relativePath
	fi, statErr := os.Stat(realPath)
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		sendSSEError(sender, fmt.Sprintf("cannot access %s: %v", realPath, statErr))
		return
	}
	if statErr == nil && !fi.IsDir() {
		sendSSEError(sender, fmt.Sprintf("%s is a file; reindex its parent directory instead", realPath))
		return
	}

	if !sendSSEEvent(sender, "progress", WorkProgressEvent{
		Operation:   "reindex",
		CurrentPath: "Deleting old entries...",
	}) {
		return
	}

	if deleteErr := storage.DeletePathRecursive(ctx, d.db, indexID, relativePath); deleteErr != nil {
		sendSSEError(sender, fmt.Sprintf("failed to delete existing entries: %v", deleteErr))
		return
	}

	index := indexing.Initialize(
		cfg.IndexName,
		cfg.IndexPath,
		cfg.IndexPath,
		cfg.IncludeHidden,
		indexing.WithNetworkMounts(cfg.IncludeNetworkMounts),
		indexing.WithExcludePaths(cfg.ExcludePaths),
	)
	writer := storage.NewStreamingWriter(ctx, d.db, indexID, 1000, reindexProgressCallback(sender))
	index.EnableStreaming(writer)

	if indexErr := index.StartIndexingFromPath(relativePath); indexErr != nil {
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

	newSize, finishErr := d.finishPathReindex(ctx, indexID, relativePath, writer.ScanTime())
	if finishErr != nil {
		sendSSEError(sender, finishErr.Error())
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
		Path:         relativePath,
		FilesIndexed: int64(index.NumFiles),
		DirsIndexed:  int64(index.NumDirs),
		TotalSize:    newSize,
		DurationMs:   duration.Milliseconds(),
	})
}

func reindexProgressCallback(sender sseEventSender) func(int64, int64, string) {
	return func(filesWritten, dirsWritten int64, lastPath string) {
		if (filesWritten+dirsWritten)%100 == 0 {
			sendSSEEvent(sender, "progress", WorkProgressEvent{
				Operation:    "reindex",
				FilesIndexed: filesWritten,
				DirsIndexed:  dirsWritten,
				CurrentPath:  lastPath,
			})
		}
	}
}

func (d *daemon) finishPathReindex(ctx context.Context, indexID int64, relativePath string, scanTime int64) (int64, error) {
	deleted, err := storage.CleanupDeletedEntriesUnderPath(ctx, d.db, indexID, relativePath, scanTime)
	if err != nil {
		return 0, fmt.Errorf("cleanup deleted entries: %w", err)
	}
	if deleted > 0 {
		slog.Info("cleaned up deleted entries under path", "deleted", deleted, "path", relativePath)
	}

	// Directory rows already aggregate descendants, so read the reindexed
	// path's own row instead of summing and double-counting its subtree.
	var newSizeRow sql.NullInt64
	err = d.db.QueryRowContext(ctx, `
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
	if relativePath != "/" && newSize != 0 {
		if updateErr := storage.UpdateParentDirectorySizes(ctx, d.db, indexID, relativePath, newSize); updateErr != nil {
			return 0, fmt.Errorf("update parent sizes: %w", updateErr)
		}
	}
	return newSize, nil
}

// vacuumWithProgress runs vacuum and emits progress events.
func (d *daemon) vacuumWithProgress(ctx context.Context, sender sseEventSender) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 1*time.Hour)
	defer cancel()

	indexID, err := d.store.LatestIndexID(ctx)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		slog.Warn("vacuum: latest index id unavailable", "err", err)
		indexID = 0
	}

	if !sendSSEEvent(sender, "progress", WorkProgressEvent{
		Operation: "vacuum",
		Phase:     "pre_checkpoint",
		Message:   "Running WAL checkpoint before vacuum...",
	}) {
		return
	}

	if _, checkpointErr := storage.WALCheckpointTruncate(ctx, d.db); checkpointErr != nil {
		slog.Warn("vacuum: wal checkpoint pre failed", "err", checkpointErr)
		if !sendSSEEvent(sender, "progress", WorkProgressEvent{
			Operation: "vacuum",
			Phase:     "pre_checkpoint",
			Message:   fmt.Sprintf("WAL checkpoint warning: %v", checkpointErr),
		}) {
			return
		}
	}

	if !sendSSEEvent(sender, "progress", WorkProgressEvent{
		Operation: "vacuum",
		Phase:     "vacuum",
		Message:   "Running VACUUM (this may take a while)...",
	}) {
		return
	}

	vs, err := storage.Vacuum(ctx, d.db)
	if err != nil {
		slog.Error("vacuum failed", "err", err)
		sendSSEError(sender, fmt.Sprintf("vacuum failed: %v", err))
		return
	}

	slog.Info("vacuum complete", "duration", vs.Duration)

	if !sendSSEEvent(sender, "progress", WorkProgressEvent{
		Operation: "vacuum",
		Phase:     "post_checkpoint",
		Message:   "Running WAL checkpoint after vacuum...",
	}) {
		return
	}

	if _, err := storage.WALCheckpointTruncate(ctx, d.db); err != nil {
		slog.Warn("vacuum: wal checkpoint post failed", "err", err)
	}
	if err := storage.ReleaseSQLiteMemory(ctx, d.db); err != nil {
		slog.Warn("failed to release SQLite memory after vacuum", "err", err)
	}
	logDatabaseDiskUsage(ctx, d.db)

	if indexID != 0 {
		if _, err := d.db.ExecContext(ctx, `UPDATE indexes SET vacuum_duration_ms = ? WHERE id = ?;`, vs.Duration.Milliseconds(), indexID); err != nil {
			slog.Warn("vacuum: failed to persist duration", "err", err)
		}
	}

	duration := time.Since(start)
	sendSSEEvent(sender, "complete", WorkCompleteEvent{
		Status:     "complete",
		Operation:  "vacuum",
		DurationMs: duration.Milliseconds(),
	})
}

// logDatabaseDiskUsage logs per-table/index disk usage via DBSTAT.
// Silently skipped when the binary was built without -tags sqlite_dbstat.
func logDatabaseDiskUsage(ctx context.Context, db *sql.DB) {
	usage, err := storage.DatabaseDiskUsage(ctx, db)
	if err != nil {
		if !errors.Is(err, storage.ErrDBStatUnavailable) {
			slog.Warn("dbstat disk usage query failed", "err", err)
		}
		return
	}
	for _, t := range usage {
		slog.Info("db disk usage", "object", t.Name, "pages", t.Pages, "bytes", t.Bytes, "unused_bytes", t.UnusedBytes)
	}
}

// pruneWithProgress prunes historical index data and emits status events.
func (d *daemon) pruneWithProgress(ctx context.Context, keepLatest, maxAgeDays int, sender sseEventSender) {
	ctx, cancel := context.WithTimeout(ctx, 1*time.Hour)
	defer cancel()

	maxAge := time.Duration(maxAgeDays) * 24 * time.Hour
	if !sendSSEEvent(sender, "progress", WorkProgressEvent{
		Operation: "prune",
		Phase:     "prune",
		Message:   "Pruning old index records...",
	}) {
		return
	}

	stats, err := storage.PruneOldIndexes(ctx, d.db, keepLatest, maxAge)
	if err != nil {
		slog.Error("prune failed", "err", err)
		sendSSEError(sender, fmt.Sprintf("prune failed: %v", err))
		return
	}

	slog.Info("prune complete", "duration", stats.Duration, "deleted_indexes", stats.DeletedIndexes, "deleted_entries", stats.DeletedEntries)

	if !sendSSEEvent(sender, "progress", WorkProgressEvent{
		Operation: "prune",
		Phase:     "post_cleanup",
		Message:   "Running WAL checkpoint after prune...",
	}) {
		return
	}

	if _, err := storage.WALCheckpointTruncate(ctx, d.db); err != nil {
		slog.Warn("prune: wal checkpoint failed", "err", err)
	}
	if err := storage.ReleaseSQLiteMemory(ctx, d.db); err != nil {
		slog.Warn("prune: failed to release SQLite memory", "err", err)
	}

	sendSSEEvent(sender, "complete", WorkCompleteEvent{
		Status:         "complete",
		Operation:      "prune",
		DurationMs:     stats.Duration.Milliseconds(),
		DeletedIndexes: stats.DeletedIndexes,
		DeletedEntries: stats.DeletedEntries,
	})
}
