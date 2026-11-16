package memory

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/power"
	"github.com/mordilloSan/LinuxIO/backend/common/hostcmd"
)

// zramCommand is the zram-tools script whose presence is the zram capability.
const zramCommand = "zramswap"

// CheckZramAvailability reports whether zram-tools is installed. zramswap
// is in /usr/sbin, which the bridge's PATH may omit.
func CheckZramAvailability() (bool, error) {
	if _, err := hostcmd.LookPath(zramCommand); err != nil {
		return false, fmt.Errorf("zram-tools is not installed: %w", err)
	}
	return true, nil
}

// GetSwapStatus reads how the host handles running out of memory from files
// under /proc, /sys and /etc, and asks TuneD over D-Bus whether it is
// running; TuneD counts as inactive when it cannot be asked.
func GetSwapStatus(ctx context.Context) (apischema.SwapStatus, error) {
	return host.status(ctx)
}

// status is GetSwapStatus for the host under the root.
func (h hostRoot) status(ctx context.Context) (apischema.SwapStatus, error) {
	// The error only says why zram-tools counts as missing.
	zramAvailable, _ := CheckZramAvailability()
	status, err := h.readSwapStatus(zramAvailable)
	if err != nil {
		return apischema.SwapStatus{}, err
	}
	status.TunedActive, status.TunedProfile = tunedState(ctx)
	return status, nil
}

// tunedState reports whether TuneD is running and its active profile: TuneD
// sets swappiness itself on every profile switch and at boot.
func tunedState(ctx context.Context) (active bool, profile string) {
	status, err := power.GetStatus(ctx)
	if err != nil {
		slog.Debug("read tuned status failed", "error", err)
		return false, ""
	}
	if !status.TunedActive {
		return false, ""
	}
	return true, status.ActiveProfile
}

// readSwapStatus reads everything the status reports except TuneD, given
// whether zram-tools is installed.
func (h hostRoot) readSwapStatus(zramAvailable bool) (apischema.SwapStatus, error) {
	devices, err := h.readSwaps()
	if err != nil {
		return apischema.SwapStatus{}, err
	}
	mem, err := h.readMeminfo()
	if err != nil {
		return apischema.SwapStatus{}, err
	}
	zswap, err := h.readZswap()
	if err != nil {
		return apischema.SwapStatus{}, fmt.Errorf("read zswap parameters: %w", err)
	}
	zram, err := h.readZramDevices()
	if err != nil {
		return apischema.SwapStatus{}, fmt.Errorf("read zram devices: %w", err)
	}
	mounts, err := h.readMounts()
	if err != nil {
		return apischema.SwapStatus{}, fmt.Errorf("read mounts: %w", err)
	}
	fstab, err := h.fstabSwaps()
	if err != nil {
		return apischema.SwapStatus{}, fmt.Errorf("read swap entries from fstab: %w", err)
	}
	fstab = h.matchFstabSwaps(fstab, devices)
	status := apischema.SwapStatus{
		Devices:       h.describeSwapFiles(devices, mounts),
		MemTotal:      mem.memTotal,
		MemAvailable:  mem.memAvailable,
		SwapTotal:     mem.swapTotal,
		SwapUsed:      mem.swapTotal - min(mem.swapFree, mem.swapTotal),
		Zswap:         zswap,
		ZramDevices:   zram,
		ZramAvailable: zramAvailable,
		SwapFile:      h.statusSwapFile(fstab, devices, mounts),
		Presets:       presetList(),
	}
	if err := h.readVMSysctls(&status); err != nil {
		return apischema.SwapStatus{}, fmt.Errorf("read vm sysctls: %w", err)
	}
	partitionActive := slices.ContainsFunc(devices, func(d apischema.SwapDevice) bool {
		return d.Kind == apischema.SwapDevicePartition
	})
	status.Mode = deriveMode(devices, zswap.Enabled)
	status.RecommendedMode = recommendMode(status.SwapFile, partitionActive, zswap.Available)
	status.RecommendedSwappiness = recommendedSwappiness(status.Mode, status.RecommendedMode)
	status.ZramUnavailableReason = zramUnavailableReason(zramAvailable, partitionActive)
	status.ZramPlannedSize = zramPlannedSize(mem.memTotal)
	status.DrainBlockedReason = drainBlockedReason(diskSwapUsed(devices), managedDiskUsed(fstab, devices), mem.memAvailable,
		slices.ContainsFunc(holdingData(fstab, devices), func(e fstabSwap) bool { return h.unitMasked(e.unit) }))
	status.CanDrain = status.DrainBlockedReason == ""
	return status, nil
}

// describeSwapFiles fills in the filesystem and disk hint of each swap file.
func (h hostRoot) describeSwapFiles(devices []apischema.SwapDevice, mounts []mountEntry) []apischema.SwapDevice {
	for i, device := range devices {
		if device.Kind != apischema.SwapDeviceFile {
			continue
		}
		mount, _ := mountHolding(mounts, device.Path)
		devices[i].FSType, devices[i].DiskHint = mount.fsType, h.diskHint(mount.source)
	}
	return devices
}

// statusSwapFile is the swap file the status reports: the first one fstab
// names, active when /proc/swaps holds it and managed, since its unit can be
// stopped, masked and started; or else the first active one, which is not
// managed. It is nil when there is none.
func (h hostRoot) statusSwapFile(fstab []fstabSwap, devices []apischema.SwapDevice, mounts []mountEntry) *apischema.SwapFile {
	var file apischema.SwapFile
	if entry, ok := fstabSwapFile(fstab); ok {
		file.Path, file.Active, file.Managed = entry.path, entry.device != "", true
	} else {
		i := slices.IndexFunc(devices, func(d apischema.SwapDevice) bool { return d.Kind == apischema.SwapDeviceFile })
		if i < 0 {
			return nil
		}
		file.Path, file.Active = devices[i].Path, true
	}
	mount, _ := mountHolding(mounts, file.Path)
	file.FSType, file.DiskHint = mount.fsType, h.diskHint(mount.source)
	return &file
}

// diskSwapUsed is the data parked on disk swap devices; data in zram is
// already in memory.
func diskSwapUsed(devices []apischema.SwapDevice) uint64 {
	var used uint64
	for _, device := range devices {
		if device.Kind != apischema.SwapDeviceZram {
			used += device.Used
		}
	}
	return used
}
