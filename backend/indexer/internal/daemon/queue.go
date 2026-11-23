package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"slices"
	"strings"
	"sync"

	"github.com/mordilloSan/LinuxIO/backend/indexer/api"
)

// The work queue. One piece of work runs at a time: a full index, the reindex
// of a folder or the update of one entry. A request that arrives while another
// runs waits for its turn instead of being refused, so an update the bridge
// sends during a scan or right behind another update is applied when the work
// ahead of it ends. Nothing is kept across a restart: an update the daemon
// held when it stopped is made good by the next full index.

// indexJobKey is the key of a full index. A second one is refused while the
// first waits or runs.
const indexJobKey = "index"

type job struct {
	// key collapses repeats: a job joins one that is still waiting under the
	// same key, so many updates to one path are one running and one waiting.
	// Work with no key never joins another.
	key string
	// ctx is the context the work runs under; nil is the daemon's own.
	ctx context.Context
	run func(ctx context.Context) error
	// stream is what a client attaches to for progress; nil for an entry update.
	stream *workStreamBroadcaster
	// done is closed when the work has run; err is set before that.
	done chan struct{}
	err  error
	// behind is set when the work is queued: other work ran or waited ahead
	// of it, so its caller does not wait for it.
	behind bool
	// deferred is set by an entry update that handed its path to a reindex
	// of its own: the index is not up to date for it yet.
	deferred bool
	// paths are the paths the work is about: the entry, the folder, both
	// ends of a move. A full index has none; it is about all of them.
	paths []string
	// scan is the full index the job holds the queue for; nil for other work.
	scan *scanSession
	// covers is the folder a reindex walks: its work is about every path
	// below it, whichever they turn out to be.
	covers string
	// source is the path whose rows a move renames. sourceBusy is set when
	// the move is queued: other work about a path at or below source ran or
	// waited then, so the rows there may not be what the disk held when the
	// folder moved.
	source     string
	sourceBusy bool
}

type workQueue struct {
	mu      sync.Mutex
	waiting []*job
	// byKey finds the waiting job of a key without a walk of the queue,
	// which grows to one job a file while a long scan holds it.
	byKey    map[string]*job
	running  *job
	draining bool
	// closed refuses new work: the daemon is waiting for what runs to end.
	closed bool
}

// errShuttingDown answers work that arrives after the daemon began to stop.
var errShuttingDown = errors.New("indexer is shutting down")

// enqueue adds j behind the work that waits and returns the job its caller
// follows: j itself, or with joined the one already waiting (for a full index,
// also the one running) under the same key. Once the daemon is stopping, j is
// answered with errShuttingDown and never runs.
func (d *daemon) enqueue(j *job) (queued *job, joined bool) {
	q := &d.queue
	q.mu.Lock()
	defer q.mu.Unlock()
	if j.key != "" {
		if j.key == indexJobKey && q.running != nil && q.running.key == indexJobKey && joinable(q.running) {
			return q.running, true
		}
		if waiting := q.byKey[j.key]; waiting != nil && joinable(waiting) {
			return waiting, true
		}
	}
	j.done = make(chan struct{})
	if q.closed {
		j.err = errShuttingDown
		close(j.done)
		return j, false
	}
	j.behind = q.running != nil || len(q.waiting) > 0
	if j.source != "" {
		j.sourceBusy = q.touches(j.source)
	}
	q.waiting = append(q.waiting, j)
	if j.key != "" {
		if q.byKey == nil {
			q.byKey = make(map[string]*job)
		}
		q.byKey[j.key] = j
	}
	if !q.draining {
		q.draining = true
		// Under the lock, so that it is ordered before closeQueue: Close never
		// waits on a group this could still add to.
		d.goBackground(d.drain)
	}
	return j, false
}

// joinable reports whether a request may follow j. A full index whose scan
// has failed is in its last lines: whoever joined it would follow an
// operation that is over and could take the index it asked for as done. One
// that has just completed is what was asked for, and is followed: systemd
// does not start the scan unit again while it is still ending, so a new
// index would wait for a scanner that never comes.
func joinable(j *job) bool {
	return j.scan == nil || !j.scan.hasFailed()
}

// indexWorkQueued reports whether a full index or a reindex runs or waits:
// the work that has a stream to follow. The updates of file operations and
// the refreshes of opened folders do not count.
func (d *daemon) indexWorkQueued() bool {
	q := &d.queue
	q.mu.Lock()
	defer q.mu.Unlock()
	streamed := func(j *job) bool { return j != nil && j.stream != nil }
	return streamed(q.running) || slices.ContainsFunc(q.waiting, streamed)
}

// withdraw takes work that still waits out of the queue and ends it with
// err. It reports false when the work has started or is over.
func (d *daemon) withdraw(j *job, err error) bool {
	q := &d.queue
	q.mu.Lock()
	i := slices.Index(q.waiting, j)
	if i < 0 {
		q.mu.Unlock()
		return false
	}
	q.waiting = slices.Delete(q.waiting, i, i+1)
	if q.byKey[j.key] == j {
		delete(q.byKey, j.key)
	}
	j.err = err
	if j.stream != nil {
		// Kept before the queue is let go of: an operation is never seen
		// neither waiting nor ended.
		sendSSEError(j.stream, err.Error())
		d.outcomes.keep(j.stream)
	}
	q.mu.Unlock()

	if j.stream != nil {
		j.stream.close()
	}
	close(j.done)
	return true
}

// touches reports whether work that runs or waits is about a path at or
// below folder. The caller holds the lock.
func (q *workQueue) touches(folder string) bool {
	about := func(j *job) bool {
		return j != nil && slices.ContainsFunc(j.paths, func(path string) bool { return pathWithin(path, folder) })
	}
	return about(q.running) || slices.ContainsFunc(q.waiting, about)
}

