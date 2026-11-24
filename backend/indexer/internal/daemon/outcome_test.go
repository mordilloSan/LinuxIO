package daemon

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/indexer/api"
)

func getOutcome(d *daemon, operationID string) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	d.handleOperation(rr, httptest.NewRequest(http.MethodGet, api.RouteOperation+"?operation_id="+url.QueryEscape(operationID), nil))
	return rr
}

func decodeOutcome(t *testing.T, rr *httptest.ResponseRecorder) api.OperationOutcome {
	t.Helper()
	if rr.Code != http.StatusOK {
		t.Fatalf("the outcome answered %d %q, want 200", rr.Code, rr.Body.String())
	}
	var outcome api.OperationOutcome
	if err := json.Unmarshal(rr.Body.Bytes(), &outcome); err != nil {
		t.Fatalf("decode the outcome: %v", err)
	}
	return outcome
}

// A reindex is often over before whoever asked for it has attached to its
// stream. What it did must still be there to ask for.
func TestAnOperationThatHasEndedAnswersForWhatItDid(t *testing.T) {
	d, _ := newIndexedDaemon(t)
	folder := filepath.Join(t.TempDir(), "folder")
	writeFile(t, filepath.Join(folder, "a.txt"))
	writeFile(t, filepath.Join(folder, "b.txt"))

	_, queued := postReindex(d, folder)
	waitUntil(t, "the reindex has ended", func() bool { return !d.busy() })

	outcome := decodeOutcome(t, getOutcome(d, queued.OperationID))
	if outcome.Event != api.EventComplete {
		t.Fatalf("the outcome is %q %s, want complete", outcome.Event, outcome.Data)
	}
	var complete api.WorkCompleteEvent
	if err := json.Unmarshal(outcome.Data, &complete); err != nil {
		t.Fatalf("decode the last event: %v", err)
	}
	if complete.OperationID != queued.OperationID || complete.Operation != "reindex" || complete.Path != folder || complete.FilesIndexed != 2 {
		t.Fatalf("the outcome is %+v, want the reindex of %s with its 2 files", complete, folder)
	}
}

func TestAnOperationThatFailedAnswersWithItsError(t *testing.T) {
	// Restored after the daemon's work has ended, which reads it.
	previous := scanAttachTimeout
	scanAttachTimeout = 20 * time.Millisecond
	t.Cleanup(func() { scanAttachTimeout = previous })
	d, _ := newIndexedDaemon(t)

	// A full index whose scanner never comes.
	queued := d.enqueueScan()
	operationID := queued.stream.OperationID()
	waitUntil(t, "the index has ended", func() bool { return !d.busy() })

	outcome := decodeOutcome(t, getOutcome(d, operationID))
	if outcome.Event != api.EventError {
		t.Fatalf("the outcome is %q %s, want error", outcome.Event, outcome.Data)
	}
	var failed api.WorkErrorEvent
	if err := json.Unmarshal(outcome.Data, &failed); err != nil {
		t.Fatalf("decode the last event: %v", err)
	}
	if failed.OperationID != operationID || failed.Operation != "index" || failed.Message == "" {
		t.Fatalf("the outcome is %+v, want the index and why it failed", failed)
	}
}

// Taken out of the queue before its turn, it never ran: that is its outcome.
func TestAnOperationWithdrawnBeforeItsTurnAnswersWithItsError(t *testing.T) {
	d, _ := newIndexedDaemon(t)
	release := holdQueue(t, d)
	defer release()
	queued := d.enqueueScan()
	operationID := queued.stream.OperationID()

	if !d.withdraw(queued, errors.New("the scanner left")) {
		t.Fatal("the waiting index was not withdrawn")
	}

	outcome := decodeOutcome(t, getOutcome(d, operationID))
	var failed api.WorkErrorEvent
	if err := json.Unmarshal(outcome.Data, &failed); err != nil {
		t.Fatalf("decode the last event: %v", err)
	}
	if outcome.Event != api.EventError || failed.Message != "the scanner left" {
		t.Fatalf("the outcome is %q %+v, want the error it was withdrawn with", outcome.Event, failed)
	}
}

