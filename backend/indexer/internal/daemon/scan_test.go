package daemon

import (
	"context"
	"encoding/json/v2"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/indexer/api"
)

// serveScanRoutes serves the routes a full index uses and returns a client
// that reaches them the way the scanner reaches the daemon's socket.
func serveScanRoutes(t *testing.T, d *daemon) *http.Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc(api.RouteIndex, d.handleIndex)
	mux.HandleFunc(api.RouteScan, d.handleScan)
	mux.HandleFunc(api.RouteScanEvent, d.handleScanEvent)
	server := httptest.NewServer(mux)
	client := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", server.Listener.Addr().String())
	}}}
	// Registered after the daemon's own cleanup, so it runs first: open scan
	// connections end before the daemon waits for its work.
	t.Cleanup(func() {
		client.CloseIdleConnections()
		server.CloseClientConnections()
		server.Close()
	})
	return client
}

// openScan opens a scan link on its own goroutine and returns a channel that
// delivers it when the daemon has given the scanner its turn.
func openScan(t *testing.T, ctx context.Context, client *http.Client) <-chan *ScanLink {
	t.Helper()
	opened := make(chan *ScanLink, 1)
	go func() {
		link, err := OpenScanLink(ctx, client)
		if err != nil {
			if ctx.Err() == nil {
				t.Errorf("open scan link: %v", err)
			}
			close(opened)
			return
		}
		opened <- link
	}()
	return opened
}

func awaitScan(t *testing.T, opened <-chan *ScanLink) *ScanLink {
	t.Helper()
	select {
	case link := <-opened:
		if link == nil {
			t.Fatal("the scan link was not opened")
		}
		t.Cleanup(link.Close)
		return link
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the scanner's turn")
		return nil
	}
}

func activeOperation(t *testing.T, d *daemon) (operation, id string) {
	t.Helper()
	rr := httptest.NewRecorder()
	d.handleStatus(rr, httptest.NewRequest(http.MethodGet, "/status", nil))
	var status api.StatusResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	return status.ActiveOperation, status.ActiveOperationID
}

func TestAScannerTakesItsTurnInTheQueueAndHoldsItUntilItEnds(t *testing.T) {
	d, db := newIndexedDaemon(t)
	client := serveScanRoutes(t, d)
	release := holdQueue(t, d)
	defer release()

	opened := openScan(t, context.Background(), client)
	waitUntil(t, "the scan waits in the queue", func() bool { return d.queued() == 1 })
	select {
	case <-opened:
		t.Fatal("the scanner was given its turn while other work ran")
	case <-time.After(50 * time.Millisecond):
	}

	release()
	link := awaitScan(t, opened)
	if operation, id := activeOperation(t, d); operation != "index" || id != link.OperationID() {
		t.Fatalf("status names %q %q as the active operation, want the index %q", operation, id, link.OperationID())
	}

	// An update that arrives during the scan waits for it.
	file := t.TempDir() + "/during-scan.txt"
	writeFile(t, file)
	if rr := postAdd(d, file); rr.Code != http.StatusAccepted {
		t.Fatalf("an update during a scan answered %d, want 202", rr.Code)
	}
	if got := indexedPaths(t, db, file); len(got) != 0 {
		t.Fatalf("an update was applied while the scan ran: %v", got)
	}

	if err := link.End(context.Background(), IndexRunStats{Files: 3, Dirs: 2, TotalSize: 10}, nil); err != nil {
		t.Fatalf("end scan: %v", err)
	}
	waitUntil(t, "the update that waited is applied", func() bool { return len(indexedPaths(t, db, file)) == 1 })
}

func TestTheScannersProgressAndResultReachTheStream(t *testing.T) {
	d, _ := newIndexedDaemon(t)
	client := serveScanRoutes(t, d)
	link := awaitScan(t, openScan(t, context.Background(), client))
	stream := d.streamOf(link.OperationID())
	if stream == nil {
		t.Fatal("the scan has no progress stream")
	}
	_, events, err := stream.subscribe()
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	link.Step("Scanning filesystem")
	link.ScanProgress(2, 3, 10)
	if err := link.End(context.Background(), IndexRunStats{Files: 3, Dirs: 2, TotalSize: 10, DeletedEntries: 1}, nil); err != nil {
		t.Fatalf("end scan: %v", err)
	}

	var seen []string
	var complete api.WorkCompleteEvent
	for evt := range events {
		seen = append(seen, evt.event)
		if evt.event == api.EventComplete {
			complete, _ = evt.data.(api.WorkCompleteEvent)
		}
	}
	if len(seen) < 3 || seen[len(seen)-1] != api.EventComplete {
		t.Fatalf("stream events = %v, want progress and then complete", seen)
	}
	if complete.FilesIndexed != 3 || complete.DirsIndexed != 2 || complete.TotalSize != 10 || complete.DeletedEntries != 1 || complete.OperationID != link.OperationID() {
		t.Fatalf("complete event = %+v", complete)
	}
}

