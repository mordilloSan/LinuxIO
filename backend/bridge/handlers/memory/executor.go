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
// looks. swapOff is long: stopping a swap unit runs swapoff, which reads
// every swapped page back and can take minutes on a full swap file.
// stopGrace is how long a unit may stay active after a stop before it is
// seen deactivating.
type waits struct {
	swapOn, swapOff, poll, stopGrace time.Duration
}

var hostWaits = waits{swapOn: time.Minute, swapOff: 10 * time.Minute, poll: 250 * time.Millisecond, stopGrace: 15 * time.Second}

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

// waitStopped waits, within the swapOff bound, until gone reports that the
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
	return waitUntil(ctx, e.waits.swapOff, e.waits.poll, func() (bool, error) {
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

// stopSwapUnit stops the unit of a swap file or partition holding used
// bytes and waits for device to leave /proc/swaps. Stopping a swap unit runs
// swapoff, which systemd kills after the unit's timeout, 90 seconds by
// default, while a full swap file on a slow disk takes minutes to read
// back. So for a device that holds data the unit's timeout is raised to
// this wait's own bound for the length of the stop, with a drop-in under
// /run, and put back afterwards, past a cancelled request. Putting it back
// while a swapoff still runs lets systemd end that swapoff at its usual
// timeout, which is what giving up on the stop means. A timeout that cannot
// be raised does not hold the stop back: the unit then stops within
// systemd's own.
func (e *executor) stopSwapUnit(ctx context.Context, unit, device string, used uint64, gone func() (bool, error)) error {
	if used > 0 {
		defer func() {
			restoreCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
			defer cancel()
			if err := e.sys.restoreStopTimeout(restoreCtx, unit); err != nil {
				slog.Warn("restore swap unit stop timeout failed", "unit", unit, "error", err)
			}
		}()
		if err := e.sys.extendStopTimeout(ctx, unit, e.waits.swapOff); err != nil {
			slog.Warn("raise swap unit stop timeout failed", "unit", unit, "error", err)
		}
	}
	if err := e.sys.stopUnit(ctx, unit); err != nil {
		return fmt.Errorf("stop %s: %w", unit, err)
	}
	if err := e.waitStopped(ctx, unit, device, gone); err != nil {
		return fmt.Errorf("wait for %s to leave %s: %w", device, procSwaps, err)
	}
	return nil
}

// unitNotRunning reports whether systemd says unit is inactive or failed.
// A stop of such a unit does nothing, so the device it should take away
// would stay and the wait would run to its bound; this happens for zram set
// up by another tool, or a swap file systemd tracks under another unit name.
// A state that cannot be read does not hold the stop back.
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