// The daemon it is asked of may not be the one that ran the operation: a
// daemon that stopped and started again holds nothing of it.
func TestAnOperationTheDaemonDoesNotKnowIsNotFound(t *testing.T) {
	d, _ := newIndexedDaemon(t)
	if rr := getOutcome(d, "never-ran"); rr.Code != http.StatusNotFound {
		t.Fatalf("an unknown operation answered %d %q, want 404", rr.Code, rr.Body.String())
	}
	if rr := getOutcome(d, ""); rr.Code != http.StatusBadRequest {
		t.Fatalf("a request with no operation answered %d, want 400", rr.Code)
	}
}

func TestAnOperationThatStillRunsHasNoOutcome(t *testing.T) {
	d, _ := newIndexedDaemon(t)
	release := holdQueue(t, d)
	defer release()
	_, queued := postReindex(d, t.TempDir())

	if rr := getOutcome(d, queued.OperationID); rr.Code != http.StatusNotFound {
		t.Fatalf("a waiting operation answered %d %q for its outcome, want 404", rr.Code, rr.Body.String())
	}
}

func TestOnlyTheLatestOutcomesAreKept(t *testing.T) {
	var outcomes outcomeLog
	for i := range keptOutcomes + 3 {
		stream := newWorkStreamBroadcaster("reindex", fmt.Sprintf("operation-%d", i), "/srv")
		sendSSEError(stream, "failed")
		outcomes.keep(stream)
	}

	if _, found := outcomes.of("operation-2"); found {
		t.Fatal("an outcome older than the ones kept is still held")
	}
	for _, id := range []string{"operation-3", fmt.Sprintf("operation-%d", keptOutcomes+2)} {
		if _, found := outcomes.of(id); !found {
			t.Fatalf("the outcome of %s is not held", id)
		}
	}
}

// A stream that closes with no last event says nothing of how its work
// ended, and must not be read as work that completed.
func TestAnOperationThatEndedWithoutALastEventIsKeptAsFailed(t *testing.T) {
	var outcomes outcomeLog
	stream := newWorkStreamBroadcaster("reindex", "silent", "/srv")
	outcomes.keep(stream)

	outcome, found := outcomes.of("silent")
	if !found || outcome.event != api.EventError {
		t.Fatalf("the outcome is %q (found %v), want an error", outcome.event, found)
	}
}

// File names are bytes, and an error names the path it failed at.
func TestAnOutcomeThatNamesAPathThatIsNotUTF8IsAnsweredWhole(t *testing.T) {
	d, _ := newIndexedDaemon(t)
	stream := newWorkStreamBroadcaster("reindex", "latin-1", "/srv/caf\xe9")
	sendSSEError(stream, "cannot access /srv/caf\xe9: permission denied")
	d.outcomes.keep(stream)

	rr := getOutcome(d, "latin-1")
	if rr.Code != http.StatusOK {
		t.Fatalf("the outcome answered %d %q, want 200", rr.Code, rr.Body.String())
	}
	var outcome api.OperationOutcome
	if err := json.Unmarshal(rr.Body.Bytes(), &outcome, jsontext.AllowInvalidUTF8(true)); err != nil {
		t.Fatalf("decode the outcome: %v", err)
	}
	var failed api.WorkErrorEvent
	if err := json.Unmarshal(outcome.Data, &failed, jsontext.AllowInvalidUTF8(true)); err != nil {
		t.Fatalf("decode the last event: %v", err)
	}
	if outcome.Event != api.EventError || !strings.Contains(failed.Message, "permission denied") {
		t.Fatalf("the outcome is %q %+v, want the error with its message", outcome.Event, failed)
	}
}
