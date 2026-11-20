package memory

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/power"
	"github.com/mordilloSan/LinuxIO/backend/common/hostcmd"
	"github.com/mordilloSan/LinuxIO/backend/common/systemd"
)

// zramCommand is the zram-tools script whose presence means zram-tools is
// installed.
const zramCommand = "zramswap"

// ZramGeneratorPath is systemd's zram-generator, whose presence means it is
// installed. A variable so that a test can point it at a file of its own.
var ZramGeneratorPath = "/usr/lib/systemd/system-generators/zram-generator"

// CheckZramAvailability reports whether a tool that sets zram up is
// installed: this is the zram capability.
func CheckZramAvailability() (bool, error) {
	if tools, generator := host.zramToolsInstalled(); !tools && !generator {
		return false, errors.New("neither zram-tools nor zram-generator is installed")
	}
	return true, nil
}

// zramToolsInstalled reports whether zram-tools and zram-generator are
// installed. zramswap is in /usr/sbin, which the bridge's PATH may omit.
func (h hostRoot) zramToolsInstalled() (tools, generator bool) {
	_, err := hostcmd.LookPath(zramCommand)
	return err == nil, h.exists(ZramGeneratorPath)
}

// zramProvider is the installed tool that sets zram up. With both installed
// it is the one whose unit runs zram now: zram-generator when systemd reads
// its setup service for zram0 active and zramswap.service inactive or
// failed, zram-tools otherwise, as when neither runs or a state is not proof.
func (h hostRoot) zramProvider(ctx context.Context, activeState func(context.Context, string) (string, error)) apischema.ZramProvider {
	tools, generator := h.zramToolsInstalled()
	switch {
	case generator && (!tools || generatorRunsZram(ctx, activeState)):
		return apischema.ZramProviderGenerator
	case tools:
		return apischema.ZramProviderTools
	default:
		return ""
	}
}

// generatorRunsZram reports whether systemd reads zram-generator's setup
// service for zram0 active and zramswap.service inactive or failed, which is
// zram-tools not running it. Any other state, such as unknown or activating,
// and a state that cannot be read, is no proof.
func generatorRunsZram(ctx context.Context, activeState func(context.Context, string) (string, error)) bool {
	if !zramSetupActive(ctx, activeState) {
		return false
	}
	tools, err := activeState(ctx, zramService)
	if err != nil {
		slog.Debug("read unit state failed", "unit", zramService, "error", err)
		return false
	}
	return tools == "inactive" || tools == "failed"
}

// zramSetupActive reports whether systemd reads zram-generator's setup
// service for zram0 active. A state that cannot be read is no proof.
func zramSetupActive(ctx context.Context, activeState func(context.Context, string) (string, error)) bool {
	state, err := activeState(ctx, zramGeneratorSetupUnit)
	if err != nil {
		slog.Debug("read unit state failed", "unit", zramGeneratorSetupUnit, "error", err)
		return false
	}
	return state == "active"
}

// GetSwapStatus reads how the host handles running out of memory from files
// under /proc, /sys and /etc, and asks over D-Bus whether TuneD is running
// and, with both zram tools installed, which one systemd runs zram with;
// TuneD counts as inactive, and zram-tools as the provider, when they cannot
// be asked.
func GetSwapStatus(ctx context.Context) (apischema.SwapStatus, error) {
	status, _, err := host.status(ctx)
	return status, err
}

