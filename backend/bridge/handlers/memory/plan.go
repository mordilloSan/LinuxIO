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

// managedSwapFile is the one swap file fstab names that a change stops,
// holds off at boot or starts (see fstabSwapFile). One others can redirect is returned
// as not managed.
func (v swapView) managedSwapFile() (fstabSwap, bool) {
	file, ok := fstabSwapFile(v.fstab)
	return file, ok && !file.othersCanWrite
}

func hasDevice(devices []apischema.SwapDevice, kind apischema.SwapDeviceKind) bool {
	return slices.ContainsFunc(devices, func(d apischema.SwapDevice) bool { return d.Kind == kind })
}

// planSwapMode orders the steps that take the host in view to the requested
// mode, each verified before the next, so a failure leaves the host no less
// safe than before: swap is added before any is taken away. It refuses a
// mode the host cannot take before anything changes.
func planSwapMode(v swapView, req apischema.SwapModeRequest) ([]swapStep, error) {
	target := req.Mode
	if _, ok := presetFor(target); !ok {
		return nil, fmt.Errorf("%w: unknown swap mode %q", bridgeipc.ErrInvalidArgs, target)
	}
	zramRunning := hasDevice(v.status.Devices, apischema.SwapDeviceZram)
	file, managed := v.managedSwapFile()
	if target == apischema.SwapModeZram {
		if reason := v.status.ZramUnavailableReason; reason != "" {
			return nil, fmt.Errorf("%w: %s", bridgeipc.ErrConflict, zramRefusal(reason))
		}
		// write_config replaces the provider's configuration file.
		if err := confirmedReplace(v.status, req.ConfirmReplace); err != nil {
			return nil, err
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
		if file.othersCanWrite {
			return nil, fmt.Errorf("%w: %s needs the swap file %s, which LinuxIO does not start: %w", bridgeipc.ErrConflict, target, file.path, errOthersCanRedirect)
		}
		return nil, fmt.Errorf("%w: %s needs a swap file or an active swap partition", bridgeipc.ErrConflict, target)
	}
	if target == apischema.SwapModeZswap && !v.status.Zswap.Available {
		return nil, fmt.Errorf("%w: the kernel has no zswap", bridgeipc.ErrConflict)
	}
	if err := generatorRefusal(v, req.ConfirmReplace); err != nil {
		return nil, err
	}
	var steps []swapStep
	// A file that swaps while held off, as a failed switch away from zram
	// leaves it, is let go too, or the next boot would hold it off.
	if managed && (file.device == "" || file.heldOff) {
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

// generatorRefusal refuses a switch to disk or zswap with zram-generator as
// the provider, which turns zram off at boot through its configuration file:
// while another of its files defines a swap device, which would come back at
// every boot whatever LinuxIO's file says, or root cannot read one of its
// files, which may; and while that file is not LinuxIO's and the request did not name it,
// since stop_zram replaces it.
func generatorRefusal(v swapView, confirmed string) error {
	if v.status.ZramProvider != apischema.ZramProviderGenerator {
		return nil
	}
	if len(v.generatorUnreadable) > 0 {
		file := v.generatorUnreadable[0]
		return fmt.Errorf("%w: %s cannot be read, and zram-generator may set up zram from it at every boot: %w",
			bridgeipc.ErrConflict, file.path, file.err)
	}
	if devices := v.status.ZramGeneratorDevices; len(devices) > 0 {
		return fmt.Errorf("%w: %s sets up the zram swap device %s at every boot, which LinuxIO does not change; %s",
			bridgeipc.ErrConflict, devices[0].Path, devices[0].Device, generatorTurnOff(devices[0].Path))
	}
	return confirmedReplace(v.status, confirmed)
}

// confirmedReplace refuses a run that would replace the provider's
// configuration file while the file is not LinuxIO's and the request does
// not name it, so an admin's settings are never replaced without asking.
func confirmedReplace(status apischema.SwapStatus, confirmed string) error {
	config := status.ForeignZramConfig
	if config == nil || config.Path == confirmed {
		return nil
	}
	return fmt.Errorf("%w: %s holds zram settings that are not LinuxIO's; confirm that it may be replaced, and a copy is kept as %s",
		bridgeipc.ErrConflict, config.Path, config.CopyPath)
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
