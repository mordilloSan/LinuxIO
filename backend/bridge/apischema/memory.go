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
	// SwapDrainUnitMasked: the unit of a device holding data is masked, so
	// it could not be started again.
	SwapDrainUnitMasked SwapDrainBlockedReason = "unit_masked"
)

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
type SwapModeRequest struct {
	Mode SwapMode `json:"mode"`
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

// SwapFile is the swap file LinuxIO manages: the first one /etc/fstab names,
// active or not, or else the first active one.
type SwapFile struct {
	Active   bool         `json:"active"`
	DiskHint SwapDiskHint `json:"disk_hint"`
	FSType   string       `json:"fs_type"`
	// Managed is true when /etc/fstab names the file, so it has a unit that
	// LinuxIO can stop, mask and start; a file fstab does not name is left
	// as it is.
	Managed bool   `json:"managed"`
	Path    string `json:"path"`
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

// SwapModePreset is the swappiness choosing a mode applies.
type SwapModePreset struct {
	Mode       SwapMode `json:"mode"`
	Swappiness int      `json:"swappiness"`
}

// SwapStatus is how the host handles running out of memory, with what the
// Memory dialog needs to offer each mode. Sizes are bytes.
type SwapStatus struct {
	CanDrain              bool                   `json:"can_drain"`
	Devices               []SwapDevice           `json:"devices"`
	DrainBlockedReason    SwapDrainBlockedReason `json:"drain_blocked_reason"`
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
	ZramPlannedSize       uint64                 `json:"zram_planned_size"`
	ZramUnavailableReason ZramUnavailableReason  `json:"zram_unavailable_reason"`
	Zswap                 ZswapStatus            `json:"zswap"`
}
