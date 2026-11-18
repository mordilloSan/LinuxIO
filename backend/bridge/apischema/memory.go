package apischema

// SwapMode names how the host handles running out of memory. Disk, zswap and
// zram are the modes the Memory dialog offers; none and mixed only describe a
// host.
type SwapMode string

const (
	SwapModeDisk  SwapMode = "disk"
	SwapModeZswap SwapMode = "zswap"
	SwapModeZram  SwapMode = "zram"
	SwapModeNone  SwapMode = "none"
	SwapModeMixed SwapMode = "mixed"
)

// SwapDeviceKind is what backs an active swap device.
type SwapDeviceKind string

const (
	SwapDeviceFile      SwapDeviceKind = "file"
	SwapDevicePartition SwapDeviceKind = "partition"
	SwapDeviceZram      SwapDeviceKind = "zram"
)

// SwapDiskHint names a disk that swapping wears out: "mmc" for an SD card or
// eMMC, "removable" for a disk the kernel marks removable (a USB stick or a
// card reader), empty for any other disk.
type SwapDiskHint string

const (
	SwapDiskHintMMC       SwapDiskHint = "mmc"
	SwapDiskHintRemovable SwapDiskHint = "removable"
)

// ZramProvider is the installed tool that sets zram up, or is empty when
// there is none. zram-tools runs zramswap.service from /etc/default/zramswap
// (Debian and Ubuntu); zram-generator makes dev-zram0.swap from
// /etc/systemd/zram-generator.conf (Fedora, RHEL, Arch and Debian's
// systemd-zram-generator). zram-tools is used when both are installed.
type ZramProvider string

const (
	ZramProviderTools     ZramProvider = "zram-tools"
	ZramProviderGenerator ZramProvider = "zram-generator"
)

// ZramUnavailableReason says why the zram mode cannot be chosen, or is empty
// when it can.
type ZramUnavailableReason string

const (
	ZramPackageMissing      ZramUnavailableReason = "package_missing"
	ZramSwapPartitionActive ZramUnavailableReason = "swap_partition_active"
)

// SwapDrainBlockedReason says why data parked on disk swap cannot be brought
// back into memory, or is empty when it can.
type SwapDrainBlockedReason string

const (
	SwapDrainNothingParked   SwapDrainBlockedReason = "nothing_parked"
	SwapDrainNotEnoughMemory SwapDrainBlockedReason = "not_enough_memory"
	// SwapDrainUnmanagedDevice: the parked data sits only on swap devices
	// /etc/fstab does not name, whose units could not be started again.
	SwapDrainUnmanagedDevice SwapDrainBlockedReason = "unmanaged_device"
	// SwapDrainUnitHeldOff: the unit of a device holding data is held off at
	// boot, by LinuxIO's condition drop-in or a mask, so a start of it would
	// not bring the device back.
	SwapDrainUnitHeldOff SwapDrainBlockedReason = "unit_held_off"
	// SwapDrainOthersCanWrite: the parked data sits on a swap device fstab
	// names that LinuxIO does not manage, since users other than root can
	// change a folder on its path.
	SwapDrainOthersCanWrite SwapDrainBlockedReason = "others_can_write"
)

// SwapLeftWhat names what a mode change left as it was.
type SwapLeftWhat string

const (
	// SwapLeftSwapFile: the swap file still swaps, beside zram.
	SwapLeftSwapFile SwapLeftWhat = "swap_file"
	// SwapLeftZram: zram still swaps, beside disk swap.
	SwapLeftZram SwapLeftWhat = "zram"
	// SwapLeftZramSize: zram runs at the size it had, not the planned one.
	SwapLeftZramSize SwapLeftWhat = "zram_size"
)

// SwapLeftReason says why a mode change left something as it was.
type SwapLeftReason string

const (
	// SwapLeftNotEnoughMemory: the data on the device does not fit in free
	// memory now. Applying again later can finish the change.
	SwapLeftNotEnoughMemory SwapLeftReason = "not_enough_memory"
	// SwapLeftZramNotSwapping: zram was not swapping when the swap file was
	// to be stopped, which would have left the host without swap.
	SwapLeftZramNotSwapping SwapLeftReason = "zram_not_swapping"
	// SwapLeftNotIdentified: an active swap file could not be told from the
	// one fstab names, so that file's unit was not held off at boot.
	SwapLeftNotIdentified SwapLeftReason = "not_identified"
	// SwapLeftOthersCanWrite: a folder on the swap file's path can be
	// changed by users other than root, who could send a swapoff of that
	// path to another swap device, so the file was not stopped.
	SwapLeftOthersCanWrite SwapLeftReason = "others_can_write"
	// SwapLeftNotManaged: nothing LinuxIO manages runs the device. For a
	// swap file, it is not the first one /etc/fstab names, the only one with
	// a unit a change stops. For zram, it was set up by something other
	// than the installed zram provider, or no provider is installed.
	SwapLeftNotManaged SwapLeftReason = "not_managed"
	// SwapLeftNoOtherSwap: restarting zram at the planned size would leave
	// the host without swap meanwhile, since no disk swap is active.
	SwapLeftNoOtherSwap SwapLeftReason = "no_other_swap"
	// SwapLeftOtherSize: zram was started and came up at another size than
	// the planned one.
	SwapLeftOtherSize SwapLeftReason = "other_size"
)

