package memory

import (
	"slices"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

// sysctlSetting is one vm sysctl value, by its file name under vmSysctlDir.
type sysctlSetting struct {
	name  string
	value int
}

// modePreset is what choosing a mode applies: its swappiness and the other
// vm sysctls it sets. A sysctl a preset does not list stays at the kernel
// default. zram follows the Pop!_OS and Arch values.
type modePreset struct {
	mode       apischema.SwapMode
	swappiness int
	sysctls    []sysctlSetting
}

// modePresets lists the selectable modes in the order the dialog offers them.
var modePresets = []modePreset{
	{mode: apischema.SwapModeZram, swappiness: 180, sysctls: []sysctlSetting{
		{name: sysctlPageCluster, value: 0},
		{name: sysctlWatermarkBoostFactor, value: 0},
		{name: sysctlWatermarkScaleFactor, value: 125},
	}},
	{mode: apischema.SwapModeZswap, swappiness: 60},
	{mode: apischema.SwapModeDisk, swappiness: 60},
}

// kernelDefaults are the kernel's values of the vm sysctls only zram's
// preset sets. Leaving zram's values, a mode writes these back live and
// drops the keys from the drop-in.
var kernelDefaults = []sysctlSetting{
	{name: sysctlPageCluster, value: 3},
	{name: sysctlWatermarkBoostFactor, value: 15000},
	{name: sysctlWatermarkScaleFactor, value: 10},
}

// sets reports whether the preset sets the vm sysctl name.
func (p modePreset) sets(name string) bool {
	return slices.ContainsFunc(p.sysctls, func(s sysctlSetting) bool { return s.name == name })
}

// dropInChanges is what a mode does to the sysctl drop-in: the settings it
// writes and the keys it drops. Under TuneD swappiness is neither written
// nor dropped, since TuneD sets it itself.
func dropInChanges(mode apischema.SwapMode, tuned bool) (set []sysctlSetting, remove []string) {
	preset, _ := presetFor(mode)
	if !tuned {
		set = append(set, sysctlSetting{name: sysctlSwappiness, value: preset.swappiness})
	}
	set = append(set, preset.sysctls...)
	for _, def := range kernelDefaults {
		if !preset.sets(def.name) {
			remove = append(remove, def.name)
		}
	}
	return set, remove
}

// liveSysctls is what a mode writes to /proc/sys/vm: its drop-in settings,
// and the kernel default of each sysctl it does not set whose live value is
// still the one zram's preset gave it: LinuxIO's drop-in held the key (held,
// by sysctl name) and the live value is zram's. A live value that only
// equals zram's on a host whose drop-in never held the key is an admin's.
func liveSysctls(mode apischema.SwapMode, status apischema.SwapStatus, held map[string]bool) []sysctlSetting {
	set, _ := dropInChanges(mode, status.TunedActive)
	preset, _ := presetFor(mode)
	zram, _ := presetFor(apischema.SwapModeZram)
	live := map[string]int{
		sysctlPageCluster:          status.PageCluster,
		sysctlWatermarkBoostFactor: status.WatermarkBoostFactor,
		sysctlWatermarkScaleFactor: status.WatermarkScaleFactor,
	}
	for _, def := range kernelDefaults {
		i := slices.IndexFunc(zram.sysctls, func(s sysctlSetting) bool { return s.name == def.name })
		if !preset.sets(def.name) && held[def.name] && i >= 0 && live[def.name] == zram.sysctls[i].value {
			set = append(set, def)
		}
	}
	return set
}

// presetFor returns the preset of a selectable mode; none and mixed have none.
func presetFor(mode apischema.SwapMode) (modePreset, bool) {
	for _, preset := range modePresets {
		if preset.mode == mode {
			return preset, true
		}
	}
	return modePreset{}, false
}

// presetList is the presets as the status reports them.
func presetList() []apischema.SwapModePreset {
	list := make([]apischema.SwapModePreset, 0, len(modePresets))
	for _, preset := range modePresets {
		list = append(list, apischema.SwapModePreset{Mode: preset.mode, Swappiness: preset.swappiness})
	}
	return list
}

// deriveMode names how the active swap devices and zswap handle running out
// of memory.
func deriveMode(devices []apischema.SwapDevice, zswapEnabled bool) apischema.SwapMode {
	var zram, disk bool
	for _, device := range devices {
		if device.Kind == apischema.SwapDeviceZram {
			zram = true
		} else {
			disk = true
		}
	}
	switch {
	case zram && disk:
		return apischema.SwapModeMixed
	case zram:
		return apischema.SwapModeZram
	case !disk:
		return apischema.SwapModeNone
	case zswapEnabled:
		return apischema.SwapModeZswap
	default:
		return apischema.SwapModeDisk
	}
}

// recommendMode picks the mode for the host, first match wins: zram without a
// swap file, or with one on an SD card, eMMC or removable drive (which
// swapping wears out) or on btrfs (where it blocks snapshots), and zswap for
// a swap file anywhere else. Disk swap that stays whatever is chosen rules
// zram out (diskSwapStays): this feature never stops a swap partition, nor
// an active swap file it does not manage, so such a host gets zswap. A
// kernel without zswap never gets it recommended: disk beside such swap,
// zram otherwise.
func recommendMode(swapFile *apischema.SwapFile, diskSwapStays, zswapAvailable bool) apischema.SwapMode {
	switch {
	case diskSwapStays && zswapAvailable:
		return apischema.SwapModeZswap
	case diskSwapStays:
		return apischema.SwapModeDisk
	case swapFile == nil:
		return apischema.SwapModeZram
	case swapFile.DiskHint != "":
		return apischema.SwapModeZram
	case swapFile.FSType == "btrfs":
		return apischema.SwapModeZram
	case !zswapAvailable:
		return apischema.SwapModeZram
	default:
		return apischema.SwapModeZswap
	}
}

// recommendedSwappiness is the preset swappiness of the current mode, or of
// the recommended one when the host has no swap or mixes zram and disk.
func recommendedSwappiness(mode, recommended apischema.SwapMode) int {
	preset, ok := presetFor(mode)
	if !ok {
		preset, _ = presetFor(recommended)
	}
	return preset.swappiness
}

// swapReturnMargin is the memory left free after swapped-out data comes back
// into RAM, so that bringing it back does not itself wake the OOM killer.
const swapReturnMargin = 512 << 20

// fitsInMemory reports whether used bytes of swap can come back into RAM
// with swapReturnMargin to spare. Draining disk swap and stopping the swap
// file both ask it before they take a swap device away.
func fitsInMemory(used, memAvailable uint64) bool {
	return used+swapReturnMargin < memAvailable
}

// zramFits reports whether the data in the swapping zram devices can come
// back into RAM: while zram empties the kernel can also push pages to disk
// swap, so free disk swap counts as room. A device's data is the larger of
// its original size and its used swap, which also counts same-filled pages.
func zramFits(status apischema.SwapStatus) bool {
	var data, diskFree uint64
	for _, device := range status.Devices {
		if device.Kind != apischema.SwapDeviceZram {
			diskFree += device.Size - min(device.Used, device.Size)
			continue
		}
		used := device.Used
		for _, zram := range status.ZramDevices {
			if "/dev/"+zram.Name == device.Path {
				used = max(used, zram.OriginalSize)
			}
		}
		data += used
	}
	return fitsInMemory(data, status.MemAvailable+diskFree)
}

// drainBlockedReason says why the data parked on disk swap cannot be brought
// back into memory now, or is empty when it can. diskUsed is the data on
// every disk swap device, managedUsed the share on the devices LinuxIO
// manages, which are the only ones a drain stops and starts again;
// leftOutUsed is set when a device left out because others can redirect it
// holds data, and unitMasked when a managed device holding data has a
// masked unit.
func drainBlockedReason(diskUsed, managedUsed, memAvailable uint64, leftOutUsed, unitMasked bool) apischema.SwapDrainBlockedReason {
	switch {
	case diskUsed == 0:
		return apischema.SwapDrainNothingParked
	case managedUsed == 0 && leftOutUsed:
		return apischema.SwapDrainOthersCanWrite
	case managedUsed == 0:
		return apischema.SwapDrainUnmanagedDevice
	case unitMasked:
		return apischema.SwapDrainUnitMasked
	case !fitsInMemory(managedUsed, memAvailable):
		return apischema.SwapDrainNotEnoughMemory
	default:
		return ""
	}
}

// zramMaxSize caps the zram device, as Fedora's default does.
const zramMaxSize = 8 << 30

// zramPlannedSize is the size the zram mode gives its device: half of RAM,
// capped at zramMaxSize.
func zramPlannedSize(memTotal uint64) uint64 {
	return min(memTotal/2, zramMaxSize)
}

// zramUnavailableReason says why the zram mode cannot be chosen, or is empty
// when it can; a missing package wins when both reasons hold.
func zramUnavailableReason(toolsInstalled, partitionActive bool) apischema.ZramUnavailableReason {
	switch {
	case !toolsInstalled:
		return apischema.ZramPackageMissing
	case partitionActive:
		return apischema.ZramSwapPartitionActive
	default:
		return ""
	}
}
