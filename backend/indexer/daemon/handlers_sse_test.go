package daemon

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/indexer/indexing"
	"github.com/mordilloSan/LinuxIO/backend/indexer/storage"
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
		b := newWorkStreamBroadcaster("reindex", "op-1", "/docs")
		defer b.close()

		if got := b.Operation(); got != "reindex" {
			t.Fatalf("operation = %q, want %q", got, "reindex")
		}
		if got := b.Path(); got != "/docs" {
			t.Fatalf("path = %q, want %q", got, "/docs")
		}
	})

	t.Run("fanout_to_subscribers", func(t *testing.T) {
		b := newWorkStreamBroadcaster("reindex", "op-1", "/docs")
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

		if err := b.SendEvent("progress", WorkProgressEvent{Operation: "reindex", OperationID: "op-1", Path: "/docs", FilesIndexed: 10}); err != nil {
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
		b := newWorkStreamBroadcaster("reindex", "op-1", "/docs")
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
		b := newWorkStreamBroadcaster("reindex", "op-1", "/docs")
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
		errorData, ok := evt.data.(WorkErrorEvent)
		if !ok {
			t.Fatalf("error payload type = %T, want WorkErrorEvent", evt.data)
		}
		if errorData.Message != "boom" || errorData.OperationID != "op-1" {
			t.Fatalf("error payload = %+v", errorData)
		}
	})

	t.Run("close_broadcaster", func(t *testing.T) {
		b := newWorkStreamBroadcaster("reindex", "op-1", "/docs")

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
		if err := b.SendEvent("progress", WorkProgressEvent{Operation: "reindex", OperationID: "op-1", Path: "/docs"}); err == nil {
			t.Fatal("expected send error after broadcaster close")
		}
	})
}

func TestHandleStatusStreamFallsBackToUninitializedJSON(t *testing.T) {
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
	if !strings.Contains(rr.Body.String(), `"status":"uninitialized"`) {
		t.Fatalf("body = %q, expected uninitialized json status", rr.Body.String())
	}
}

func TestReindexPathRejectsDirectorySymlink(t *testing.T) {
	d, _ := newDaemonWithDB(t)
	if _, err := d.db.Exec(`INSERT INTO indexes (last_indexed) VALUES (1)`); err != nil {
		t.Fatalf("insert index: %v", err)
	}

	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(t.TempDir(), link); err != nil {
		t.Fatalf("create directory symlink: %v", err)
	}
	broadcaster := newWorkStreamBroadcaster("reindex", "op-1", link)
	defer broadcaster.close()
	_, events, err := broadcaster.subscribe()
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	d.reindexPathWithProgress(context.Background(), "op-1", link, broadcaster)
	evt := mustReceiveWorkEvent(t, events)
	if evt.event != "error" {
		t.Fatalf("event = %q, want error", evt.event)
	}
	got, ok := evt.data.(WorkErrorEvent)
	if !ok {
		t.Fatalf("event data type = %T, want WorkErrorEvent", evt.data)
	}
	if !strings.Contains(got.Message, "is not a directory") {
		t.Fatalf("error = %q, want non-directory rejection", got.Message)
	}
}

