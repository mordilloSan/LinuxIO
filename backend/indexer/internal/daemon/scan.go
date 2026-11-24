package daemon

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/indexer/api"
)

// The full index. The scanner is a process of its own, which systemd starts
// from the index timer or because someone asked for an index
// (linuxio-indexer-index.service). It does not scan on its own word: it opens
// a connection to the daemon and waits for its turn in the work queue, so
// that a scan and the daemon's own writes to the index never run together,
// and it holds that turn until it reports its result or its connection
// closes. The connection is the scan: a scanner that dies takes it along, and
// a daemon that stops closes it, which stops the scanner. The scanner's side
// is in scanlink.go.

// scanAttachTimeout is how long an index someone asked for waits, once its
// turn has come, for the scanner systemd was asked to start. A variable for
// tests.
var scanAttachTimeout = 30 * time.Second

var (
	errScannerGone       = errors.New("the scanner stopped without reporting a result")
	errScannerNotStarted = errors.New("the scanner did not start; see the journal of linuxio-indexer-index.service")
)

// scanSession is one full index, from the request for it or the scanner's
// arrival, whichever came first, to its end.
type scanSession struct {
	stream *workStreamBroadcaster
	// turn is closed when the index reaches the front of the queue.
	turn chan struct{}
	// attached is closed when a scanner has joined.
	attached chan struct{}
	// ended is closed when the scanner has reported its result or is gone.
	ended chan struct{}

	mu       sync.Mutex
	joined   bool
	finished bool
	stats    IndexRunStats
	err      error
}

func newScanSession() *scanSession {
	return &scanSession{
		stream:   newWorkStreamBroadcaster("index", newOperationID(), "/"),
		turn:     make(chan struct{}),
		attached: make(chan struct{}),
		ended:    make(chan struct{}),
	}
}

// attach gives the session its scanner. It reports false when it has one,
// and when the index has ended: a scanner told that its turn has come would
// then scan with nothing holding the queue for it.
func (s *scanSession) attach() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.joined || s.finished {
		return false
	}
	s.joined = true
	close(s.attached)
	return true
}

// end records how the scan ended. Only the first call counts, and is the
// one that reports true.
func (s *scanSession) end(stats IndexRunStats, err error) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.endLocked(stats, err)
}

func (s *scanSession) hasEnded() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.finished
}

func (s *scanSession) hasFailed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.finished && s.err != nil
}

// endUnlessAttached ends an index no scanner has joined. The wait for one
// can run out in the instant one attaches; that scanner then keeps its scan.
func (s *scanSession) endUnlessAttached(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.joined {
		s.endLocked(IndexRunStats{}, err)
	}
}

func (s *scanSession) endLocked(stats IndexRunStats, err error) bool {
	if s.finished {
		return false
	}
	s.finished = true
	s.stats, s.err = stats, err
	close(s.ended)
	return true
}

func (s *scanSession) result() (IndexRunStats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stats, s.err
}

// enqueueScan queues a full index, or returns the one that waits or runs.
func (d *daemon) enqueueScan() *job {
	session := newScanSession()
	queued, _ := d.enqueue(&job{key: indexJobKey, stream: session.stream, scan: session, run: func(ctx context.Context) error {
		d.runScan(ctx, session)
		return nil
	}})
	return queued
}

// handleIndex answers a request for a full index with the operation to
// follow. It does not start the scanner: its caller asks systemd for that
// next, and the scanner joins this operation. A request while an index waits
// or runs gets that one.
func (d *daemon) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "use POST", http.StatusMethodNotAllowed)
		return
	}
	queued := d.enqueueScan()
	if refused(queued) {
		http.Error(w, errShuttingDown.Error(), http.StatusServiceUnavailable)
		return
	}
	writeJSONStatus(w, http.StatusAccepted, api.OperationResponse{Status: api.StatusRunning, Path: "/", OperationID: queued.stream.OperationID()})
}

// runScan holds the queue for a full index: it waits for the scanner and
// then for its result, and reports both on the index's stream.
func (d *daemon) runScan(ctx context.Context, session *scanSession) {
	start := time.Now()
	stream := session.stream
	close(session.turn)
	sendSSEEvent(stream, api.EventStarted, api.WorkStartedEvent{
		Status:      api.StatusRunning,
		Operation:   "index",
		OperationID: stream.OperationID(),
		Path:        "/",
	})

	timer := time.NewTimer(scanAttachTimeout)
	defer timer.Stop()
	select {
	case <-session.attached:
	case <-session.ended:
	case <-timer.C:
		session.endUnlessAttached(errScannerNotStarted)
	case <-ctx.Done():
		session.end(IndexRunStats{}, ctx.Err())
	}
	select {
	case <-session.ended:
	case <-ctx.Done():
		session.end(IndexRunStats{}, ctx.Err())
	}

	stats, err := session.result()
	if err != nil {
		if ctx.Err() != nil {
			// An ordinary stop: nobody has to look at it.
			slog.Info("full index stopped with the daemon")
		} else {
			slog.Error("full index failed", "error", err)
		}
		sendSSEError(stream, fmt.Sprintf("index failed: %v", err))
		return
	}
	sendSSEEvent(stream, api.EventComplete, api.WorkCompleteEvent{
		Status:         api.StatusComplete,
		Operation:      "index",
		OperationID:    stream.OperationID(),
		Path:           "/",
		FilesIndexed:   stats.Files,
		DirsIndexed:    stats.Dirs,
		TotalSize:      stats.TotalSize,
		DeletedEntries: stats.DeletedEntries,
		DurationMs:     time.Since(start).Milliseconds(),
	})
}

