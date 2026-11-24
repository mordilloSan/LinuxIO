package memory

import (
	"fmt"
	"slices"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

// swapStep is one step of a mode change.
type swapStep struct {
	code apischema.SwapStep
	// run is, for stop_zram, whether zram is stopped as well as turned off
	// at boot.
	run bool
	// path is the swap file of create_swap_file and remove_swap_file, and
	// size the size create_swap_file makes it at.
	path string
	size uint64
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
	if target == apischema.SwapModeZram {
		return planZram(v, req)
	}
	if target == apischema.SwapModeZswap && !v.status.Zswap.Available {
		return nil, fmt.Errorf("%w: the kernel has no zswap", bridgeipc.ErrConflict)
	}
	steps, err := planDiskSwap(v, req)
	if err != nil {
		return nil, err
	}
	if err := generatorRefusal(v, req.ConfirmReplace); err != nil {
		return nil, err
	}
	// zram is stopped through its provider's unit, so the step is planned
	// only with a provider installed; without one nothing here can stop
	// zram. The executor also leaves zram when systemd does not report that
	// unit running, as when another tool set zram up.
	if v.status.ZramAvailable {
		steps = append(steps, swapStep{code: apischema.SwapStepStopZram, run: hasDevice(v.status.Devices, apischema.SwapDeviceZram)})
	}
	steps = append(steps, swapStep{code: apischema.SwapStepApplySysctls})
	if target == apischema.SwapModeZswap {
		return append(steps, swapStep{code: apischema.SwapStepEnableZswap}), nil
	}
	return append(steps, swapStep{code: apischema.SwapStepDisableZswap}), nil
}

// planZram is the plan of a switch to zram: zram first, then the removal of
// every swap file that passes the strict check, which the request must
// confirm.
func planZram(v swapView, req apischema.SwapModeRequest) ([]swapStep, error) {
	if reason := v.status.ZramUnavailableReason; reason != "" {
		return nil, fmt.Errorf("%w: %s", bridgeipc.ErrConflict, zramRefusal(reason))
	}
	// write_config replaces the provider's configuration file.
	if err := confirmedReplace(v.status, req.ConfirmReplace); err != nil {
		return nil, err
	}
	removed := zramRemovals(v.status.SwapFiles)
	if err := confirmedRemove(removed, req.ConfirmRemove); err != nil {
		return nil, err
	}
	steps := []swapStep{
		{code: apischema.SwapStepWriteConfig},
		{code: apischema.SwapStepStartZram},
		{code: apischema.SwapStepApplySysctls},
		{code: apischema.SwapStepDisableZswap},
	}
	for _, path := range removed {
		steps = append(steps, swapStep{code: apischema.SwapStepRemoveSwapFile, path: path})
	}
	return steps, nil
}

// planDiskSwap is the disk swap a switch to disk or zswap counts on: the
// active swap files and partitions, or else LinuxIO's swap file, made before
// zram stops. A swap file in fstab that is off is never turned on, and
// LinuxIO's is not made beside it.
func planDiskSwap(v swapView, req apischema.SwapModeRequest) ([]swapStep, error) {
	if hasDevice(v.status.Devices, apischema.SwapDeviceFile) || hasDevice(v.status.Devices, apischema.SwapDevicePartition) {
		return nil, nil
	}
	if i := slices.IndexFunc(v.status.SwapFiles, func(r apischema.SwapFileRow) bool { return r.InFstab }); i >= 0 {
		return nil, fmt.Errorf("%w: %s needs disk swap, and %s is in /etc/fstab and does not swap, which LinuxIO does not turn on; remove it, and LinuxIO makes a swap file of its own",
			bridgeipc.ErrConflict, req.Mode, v.status.SwapFiles[i].Path)
	}
	if reason := v.status.Create.BlockedReason; reason != "" {
		return nil, fmt.Errorf("%w: %s needs disk swap, and LinuxIO cannot make its swap file: %s", bridgeipc.ErrConflict, req.Mode, createRefusal(reason))
	}
	size := req.SwapFileSize
	if size == 0 {
		size = v.status.Create.DefaultSize
	}
	if err := fitsLimit(size, v.status.Create.MaxSize); err != nil {
		return nil, err
	}
	return []swapStep{{code: apischema.SwapStepCreateSwapFile, path: ownSwapFilePath, size: size}}, nil
}

// zramRemovals are the swap files a switch to zram removes, every one that
// passes the strict check, in the order of the status, a temporary swap file
// a resize left before the file it stood in for.
func zramRemovals(rows []apischema.SwapFileRow) []string {
	var temporary, files []string
	for _, row := range rows {
		switch {
		case row.LockedReason != "":
		case row.Temporary:
			temporary = append(temporary, row.Path)
		default:
			files = append(files, row.Path)
		}
	}
	return append(temporary, files...)
}

// confirmedRemove refuses a switch to zram unless the request names exactly
// the swap files it removes, in any order, so a file is never removed that
// the confirm did not show.
func confirmedRemove(removed, confirmed []string) error {
	want, got := slices.Sorted(slices.Values(removed)), slices.Compact(slices.Sorted(slices.Values(confirmed)))
	if slices.Equal(want, got) {
		return nil
	}
	if len(want) == 0 {
		return fmt.Errorf("%w: the switch removes no swap file, and the request confirms removing %s", bridgeipc.ErrConflict, strings.Join(got, ", "))
	}
	return fmt.Errorf("%w: the switch removes the swap files %s; confirm that they may be removed", bridgeipc.ErrConflict, strings.Join(want, ", "))
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
	default:
		return "zram cannot be chosen: " + string(reason)
	}
}
