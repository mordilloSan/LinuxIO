package daemon

import (
	"context"
	"database/sql"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/indexer/api"
)

// holdQueue keeps the daemon busy with a piece of work that ends when the
// returned function is called, and waits until that work has started.
func holdQueue(t *testing.T, d *daemon) (release func()) {
	t.Helper()
	started, gate := make(chan struct{}), make(chan struct{})
	d.enqueue(&job{run: func(context.Context) error {
		close(started)
		<-gate
		return nil
	}})
	<-started
	released := false
	return func() {
		if !released {
			released = true
			close(gate)
		}
	}
}

// queued is how many pieces of work wait behind the one that runs.
func (d *daemon) queued() int {
	q := &d.queue
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.waiting)
}

func waitUntil(t *testing.T, what string, done func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if done() {
			return
		}
	}
	t.Fatalf("timed out waiting until %s", what)
}

func newIndexedDaemon(t *testing.T) (*daemon, *sql.DB) {
	t.Helper()
	d, db := newDaemonWithDB(t)
	if _, err := db.Exec(`INSERT INTO indexes (last_indexed) VALUES (1)`); err != nil {
		t.Fatalf("insert index: %v", err)
	}
	return d, db
}

func indexedPaths(t *testing.T, db *sql.DB, below string) []string {
	t.Helper()
	rows, err := db.Query(`SELECT relative_path FROM entries WHERE relative_path LIKE ? ORDER BY relative_path`, below+"%")
	if err != nil {
		t.Fatalf("query entries: %v", err)
	}
	defer rows.Close()
	var paths []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			t.Fatalf("scan entry: %v", err)
		}
		paths = append(paths, path)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read entries: %v", err)
	}
	return paths
}

func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("content"), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func postAdd(d *daemon, path string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(api.EntryRequest{Path: path})
	rr := httptest.NewRecorder()
	d.handleAdd(rr, httptest.NewRequest(http.MethodPost, "/add", strings.NewReader(string(body))))
	return rr
}

func postReindex(d *daemon, path string) (*httptest.ResponseRecorder, api.OperationResponse) {
	rr := httptest.NewRecorder()
	d.handleReindex(context.Background(), rr, httptest.NewRequest(http.MethodPost, "/reindex?path="+url.QueryEscape(path), nil))
	var resp api.OperationResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	return rr, resp
}

func TestAnUpdateOnAFreeDaemonAnswersWhenItIsApplied(t *testing.T) {
	d, db := newIndexedDaemon(t)
	file := filepath.Join(t.TempDir(), "report.txt")
	writeFile(t, file)

	rr := postAdd(d, file)
	if rr.Code != http.StatusOK {
		t.Fatalf("the update answered %d %q", rr.Code, rr.Body.String())
	}
	if got := indexedPaths(t, db, file); len(got) != 1 {
		t.Fatalf("indexed %v when the answer came, want the file", got)
	}
}

