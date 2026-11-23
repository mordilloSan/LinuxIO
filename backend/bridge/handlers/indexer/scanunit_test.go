package indexer

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
)

// No test of this package starts a unit of the host: a full index asks a
// function that tests replace.
func TestMain(m *testing.M) {
	startScanUnit = func(context.Context, string) error { return nil }
	os.Exit(m.Run())
}

func recordStartedUnits(t *testing.T, startErr error) *[]string {
	t.Helper()
	original := startScanUnit
	var units []string
	startScanUnit = func(_ context.Context, unit string) error {
		units = append(units, unit)
		return startErr
	}
	t.Cleanup(func() { startScanUnit = original })
	return &units
}

func completeAtOnce(t *testing.T) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		flusher := requireFlusher(t, w)
		w.Header().Set("Content-Type", "text/event-stream")
		mustWrite(t, w, "event: complete\ndata: {\"status\":\"complete\",\"operation\":\"index\",\"operation_id\":\"test-operation\",\"path\":\"/\"}\n\n")
		flusher.Flush()
	}
}

func TestAFullIndexAsksSystemdForTheScanner(t *testing.T) {
	srv := newTwoStepServer(t, http.StatusAccepted, completeAtOnce(t))
	defer srv.Close()
	overrideClient(t, srv)
	units := recordStartedUnits(t, nil)

	if err := StreamIndexer(context.Background(), "/", IndexerCallbacks{}); err != nil {
		t.Fatalf("StreamIndexer: %v", err)
	}
	if want := []string{"linuxio-indexer-index.service"}; !slices.Equal(*units, want) {
		t.Fatalf("units started = %v, want %v", *units, want)
	}
}

func TestAReindexOfAFolderStartsNoUnit(t *testing.T) {
	srv := newTwoStepServer(t, http.StatusAccepted, func(w http.ResponseWriter, _ *http.Request) {
		flusher := requireFlusher(t, w)
		w.Header().Set("Content-Type", "text/event-stream")
		mustWrite(t, w, "event: complete\ndata: {\"status\":\"complete\",\"operation\":\"reindex\",\"operation_id\":\"test-operation\",\"path\":\"/srv\"}\n\n")
		flusher.Flush()
	})
	defer srv.Close()
	overrideClient(t, srv)
	units := recordStartedUnits(t, nil)

	if err := StreamIndexer(context.Background(), "/srv", IndexerCallbacks{}); err != nil {
		t.Fatalf("StreamIndexer: %v", err)
	}
	if len(*units) != 0 {
		t.Fatalf("a reindex of a folder started %v: the daemon does that work itself", *units)
	}
}

func TestAFullIndexWhoseScannerCannotBeStartedFails(t *testing.T) {
	srv := newTwoStepServer(t, http.StatusAccepted, completeAtOnce(t))
	defer srv.Close()
	overrideClient(t, srv)
	recordStartedUnits(t, errors.New("unit is masked"))
	var reported string

	err := StreamIndexer(context.Background(), "/", IndexerCallbacks{OnError: func(msg string, _ int) error {
		reported = msg
		return nil
	}})

	if err == nil || !strings.Contains(err.Error(), "unit is masked") {
		t.Fatalf("StreamIndexer error = %v, want the start failure", err)
	}
	if !strings.Contains(reported, "scanner") {
		t.Fatalf("the caller was told %q, want that the scanner could not be started", reported)
	}
}

// The scanner is started before the daemon is asked for the operation: an
// index reserved first would hold the daemon's queue for a scanner that
// never comes when the start fails.
func TestAFullIndexWhoseScannerCannotBeStartedReservesNothing(t *testing.T) {
	asked := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = true
		w.WriteHeader(http.StatusAccepted)
		mustWrite(t, w, `{"status":"running","operation_id":"test-operation"}`)
	}))
	defer srv.Close()
	overrideClient(t, srv)
	recordStartedUnits(t, errors.New("unit is masked"))

	if err := StreamIndexer(context.Background(), "/", IndexerCallbacks{}); err == nil {
		t.Fatal("StreamIndexer succeeded although the scanner could not be started")
	}
	if asked {
		t.Fatal("the daemon was asked for an index whose scanner could not be started")
	}
}

// A stream that breaks while its operation still waits in the daemon's queue
// is attached to again: the operation has not run yet.
func TestAStreamThatBreaksWhileItsOperationWaitsIsAttachedToAgain(t *testing.T) {
	attached := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/reindex":
			w.WriteHeader(http.StatusAccepted)
			mustWrite(t, w, `{"status":"running","path":"/srv/data","operation_id":"mine"}`)
		case r.Method == http.MethodGet && r.URL.Query().Get("stream") == "true":
			attached++
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			if attached > 1 {
				mustWrite(t, w, "event: complete\ndata: {\"status\":\"complete\",\"operation\":\"reindex\",\"operation_id\":\"mine\",\"path\":\"/srv/data\",\"files_indexed\":7}\n\n")
			}
		case r.Method == http.MethodGet && r.URL.Path == "/status":
			w.Header().Set("Content-Type", "application/json")
			mustWrite(t, w, `{"status":"running","active_operation":"index","active_operation_id":"scan","active_path":"/","num_files":4,`+
				`"waiting_operations":[{"operation":"reindex","operation_id":"mine","path":"/srv/data"}]}`)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer srv.Close()
	overrideClient(t, srv)

	var got IndexerResult
	err := StreamIndexer(context.Background(), "/srv/data", IndexerCallbacks{
		OnResult: func(result IndexerResult) error { got = result; return nil },
	})
	if err != nil {
		t.Fatalf("StreamIndexer: %v", err)
	}
	if attached != 2 || got.FilesIndexed != 7 {
		t.Fatalf("attached %d times with result %+v, want a second attach and the operation's own result", attached, got)
	}
}

// The daemon reindexes folders of its own accord, such as one a refresh
// found new. A page asking whether its own operation is in progress is told
// there is none, not that the identities conflict.
func TestAttachWhileOtherWorkRunsFindsNoOperationOfItsOwn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mustWrite(t, w, `{"status":"running","active_operation":"reindex","active_operation_id":"background","active_path":"/home/u/new"}`)
	}))
	defer srv.Close()
	overrideClient(t, srv)
	var reported string

	err := StreamIndexerAttach(context.Background(), "/", IndexerCallbacks{OnError: func(msg string, _ int) error {
		reported = msg
		return nil
	}})

	if err == nil || reported != "no active indexer operation" {
		t.Fatalf("attach = %v, reported %q; want no active indexer operation", err, reported)
	}
}