// SwapLeft is one thing a mode change left as it was, and why.
type SwapLeft struct {
	Reason SwapLeftReason `json:"reason"`
	What   SwapLeftWhat   `json:"what"`
}

// SwapModeResult is memory.set_swap_mode's result: the host's status after
// the change and what the change left as it was. Left is empty when the host
// reached the mode in full.
type SwapModeResult struct {
	SwapStatus
	Left []SwapLeft `json:"left"`
}

// SwapStep names one step of memory.set_swap_mode or memory.drain_swap, as
// their progress reports it.
type SwapStep string

const (
	SwapStepWriteConfig   SwapStep = "write_config"
	SwapStepStartZram     SwapStep = "start_zram"
	SwapStepApplySysctls  SwapStep = "apply_sysctls"
	SwapStepDisableZswap  SwapStep = "disable_zswap"
	SwapStepStopSwapFile  SwapStep = "stop_swap_file"
	SwapStepStartSwapFile SwapStep = "start_swap_file"
	SwapStepStopZram      SwapStep = "stop_zram"
	SwapStepEnableZswap   SwapStep = "enable_zswap"
	SwapStepDrainDevice   SwapStep = "drain_device"
)

// SwapSteps lists every step, in the order a run can take them.
var SwapSteps = []SwapStep{
	SwapStepWriteConfig, SwapStepStartSwapFile, SwapStepStartZram, SwapStepStopZram, SwapStepApplySysctls,
	SwapStepDisableZswap, SwapStepEnableZswap, SwapStepStopSwapFile, SwapStepDrainDevice,
}

var swapStepMessages = map[SwapStep]string{
	SwapStepWriteConfig:   "Writing the zram and sysctl configuration",
	SwapStepStartZram:     "Starting zram",
	SwapStepApplySysctls:  "Applying the memory sysctls",
	SwapStepDisableZswap:  "Turning zswap off",
	SwapStepStopSwapFile:  "Stopping the swap file",
	SwapStepStartSwapFile: "Starting the swap file",
	SwapStepStopZram:      "Stopping zram",
	SwapStepEnableZswap:   "Turning zswap on",
	SwapStepDrainDevice:   "Moving swapped data back into memory",
}

// SwapProgress is one step starting: its 1-based Index of Total, and for a
// drain the swap device it drains.
type SwapProgress struct {
	Device string   `json:"device,omitzero"`
	Index  int      `json:"index"`
	Step   SwapStep `json:"step"`
	Total  int      `json:"total"`
}

func (p SwapProgress) ProgressEnvelope() TaskProgress {
	var percent int
	if p.Total > 0 {
		percent = (p.Index - 1) * 100 / p.Total
	}
	return TaskProgress{Percentage: &percent, Phase: string(p.Step), Message: swapStepMessages[p.Step], Detail: p}
}

// SwapModeRequest is memory.set_swap_mode's request: disk, zswap or zram.
// ConfirmReplace is the path of a zram configuration file that is not
// LinuxIO's (SwapStatus.ForeignZramConfig) which the user agreed the change
// may replace; a change that would replace such a file is refused unless it
// names that file here.
type SwapModeRequest struct {
	ConfirmReplace string   `json:"confirm_replace,omitzero"`
	Mode           SwapMode `json:"mode"`
}

// SwappinessRequest is memory.set_swappiness's request, 0 to 200.
type SwappinessRequest struct {
	Swappiness int `json:"swappiness"`
}

// SwapDevice is one active swap device from /proc/swaps. Sizes are bytes. A
// swap file also carries the filesystem it is on and its disk hint.
type SwapDevice struct {
	DiskHint SwapDiskHint   `json:"disk_hint,omitzero"`
	FSType   string         `json:"fs_type,omitzero"`
	Kind     SwapDeviceKind `json:"kind"`
	Path     string         `json:"path"`
	Priority int            `json:"priority"`
	Size     uint64         `json:"size"`
	Used     uint64         `json:"used"`
}

// SwapFileUnmanagedReason says why LinuxIO does not stop, start or drain the
// swap file.
type SwapFileUnmanagedReason string

const (
	// SwapFileNotInFstab: /etc/fstab does not name the file, so it has no
	// unit to stop, hold off at boot or start.
	SwapFileNotInFstab SwapFileUnmanagedReason = "not_in_fstab"
	// SwapFileOthersCanWrite: users other than root can change a folder on
	// the file's path and so put another entry at it, which a swapoff or a
	// start of that path would then act on.
	SwapFileOthersCanWrite SwapFileUnmanagedReason = "others_can_write"
)