func TestAnIndexRequestIsJoinedByTheScannerItStarts(t *testing.T) {
	d, _ := newIndexedDaemon(t)
	client := serveScanRoutes(t, d)

	rr := httptest.NewRecorder()
	d.handleIndex(rr, httptest.NewRequest(http.MethodPost, "/index", nil))
	var resp api.OperationResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil || rr.Code != http.StatusAccepted || resp.OperationID == "" {
		t.Fatalf("index request answered %d %q", rr.Code, rr.Body.String())
	}

	link := awaitScan(t, openScan(t, context.Background(), client))
	if link.OperationID() != resp.OperationID {
		t.Fatalf("the scanner got operation %q, want the one the request was given, %q", link.OperationID(), resp.OperationID)
	}
	// A second request while it runs follows the same index.
	rr = httptest.NewRecorder()
	d.handleIndex(rr, httptest.NewRequest(http.MethodPost, "/index", nil))
	var again api.OperationResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &again)
	if again.OperationID != resp.OperationID || d.queued() != 0 {
		t.Fatalf("a second request got operation %q with %d waiting, want %q and none", again.OperationID, d.queued(), resp.OperationID)
	}
	if err := link.End(context.Background(), IndexRunStats{}, nil); err != nil {
		t.Fatalf("end scan: %v", err)
	}
	waitUntil(t, "the index has ended", func() bool { return !d.busy() })
}

// followStream subscribes to stream now and returns a function that waits
// for the stream to end and gives its last event.
func followStream(t *testing.T, stream *workStreamBroadcaster) (last func() workStreamEvent) {
	t.Helper()
	if stream == nil {
		t.Fatal("the index has no stream")
	}
	_, events, err := stream.subscribe()
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	return func() workStreamEvent {
		var last workStreamEvent
		for evt := range events {
			last = evt
		}
		return last
	}
}

func TestAnIndexRequestThatNoScannerJoinsFails(t *testing.T) {
	// Restored after the daemon's work has ended, which reads it.
	previous := scanAttachTimeout
	scanAttachTimeout = 20 * time.Millisecond
	t.Cleanup(func() { scanAttachTimeout = previous })
	d, _ := newIndexedDaemon(t)
	release := holdQueue(t, d)
	defer release()

	rr := httptest.NewRecorder()
	d.handleIndex(rr, httptest.NewRequest(http.MethodPost, "/index", nil))
	var resp api.OperationResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	last := followStream(t, d.streamOf(resp.OperationID))
	release()

	if last := last(); last.event != api.EventError {
		t.Fatalf("an index no scanner joined ended with %q, want an error", last.event)
	}
	waitUntil(t, "the queue moved on", func() bool { return !d.busy() })
}

func TestAScannerThatGoesAwayEndsItsIndexWithAnError(t *testing.T) {
	d, _ := newIndexedDaemon(t)
	client := serveScanRoutes(t, d)
	ctx := t.Context()
	link := awaitScan(t, openScan(t, ctx, client))
	last := followStream(t, d.streamOf(link.OperationID()))

	// The scanner dies: its connection closes without a result.
	link.Close()

	if last := last(); last.event != api.EventError {
		t.Fatalf("the index of a scanner that went away ended with %q, want an error", last.event)
	}
	waitUntil(t, "the queue moved on", func() bool { return !d.busy() })
}

func TestAFailedScanEndsItsIndexWithTheScannersError(t *testing.T) {
	d, _ := newIndexedDaemon(t)
	client := serveScanRoutes(t, d)
	link := awaitScan(t, openScan(t, context.Background(), client))
	ended := followStream(t, d.streamOf(link.OperationID()))

	if err := link.End(context.Background(), IndexRunStats{}, errors.New("open database: disk full")); err != nil {
		t.Fatalf("end scan: %v", err)
	}

	last := ended()
	failure, _ := last.data.(api.WorkErrorEvent)
	if last.event != api.EventError || !strings.Contains(failure.Message, "disk full") {
		t.Fatalf("the failed index ended with %q %+v, want the scanner's error", last.event, last.data)
	}
}