func TestAnUpdateBehindOtherWorkIsAcceptedAtOnceAndAppliedInItsTurn(t *testing.T) {
	d, db := newIndexedDaemon(t)
	file := filepath.Join(t.TempDir(), "report.txt")
	writeFile(t, file)

	release := holdQueue(t, d)
	defer release()
	answered := make(chan *httptest.ResponseRecorder, 1)
	go func() { answered <- postAdd(d, file) }()

	// The caller is a file operation in the UI: it must not wait for a scan.
	select {
	case rr := <-answered:
		if rr.Code != http.StatusAccepted || !strings.Contains(rr.Body.String(), `"status":"queued"`) {
			t.Fatalf("the update answered %d %q while other work ran, want 202 queued", rr.Code, rr.Body.String())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the update kept its caller waiting behind other work")
	}
	if got := indexedPaths(t, db, file); len(got) != 0 {
		t.Fatalf("indexed %v before the update's turn", got)
	}
	release()
	waitUntil(t, "the update is applied", func() bool { return len(indexedPaths(t, db, file)) == 1 })
}

func TestRepeatedUpdatesToOnePathCollapse(t *testing.T) {
	d, db := newIndexedDaemon(t)
	file := filepath.Join(t.TempDir(), "report.txt")
	writeFile(t, file)

	release := holdQueue(t, d)
	defer release()
	for range 3 {
		if rr := postAdd(d, file); rr.Code != http.StatusAccepted {
			t.Fatalf("an update answered %d %q", rr.Code, rr.Body.String())
		}
	}
	if got := d.queued(); got != 1 {
		t.Fatalf("%d pieces of work are waiting for one path, want 1", got)
	}
	release()
	waitUntil(t, "the update is applied", func() bool { return len(indexedPaths(t, db, file)) == 1 })
}

func TestReindexRequestsQueueOneAfterAnother(t *testing.T) {
	d, db := newIndexedDaemon(t)
	root := t.TempDir()
	first, second := filepath.Join(root, "first"), filepath.Join(root, "second")
	writeFile(t, filepath.Join(first, "a.txt"))
	writeFile(t, filepath.Join(second, "b.txt"))

	release := holdQueue(t, d)
	defer release()
	rrFirst, opFirst := postReindex(d, first)
	rrSecond, opSecond := postReindex(d, second)
	rrAgain, opAgain := postReindex(d, second)
	for _, rr := range []*httptest.ResponseRecorder{rrFirst, rrSecond, rrAgain} {
		if rr.Code != http.StatusAccepted {
			t.Fatalf("a reindex request answered %d %q, want 202", rr.Code, rr.Body.String())
		}
	}
	if opFirst.OperationID == "" || opFirst.OperationID == opSecond.OperationID {
		t.Fatalf("two folders share operation %q", opFirst.OperationID)
	}
	if opAgain.OperationID != opSecond.OperationID {
		t.Fatalf("a repeated request got operation %q, want the waiting one %q", opAgain.OperationID, opSecond.OperationID)
	}

	release()
	waitUntil(t, "both folders are indexed", func() bool {
		return len(indexedPaths(t, db, first)) == 2 && len(indexedPaths(t, db, second)) == 2
	})
}

func TestASecondFullIndexFollowsTheOneThatWaitsOrRuns(t *testing.T) {
	d := &daemon{}
	t.Cleanup(d.waitBackground)
	started, gate := make(chan struct{}), make(chan struct{})
	defer close(gate)
	first := newWorkStreamBroadcaster("index", "first-index", "/")
	d.enqueue(&job{key: indexJobKey, stream: first, run: func(context.Context) error {
		close(started)
		<-gate
		return nil
	}})
	<-started

	rr := httptest.NewRecorder()
	d.handleIndex(rr, httptest.NewRequest(http.MethodPost, "/index", nil))
	var resp api.OperationResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if rr.Code != http.StatusAccepted || resp.OperationID != "first-index" {
		t.Fatalf("a second full index answered %d with operation %q, want 202 and the first one's", rr.Code, resp.OperationID)
	}
	if got := d.queued(); got != 0 {
		t.Fatalf("%d pieces of work wait, want no second full index", got)
	}
}

func TestADeleteOfAFolderThatIsStillThereReindexesIt(t *testing.T) {
	d, db := newIndexedDaemon(t)
	dir := filepath.Join(t.TempDir(), "kept")
	writeFile(t, filepath.Join(dir, "a.txt"))

	// The folder's reindex is only queued when the answer comes, and the
	// answer says so.
	rr := httptest.NewRecorder()
	d.handleDelete(rr, httptest.NewRequest(http.MethodDelete, "/delete?path="+url.QueryEscape(dir), nil))
	if rr.Code != http.StatusAccepted || !strings.Contains(rr.Body.String(), `"status":"queued"`) {
		t.Fatalf("delete answered %d %q, want 202 queued", rr.Code, rr.Body.String())
	}
	waitUntil(t, "the folder is indexed", func() bool { return len(indexedPaths(t, db, dir)) == 2 })
}

func TestWorkThatPanicsDoesNotStopTheQueue(t *testing.T) {
	d := &daemon{}
	t.Cleanup(d.waitBackground)
	broken, _ := d.enqueue(&job{run: func(context.Context) error { panic("boom") }})
	<-broken.done
	if broken.err == nil {
		t.Fatal("work that panicked reported no error")
	}
	ran := false
	next, _ := d.enqueue(&job{run: func(context.Context) error { ran = true; return nil }})
	<-next.done
	if !ran {
		t.Fatal("the queue stopped after work that panicked")
	}
}

func TestWorkThatArrivesAfterShutdownIsNotRun(t *testing.T) {
	d, _ := newDaemonWithDB(t)
	d.Close()
	ran := false
	late, _ := d.enqueue(&job{run: func(context.Context) error { ran = true; return nil }})
	<-late.done
	if ran || late.err == nil {
		t.Fatalf("work queued after shutdown: ran=%v err=%v, want it refused", ran, late.err)
	}
}

func TestAttachingToWorkThatWaitsSaysSoUntilItStarts(t *testing.T) {
	d, _ := newIndexedDaemon(t)
	dir := filepath.Join(t.TempDir(), "folder")
	writeFile(t, filepath.Join(dir, "a.txt"))

	release := holdQueue(t, d)
	defer release()
	_, op := postReindex(d, dir)
	query := url.Values{"stream": {"true"}, "operation": {"reindex"}, "operation_id": {op.OperationID}, "path": {dir}}
	rr := httptest.NewRecorder()
	streamed := make(chan struct{})
	go func() {
		d.handleStatus(rr, httptest.NewRequest(http.MethodGet, "/status?"+query.Encode(), nil))
		close(streamed)
	}()
	waitUntil(t, "the stream has a subscriber", func() bool {
		stream := d.streamOf(op.OperationID)
		if stream == nil {
			return false
		}
		stream.mu.Lock()
		defer stream.mu.Unlock()
		return len(stream.subscribers) == 1
	})
	release()
	<-streamed

	body := rr.Body.String()
	waiting, started := strings.Index(body, "Waiting for other index work"), strings.Index(body, "event: started")
	if waiting < 0 || started < 0 || waiting > started || !strings.Contains(body, "event: complete") {
		t.Fatalf("stream = %q, want a waiting notice, then started, then complete", body)
	}
}

func TestAnUpdateFollowsTheDiskNotTheRequest(t *testing.T) {
	d, db := newIndexedDaemon(t)
	dir := t.TempDir()
	gone, kept := filepath.Join(dir, "gone.txt"), filepath.Join(dir, "kept.txt")
	writeFile(t, gone)
	writeFile(t, kept)
	for _, path := range []string{gone, kept} {
		if rr := postAdd(d, path); rr.Code != http.StatusOK {
			t.Fatalf("add %s answered %d %q", path, rr.Code, rr.Body.String())
		}
	}
	if err := os.Remove(gone); err != nil {
		t.Fatalf("remove: %v", err)
	}

	// An add for a file that is gone removes its row, and a delete for a file
	// that is still there keeps it: what is on disk decides.
	if rr := postAdd(d, gone); rr.Code != http.StatusOK {
		t.Fatalf("add of a missing file answered %d %q", rr.Code, rr.Body.String())
	}
	rr := httptest.NewRecorder()
	d.handleDelete(rr, httptest.NewRequest(http.MethodDelete, "/delete?path="+url.QueryEscape(kept), nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("delete of an existing file answered %d %q", rr.Code, rr.Body.String())
	}
	if got := indexedPaths(t, db, dir); len(got) != 1 || got[0] != kept {
		t.Fatalf("indexed %v, want only %s", got, kept)
	}
}

func putConfig(d *daemon) *httptest.ResponseRecorder {
	put := httptest.NewRequest(http.MethodPut, "/config", strings.NewReader(`{"include_network_mounts":true}`))
	put = put.WithContext(context.WithValue(put.Context(), connectionKindContextKey{}, true))
	rr := httptest.NewRecorder()
	d.handleConfigPut(rr, put)
	return rr
}

func TestTheDaemonReportsRunningAndRefusesConfigWhileAnIndexOrReindexWaits(t *testing.T) {
	d, _ := newIndexedDaemon(t)
	d.settings.path = filepath.Join(t.TempDir(), "config.yaml")
	release := holdQueue(t, d)
	defer release()

	rr := httptest.NewRecorder()
	d.handleStatus(rr, httptest.NewRequest(http.MethodGet, "/status", nil))
	var status api.StatusResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &status); err != nil || status.Status != api.StatusRunning {
		t.Fatalf("status = %q (%v), want running", status.Status, err)
	}

	postReindex(d, "/srv/data")
	if rr := putConfig(d); rr.Code != http.StatusConflict {
		t.Fatalf("a configuration write answered %d %q while a reindex waited, want 409", rr.Code, rr.Body.String())
	}
}

// The file browser sends a refresh with every listing and an update with
// every file operation: settings saved while someone browses must not be
// refused for those.
func TestAConfigurationWriteIsNotRefusedForSmallUpdates(t *testing.T) {
	d, _ := newIndexedDaemon(t)
	d.settings.path = filepath.Join(t.TempDir(), "config.yaml")
	release := holdQueue(t, d)
	defer release()
	postRefresh(d, "/srv/data")
	postAdd(d, "/srv/data/file.txt")

	if rr := putConfig(d); rr.Code != http.StatusOK {
		t.Fatalf("a configuration write answered %d %q while only small updates waited, want 200", rr.Code, rr.Body.String())
	}
}

func TestEnqueueTellsWorkThatWentInBehindOtherWork(t *testing.T) {
	d := &daemon{}
	t.Cleanup(d.waitBackground)
	first, _ := d.enqueue(&job{run: func(context.Context) error { return nil }})
	<-first.done
	waitUntil(t, "the daemon is free", func() bool { return !d.busy() })
	if first.behind {
		t.Fatal("work on a free daemon was told it went in behind other work")
	}

	release := holdQueue(t, d)
	defer release()
	second, _ := d.enqueue(&job{run: func(context.Context) error { return nil }})
	if !second.behind {
		t.Fatal("work behind running work was not told so")
	}
}

func TestWorkWithAStreamThatFailsEndsItWithAnError(t *testing.T) {
	d := &daemon{}
	t.Cleanup(d.waitBackground)
	release := holdQueue(t, d)
	defer release()
	stream := newWorkStreamBroadcaster("reindex", "broken", "/srv")
	d.enqueue(&job{key: "tree:/srv", stream: stream, run: func(context.Context) error { panic("boom") }})
	_, events, err := stream.subscribe()
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	release()

	var last workStreamEvent
	for evt := range events {
		last = evt
	}
	if last.event != api.EventError {
		t.Fatalf("the stream ended with %q, want an error event: a client would take the work for done", last.event)
	}
}

func TestIndexWorkAskedOfAStoppingDaemonIsRefused(t *testing.T) {
	d, _ := newDaemonWithDB(t)
	d.Close()

	rr, _ := postReindex(d, "/srv")
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("a reindex asked of a stopping daemon answered %d %q, want 503", rr.Code, rr.Body.String())
	}
	rr = httptest.NewRecorder()
	d.handleIndex(rr, httptest.NewRequest(http.MethodPost, "/index", nil))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("a full index asked of a stopping daemon answered %d %q, want 503", rr.Code, rr.Body.String())
	}
}

func TestStatusNamesTheOperationsThatWait(t *testing.T) {
	d, _ := newIndexedDaemon(t)
	release := holdQueue(t, d)
	defer release()
	_, op := postReindex(d, "/srv/data")

	rr := httptest.NewRecorder()
	d.handleStatus(rr, httptest.NewRequest(http.MethodGet, "/status", nil))
	var status api.StatusResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if len(status.WaitingOperations) != 1 {
		t.Fatalf("status names %d waiting operations, want the reindex: %s", len(status.WaitingOperations), rr.Body.String())
	}
	waiting := status.WaitingOperations[0]
	if waiting.Operation != "reindex" || waiting.OperationID != op.OperationID || waiting.Path != "/srv/data" {
		t.Fatalf("waiting operation = %+v", waiting)
	}
}