// status is GetSwapStatus for the host under the root, with the fstab swap
// entries it was derived from (see readSwapStatus).
func (h hostRoot) status(ctx context.Context) (apischema.SwapStatus, []fstabSwap, error) {
	status, fstab, err := h.readSwapStatus(h.zramProvider(ctx, systemd.GetActiveState))
	if err != nil {
		return apischema.SwapStatus{}, nil, err
	}
	status.TunedActive, status.TunedProfile = tunedState(ctx)
	return status, fstab, nil
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
// the installed zram provider. It also returns the fstab swap entries,
// matched to the active devices, that the status was derived from, so that a
// change acts on the same reading of /etc/fstab its status decided on.
func (h hostRoot) readSwapStatus(provider apischema.ZramProvider) (apischema.SwapStatus, []fstabSwap, error) {
	zramAvailable := provider != ""
	devices, err := h.readSwaps()
	if err != nil {
		return apischema.SwapStatus{}, nil, err
	}
	mem, err := h.readMeminfo()
	if err != nil {
		return apischema.SwapStatus{}, nil, err
	}
	zswap, err := h.readZswap()
	if err != nil {
		return apischema.SwapStatus{}, nil, fmt.Errorf("read zswap parameters: %w", err)
	}
	zram, err := h.readZramDevices()
	if err != nil {
		return apischema.SwapStatus{}, nil, fmt.Errorf("read zram devices: %w", err)
	}
	mounts, err := h.readMounts()
	if err != nil {
		return apischema.SwapStatus{}, nil, fmt.Errorf("read mounts: %w", err)
	}
	fstab, unusable, err := h.fstabSwaps()
	if err != nil {
		return apischema.SwapStatus{}, nil, fmt.Errorf("read swap entries from fstab: %w", err)
	}
	fstab = h.matchFstabSwaps(fstab, devices)
	status := apischema.SwapStatus{
		Devices:              h.describeSwapFiles(devices, mounts),
		MemTotal:             mem.memTotal,
		MemAvailable:         mem.memAvailable,
		SwapTotal:            mem.swapTotal,
		SwapUsed:             mem.swapTotal - min(mem.swapFree, mem.swapTotal),
		Zswap:                zswap,
		ZramDevices:          zram,
		ZramAvailable:        zramAvailable,
		ZramProvider:         provider,
		ForeignZramConfig:    h.foreignZramConfig(provider),
		ZramGeneratorDevices: h.zramGeneratorDevices(),
		SwapFile:             h.statusSwapFile(fstab, unusable, devices, mounts),
		UnmanagedFstabSwaps:  unmanagedFstabSwaps(unusable),
		Presets:              presetList(),
	}
	if err := h.readVMSysctls(&status); err != nil {
		return apischema.SwapStatus{}, nil, fmt.Errorf("read vm sysctls: %w", err)
	}
	partitionActive := slices.ContainsFunc(devices, func(d apischema.SwapDevice) bool {
		return d.Kind == apischema.SwapDevicePartition
	})
	status.Mode = deriveMode(devices, zswap.Enabled)
	diskSwapStays := partitionActive || (status.SwapFile != nil && status.SwapFile.Active && !status.SwapFile.Managed)
	status.RecommendedMode = recommendMode(usableSwapFile(status), diskSwapStays, zswap.Available)
	status.RecommendedSwappiness = recommendedSwappiness(status.Mode, status.RecommendedMode)
	status.ZramUnavailableReason = zramUnavailableReason(zramAvailable, partitionActive)
	status.ZramPlannedSize = zramPlannedSize(mem.memTotal)
	status.DrainBlockedReason = drainBlockedReason(diskSwapUsed(devices), managedDiskUsed(fstab, devices), mem.memAvailable,
		leftOutHoldsData(fstab, devices),
		slices.ContainsFunc(holdingData(fstab, devices), func(e fstabSwap) bool { return e.heldOff }))
	status.CanDrain = status.DrainBlockedReason == ""
	return status, fstab, nil
}

// usableSwapFile is the swap file the recommendation goes by: the one the
// status reports, unless it is off and not managed, which nothing here can
// start; then the first active swap file, or none.
func usableSwapFile(status apischema.SwapStatus) *apischema.SwapFile {
	file := status.SwapFile
	if file == nil || file.Active || file.Managed {
		return file
	}
	i := slices.IndexFunc(status.Devices, func(d apischema.SwapDevice) bool { return d.Kind == apischema.SwapDeviceFile })
	if i < 0 {
		return nil
	}
	device := status.Devices[i]
	return &apischema.SwapFile{Path: device.Path, Active: true, FSType: device.FSType, DiskHint: device.DiskHint}
}

// describeSwapFiles fills in the filesystem and disk hint of each swap file.
func (h hostRoot) describeSwapFiles(devices []apischema.SwapDevice, mounts []mountEntry) []apischema.SwapDevice {
	for i, device := range devices {
		if device.Kind != apischema.SwapDeviceFile {
			continue
		}
		mount, _ := mountHolding(mounts, device.Path)
		devices[i].FSType, devices[i].DiskHint = mount.fsType, h.diskHint(mount)
	}
	return devices
}

// statusSwapFile is the swap file the status reports: the one fstab names
// that LinuxIO manages (see fstabSwapFile), active when /proc/swaps holds it,
// and managed, since its unit can be stopped, held off and started, unless
// others can change its path; or else the first active one, which is not
// managed, since fstab does not name it or names it by a path without a unit
// LinuxIO can use. It is nil when there is none.
func (h hostRoot) statusSwapFile(fstab []fstabSwap, unusable []unusableFstabSwap, devices []apischema.SwapDevice, mounts []mountEntry) *apischema.SwapFile {
	var file apischema.SwapFile
	if entry, ok := fstabSwapFile(fstab); ok {
		file.Path, file.Active, file.Managed = entry.path, entry.device != "", !entry.othersCanWrite
		if entry.othersCanWrite {
			file.UnmanagedReason = apischema.SwapFileOthersCanWrite
		}
	} else {
		i := slices.IndexFunc(devices, func(d apischema.SwapDevice) bool { return d.Kind == apischema.SwapDeviceFile })
		if i < 0 {
			return nil
		}
		file.Path, file.Active, file.UnmanagedReason = devices[i].Path, true, apischema.SwapFileNotInFstab
		if slices.ContainsFunc(unusable, func(e unusableFstabSwap) bool { return e.path != "" && h.sameSwap(e.path, file.Path) }) {
			file.UnmanagedReason = apischema.SwapFileUnitUnusable
		}
	}
	mount, _ := mountHolding(mounts, file.Path)
	file.FSType, file.DiskHint = mount.fsType, h.diskHint(mount)
	return &file
}

// unmanagedFstabSwaps lists the fstab swap entries without a unit LinuxIO
// can use, which it leaves alone; an empty list and not null on the wire.
func unmanagedFstabSwaps(unusable []unusableFstabSwap) []apischema.UnmanagedFstabSwap {
	list := []apischema.UnmanagedFstabSwap{}
	for _, entry := range unusable {
		list = append(list, apischema.UnmanagedFstabSwap{Reason: apischema.SwapFileUnitUnusable, Source: entry.source})
	}
	return list
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