// sizePending reports whether work that runs or waits can change what is
// counted below folder: work about a path at or below it, or the reindex of
// a folder it is in. A full index is not counted: it would make every size
// pending for as long as a scan runs.
func (d *daemon) sizePending(folder string) bool {
	q := &d.queue
	q.mu.Lock()
	defer q.mu.Unlock()
	covered := func(j *job) bool { return j != nil && j.covers != "" && pathWithin(folder, j.covers) }
	return q.touches(folder) || covered(q.running) || slices.ContainsFunc(q.waiting, covered)
}

// pathWithin reports whether path is folder or below it.
func pathWithin(path, folder string) bool {
	return folder == "/" || path == folder || strings.HasPrefix(path, folder+"/")
}

// closeQueue stops new work from being queued. Close calls it before it waits
// for the work that runs.
func (d *daemon) closeQueue() {
	d.queue.mu.Lock()
	d.queue.closed = true
	d.queue.mu.Unlock()
}

// drain runs the waiting work in the order it arrived and ends when none is
// left. After a shutdown it runs nothing more: what still waits is answered
// with the context's error.
func (d *daemon) drain() {
	q := &d.queue
	for {
		q.mu.Lock()
		if len(q.waiting) == 0 {
			q.draining = false
			q.mu.Unlock()
			return
		}
		j := q.waiting[0]
		q.waiting = q.waiting[1:]
		if q.byKey[j.key] == j {
			delete(q.byKey, j.key)
		}
		q.running = j
		// Published with the move, so the status never shows the daemon busy
		// with nothing: a client would take its operation for ended.
		if j.stream != nil {
			d.setWorkStreamBroadcaster(j.stream)
		}
		q.mu.Unlock()

		ctx := j.ctx
		if ctx == nil {
			ctx = d.workContext()
		}
		if err := ctx.Err(); err != nil {
			j.err = err
			if j.scan != nil {
				// Its scanner waits for a turn that will not come.
				j.scan.end(IndexRunStats{}, err)
			}
		} else {
			j.err = runJob(ctx, j)
		}
		switch {
		case j.err == nil:
		case j.stream != nil:
			// Without a last event a client takes a stream that closed for
			// work that is done.
			sendSSEError(j.stream, j.err.Error())
		case errors.Is(j.err, context.Canceled):
			// The daemon is stopping: the next full index makes it good.
		case j.behind:
			// Nobody waited for this update's answer.
			slog.Warn("queued index update failed", "key", j.key, "error", j.err)
		}
		if j.stream != nil {
			d.endWorkStream(j.stream)
		}
		// The end of the work is published before the daemon stops being
		// busy: the idle watcher must never see a free daemon with an old
		// activity time.
		d.markActivity()
		q.mu.Lock()
		q.running = nil
		q.mu.Unlock()
		close(j.done)
	}
}

// busy reports whether work runs or waits. It pins the daemon against the idle
// exit and is what /status calls running.
func (d *daemon) busy() bool {
	q := &d.queue
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.running != nil || len(q.waiting) > 0
}

// runJob runs the work of j. A panic in it becomes its error: one broken
// update must not take the daemon and the rest of the queue with it.
func runJob(ctx context.Context, j *job) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			slog.Error("index work panicked", "key", j.key, "panic", recovered, "stack", string(debug.Stack()))
			err = fmt.Errorf("index work panicked: %v", recovered)
		}
	}()
	return j.run(ctx)
}

// refused reports whether the daemon turned j away because it is stopping.
func refused(j *job) bool {
	select {
	case <-j.done:
		return errors.Is(j.err, errShuttingDown)
	default:
		return false
	}
}

// streamOf returns the progress stream of the operation with that ID, running
// or waiting, or nil.
func (d *daemon) streamOf(operationID string) *workStreamBroadcaster {
	if stream := d.queuedStream(func(stream *workStreamBroadcaster) bool { return stream.OperationID() == operationID }); stream != nil {
		return stream
	}
	if active := d.getWorkStreamBroadcaster(); active != nil && active.OperationID() == operationID {
		return active
	}
	return nil
}

// queuedStream returns the first stream of running or waiting work that match
// accepts, or nil.
func (d *daemon) queuedStream(match func(*workStreamBroadcaster) bool) *workStreamBroadcaster {
	q := &d.queue
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.running != nil && q.running.stream != nil && match(q.running.stream) {
		return q.running.stream
	}
	for _, waiting := range q.waiting {
		if waiting.stream != nil && match(waiting.stream) {
			return waiting.stream
		}
	}
	return nil
}

// waitingOperations names the index and reindex operations that wait, in the
// order they will run.
func (d *daemon) waitingOperations() []api.WaitingOperation {
	q := &d.queue
	q.mu.Lock()
	defer q.mu.Unlock()
	var operations []api.WaitingOperation
	for _, waiting := range q.waiting {
		if stream := waiting.stream; stream != nil {
			operations = append(operations, api.WaitingOperation{Operation: stream.Operation(), OperationID: stream.OperationID(), Path: stream.Path()})
		}
	}
	return operations
}

// waits reports whether stream belongs to work that has not started yet.
func (d *daemon) waits(stream *workStreamBroadcaster) bool {
	q := &d.queue
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, waiting := range q.waiting {
		if waiting.stream == stream {
			return true
		}
	}
	return false
}

// workContext is the context work runs under: the daemon's, which ends at
// shutdown.
func (d *daemon) workContext() context.Context {
	if d.workCtx != nil {
		return d.workCtx
	}
	return context.Background()
}
