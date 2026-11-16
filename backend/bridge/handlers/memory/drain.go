package memory

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

// DrainSwap brings the data parked on disk swap back into memory by
// stopping and starting, one at a time, each active disk swap device
// /etc/fstab names. zram is never drained: its data is already in memory.
func DrainSwap(ctx context.Context, report func(apischema.SwapProgress)) (apischema.SwapStatus, error) {
	return newExecutor(report).drainSwap(ctx)
}

func (e *executor) drainSwap(ctx context.Context) (apischema.SwapStatus, error) {
	unlock, err := lockMutation()
	if err != nil {
		return apischema.SwapStatus{}, err
	}
	defer unlock()
	view, err := e.readView(ctx)
	if err != nil {
		return apischema.SwapStatus{}, err
	}
	if reason := view.status.DrainBlockedReason; reason != "" {
		return apischema.SwapStatus{}, fmt.Errorf("%w: %s", bridgeipc.ErrConflict, drainRefusal(reason))
	}
	targets := drainTargets(view)
	// A masked unit could not start its device again: refuse before any
	// device stops. The status found the masks it can see on disk; systemd
	// is asked as well.
	for _, target := range targets {
		state, err := e.sys.unitFileState(ctx, target.unit)
		if err != nil {
			return apischema.SwapStatus{}, err
		}
		if strings.HasPrefix(state, "masked") {
			return apischema.SwapStatus{}, fmt.Errorf("%w: %s is masked, so %s could not be started again", bridgeipc.ErrConflict, target.unit, target.device)
		}
	}
	for i, target := range targets {
		if err := ctx.Err(); err != nil {
			return apischema.SwapStatus{}, err
		}
		e.report(apischema.SwapProgress{Step: apischema.SwapStepDrainDevice, Index: i + 1, Total: len(targets), Device: target.device})
		if err := e.drainDevice(ctx, target, i); err != nil {
			return apischema.SwapStatus{}, err
		}
	}
	devices := make([]string, 0, len(targets))
	for _, target := range targets {
		devices = append(devices, target.device)
	}
	slog.Info("swap drained", "devices", devices)
	return e.sys.status(ctx)
}

// drainTargets are the fstab entries active as a disk swap device that
// holds data, in fstab order.
func drainTargets(view swapView) []fstabSwap {
	return holdingData(view.fstab, view.status.Devices)
}

// holdingData returns the fstab entries active as a device that holds data.
func holdingData(fstab []fstabSwap, devices []apischema.SwapDevice) []fstabSwap {
	var entries []fstabSwap
	for _, entry := range fstab {
		if slices.ContainsFunc(devices, func(d apischema.SwapDevice) bool { return d.Path == entry.device && d.Used > 0 }) {
			entries = append(entries, entry)
		}
	}
	return entries
}

// drainDevice stops one device's unit when its data fits in memory on a
// fresh status, waits for it to leave /proc/swaps, then starts it again and
// waits for it to return. drained counts the devices drained before it.
func (e *executor) drainDevice(ctx context.Context, target fstabSwap, drained int) error {
	status, err := e.sys.status(ctx)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(status.Devices, func(d apischema.SwapDevice) bool { return d.Path == target.device })
	if i < 0 {
		return nil
	}
	if !fitsInMemory(status.Devices[i].Used, status.MemAvailable) {
		return stayDrained(fmt.Errorf("%w: not enough free memory to take back the data on %s", bridgeipc.ErrConflict, target.device), drained)
	}
	// A stop that failed or was not waited out may still take effect: a
	// D-Bus call cancelled after systemd took the job still stops the unit,
	// and a swapoff goes on after the wait gives up. A start queued behind
	// it brings the device back, and does nothing to a unit still active.
	// A swap file's fstab unit has the file's own name, so systemd reports it
	// running while the file swaps; one it does not, as for a file fstab
	// names through a symlink, would not stop it. A partition's unit is an
	// alias named after its /dev/disk/by-* link, and its state is left to the
	// wait that follows.
	if isSwapFilePath(target.path) && e.unitNotRunning(ctx, target.unit) {
		return stayDrained(fmt.Errorf("%w: systemd does not report %s active, so %s cannot be stopped and started again", bridgeipc.ErrConflict, target.unit, target.device), drained)
	}
	if err := e.stopSwapUnit(ctx, target.unit, target.device, status.Devices[i].Used, e.deviceIs(target.device, false)); err != nil {
		return e.startAfterFailedStop(ctx, target, err)
	}
	// The device is off: bringing it back is not cancelled with the request.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout+e.waits.swapOn)
	defer cancel()
	if err := e.sys.startUnit(ctx, target.unit); err != nil {
		return fmt.Errorf("%s is off: start %s: %w", target.device, target.unit, err)
	}
	if err := waitUntil(ctx, e.waits.swapOn, e.waits.poll, e.deviceIs(target.device, true)); err != nil {
		return fmt.Errorf("%s is off: %s did not bring it back into %s: %w", target.device, target.unit, procSwaps, err)
	}
	return nil
}

// stayDrained adds to a refusal at one device that the drained devices
// before it stay drained.
func stayDrained(err error, drained int) error {
	if drained > 0 {
		return fmt.Errorf("%w; the %d devices drained before it stay drained", err, drained)
	}
	return err
}

// startAfterFailedStop asks systemd to start the unit again past a cancelled
// request, without waiting for the device, and returns the stop's error with
// what became of the start: that it was queued, or why it was not.
func (e *executor) startAfterFailedStop(ctx context.Context, target fstabSwap, stopErr error) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
	defer cancel()
	if err := e.sys.startUnit(ctx, target.unit); err != nil {
		return errors.Join(stopErr, fmt.Errorf("%s may be off: start %s: %w", target.device, target.unit, err))
	}
	return fmt.Errorf("%w; the stop may still take effect, so a start was queued", stopErr)
}

// deviceIs returns a check that the /proc/swaps path is, or is not, active.
func (e *executor) deviceIs(path string, active bool) func() (bool, error) {
	return func() (bool, error) {
		devices, err := e.root.readSwaps()
		if err != nil {
			return false, err
		}
		on := slices.ContainsFunc(devices, func(d apischema.SwapDevice) bool { return d.Path == path })
		return on == active, nil
	}
}

// drainRefusal says why a drain cannot start.
func drainRefusal(reason apischema.SwapDrainBlockedReason) string {
	switch reason {
	case apischema.SwapDrainNothingParked:
		return "no data is parked on disk swap"
	case apischema.SwapDrainNotEnoughMemory:
		return "there is not enough free memory to take back the data parked on disk swap"
	case apischema.SwapDrainUnmanagedDevice:
		return "the parked data is on swap devices /etc/fstab does not name, which could not be started again"
	case apischema.SwapDrainUnitMasked:
		return "a swap device holding parked data has a masked unit, so it could not be started again"
	default:
		return "drain is blocked: " + string(reason)
	}
}
