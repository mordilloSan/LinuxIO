package memory

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

// waits bounds how long a change waits for /proc/swaps, and how often it
// looks. zramOff is for zram's unit to take zram away. stopGrace is how long
// a unit may stay active after a stop before it is seen deactivating, how
// long systemd is given to see that a device left, and how often a start it
// did not act on is sent again. Taking a swap file or partition away has no
// bound (see swapOffDevice).
type waits struct {
	swapOn, zramOff, poll, stopGrace time.Duration
}

var hostWaits = waits{swapOn: time.Minute, zramOff: 10 * time.Minute, poll: 250 * time.Millisecond, stopGrace: 15 * time.Second}

var errWaitTimedOut = errors.New("timed out")

var (
	// errStopAbandoned is systemd giving up a stop while the device swaps on.
	errStopAbandoned = errors.New("systemd stopped waiting")
	// errStopEnded is a stop that ended without taking the device away.
	errStopEnded = errors.New("the stop ended without taking the device away")
	// errStopNotCarriedOut is a stop systemd never started to carry out.
	errStopNotCarriedOut = errors.New("systemd did not carry out the stop")
)

// waitUntil polls done until it reports true, it fails, ctx ends or bound
// passes.
func waitUntil(ctx context.Context, bound, every time.Duration, done func() (bool, error)) error {
	deadline := time.NewTimer(bound)
	defer deadline.Stop()
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		ok, err := done()
		if err != nil || ok {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("%w after %s", errWaitTimedOut, bound)
		case <-tick.C:
		}
	}
}

// waitStopped waits, within the zramOff bound, until gone reports that the
// device unit swaps on has left /proc/swaps. It ends early, with the device
// still listed, when the unit's state shows the stop is over:
//
//   - failed, or active again after it was seen deactivating: systemd gave
//     up, as it does when a swapoff, or a oneshot's stop command, outlives
//     the unit's timeout and is killed;
//   - inactive: the stop finished without taking the device away, as when
//     zramswap.service stops and a zram device another tool set up swaps on;
//   - active for stopGrace without ever deactivating: systemd did not carry
//     out the stop, as when it ends at once because the kernel refused it.
//
// The device is looked for once more before any of these, since a stop can
// remove it just before its unit changes state: a device that has left
// always counts as stopped. A state that cannot be read is waited for.
func (e *executor) waitStopped(ctx context.Context, unit, device string, gone func() (bool, error)) error {
	start := time.Now()
	deactivating := false
	return waitUntil(ctx, e.waits.zramOff, e.waits.poll, func() (bool, error) {
		ok, err := gone()
		if err != nil || ok {
			return ok, err
		}
		state, err := e.sys.activeState(ctx, unit)
		if err != nil {
			slog.Debug("read unit state failed", "unit", unit, "error", err)
			return false, nil
		}
		var over error
		switch {
		case state == "deactivating":
			deactivating = true
		case state == "failed", state == "active" && deactivating:
			over = fmt.Errorf("%w for %s to stop, which is now %s, and %s is still active", errStopAbandoned, unit, state, device)
		case state == "inactive":
			over = fmt.Errorf("%w: %s is inactive and %s is still active", errStopEnded, unit, device)
		case state == "active" && time.Since(start) >= e.waits.stopGrace:
			over = fmt.Errorf("%w: %s stayed active for %s without deactivating, and %s is still active", errStopNotCarriedOut, unit, e.waits.stopGrace, device)
		}
		if over == nil {
			return false, nil
		}
		if ok, err := gone(); err != nil || ok {
			return ok, err
		}
		return false, over
	})
}

// swapOffDevice takes a swap file or partition away by running swapoff
// itself and not by stopping its unit. systemd runs the same swapoff for a
// stop, but kills it after the unit's timeout, 90 seconds by default, while
// a full swap file on a slow disk takes minutes to read back; run from here
// it has as long as the disk needs, and a cancelled request ends it with
// the device still swapping. systemd sees in /proc/swaps that the device
// left and marks its unit inactive (swap_process_proc_swaps in its
// src/core/swap.c). A device that left counts as stopped whatever swapoff
// returned, as when the request ended in the moment it finished.
func (e *executor) swapOffDevice(ctx context.Context, device string) error {
	err := e.sys.swapOff(ctx, device)
	if err == nil {
		return nil
	}
	if gone, readErr := e.deviceIs(device, false)(); readErr == nil && gone {
		return nil
	}
	return fmt.Errorf("swapoff %s: %w", device, err)
}

