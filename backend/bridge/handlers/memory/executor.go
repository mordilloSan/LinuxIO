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
// looks. swapOn is for a device to appear, zramOff for zram's unit to take
// zram away. stopGrace is how long zram's unit may stay active after a stop
// before it is seen deactivating. Taking a swap file away has no bound (see
// swapOffDevice).
type waits struct {
	swapOn, zramOff, poll, stopGrace time.Duration
	// zramReset is how long a reset of zram0 is tried again while something
	// still holds the device, as udev does for a moment after a swapoff.
	zramReset time.Duration
}

var hostWaits = waits{swapOn: time.Minute, zramOff: 10 * time.Minute, poll: 250 * time.Millisecond, stopGrace: 15 * time.Second, zramReset: zramResetWait}

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

// mutationMu lets one memory change run at a time, across the routes.
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

// swapView is the host a change is planned on: the status, the swap files
// /etc/fstab names that it was derived from and, with zram-generator as the
// provider, the generator's files root could not read.
type swapView struct {
	status              apischema.SwapStatus
	fstab               []fstabSwap
	generatorUnreadable []unreadableFile
}

// statusAfterChangeTries is how many times a route reads the status after
// its change before it gives up.
const statusAfterChangeTries = 5

// statusAfterChange reads the status once a route's change is made. The
// route read it before the change, so a read that fails now most likely met
// files under /proc and /sys while the change still moved them (a zram device
// that leaves is skipped by readZramDevices itself): it is tried again,
// waits.poll apart, until ctx ends. A read that still fails says that the
// change was made.
func (e *executor) statusAfterChange(ctx context.Context) (apischema.SwapStatus, error) {
	status, _, err := e.sys.status(ctx)
	for try := 1; err != nil && try < statusAfterChangeTries && ctx.Err() == nil; try++ {
		select {
		case <-ctx.Done():
		case <-time.After(e.waits.poll):
			status, _, err = e.sys.status(ctx)
		}
	}
	if err != nil {
		return apischema.SwapStatus{}, fmt.Errorf("the change was made, and the status after it could not be read: %w", err)
	}
	return status, nil
}

// readView reads the status fresh. Its fstab swap files are the ones that
// status was derived from, so the options a change turns a file on with come
// from the reading of /etc/fstab its rows were decided on.
func (e *executor) readView(ctx context.Context) (swapView, error) {
	status, fstab, err := e.sys.status(ctx)
	if err != nil {
		return swapView{}, err
	}
	view := swapView{status: status, fstab: fstab}
	if status.ZramProvider == apischema.ZramProviderGenerator {
		_, view.generatorUnreadable = e.root.readZramGeneratorDevices()
	}
	return view, nil
}
