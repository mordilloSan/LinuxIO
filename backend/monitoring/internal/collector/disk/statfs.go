package disk

import (
	"context"
	"errors"
	"sync"
	"time"

	psutilDisk "github.com/shirou/gopsutil/v4/disk"
)

var (
	// ErrStatfsSlow is a statfs this collection started that did not return
	// within its wait; it goes on running and a later collection serves it.
	ErrStatfsSlow = errors.New("statfs did not return in time")
	// ErrStatfsPending is a statfs an earlier collection started that has not
	// returned yet; it is not waited for again.
	ErrStatfsPending = errors.New("statfs still running")
)

// StatfsFunc reads the usage of the filesystem mounted at mountpoint.
type StatfsFunc func(ctx context.Context, mountpoint string) (*psutilDisk.UsageStat, error)

// StatfsCalls runs each mount's statfs on a goroutine of its own. statfs takes
// no deadline, and on a dead hard-mounted share it blocks until the server
// answers, so a collection waits a bounded time for it and goes on without
// it; at most one call runs per mount, and its goroutine exits when the call
// returns. The zero value is ready to use.
type StatfsCalls struct {
	mu    sync.Mutex
	calls map[string]*statfsCall
}

type statfsCall struct {
	done  chan struct{} // closed once usage, err and at are set
	usage *psutilDisk.UsageStat
	err   error
	at    time.Time
}

// Usage returns the mount's usage and when statfs returned it, from a call
// this one starts and waits up to wait for (ErrStatfsSlow when it does not
// return), or from one an earlier call started that has returned since, unless
// that answer is older than maxAge; a caller sets maxAge above the time
// between its collections, or it never sees a late answer. A call still
// running is not waited for again (ErrStatfsPending) and starts no second one.
func (c *StatfsCalls) Usage(ctx context.Context, mountpoint string, wait, maxAge time.Duration, statfs StatfsFunc) (*psutilDisk.UsageStat, time.Time, error) {
	c.mu.Lock()
	call := c.calls[mountpoint]
	if call != nil {
		select {
		case <-call.done:
			delete(c.calls, mountpoint)
			if time.Since(call.at) <= maxAge {
				c.mu.Unlock()
				return call.usage, call.at, call.err
			}
		default:
			c.mu.Unlock()
			return nil, time.Time{}, ErrStatfsPending
		}
	}
	call = &statfsCall{done: make(chan struct{})}
	if c.calls == nil {
		c.calls = make(map[string]*statfsCall)
	}
	c.calls[mountpoint] = call
	// The call outlives this collection when the mount does not answer, so it
	// does not take the collection's cancellation; statfs ignores it anyway.
	go func(ctx context.Context) {
		call.usage, call.err = statfs(ctx, mountpoint)
		call.at = time.Now()
		close(call.done)
	}(context.WithoutCancel(ctx))
	c.mu.Unlock()

	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-call.done:
	case <-timer.C:
		return nil, time.Time{}, ErrStatfsSlow
	case <-ctx.Done():
		return nil, time.Time{}, ctx.Err()
	}
	c.mu.Lock()
	if c.calls[mountpoint] == call {
		delete(c.calls, mountpoint)
	}
	c.mu.Unlock()
	return call.usage, call.at, call.err
}

// Forget drops the finished calls of mounts that keep reports as gone; a call
// still running stays until it returns and a later Forget drops it.
func (c *StatfsCalls) Forget(keep func(mountpoint string) bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for mountpoint, call := range c.calls {
		if keep(mountpoint) {
			continue
		}
		select {
		case <-call.done:
			delete(c.calls, mountpoint)
		default:
		}
	}
}
