package daemon

import (
	"context"
	"encoding/json/v2"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/indexer/api"
	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/indexing"
)

// shortStallLimit makes a scan count as stuck after a moment instead of half
// an hour. It is restored after the daemon's work, which reads it, has ended:
// call it before the daemon is made.
func shortStallLimit(t *testing.T) {
	t.Helper()
	limit, check, grace := scanStallLimit, scanStallCheck, scanStallGrace
	scanStallLimit, scanStallCheck, scanStallGrace = 500*time.Millisecond, 10*time.Millisecond, 50*time.Millisecond
	t.Cleanup(func() { scanStallLimit, scanStallCheck, scanStallGrace = limit, check, grace })
}

// runScanFor runs a scanner whose scan is scan against d, and returns how
// the scanner ended and how the daemon recorded the index.
func runScanFor(t *testing.T, d *daemon, scan scanFunc) (api.OperationOutcome, error) {
	t.Helper()
	client := serveScanRoutes(t, d)
	operationID := d.enqueueScan().stream.OperationID()

	ended := make(chan error, 1)
	go func() {
		_, err := runScanWith(context.Background(), client, scan)
		ended <- err
	}()
	var err error
	select {
	case err = <-ended:
	case <-time.After(10 * time.Second):
		t.Fatal("the scanner did not end")
	}
	waitUntil(t, "the index has ended", func() bool { return !d.busy() })
	return decodeOutcome(t, getOutcome(d, operationID)), err
}

func outcomeMessage(t *testing.T, outcome api.OperationOutcome) string {
	t.Helper()
	var failed api.WorkErrorEvent
	if err := json.Unmarshal(outcome.Data, &failed); err != nil {
		t.Fatalf("decode the last event: %v", err)
	}
	return failed.Message
}

// A scanner stuck in a call that never returns holds the queue, and with it
// every update of the index, for as long as it lives.
func TestAScanThatStopsMovingGivesUpItsTurnAndSaysWhy(t *testing.T) {
	shortStallLimit(t)
	d, _ := newIndexedDaemon(t)
	stuck := make(chan struct{})
	t.Cleanup(func() { close(stuck) })

	outcome, err := runScanFor(t, d, func(_ context.Context, progress IndexProgress) (IndexRunStats, error) {
		progress.Step("Scanning filesystem")
		// A call the kernel does not return from: it does not end with the
		// context either.
		<-stuck
		return IndexRunStats{}, nil
	})

	if !errors.Is(err, errScanStalled) {
		t.Fatalf("the scanner ended with %v, want errScanStalled", err)
	}
	if outcome.Event != api.EventError {
		t.Fatalf("the index ended as %q %s, want an error", outcome.Event, outcome.Data)
	}
	if message := outcomeMessage(t, outcome); !strings.Contains(message, "no progress") || !strings.Contains(message, "Scanning filesystem") {
		t.Fatalf("the index failed with %q, want the stall and the step it was in", message)
	}
}

// The sampler keeps reporting the counters of a walk that stands still:
// reports are not movement, counters that grow are.
func TestAScanWhoseCountersStandStillIsStuck(t *testing.T) {
	shortStallLimit(t)
	d, _ := newIndexedDaemon(t)

	_, err := runScanFor(t, d, func(ctx context.Context, progress IndexProgress) (IndexRunStats, error) {
		progress.Step("Scanning filesystem")
		for ctx.Err() == nil {
			progress.ScanProgress(4, 20, 4096)
			time.Sleep(5 * time.Millisecond)
		}
		// It unwinds when told to stop, as a walk between two calls does.
		return IndexRunStats{}, ctx.Err()
	})

	if !errors.Is(err, errScanStalled) {
		t.Fatalf("the scanner ended with %v, want errScanStalled", err)
	}
}

// A scan on a slow disk takes hours and is not stuck: it is never ended for
// being long.
func TestAScanThatKeepsMovingIsNotEnded(t *testing.T) {
	shortStallLimit(t)
	d, _ := newIndexedDaemon(t)

	outcome, err := runScanFor(t, d, func(ctx context.Context, progress IndexProgress) (IndexRunStats, error) {
		progress.Step("Scanning filesystem")
		// Twice the limit, a little further each time.
		for visited := uint64(1); visited <= 20; visited++ {
			if ctx.Err() != nil {
				return IndexRunStats{}, ctx.Err()
			}
			progress.ScanProgress(1, visited, visited*4096)
			time.Sleep(scanStallLimit / 10)
		}
		// The steps after the walk have no counters: a new step is movement.
		progress.Step("Updating folder sizes")
		time.Sleep(scanStallLimit / 5)
		progress.Step("Checkpointing database")
		time.Sleep(scanStallLimit / 5)
		return IndexRunStats{Files: 20, Dirs: 1}, nil
	})

	if err != nil {
		t.Fatalf("a scan that kept moving ended with %v", err)
	}
	var complete api.WorkCompleteEvent
	if decodeErr := json.Unmarshal(outcome.Data, &complete); decodeErr != nil {
		t.Fatalf("decode the last event: %v", decodeErr)
	}
	if outcome.Event != api.EventComplete || complete.FilesIndexed != 20 {
		t.Fatalf("the index ended as %q %+v, want complete with its 20 files", outcome.Event, complete)
	}
}

