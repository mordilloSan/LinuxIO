package collector

import (
	"context"
	"sync"
	"time"
)

// Run is one collection that concurrent callers can join. Started is set when
// the run is registered and never changes. The owner writes Value and Err, then
// Finish stamps At and closes Done; those three, except the owner-set seed in
// Value, are safe to read only after Done is closed.
type Run[T any] struct {
	done    chan struct{}
	Started time.Time // when the run was registered
	At      time.Time // when the run finished
	Value   T
	Err     error
}

// Done is closed once Finish has run.
func (r *Run[T]) Done() <-chan struct{} { return r.done }

// Active reports whether the run is still collecting.
func (r *Run[T]) Active() bool {
	select {
	case <-r.done:
		return false
	default:
		return true
	}
}

// Finish stamps the finish time and releases the waiters. The owner calls it
// exactly once, deferred, so a panic in the collection still releases them:
// net/http recovers per connection, and a run left open would block every
// later request for its key until each caller's context expired.
func (r *Run[T]) Finish() {
	r.At = time.Now()
	close(r.done)
}

// Fresh reports whether the run is still collecting, or finished without error
// and started less than window ago. The window runs from the start, not the
// finish, so a caller that returns one window later gets a sample of its own:
// counted from the finish, a caller polling at the window's own interval found
// the last sample one collection time short of the window and was handed it
// again, every other time. A run that took more than half the window stays
// fresh until it has rested as long as it took, so a slow collection is never
// followed at once by the next one.
func (r *Run[T]) Fresh(window time.Duration) bool {
	if r.Active() {
		return true
	}
	return r.Err == nil && time.Since(r.Started) < max(window, 2*r.At.Sub(r.Started))
}

// Flight keeps the newest Run per key so concurrent callers share one
// collection. The zero value is ready to use.
type Flight[K comparable, T any] struct {
	mu   sync.Mutex
	runs map[K]*Run[T]
}

// Begin registers a fresh run under key, replacing whatever was there. seed is
// the run's initial Value: it must carry whatever usable inspects, because the
// run is visible to other callers as soon as Begin returns.
func (f *Flight[K, T]) Begin(key K, seed T) *Run[T] {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.beginLocked(key, seed)
}

func (f *Flight[K, T]) beginLocked(key K, seed T) *Run[T] {
	if f.runs == nil {
		f.runs = make(map[K]*Run[T])
	}
	run := &Run[T]{done: make(chan struct{}), Started: time.Now(), Value: seed}
	f.runs[key] = run
	return run
}

// End unregisters run when it is still the one registered under key.
func (f *Flight[K, T]) End(key K, run *Run[T]) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.runs[key] == run {
		delete(f.runs, key)
	}
}

// Current returns the run registered under key, or nil.
func (f *Flight[K, T]) Current(key K) *Run[T] {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.runs[key]
}

// Join returns the run serving key: the registered one when usable accepts it,
// otherwise a new run seeded with seed that the caller owns (owned is true) and
// must collect.
func (f *Flight[K, T]) Join(key K, seed T, usable func(*Run[T]) bool) (run *Run[T], owned bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if run := f.runs[key]; run != nil && usable(run) {
		return run, false
	}
	return f.beginLocked(key, seed), true
}

// Do returns a finished, error-free run for key. It joins a usable run,
// otherwise it calls collect to fill a run of its own; collect needs no Finish
// call. When a joined run fails the caller retries with a collection of its own.
func (f *Flight[K, T]) Do(ctx context.Context, key K, seed T, usable func(*Run[T]) bool, collect func(*Run[T])) (*Run[T], error) {
	for {
		run, owned := f.Join(key, seed, usable)
		if owned {
			func() {
				defer run.Finish()
				collect(run)
			}()
			if run.Err != nil {
				return nil, run.Err
			}
			return run, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-run.Done():
		}
		if run.Err == nil {
			return run, nil
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
}
