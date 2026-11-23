package daemon

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// A scanner that hangs in a call the kernel does not return from, on a mount
// whose server went away or a disk that is failing, would hold the daemon's
// queue for as long as it lives: no update of the index runs behind it, and
// the timer cannot start another scan while its unit is active. The scanner
// therefore watches itself. Elapsed time says nothing, since a scan at idle
// priority on a busy disk takes hours; movement does, since even then it
// visits an entry now and again.
var (
	// scanStallLimit is how long a scan may go without looking at an entry or
	// starting a step before it counts as stuck. The steps after the walk
	// have no counter, so it is also the longest any of them may take.
	scanStallLimit = 30 * time.Minute
	// scanStallCheck is how often the scanner looks.
	scanStallCheck = time.Minute
	// scanStallGrace is how long a scan told to stop has to unwind before
	// the scanner reports without it.
	scanStallGrace = 10 * time.Second
)

// errScanStalled ends a scan that stopped moving.
var errScanStalled = errors.New("the scan made no progress")

// stallWatch passes a scan's progress on to the daemon and notes when the
// scan last moved: a step that begins, counters that have grown, or a walk
// that has looked at more entries. The last one counts the entries that were
// not indexed too, so a walk through a folder whose entries all fail, each
// after a long wait on a failing disk, is moving. The sampler reports the
// numbers of a walk that stands still as well, so a report alone is not
// movement.
type stallWatch struct {
	next  IndexProgress
	limit time.Duration
	check time.Duration

	mu                sync.Mutex
	moved             time.Time
	step              string
	dirs, files, size uint64
	looked            uint64
}

func newStallWatch(next IndexProgress) *stallWatch {
	return &stallWatch{next: next, limit: scanStallLimit, check: scanStallCheck, moved: time.Now()}
}

func (w *stallWatch) Step(message string) {
	w.mu.Lock()
	w.step = message
	w.moved = time.Now()
	w.mu.Unlock()
	w.next.Step(message)
}

func (w *stallWatch) ScanProgress(dirs, files, size uint64) {
	w.mu.Lock()
	if dirs != w.dirs || files != w.files || size != w.size {
		w.dirs, w.files, w.size = dirs, files, size
		w.moved = time.Now()
	}
	w.mu.Unlock()
	w.next.ScanProgress(dirs, files, size)
}

// Looked takes the walk's own count (walkWatcher). The daemon is not told.
func (w *stallWatch) Looked(entries uint64) {
	w.mu.Lock()
	if entries != w.looked {
		w.looked = entries
		w.moved = time.Now()
	}
	w.mu.Unlock()
}

// stalled returns an error that says so when the scan has not moved for
// longer than the limit.
func (w *stallWatch) stalled() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if time.Since(w.moved) <= w.limit {
		return nil
	}
	step := w.step
	if step == "" {
		step = "starting"
	}
	return fmt.Errorf("%w for %s during %q; it was stopped", errScanStalled, w.limit, step)
}

// run cancels the scan with the stall as its cause once it has stopped
// moving. It ends with ctx.
func (w *stallWatch) run(ctx context.Context, cancel context.CancelCauseFunc) {
	ticker := time.NewTicker(w.check)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := w.stalled(); err != nil {
				cancel(err)
				return
			}
		}
	}
}