// SwapFile is the swap file the status reports: the first one /etc/fstab
// names, active or not, or else the first active one.
type SwapFile struct {
	Active   bool         `json:"active"`
	DiskHint SwapDiskHint `json:"disk_hint"`
	FSType   string       `json:"fs_type"`
	// Managed is true when LinuxIO stops, holds off, starts and drains the
	// file: /etc/fstab names it, so it has a unit, and only root can change
	// the folders on its path. A file that is not managed is left as it is,
	// and UnmanagedReason says why.
	Managed         bool                    `json:"managed"`
	Path            string                  `json:"path"`
	UnmanagedReason SwapFileUnmanagedReason `json:"unmanaged_reason"`
}

// ZswapStatus is the zswap module's parameters; Available is false on a
// kernel built without zswap.
type ZswapStatus struct {
	Available      bool   `json:"available"`
	Compressor     string `json:"compressor"`
	Enabled        bool   `json:"enabled"`
	MaxPoolPercent int    `json:"max_pool_percent"`
}

// ZramDevice is one /sys/block/zram<N> device. Sizes are bytes: OriginalSize
// is the data stored, CompressedSize what it compressed to, and MemUsed the
// memory the device takes including its overhead.
type ZramDevice struct {
	CompressedSize uint64 `json:"compressed_size"`
	Disksize       uint64 `json:"disksize"`
	MemUsed        uint64 `json:"mem_used"`
	Name           string `json:"name"`
	OriginalSize   uint64 `json:"original_size"`
}

// ForeignZramConfig is the zram provider's configuration file when it is not
// LinuxIO's: it does not start with LinuxIO's header and holds more than
// comments and blank lines. A mode change that would replace it needs the
// request to name Path, and copies the file to CopyPath first.
type ForeignZramConfig struct {
	CopyPath string `json:"copy_path"`
	Path     string `json:"path"`
}

// ZramGeneratorDevice is a zram swap device that a zram-generator
// configuration file other than LinuxIO's defines and that takes effect: the
// file, the device its [zramN] section names, and the size settings it
// writes, each as written and empty when it sets none: zram-size, and the
// older zram-fraction and max-zram-size that upstream still honours.
type ZramGeneratorDevice struct {
	Device       string `json:"device"`
	MaxZramSize  string `json:"max_zram_size"`
	Path         string `json:"path"`
	ZramFraction string `json:"zram_fraction"`
	ZramSize     string `json:"zram_size"`
}

// SwapModePreset is the swappiness choosing a mode applies.
type SwapModePreset struct {
	Mode       SwapMode `json:"mode"`
	Swappiness int      `json:"swappiness"`
}

// SwapStatus is how the host handles running out of memory, with what the
// Memory dialog needs to offer each mode. Sizes are bytes.
// ZramGeneratorDevices lists, while zram-generator is installed, the devices
// its configuration files other than LinuxIO's define.
type SwapStatus struct {
	CanDrain              bool                   `json:"can_drain"`
	Devices               []SwapDevice           `json:"devices"`
	DrainBlockedReason    SwapDrainBlockedReason `json:"drain_blocked_reason"`
	ForeignZramConfig     *ForeignZramConfig     `json:"foreign_zram_config,omitzero"`
	MemAvailable          uint64                 `json:"mem_available"`
	MemTotal              uint64                 `json:"mem_total"`
	Mode                  SwapMode               `json:"mode"`
	PageCluster           int                    `json:"page_cluster"`
	Presets               []SwapModePreset       `json:"presets"`
	RecommendedMode       SwapMode               `json:"recommended_mode"`
	RecommendedSwappiness int                    `json:"recommended_swappiness"`
	SwapFile              *SwapFile              `json:"swap_file,omitzero"`
	SwapTotal             uint64                 `json:"swap_total"`
	SwapUsed              uint64                 `json:"swap_used"`
	Swappiness            int                    `json:"swappiness"`
	TunedActive           bool                   `json:"tuned_active"`
	TunedProfile          string                 `json:"tuned_profile"`
	WatermarkBoostFactor  int                    `json:"watermark_boost_factor"`
	WatermarkScaleFactor  int                    `json:"watermark_scale_factor"`
	ZramAvailable         bool                   `json:"zram_available"`
	ZramDevices           []ZramDevice           `json:"zram_devices"`
	ZramGeneratorDevices  []ZramGeneratorDevice  `json:"zram_generator_devices"`
	ZramPlannedSize       uint64                 `json:"zram_planned_size"`
	ZramProvider          ZramProvider           `json:"zram_provider"`
	ZramUnavailableReason ZramUnavailableReason  `json:"zram_unavailable_reason"`
	Zswap                 ZswapStatus            `json:"zswap"`
}