// startUntilBack starts unit and waits, within the swapOn bound, for its
// device at path to swap again. systemd does nothing for a start of a unit
// it still holds active, as it may one whose device was taken away behind
// it a moment ago, so while the device is not back the start is sent again
// every stopGrace.
func (e *executor) startUntilBack(ctx context.Context, unit string, back func() (bool, error)) error {
	if err := e.sys.startUnit(ctx, unit); err != nil {
		return fmt.Errorf("start %s: %w", unit, err)
	}
	started := time.Now()
	err := waitUntil(ctx, e.waits.swapOn, e.waits.poll, func() (bool, error) {
		ok, err := back()
		if err != nil || ok {
			return ok, err
		}
		if time.Since(started) >= e.waits.stopGrace {
			started = time.Now()
			if err := e.sys.startUnit(ctx, unit); err != nil {
				slog.Debug("start unit again failed", "unit", unit, "error", err)
			}
		}
		return false, nil
	})
	if err != nil {
		return fmt.Errorf("%s did not bring it back into %s: %w", unit, procSwaps, err)
	}
	return nil
}

// waitUnitSeenOff waits, within stopGrace, until systemd no longer reports
// unit active or stopping, after its device was taken away behind it: a
// start of a unit systemd still holds active does nothing. A state that
// cannot be read is not waited for, and after the grace the caller goes on
// and finds out from /proc/swaps.
func (e *executor) waitUnitSeenOff(ctx context.Context, unit string) {
	err := waitUntil(ctx, e.waits.stopGrace, e.waits.poll, func() (bool, error) {
		state, err := e.sys.activeState(ctx, unit)
		if err != nil {
			slog.Debug("read unit state failed", "unit", unit, "error", err)
			return true, nil
		}
		return state != "active" && state != "deactivating", nil
	})
	if err != nil {
		slog.Debug("unit still reads active after its device left", "unit", unit, "error", err)
	}
}

// unitNotRunning reports whether systemd says unit is inactive or failed.
// A stop of such a unit does nothing, so the zram it should take away would
// stay and the wait would run to its bound; this happens for zram set up by
// another tool. A state that cannot be read does not hold the stop back.
func (e *executor) unitNotRunning(ctx context.Context, unit string) bool {
	state, err := e.sys.activeState(ctx, unit)
	if err != nil {
		slog.Debug("read unit state failed", "unit", unit, "error", err)
		return false
	}
	return state == "inactive" || state == "failed"
}

// mutationMu lets one memory change run at a time, across the three routes.
var mutationMu sync.Mutex

// lockMutation takes mutationMu, or refuses when another change holds it.
func lockMutation() (unlock func(), err error) {
	if !mutationMu.TryLock() {
		return nil, fmt.Errorf("%w: another memory change is running", bridgeipc.ErrConflict)
	}
	return mutationMu.Unlock, nil
}

// executor runs the memory changes against a host.
type executor struct {
	root   hostRoot
	sys    hostSystem
	report func(apischema.SwapProgress)
	waits  waits
}

func newExecutor(report func(apischema.SwapProgress)) *executor {
	return &executor{root: host, sys: liveSystem{root: host}, report: report, waits: hostWaits}
}

// swapView is the host a change is planned on: the status and the fstab
// swap entries matched to its active devices.
type swapView struct {
	status apischema.SwapStatus
	fstab  []fstabSwap
}

// statusAfterChangeTries is how many times a route reads the status after
// its change before it gives up.
const statusAfterChangeTries = 5

// statusAfterChange reads the status once a route's change is made. The
// route read it before the change, so a read that fails now races the change
// itself, as a zram device that leaves between the listing of /sys/block and
// the read of its files: it is tried again, waits.poll apart, until ctx
// ends. A read that still fails says that the change was made.
func (e *executor) statusAfterChange(ctx context.Context) (apischema.SwapStatus, error) {
	status, err := e.sys.status(ctx)
	for try := 1; err != nil && try < statusAfterChangeTries && ctx.Err() == nil; try++ {
		select {
		case <-ctx.Done():
		case <-time.After(e.waits.poll):
			status, err = e.sys.status(ctx)
		}
	}
	if err != nil {
		return apischema.SwapStatus{}, fmt.Errorf("the change was made, and the status after it could not be read: %w", err)
	}
	return status, nil
}

// readView reads the status and the fstab swap entries fresh.
func (e *executor) readView(ctx context.Context) (swapView, error) {
	status, err := e.sys.status(ctx)
	if err != nil {
		return swapView{}, err
	}
	fstab, err := e.root.fstabSwaps()
	if err != nil {
		return swapView{}, fmt.Errorf("read swap entries from fstab: %w", err)
	}
	return swapView{status: status, fstab: e.root.matchFstabSwaps(fstab, status.Devices)}, nil
}
