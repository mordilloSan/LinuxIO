package memory

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

// DrainSwap brings the data parked on disk swap back into memory by turning
// off and on again, one at a time, each swap file LinuxIO can remove that
// holds data. zram is never drained: its data is already in memory. A
// partition, an LVM volume and a locked swap file are never turned off.
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
	targets := drainTargets(view.status)
	// fstab options swapon would not take would leave a file off once it is
	// turned off, so they are refused before any is.
	for _, path := range targets {
		if _, err := swapOnFlags(path, fstabOptions(view.fstab, path)); err != nil {
			return apischema.SwapStatus{}, err
		}
	}
	var drained []string
	for i, path := range targets {
		if err := ctx.Err(); err != nil {
			return apischema.SwapStatus{}, err
		}
		e.report(apischema.SwapProgress{Step: apischema.SwapStepDrainDevice, Index: i + 1, Total: len(targets), Device: path})
		done, err := e.drainFile(ctx, path, fstabOptions(view.fstab, path), len(drained))
		if err != nil {
			return apischema.SwapStatus{}, err
		}
		if done {
			drained = append(drained, path)
		}
	}
	// A drain whose files all left on their own changed nothing.
	if len(drained) > 0 {
		slog.Info("swap drained", "devices", drained)
	}
	return e.statusAfterChange(ctx)
}

// drainTargets are the swap files a drain turns off and on: the active ones
// that pass the strict check and hold data, less a temporary one a resize
// left, which is removed and not drained, in the order of the status.
func drainTargets(status apischema.SwapStatus) []string {
	var paths []string
	for _, row := range status.SwapFiles {
		if row.Active && row.LockedReason == "" && !row.Temporary && row.Used > 0 {
			paths = append(paths, row.Path)
		}
	}
	return paths
}

// drainFile turns one swap file off, after the room check on a fresh
// status, and on again with options, the options of its fstab line. drained
// counts the files drained before it. It reports false for a file that
// already left /proc/swaps, which it skips.
func (e *executor) drainFile(ctx context.Context, path, options string, drained int) (bool, error) {
	// A swapoff that fails or is cancelled leaves the file swapping.
	off, err := e.turnOff(ctx, path)
	if err != nil {
		return false, stayDrained(err, drained)
	}
	if !off {
		slog.Debug("skipped swap file that left before its drain", "path", path)
		return false, nil
	}
	// The file is off: bringing it back is not cancelled with the request.
	ctx, cancel := e.cleanupContext(ctx)
	defer cancel()
	if err := e.turnOn(ctx, path, options); err != nil {
		return false, fmt.Errorf("%s is off: %w", path, err)
	}
	return true, nil
}

// stayDrained adds to a refusal at one file that the files drained before
// it stay drained.
func stayDrained(err error, drained int) error {
	if drained > 0 {
		return fmt.Errorf("%w; the %d swap files drained before it stay drained", err, drained)
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
		return "there is not enough free memory and free swap to take back the data parked on the swap files"
	case apischema.SwapDrainOtherSwap:
		return "the parked data is on swap LinuxIO does not turn off: a partition, an LVM volume or a swap file it does not change"
	default:
		return "drain is blocked: " + string(reason)
	}
}