func TestTheScannerLearnsThatTheDaemonStopped(t *testing.T) {
	d, _ := newIndexedDaemon(t)
	ctx, stop := context.WithCancel(context.Background())
	d.workCtx = ctx
	client := serveScanRoutes(t, d)
	link := awaitScan(t, openScan(t, context.Background(), client))

	stop()

	select {
	case <-link.Gone():
	case <-time.After(5 * time.Second):
		t.Fatal("the scanner was not told that the daemon stopped")
	}
}

func TestAScanEventForAnotherOperationIsRefused(t *testing.T) {
	d, _ := newIndexedDaemon(t)
	client := serveScanRoutes(t, d)
	link := awaitScan(t, openScan(t, context.Background(), client))

	rr := httptest.NewRecorder()
	d.handleScanEvent(rr, httptest.NewRequest(http.MethodPost, "/scan/event?operation_id=someone-else", nil))
	if rr.Code != http.StatusConflict {
		t.Fatalf("an event for another operation answered %d, want 409", rr.Code)
	}
	if err := link.End(context.Background(), IndexRunStats{}, nil); err != nil {
		t.Fatalf("end scan: %v", err)
	}
}

func TestASecondScannerIsRefusedWhileOneScans(t *testing.T) {
	d, _ := newIndexedDaemon(t)
	client := serveScanRoutes(t, d)
	link := awaitScan(t, openScan(t, context.Background(), client))

	if _, err := OpenScanLink(context.Background(), client); err == nil {
		t.Fatal("a second scanner was let in while one scans")
	}
	if err := link.End(context.Background(), IndexRunStats{}, nil); err != nil {
		t.Fatalf("end scan: %v", err)
	}
}

func TestAScannerCannotJoinAnIndexThatHasEnded(t *testing.T) {
	session := newScanSession()
	session.end(IndexRunStats{}, errScannerNotStarted)

	// It would be told its turn has come and scan with nothing holding the
	// queue for it.
	if session.attach() {
		t.Fatal("a scanner joined an index that had already ended")
	}
}

func TestAnIndexWithItsScannerIsNotFailedForWantOfOne(t *testing.T) {
	session := newScanSession()
	if !session.attach() {
		t.Fatal("the first scanner was refused")
	}

	// The wait for a scanner can run out in the instant one attaches.
	session.endUnlessAttached(errScannerNotStarted)

	select {
	case <-session.ended:
		t.Fatal("an index whose scanner had attached was ended as having none")
	default:
	}
}

func TestAScannerThatLeavesBeforeItsTurnTakesItsIndexOutOfTheQueue(t *testing.T) {
	d, _ := newIndexedDaemon(t)
	client := serveScanRoutes(t, d)
	release := holdQueue(t, d)
	defer release()
	ctx, kill := context.WithCancel(context.Background())
	openScan(t, ctx, client)
	waitUntil(t, "the scan waits in the queue", func() bool { return d.queued() == 1 })

	kill()

	// Left in the queue, it would be joined by every later request for an
	// index and refuse the scanner of each.
	waitUntil(t, "the dead index has left the queue", func() bool { return d.queued() == 0 })
	opened := openScan(t, context.Background(), client)
	waitUntil(t, "the new scan waits in the queue", func() bool { return d.queued() == 1 })
	release()
	if err := awaitScan(t, opened).End(context.Background(), IndexRunStats{}, nil); err != nil {
		t.Fatalf("end scan: %v", err)
	}
}

func TestAScannerToldToStopKeepsItsTurnUntilItHasReported(t *testing.T) {
	d, _ := newIndexedDaemon(t)
	client := serveScanRoutes(t, d)
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	link := awaitScan(t, openScan(t, ctx, client))
	ended := followStream(t, d.streamOf(link.OperationID()))

	// SIGTERM reaches the scanner. Its scan still has a transaction to roll
	// back: until it has said how it ended, the queue stays its.
	stop()
	select {
	case <-link.Gone():
		t.Fatal("the scan connection closed when the scanner was told to stop")
	case <-time.After(100 * time.Millisecond):
	}
	if session := d.runningScan(); session == nil {
		t.Fatal("the daemon moved on while the scanner was still stopping")
	}

	if err := link.End(context.Background(), IndexRunStats{}, context.Canceled); err != nil {
		t.Fatalf("the stopped scanner could not report: %v", err)
	}
	last := ended()
	failure, _ := last.data.(api.WorkErrorEvent)
	if last.event != api.EventError || !strings.Contains(failure.Message, "context canceled") {
		t.Fatalf("the stopped index ended with %q %+v, want the scanner's own word", last.event, last.data)
	}
}