// A scan that failed for a reason of its own keeps that reason.
func TestAScanThatFailsReportsItsOwnError(t *testing.T) {
	shortStallLimit(t)
	d, _ := newIndexedDaemon(t)

	outcome, err := runScanFor(t, d, func(context.Context, IndexProgress) (IndexRunStats, error) {
		return IndexRunStats{}, errors.New("open database: disk full")
	})

	if err == nil || errors.Is(err, errScanStalled) {
		t.Fatalf("the scanner ended with %v, want its own error", err)
	}
	if message := outcomeMessage(t, outcome); !strings.Contains(message, "disk full") {
		t.Fatalf("the index failed with %q, want the scan's own error", message)
	}
}

// systemd stops the unit, or the daemon goes away, while the scan hangs: the
// scanner must still end, or its unit stays active and no scan starts again.
func TestAStuckScanThatIsToldToStopDoesNotKeepTheScannerAlive(t *testing.T) {
	shortStallLimit(t)
	d, _ := newIndexedDaemon(t)
	client := serveScanRoutes(t, d)
	d.enqueueScan()
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	stuck, started := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() { close(stuck) })

	ended := make(chan error, 1)
	go func() {
		_, err := runScanWith(ctx, client, func(_ context.Context, progress IndexProgress) (IndexRunStats, error) {
			progress.Step("Scanning filesystem")
			close(started)
			<-stuck
			return IndexRunStats{}, nil
		})
		ended <- err
	}()
	<-started
	stop()

	select {
	case err := <-ended:
		if !errors.Is(err, errScanStalled) {
			t.Fatalf("the scanner ended with %v, want errScanStalled", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("a scanner told to stop waited for a scan that does not return")
	}
	waitUntil(t, "the index has ended", func() bool { return !d.busy() })
}

// A dying disk answers every entry with an error after a long wait. Nothing
// is indexed and the counters stand still, but the walk is getting through
// the folder and is not stuck.
func TestAWalkThatOnlyFindsEntriesItCannotReadIsNotStuck(t *testing.T) {
	shortStallLimit(t)
	d, _ := newIndexedDaemon(t)

	_, err := runScanFor(t, d, func(ctx context.Context, progress IndexProgress) (IndexRunStats, error) {
		progress.Step("Scanning filesystem")
		walk, ok := progress.(walkWatcher)
		if !ok {
			t.Error("the scanner's progress does not take the walk's own count")
			return IndexRunStats{}, nil
		}
		for looked := uint64(1); looked <= 20; looked++ {
			if ctx.Err() != nil {
				return IndexRunStats{}, ctx.Err()
			}
			progress.ScanProgress(1, 0, 0)
			walk.Looked(looked)
			time.Sleep(scanStallLimit / 10)
		}
		return IndexRunStats{Dirs: 1}, nil
	})

	if err != nil {
		t.Fatalf("a walk that kept looking at entries ended with %v", err)
	}
}

type walkRecorder struct {
	looked []uint64
}

func (*walkRecorder) Step(string)                 {}
func (*walkRecorder) ScanProgress(_, _, _ uint64) {}
func (r *walkRecorder) Looked(entries uint64)     { r.looked = append(r.looked, entries) }

func TestTheScanReportsWhatTheWalkLookedAt(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root+"/a.txt")
	index := indexing.Initialize("/")
	index.EnableStreaming(discardWriter{})
	recorder := &walkRecorder{}

	stop := startScanProgress(recorder, index)
	if err := index.StartIndexingFromPath(context.Background(), root); err != nil {
		t.Fatalf("scan: %v", err)
	}
	stop()

	if len(recorder.looked) == 0 || recorder.looked[len(recorder.looked)-1] != 1 {
		t.Fatalf("the walk's count was reported as %v, want it to end at 1", recorder.looked)
	}
}

type discardWriter struct{}

func (discardWriter) Write(indexing.IndexEntry) error { return nil }

type noProgress struct{}

func (noProgress) Step(string)                 {}
func (noProgress) ScanProgress(_, _, _ uint64) {}

// longAgo makes the watch take the scan's last movement for older than its
// limit.
func longAgo(w *stallWatch) {
	w.mu.Lock()
	w.moved = time.Now().Add(-2 * w.limit)
	w.mu.Unlock()
}

// What counts as movement, without a clock: a step that begins, numbers
// that changed. The same numbers again are a report and nothing more.
func TestWhatTheWatchTakesForMovement(t *testing.T) {
	watch := newStallWatch(noProgress{})
	watch.ScanProgress(4, 20, 4096)
	watch.Looked(30)
	moves := map[string]func(){
		"a step that begins":            func() { watch.Step("Updating folder sizes") },
		"counters that grew":            func() { watch.ScanProgress(4, 21, 8192) },
		"a walk that looked further on": func() { watch.Looked(31) },
	}
	for name, move := range moves {
		longAgo(watch)
		if watch.stalled() == nil {
			t.Fatalf("%s: a scan that has not moved for twice the limit is not taken for stuck", name)
		}
		move()
		if err := watch.stalled(); err != nil {
			t.Fatalf("%s was not taken for movement: %v", name, err)
		}
	}

	longAgo(watch)
	watch.ScanProgress(4, 21, 8192)
	watch.Looked(31)
	if watch.stalled() == nil {
		t.Fatal("a report of the same numbers was taken for movement")
	}
}