// handleScan is the scanner's connection. The answer begins when the scan's
// turn has come, with the operation it scans for, and stays open for as long
// as the scan runs.
func (d *daemon) handleScan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "use POST", http.StatusMethodNotAllowed)
		return
	}
	queued := d.enqueueScan()
	if refused(queued) {
		http.Error(w, errShuttingDown.Error(), http.StatusServiceUnavailable)
		return
	}
	session := queued.scan
	if session == nil || !session.attach() {
		http.Error(w, "the index has a scanner already, or has just ended", http.StatusConflict)
		return
	}
	// From here the scan lasts as long as this request: however it returns,
	// an index that has no result by then has failed.
	defer session.end(IndexRunStats{}, errScannerGone)

	select {
	case <-session.turn:
	case <-session.ended:
		http.Error(w, errShuttingDown.Error(), http.StatusServiceUnavailable)
		return
	case <-r.Context().Done():
		// Gone before its turn. Left in the queue, the index would be
		// joined by every later request for one and refuse each scanner.
		session.end(IndexRunStats{}, errScannerGone)
		d.withdraw(queued, errScannerGone)
		return
	}
	if session.hasEnded() {
		// Ended in the instant its turn came, as when the daemon stops: a
		// scanner told to go ahead would scan with nothing holding the
		// queue for it.
		http.Error(w, errShuttingDown.Error(), http.StatusServiceUnavailable)
		return
	}
	// One line, which the scanner reads as its turn; the rest of the answer
	// never comes, and its end is the end of the scan.
	writeJSON(w, api.OperationResponse{Status: api.StatusRunning, Path: "/", OperationID: session.stream.OperationID()})
	if _, err := io.WriteString(w, "\n"); err != nil {
		slog.Warn("failed to tell the scanner its turn has come", "error", err)
		return
	}
	if err := http.NewResponseController(w).Flush(); err != nil {
		slog.Warn("failed to tell the scanner its turn has come", "error", err)
		return
	}

	select {
	case <-session.ended:
	case <-r.Context().Done():
	}
}

// runningScan returns the full index that holds the queue, or nil.
func (d *daemon) runningScan() *scanSession {
	q := &d.queue
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.running == nil {
		return nil
	}
	return q.running.scan
}

// handleScanEvent takes what the scanner reports of the scan that runs: its
// progress, and at the end its result or its error.
func (d *daemon) handleScanEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "use POST", http.StatusMethodNotAllowed)
		return
	}
	session := d.runningScan()
	operationID := strings.TrimSpace(r.URL.Query().Get("operation_id"))
	if session == nil || operationID == "" || operationID != session.stream.OperationID() {
		http.Error(w, "no scan runs for that operation", http.StatusConflict)
		return
	}
	var evt scanEvent
	if err := json.UnmarshalRead(http.MaxBytesReader(w, r.Body, maxRequestBodyBytes), &evt, json.RejectUnknownMembers(true)); err != nil {
		http.Error(w, fmt.Sprintf("invalid body: %v", err), http.StatusBadRequest)
		return
	}

	stream := session.stream
	switch evt.Type {
	case scanEventStep:
		// CurrentPath mirrors reindex's convention so path-displaying
		// clients show phases too.
		sendSSEEvent(stream, api.EventProgress, api.WorkProgressEvent{
			Operation:   "index",
			OperationID: operationID,
			Path:        "/",
			Message:     evt.Message,
			CurrentPath: evt.Message + "...",
		})
	case scanEventProgress:
		sendSSEEvent(stream, api.EventProgress, api.WorkProgressEvent{
			Operation:    "index",
			OperationID:  operationID,
			Path:         "/",
			Phase:        "scan",
			FilesIndexed: int64(evt.Files),
			DirsIndexed:  int64(evt.Dirs),
			BytesIndexed: int64(evt.Size),
		})
	case scanEventSummary, scanEventError:
		// A result for an index that has ended another way is refused, so
		// that the scanner does not exit as having succeeded while those
		// who follow the index were told it failed.
		var scanErr error
		if evt.Type == scanEventError {
			scanErr = errors.New(evt.Message)
		}
		ended := session.end(IndexRunStats{
			Dirs:           int64(evt.Dirs),
			Files:          int64(evt.Files),
			TotalSize:      int64(evt.Size),
			DeletedEntries: evt.DeletedEntries,
			SkippedDirs:    evt.SkippedDirs,
		}, scanErr)
		if !ended {
			http.Error(w, "the index has already ended", http.StatusConflict)
			return
		}
	default:
		http.Error(w, "unknown scan event", http.StatusBadRequest)
		return
	}
	writeJSON(w, api.OperationResponse{Status: api.StatusOK})
}
