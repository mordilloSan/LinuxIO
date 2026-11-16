package memory

import (
	"fmt"
	"slices"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

// swapStep is one step of a mode change.
type swapStep struct {
	code apischema.SwapStep
	// run is, for stop_zram, whether zram is stopped as well as turned off
	// at boot.
	run bool
	// unit and path are the swap file's unit and path for stop_swap_file and
	// start_swap_file.
	unit, path string
}

// managedSwapFile is the swap file fstab names, the only one with a unit a
// change can stop, mask or start.
func (v swapView) managedSwapFile() (fstabSwap, bool) {
	return fstabSwapFile(v.fstab)
}

func hasDevice(devices []apischema.SwapDevice, kind apischema.SwapDeviceKind) bool {
	return slices.ContainsFunc(devices, func(d apischema.SwapDevice) bool { return d.Kind == kind })
}

// planSwapMode orders the steps that take the host in view to the target
// mode, each verified before the next, so a failure leaves the host no less
// safe than before: swap is added before any is taken away. It refuses a
// mode the host cannot take before anything changes.
func planSwapMode(v swapView, target apischema.SwapMode) ([]swapStep, error) {
	if _, ok := presetFor(target); !ok {
		return nil, fmt.Errorf("%w: unknown swap mode %q", bridgeipc.ErrInvalidArgs, target)
	}
	zramRunning := hasDevice(v.status.Devices, apischema.SwapDeviceZram)
	file, managed := v.managedSwapFile()
	if target == apischema.SwapModeZram {
		if reason := v.status.ZramUnavailableReason; reason != "" {
			return nil, fmt.Errorf("%w: %s", bridgeipc.ErrConflict, zramRefusal(reason))
		}
		steps := []swapStep{
			{code: apischema.SwapStepWriteConfig},
			{code: apischema.SwapStepStartZram},
			{code: apischema.SwapStepApplySysctls},
			{code: apischema.SwapStepDisableZswap},
		}
		if managed {
			steps = append(steps, swapStep{code: apischema.SwapStepStopSwapFile, unit: file.unit, path: file.path})
		}
		return steps, nil
	}

	// Disk swap the mode can count on: a swap file fstab lets it start, or
	// an active swap file or partition.
	if !managed && !hasDevice(v.status.Devices, apischema.SwapDeviceFile) && !hasDevice(v.status.Devices, apischema.SwapDevicePartition) {
		return nil, fmt.Errorf("%w: %s needs a swap file or an active swap partition", bridgeipc.ErrConflict, target)
	}
	if target == apischema.SwapModeZswap && !v.status.Zswap.Available {
		return nil, fmt.Errorf("%w: the kernel has no zswap", bridgeipc.ErrConflict)
	}
	var steps []swapStep
	if managed && file.device == "" {
		steps = append(steps, swapStep{code: apischema.SwapStepStartSwapFile, unit: file.unit, path: file.path})
	}
	// zram is stopped through its provider's unit, so the step is planned
	// only with a provider installed; without one nothing here can stop
	// zram. The executor also leaves zram when systemd does not report that
	// unit running, as when another tool set zram up.
	if v.status.ZramAvailable {
		steps = append(steps, swapStep{code: apischema.SwapStepStopZram, run: zramRunning})
	}
	steps = append(steps, swapStep{code: apischema.SwapStepApplySysctls})
	if target == apischema.SwapModeZswap {
		return append(steps, swapStep{code: apischema.SwapStepEnableZswap}), nil
	}
	return append(steps, swapStep{code: apischema.SwapStepDisableZswap}), nil
}

// zramRefusal says why zram cannot be chosen.
func zramRefusal(reason apischema.ZramUnavailableReason) string {
	switch reason {
	case apischema.ZramPackageMissing:
		return "zram needs zram-tools or zram-generator, and neither is installed"
	case apischema.ZramSwapPartitionActive:
		return "zram cannot be chosen while a swap partition is active"
	default:
		return "zram cannot be chosen: " + string(reason)
	}
}