func TestAScannerToldToStopWhileItWaitsGivesUp(t *testing.T) {
	d, _ := newIndexedDaemon(t)
	client := serveScanRoutes(t, d)
	release := holdQueue(t, d)
	defer release()
	ctx, stop := context.WithCancel(context.Background())
	opened := openScan(t, ctx, client)
	waitUntil(t, "the scan waits in the queue", func() bool { return d.queued() == 1 })

	stop()

	select {
	case link := <-opened:
		if link != nil {
			t.Fatal("a scanner told to stop was given a turn")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a scanner told to stop kept waiting for its turn")
	}
}

// runningIndexThatHasEnded leaves the daemon with an index whose scan has
// ended and whose job has not left the queue yet.
func runningIndexThatHasEnded(t *testing.T, d *daemon) (session *scanSession, finish func()) {
	t.Helper()
	session = newScanSession()
	started, gate := make(chan struct{}), make(chan struct{})
	d.enqueue(&job{key: indexJobKey, stream: session.stream, scan: session, run: func(context.Context) error {
		close(started)
		<-gate
		return nil
	}})
	<-started
	session.end(IndexRunStats{}, errScannerGone)
	return session, func() { close(gate) }
}

func TestAnIndexRequestDoesNotJoinAnIndexThatHasEnded(t *testing.T) {
	// Restored after the daemon's work has ended, which reads it.
	previous := scanAttachTimeout
	scanAttachTimeout = 20 * time.Millisecond
	t.Cleanup(func() { scanAttachTimeout = previous })
	d := &daemon{}
	t.Cleanup(d.waitBackground)
	ended, finish := runningIndexThatHasEnded(t, d)
	defer finish()

	rr := httptest.NewRecorder()
	d.handleIndex(rr, httptest.NewRequest(http.MethodPost, "/index", nil))
	var resp api.OperationResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)

	// Its caller would follow an operation that is over and take the index
	// it asked for as done.
	if resp.OperationID == "" || resp.OperationID == ended.stream.OperationID() {
		t.Fatalf("the request was given operation %q, the one that has ended", resp.OperationID)
	}
}

func TestAResultForAnIndexThatHasEndedIsRefused(t *testing.T) {
	d := &daemon{}
	t.Cleanup(d.waitBackground)
	ended, finish := runningIndexThatHasEnded(t, d)
	defer finish()

	rr := httptest.NewRecorder()
	body := strings.NewReader(`{"type":"summary","files":3}`)
	d.handleScanEvent(rr, httptest.NewRequest(http.MethodPost, "/scan/event?operation_id="+ended.stream.OperationID(), body))

	// Answered 200, the scanner would exit as having succeeded while those
	// who follow the index were told it failed.
	if rr.Code != http.StatusConflict {
		t.Fatalf("a result for an index that had ended answered %d, want 409", rr.Code)
	}
}

// Index now is pressed in the instant a scan completes. systemd does not
// start a unit that is still ending, so a new index would wait for a scanner
// that never comes; the one that has just completed is what was asked for.
func TestAnIndexRequestFollowsAnIndexThatHasJustCompleted(t *testing.T) {
	d := &daemon{}
	t.Cleanup(d.waitBackground)
	session := newScanSession()
	started, gate := make(chan struct{}), make(chan struct{})
	defer close(gate)
	d.enqueue(&job{key: indexJobKey, stream: session.stream, scan: session, run: func(context.Context) error {
		close(started)
		<-gate
		return nil
	}})
	<-started
	session.end(IndexRunStats{Files: 3}, nil)

	rr := httptest.NewRecorder()
	d.handleIndex(rr, httptest.NewRequest(http.MethodPost, "/index", nil))
	var resp api.OperationResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.OperationID != session.stream.OperationID() || d.queued() != 0 {
		t.Fatalf("the request got operation %q with %d waiting, want the completed one, %q, and none", resp.OperationID, d.queued(), session.stream.OperationID())
	}
}
