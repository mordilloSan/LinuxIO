package memory

import (
	"context"
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
	// A unit held off at boot would not start its device again: refuse
	// before any device stops. The status found LinuxIO's drop-ins and the
	// masks it can see on disk; systemd is asked about masks as well.
	for _, target := range targets {
		state, err := e.sys.unitFileState(ctx, target.unit)
		if err != nil {
			return apischema.SwapStatus{}, err
		}
		if strings.HasPrefix(state, "masked") {
			return apischema.SwapStatus{}, fmt.Errorf("%w: %s is masked, so %s could not be started again", bridgeipc.ErrConflict, target.unit, target.device)
		}
	}
	var drained []string
	for i, target := range targets {
		if err := ctx.Err(); err != nil {
			return apischema.SwapStatus{}, err
		}
		e.report(apischema.SwapProgress{Step: apischema.SwapStepDrainDevice, Index: i + 1, Total: len(targets), Device: target.device})
		done, err := e.drainDevice(ctx, target, len(drained))
		if err != nil {
			return apischema.SwapStatus{}, err
		}
		if done {
			drained = append(drained, target.device)
		}
	}
	// A drain whose devices all left on their own changed nothing.
	if len(drained) > 0 {
		slog.Info("swap drained", "devices", drained)
	}
	return e.statusAfterChange(ctx)
}

// drainTargets are the managed fstab entries active as a disk swap device
// that holds data, in fstab order.
func drainTargets(view swapView) []fstabSwap {
	return holdingData(view.fstab, view.status.Devices)
}

// holdsData reports whether the fstab entry is active as a device that
// holds data.
func holdsData(entry fstabSwap, devices []apischema.SwapDevice) bool {
	return slices.ContainsFunc(devices, func(d apischema.SwapDevice) bool { return d.Path == entry.device && d.Used > 0 })
}

// holdingData returns the fstab entries a drain stops and starts: those
// active as a device that holds data, less the ones others can redirect.
func holdingData(fstab []fstabSwap, devices []apischema.SwapDevice) []fstabSwap {
	var entries []fstabSwap
	for _, entry := range fstab {
		if !entry.othersCanWrite && holdsData(entry, devices) {
			entries = append(entries, entry)
		}
	}
	return entries
}

// leftOutHoldsData reports whether an entry a drain leaves out, since
// others can redirect it, holds data.
func leftOutHoldsData(fstab []fstabSwap, devices []apischema.SwapDevice) bool {
	return slices.ContainsFunc(fstab, func(e fstabSwap) bool { return e.othersCanWrite && holdsData(e, devices) })
}

// drainDevice takes one device away with swapoff when its data fits in
// memory on a fresh status, then starts its unit again and waits for the
// device to return. drained counts the devices drained before it. It reports
// false for a device that already left /proc/swaps, which it skips.
func (e *executor) drainDevice(ctx context.Context, target fstabSwap, drained int) (bool, error) {
	status, _, err := e.sys.status(ctx)
	if err != nil {
		return false, err
	}
	i := slices.IndexFunc(status.Devices, func(d apischema.SwapDevice) bool { return d.Path == target.device })
	if i < 0 {
		slog.Debug("skipped swap device that left before its drain", "device", target.device)
		return false, nil
	}
	if !fitsInMemory(status.Devices[i].Used, status.MemAvailable) {
		return false, stayDrained(fmt.Errorf("%w: not enough free memory to take back the data on %s", bridgeipc.ErrConflict, target.device), drained)
	}
	// A swapoff that fails or is cancelled leaves the device swapping.
	if err := e.swapOffDevice(ctx, target.device); err != nil {
		return false, stayDrained(err, drained)
	}
	// The device is off: bringing it back is not cancelled with the request.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout+e.waits.stopGrace+e.waits.swapOn)
	defer cancel()
	e.waitUnitSeenOff(ctx, target.unit)
	if err := e.startUntilBack(ctx, target.unit, e.deviceIs(target.device, true)); err != nil {
		return false, fmt.Errorf("%s is off: %w", target.device, err)
	}
	return true, nil
}

// stayDrained adds to a refusal at one device that the drained devices
// before it stay drained.
func stayDrained(err error, drained int) error {
	if drained > 0 {
		return fmt.Errorf("%w; the %d devices drained before it stay drained", err, drained)
	}
	return err
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
		return "the parked data is on swap devices /etc/fstab does not name, or names without a unit LinuxIO can use, which could not be started again"
	case apischema.SwapDrainUnitHeldOff:
		return "a swap device holding parked data is held off at boot, so it could not be started again"
	case apischema.SwapDrainOthersCanWrite:
		return "the parked data is on swap LinuxIO does not manage: " + errOthersCanRedirect.Error()
	default:
		return "drain is blocked: " + string(reason)
	}
}
