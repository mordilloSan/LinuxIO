package memory

import (
	"slices"
	"strings"

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
// still the one zram's preset gave it: the value is LinuxIO's (held, by
// sysctl name: the drop-in held the key, or the host ran zram) and is
// zram's. A live value that only equals zram's on a host that neither held
// the key nor ran zram is an admin's.
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
// of memory, first match wins: zram beside a swap file LinuxIO can remove
// (removable, one that passes the strict check) is mixed, which applying zram
// finishes; zram is zram, whatever other swap stays beside it; any disk swap,
// file or partition, is zswap or disk by zswap's enabled; otherwise none.
func deriveMode(devices []apischema.SwapDevice, removable []string, zswapEnabled bool) apischema.SwapMode {
	zram := hasDevice(devices, apischema.SwapDeviceZram)
	disk := slices.ContainsFunc(devices, func(d apischema.SwapDevice) bool { return d.Kind != apischema.SwapDeviceZram })
	removableActive := slices.ContainsFunc(devices, func(d apischema.SwapDevice) bool {
		return d.Kind == apischema.SwapDeviceFile && slices.Contains(removable, d.Path)
	})
	switch {
	case zram && removableActive:
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

// modeSavedForBoot reports whether the next start brings zswap up the way
// mode has it now, as far as LinuxIO can read it. Only zswap and disk are
// judged, the two modes deriveMode tells apart by zswap's switch: zswap
// needs it on at the next start, and disk needs it not on. zram is zram
// whatever zswap says, so nothing about zswap makes it unsaved, and none and
// mixed are not modes a host is left in. on is what the next start sets
// zswap to and set whether anything LinuxIO reads sets it (zswapAtBoot);
// with nothing set the kernel's own default decides, which no file reliably
// tells, so zswap counts as not saved (the default is off on the kernels
// LinuxIO is packaged for) and disk as saved. A kernel without zswap has
// nothing to bring up.
func modeSavedForBoot(mode apischema.SwapMode, zswapAvailable, on, set bool) bool {
	switch mode {
	case apischema.SwapModeZswap:
		return set && on
	case apischema.SwapModeDisk:
		return !zswapAvailable || !set || !on
	default:
		return true
	}
}

// zswapAtBoot is what the next start sets zswap's enabled parameter to:
// LinuxIO's tmpfiles file when it writes the parameter, since
// systemd-tmpfiles runs after the kernel read its command line, or else the
// zswap.enabled of the command line this kernel was started with. set is
// false when neither names it.
func zswapAtBoot(tmpfiles, cmdline string) (on, set bool) {
	for line := range strings.Lines(tmpfiles) {
		// "w <path> - - - - <value>", as renderZswapTmpfiles writes it.
		if f := strings.Fields(line); len(f) == 7 && f[0] == "w" && f[1] == zswapEnabledParam {
			if value, ok := kernelBool(f[6]); ok {
				on, set = value, true
			}
		}
	}
	if set {
		return on, true
	}
	for word := range strings.FieldsSeq(cmdline) {
		name, value, hasValue := strings.Cut(word, "=")
		if name != "zswap.enabled" {
			continue
		}
		// A bool parameter named without a value is set.
		if !hasValue {
			on, set = true, true
		} else if parsed, ok := kernelBool(value); ok {
			on, set = parsed, true
		}
	}
	return on, set
}

// kernelBool reads a value as the kernel's kstrtobool does: by its first
// character ("enabled" and "disabled" count since Linux 6.4), or its second
// after an "o" ("on", "off").
func kernelBool(value string) (parsed, ok bool) {
	if value == "" {
		return false, false
	}
	switch value[0] {
	case 'y', 'Y', 't', 'T', 'e', 'E', '1':
		return true, true
	case 'n', 'N', 'f', 'F', 'd', 'D', '0':
		return false, true
	case 'o', 'O':
		if len(value) > 1 {
			switch value[1] {
			case 'n', 'N':
				return true, true
			case 'f', 'F':
				return false, true
			}
		}
	}
	return false, false
}

// recommendMode picks the mode for the host, first match wins: zram with no
// active disk swap, with disk swap on an SD card, eMMC or removable drive,
// which swapping wears out, or with swap files that block snapshots on btrfs,
// when the switch removes every one of them; a kernel without zswap gets
// disk beside its disk swap; anything else gets zswap. A swap partition or a
// swap file LinuxIO cannot remove stays beside zram, which takes the place of
// nothing but the files it removes: no reason against zram, and, for a file
// that blocks snapshots, none for it either.
func recommendMode(devices []apischema.SwapDevice, rows []apischema.SwapFileRow, zswapAvailable bool) apischema.SwapMode {
	disk := slices.ContainsFunc(devices, func(d apischema.SwapDevice) bool { return d.Kind != apischema.SwapDeviceZram })
	blocking := func(r apischema.SwapFileRow) bool { return r.Active && r.BlocksSnapshots }
	switch {
	case !disk:
		return apischema.SwapModeZram
	case slices.ContainsFunc(devices, func(d apischema.SwapDevice) bool { return d.DiskHint != "" }):
		return apischema.SwapModeZram
	case slices.ContainsFunc(rows, blocking) && !slices.ContainsFunc(rows, func(r apischema.SwapFileRow) bool { return blocking(r) && r.LockedReason != "" }):
		return apischema.SwapModeZram
	case !zswapAvailable:
		return apischema.SwapModeDisk
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
// with swapReturnMargin to spare, with room counting memAvailable and any
// free swap the pages can go to meanwhile.
func fitsInMemory(used, room uint64) bool {
	return used+swapReturnMargin < room
}

// roomToTurnOff is the room check before a disk swap device is turned off:
// its used bytes, plus swapReturnMargin, must be less than MemAvailable plus
// the free space on the other active disk swap devices, which the kernel can
// push pages out to while it reads the device's back. A device that is not
// active needs no room.
func roomToTurnOff(status apischema.SwapStatus, path string) bool {
	var used, otherFree uint64
	for _, device := range status.Devices {
		switch {
		case device.Path == path:
			used = device.Used
		case device.Kind != apischema.SwapDeviceZram:
			otherFree += device.Size - min(device.Used, device.Size)
		}
	}
	return fitsInMemory(used, status.MemAvailable+otherFree)
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
// back into memory now, or is empty when it can. A drain turns off and on
// only the swap files LinuxIO can remove (drainTargets): data that sits only
// on a partition, an LVM volume or a locked swap file is other_swap, and the
// data on the targets must pass the room check counting the free space on
// the disk swap a drain leaves alone.
func drainBlockedReason(status apischema.SwapStatus) apischema.SwapDrainBlockedReason {
	targets := drainTargets(status)
	var drainable, otherFree uint64
	for _, device := range status.Devices {
		switch {
		case slices.Contains(targets, device.Path):
			drainable += device.Used
		case device.Kind != apischema.SwapDeviceZram:
			otherFree += device.Size - min(device.Used, device.Size)
		}
	}
	switch {
	case diskSwapUsed(status.Devices) == 0:
		return apischema.SwapDrainNothingParked
	case drainable == 0:
		return apischema.SwapDrainOtherSwap
	case !fitsInMemory(drainable, status.MemAvailable+otherFree):
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
// when it can. A swap partition stays beside zram.
func zramUnavailableReason(toolsInstalled bool) apischema.ZramUnavailableReason {
	if !toolsInstalled {
		return apischema.ZramPackageMissing
	}
	return ""
}
