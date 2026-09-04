package app

import (
	"context"
	"errors"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/system"
)

// liveReuseWindow bounds how often a live key collects. Requests inside the
// window share the newest sample; captured_at reports that sample's time.
const liveReuseWindow = time.Second

// liveRun is the newest collection for one live sample key. It is published
// before the collection starts so concurrent callers can join it; its result
// fields are written by the owning caller and only read after done is closed.
type liveRun struct {
	done              chan struct{}
	includeDetails    bool
	includeContainers bool
	capturedAt        time.Time
	data              *system.CombinedData
	err               error
}

// covers reports whether this run's scope satisfies a request's scope.
func (r *liveRun) covers(includeDetails, includeContainers bool) bool {
	return (r.includeDetails || !includeDetails) && (r.includeContainers || !includeContainers)
}

// usableFor reports whether a request can join or reuse this run: its scope
// covers the request and it is either still in flight or finished without
// error inside liveReuseWindow.
func (r *liveRun) usableFor(includeDetails, includeContainers bool) bool {
	if !r.covers(includeDetails, includeContainers) {
		return false
	}
	select {
	case <-r.done:
		return r.err == nil && time.Since(r.capturedAt) < liveReuseWindow
	default:
		return true
	}
}

// liveRunFor returns the run serving key, creating one when no usable run
// exists. owned is true for the caller that must run the collection.
func (a *App) liveRunFor(key uint16, includeDetails, includeContainers bool) (_ *liveRun, owned bool) {
	a.liveMu.Lock()
	defer a.liveMu.Unlock()

	if a.liveRuns == nil {
		a.liveRuns = map[uint16]*liveRun{}
	}
	if run := a.liveRuns[key]; run != nil && run.usableFor(includeDetails, includeContainers) {
		return run, false
	}
	run := &liveRun{done: make(chan struct{}), includeDetails: includeDetails, includeContainers: includeContainers}
	a.liveRuns[key] = run
	return run, true
}

// liveCurrentData returns a live sample for key. It reuses a finished sample
// under liveReuseWindow old, joins an in-flight collection, and otherwise
// collects. The returned time is that sample's capture time, which callers
// report as captured_at. It never consults awaitCollectorSample: only the
// plugin, all and summary routes take that handoff, App.Live does not.
func (a *App) liveCurrentData(ctx context.Context, key uint16, includeDetails, includeContainers bool) (*system.CombinedData, time.Time, error) {
	for {
		run, owned := a.liveRunFor(key, includeDetails, includeContainers)
		if owned {
			a.runLiveCollection(ctx, run, key, includeDetails, includeContainers)
			return run.data, run.capturedAt, run.err
		}
		select {
		case <-ctx.Done():
			return nil, time.Time{}, ctx.Err()
		case <-run.done:
		}
		if run.err == nil {
			return run.data, run.capturedAt, nil
		}
		// The shared collection failed; retry with a collection of our own.
	}
}

// runLiveCollection collects into run with liveMu released and publishes the
// result. Publication is deferred so a panic in the collection still closes
// done: net/http recovers per connection, and a run left unpublished would
// block every later request for its key until each caller's context expired.
func (a *App) runLiveCollection(ctx context.Context, run *liveRun, key uint16, includeDetails, includeContainers bool) {
	defer func() {
		if run.data == nil && run.err == nil {
			run.err = errors.New("live collection did not complete")
		}
		a.detachLiveSample(run.data)
		run.capturedAt = time.Now()
		close(run.done)
	}()

	collect := a.collectLive
	if collect == nil {
		collect = a.collectLiveCurrentData
	}
	run.data, run.err = collect(ctx, key, includeDetails, includeContainers)
}

// detachLiveSample replaces the manager-owned pointers in a sample with
// pointers to value copies. Reuse can serve a sample up to liveReuseWindow
// after capture, and both the docker container stats and the filesystem stats
// are rewritten in place by the next collection for any key; without this a
// reused sample would report another collection's values under its own
// captured_at. Both structs hold only value fields, so a shallow copy is a
// full one. No caller holds a.Lock when the publication defer runs, so it
// retakes that lock: another key's collection writes the same structs under it,
// and copying them unlocked would race that write.
func (a *App) detachLiveSample(data *system.CombinedData) {
	if data == nil {
		return
	}
	a.Lock()
	defer a.Unlock()
	for i, item := range data.Containers {
		if item != nil {
			copied := *item
			data.Containers[i] = &copied
		}
	}
	for key, stats := range data.Stats.ExtraFs {
		if stats != nil {
			copied := *stats
			data.Stats.ExtraFs[key] = &copied
		}
	}
}