func TestPartialReindexSynchronizesMetadata(t *testing.T) {
	d, _ := newDaemonWithDB(t)
	res, insertErr := d.db.Exec(`INSERT INTO indexes (num_dirs, last_indexed) VALUES (1, 1)`)
	if insertErr != nil {
		t.Fatalf("insert index: %v", insertErr)
	}
	indexID, idErr := res.LastInsertId()
	if idErr != nil {
		t.Fatalf("index id: %v", idErr)
	}
	if _, err := d.db.Exec(`
		INSERT INTO entries (index_id, relative_path, name, size, mod_time, type)
		VALUES (?, '/', '/', 0, 1, 'directory');
	`, indexID); err != nil {
		t.Fatalf("insert root entry: %v", err)
	}

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "child"), 0o755); err != nil {
		t.Fatalf("create child directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "file"), []byte("file"), 0o600); err != nil {
		t.Fatalf("create file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "child", "nested"), []byte("nested"), 0o600); err != nil {
		t.Fatalf("create nested file: %v", err)
	}

	broadcaster := newWorkStreamBroadcaster("reindex", "op-1", root)
	defer broadcaster.close()
	_, events, subscribeErr := broadcaster.subscribe()
	if subscribeErr != nil {
		t.Fatalf("subscribe: %v", subscribeErr)
	}
	d.reindexExistingPath(context.Background(), "op-1", root, broadcaster, time.Now(), DaemonConfig{IncludeNetworkMounts: true}, indexID)
	if evt := mustReceiveWorkEvent(t, events); evt.event != "complete" {
		t.Fatalf("event = %q, want complete; data = %+v", evt.event, evt.data)
	}

	var dirs, files, totalSize, lastIndexed int64
	if err := d.db.QueryRow(`
		SELECT num_dirs, num_files, total_size, last_indexed
		FROM indexes WHERE id = ?;
	`, indexID).Scan(&dirs, &files, &totalSize, &lastIndexed); err != nil {
		t.Fatalf("query metadata: %v", err)
	}
	var actualDirs, actualFiles, rootSize int64
	if err := d.db.QueryRow(`
		SELECT
			COALESCE(SUM(CASE WHEN type = 'directory' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN type = 'directory' THEN 0 ELSE 1 END), 0),
			COALESCE((SELECT size FROM entries WHERE index_id = ? AND relative_path = '/'), 0)
		FROM entries WHERE index_id = ?;
	`, indexID, indexID).Scan(&actualDirs, &actualFiles, &rootSize); err != nil {
		t.Fatalf("query entries: %v", err)
	}
	if dirs != actualDirs || files != actualFiles || totalSize != rootSize {
		t.Fatalf("metadata = dirs %d, files %d, size %d; entries = %d, %d, %d", dirs, files, totalSize, actualDirs, actualFiles, rootSize)
	}
	if lastIndexed <= 1 {
		t.Fatalf("last indexed = %d, want refreshed timestamp", lastIndexed)
	}
}

func TestPartialReindexPreservesHardlinkContributionOutsideSubtree(t *testing.T) {
	d, _ := newDaemonWithDB(t)
	res, insertErr := d.db.Exec(`INSERT INTO indexes (num_dirs, num_files, last_indexed) VALUES (3, 2, 1)`)
	if insertErr != nil {
		t.Fatalf("insert index: %v", insertErr)
	}
	indexID, idErr := res.LastInsertId()
	if idErr != nil {
		t.Fatalf("index id: %v", idErr)
	}

	root := t.TempDir()
	left := filepath.Join(root, "left")
	right := filepath.Join(root, "right")
	if err := os.Mkdir(left, 0o700); err != nil {
		t.Fatalf("mkdir left: %v", err)
	}
	if err := os.Mkdir(right, 0o700); err != nil {
		t.Fatalf("mkdir right: %v", err)
	}
	leftPath := filepath.Join(left, "link")
	rightPath := filepath.Join(right, "link")
	if err := os.WriteFile(leftPath, []byte("hardlinked"), 0o600); err != nil {
		t.Fatalf("write hardlink fixture: %v", err)
	}
	if err := os.Link(leftPath, rightPath); err != nil {
		t.Fatalf("create hardlink: %v", err)
	}

	entryForPath := func(path string) indexing.IndexEntry {
		t.Helper()
		info, statErr := os.Lstat(path)
		if statErr != nil {
			t.Fatalf("lstat %s: %v", path, statErr)
		}
		return indexing.EntryFromFileInfo(path, info)
	}
	leftEntry := entryForPath(leftPath)
	rightEntry := entryForPath(rightPath)
	leftEntry.SizeContribution = 0
	rightEntry.SizeContribution = rightEntry.Size
	leftDir := entryForPath(left)
	rightDir := entryForPath(right)
	rightDir.Size += rightEntry.SizeContribution
	rootSize := leftDir.Size + rightDir.Size
	rootEntry := indexing.IndexEntry{RelativePath: "/", Name: "/", Size: rootSize, ModTime: time.Now(), Type: "directory"}
	for _, entry := range []indexing.IndexEntry{rootEntry, leftDir, rightDir, leftEntry, rightEntry} {
		if _, err := storage.UpdateEntry(context.Background(), d.db, indexID, entry); err != nil {
			t.Fatalf("seed %s: %v", entry.RelativePath, err)
		}
	}
	if _, err := d.db.Exec(`UPDATE indexes SET total_size = ? WHERE id = ?`, rootSize, indexID); err != nil {
		t.Fatalf("seed total size: %v", err)
	}

	broadcaster := newWorkStreamBroadcaster("reindex", "op-hardlink", left)
	defer broadcaster.close()
	_, events, subscribeErr := broadcaster.subscribe()
	if subscribeErr != nil {
		t.Fatalf("subscribe: %v", subscribeErr)
	}
	d.reindexExistingPath(context.Background(), "op-hardlink", left, broadcaster, time.Now(), DaemonConfig{IncludeNetworkMounts: true}, indexID)
	if evt := mustReceiveWorkEvent(t, events); evt.event != "complete" {
		t.Fatalf("event = %q, want complete; data = %+v", evt.event, evt.data)
	}

	var gotRoot, gotContribution int64
	if err := d.db.QueryRow(`SELECT size FROM entries WHERE index_id = ? AND relative_path = '/'`, indexID).Scan(&gotRoot); err != nil {
		t.Fatalf("query root size: %v", err)
	}
	if err := d.db.QueryRow(`
		SELECT COALESCE(SUM(size_contribution), 0) FROM entries
		WHERE index_id = ? AND device = ? AND inode = ?;
	`, indexID, int64(leftEntry.Device), int64(leftEntry.Inode)).Scan(&gotContribution); err != nil {
		t.Fatalf("query hardlink contribution: %v", err)
	}
	if gotRoot != rootSize || gotContribution != leftEntry.Size {
		t.Fatalf("root size = %d, contribution = %d; want %d and %d", gotRoot, gotContribution, rootSize, leftEntry.Size)
	}
}

func TestAttachStatusSSERequiresMatchingOperationIdentity(t *testing.T) {
	d := &daemon{}
	d.setWorkStreamBroadcaster(newWorkStreamBroadcaster("reindex", "op-1", "/docs"))

	for _, query := range []string{
		"stream=true",
		"stream=true&operation_id=other&operation=reindex&path=%2Fdocs",
		"stream=true&operation_id=op-1&operation=index&path=%2Fdocs",
		"stream=true&operation_id=op-1&operation=reindex&path=%2Fother",
	} {
		req := httptest.NewRequest(http.MethodGet, "/status?"+query, nil)
		rr := httptest.NewRecorder()
		if d.attachStatusSSEIfActive(rr, req) {
			t.Fatalf("attached with mismatched query %q", query)
		}
	}
}

func TestHandleStatusStreamAttachReceivesEventAndCleansSubscriber(t *testing.T) {
	d := &daemon{}
	broadcaster := newWorkStreamBroadcaster("reindex", "op-1", "/docs")
	d.setWorkStreamBroadcaster(broadcaster)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/status?stream=true&operation_id=op-1&operation=reindex&path=%2Fdocs", nil).WithContext(ctx)
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
		OperationID: "op-1",
		Path:        "/docs",
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
	if !strings.Contains(body, "\"operation_id\":\"op-1\"") {
		t.Fatalf("body = %q, expected operation id payload", body)
	}
	if !strings.Contains(body, "event: progress") {
		t.Fatalf("body = %q, expected progress event", body)
	}
}
