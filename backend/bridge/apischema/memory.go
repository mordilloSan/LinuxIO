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
	ZramPackageMissing ZramUnavailableReason = "package_missing"
)

// SwapDrainBlockedReason says why data parked on disk swap cannot be brought
// back into memory, or is empty when it can.
type SwapDrainBlockedReason string

const (
	SwapDrainNothingParked   SwapDrainBlockedReason = "nothing_parked"
	SwapDrainNotEnoughMemory SwapDrainBlockedReason = "not_enough_memory"
	// SwapDrainOtherSwap: the parked data sits only on swap LinuxIO does not
	// turn off: a partition, an LVM volume, or a swap file with a
	// SwapFileRow.LockedReason.
	SwapDrainOtherSwap SwapDrainBlockedReason = "other_swap"
)

// SwapLeftWhat names what a mode change left as it was.
type SwapLeftWhat string

const (
	// SwapLeftSwapFile: the swap file SwapLeft.Path was not removed. It
	// still swaps, beside zram, or stays because the temporary swap file of
	// its resize does.
	SwapLeftSwapFile SwapLeftWhat = "swap_file"
	// SwapLeftZram: zram still swaps, beside disk swap, after a switch away
	// from it.
	SwapLeftZram SwapLeftWhat = "zram"
	// SwapLeftZramSize: zram runs at the size it had, not the planned one.
	SwapLeftZramSize SwapLeftWhat = "zram_size"
)

// SwapLeftReason says why a mode change left something as it was.
type SwapLeftReason string

const (
	// SwapLeftNotEnoughMemory: the data on the device does not fit in free
	// memory and free swap now. Applying again later can finish the change.
	SwapLeftNotEnoughMemory SwapLeftReason = "not_enough_memory"
	// SwapLeftZramNotSwapping: zram was not swapping when the swap file was
	// to be removed, which would have left the host without swap.
	SwapLeftZramNotSwapping SwapLeftReason = "zram_not_swapping"
	// SwapLeftNotManaged: zram was set up by something other than the
	// installed zram provider, or no provider is installed.
	SwapLeftNotManaged SwapLeftReason = "not_managed"
	// SwapLeftNoOtherSwap: restarting zram at the planned size would leave
	// the host without swap meanwhile, since no disk swap is active.
	SwapLeftNoOtherSwap SwapLeftReason = "no_other_swap"
	// SwapLeftOtherSize: zram was started and came up at another size than
	// the planned one.
	SwapLeftOtherSize SwapLeftReason = "other_size"
)

// SwapLeft is one thing a mode change left as it was, and why. Path is the
// swap file, for SwapLeftSwapFile.
type SwapLeft struct {
	Path   string         `json:"path,omitzero"`
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

// SwapStep names one step of a memory change, as its progress reports it.
type SwapStep string

const (
	SwapStepWriteConfig         SwapStep = "write_config"
	SwapStepStartZram           SwapStep = "start_zram"
	SwapStepApplySysctls        SwapStep = "apply_sysctls"
	SwapStepDisableZswap        SwapStep = "disable_zswap"
	SwapStepStopZram            SwapStep = "stop_zram"
	SwapStepEnableZswap         SwapStep = "enable_zswap"
	SwapStepDrainDevice         SwapStep = "drain_device"
	SwapStepCreateSwapFile      SwapStep = "create_swap_file"
	SwapStepMakeTemporarySwap   SwapStep = "make_temporary_swap"
	SwapStepResizeSwapFile      SwapStep = "resize_swap_file"
	SwapStepRemoveTemporarySwap SwapStep = "remove_temporary_swap"
	SwapStepRemoveSwapFile      SwapStep = "remove_swap_file"
)

// SwapSteps lists every step, in the order a run can take them.
var SwapSteps = []SwapStep{
	SwapStepWriteConfig, SwapStepCreateSwapFile, SwapStepStartZram, SwapStepStopZram, SwapStepApplySysctls,
	SwapStepDisableZswap, SwapStepEnableZswap, SwapStepMakeTemporarySwap, SwapStepResizeSwapFile,
	SwapStepRemoveTemporarySwap, SwapStepRemoveSwapFile, SwapStepDrainDevice,
}

var swapStepMessages = map[SwapStep]string{
	SwapStepWriteConfig:         "Writing the zram and sysctl configuration",
	SwapStepStartZram:           "Starting zram",
	SwapStepApplySysctls:        "Applying the memory sysctls",
	SwapStepDisableZswap:        "Turning zswap off",
	SwapStepStopZram:            "Stopping zram",
	SwapStepEnableZswap:         "Turning zswap on",
	SwapStepDrainDevice:         "Moving swapped data back into memory",
	SwapStepCreateSwapFile:      "Creating the swap file",
	SwapStepMakeTemporarySwap:   "Adding a temporary swap file",
	SwapStepResizeSwapFile:      "Resizing the swap file",
	SwapStepRemoveTemporarySwap: "Removing the temporary swap file",
	SwapStepRemoveSwapFile:      "Removing the swap file",
}

// SwapProgress is one step starting: its 1-based Index of Total, and the
// swap device or swap file the step acts on, when it acts on one.
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
//
// ConfirmRemove is, for zram, the paths of the swap files the user agreed
// the change removes: every row of SwapStatus.SwapFiles whose LockedReason
// is empty, in any order. The change is refused when they are not the files
// it would remove.
//
// SwapFileSize is, for disk and zswap on a host with no active disk swap and
// no swap file in /etc/fstab, the size in bytes of the swap file the change
// creates at SwapStatus.Create.Path; 0 is SwapStatus.Create.DefaultSize.
type SwapModeRequest struct {
	ConfirmRemove  []string `json:"confirm_remove,omitzero"`
	ConfirmReplace string   `json:"confirm_replace,omitzero"`
	Mode           SwapMode `json:"mode"`
	SwapFileSize   uint64   `json:"swap_file_size,omitzero"`
}

// SwapFileCreateRequest is memory.create_swap_file's request: the size in
// bytes, a whole number of MiB from SwapStatus.Create.MinSize to its
// MaxSize, of the swap file made at SwapStatus.Create.Path.
type SwapFileCreateRequest struct {
	Size uint64 `json:"size"`
}

// SwapFileResizeRequest is memory.resize_swap_file's request. Path selects
// the row of SwapStatus.SwapFiles with that exact path; Size is the new size
// in bytes, a whole number of MiB from SwapStatus.Create.MinSize to the
// row's MaxSize.
type SwapFileResizeRequest struct {
	Path string `json:"path"`
	Size uint64 `json:"size"`
}

// SwapFilePathRequest is the request of memory.remove_swap_file and
// memory.move_swap_file. Path selects the row of SwapStatus.SwapFiles with
// that exact path.
type SwapFilePathRequest struct {
	Path string `json:"path"`
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

// SwapFileLockedReason says why LinuxIO does not resize, remove, drain or
// move a swap file, or is empty when it does.
type SwapFileLockedReason string

const (
	// SwapFileNotInFstab: the file swaps and no /etc/fstab line names it by
	// its exact path, so something else turns it on.
	SwapFileNotInFstab SwapFileLockedReason = "not_in_fstab"
	// SwapFileNotRootOnly: a folder on the path is a link or can be changed
	// by users other than root, or the file is not a regular file root owns.
	SwapFileNotRootOnly SwapFileLockedReason = "not_root_only"
	// SwapFileHibernation: the host resumes from this filesystem at a file
	// offset, so the file may be its hibernation file.
	SwapFileHibernation SwapFileLockedReason = "hibernation"
)

// SwapFileResizeReason says why a swap file LinuxIO acts on cannot be
// resized, or is empty when it can. The first three also say what the row
// reads: "missing", "not a swap file" and "off".
type SwapFileResizeReason string

const (
	// SwapFileResizeMissing: /etc/fstab names the file and nothing is at its
	// path. Remove takes the fstab line away.
	SwapFileResizeMissing SwapFileResizeReason = "missing"
	// SwapFileResizeNoHeader: the file is off and holds no swap signature.
	SwapFileResizeNoHeader SwapFileResizeReason = "no_swap_header"
	// SwapFileResizeOff: the file is in /etc/fstab and does not swap.
	SwapFileResizeOff                   SwapFileResizeReason = "off"
	SwapFileResizeUnsupportedFilesystem SwapFileResizeReason = "unsupported_filesystem"
	SwapFileResizeReadOnly              SwapFileResizeReason = "read_only"
	SwapFileResizeBtrfsLayout           SwapFileResizeReason = "btrfs_layout"
)

// SwapFileRow is one swap file: active in /proc/swaps, named by /etc/fstab
// by a plain path, or both. Sizes are bytes.
//
// Resize, Remove and a drain are offered only while LockedReason is empty,
// and Resize only while ResizeBlockedReason is empty too. Size is the swap
// size for an active file and the file's length otherwise; MaxSize is the
// largest size a resize may ask for and FreeSpace the free space of the
// file's filesystem. BlocksSnapshots is set for a file on btrfs outside
// LinuxIO's folder, which blocks snapshots of the subvolume it is in while
// it swaps; CanMove is set when memory.move_swap_file is offered for it.
// Temporary is set for a temporary swap file a resize left behind swapping
// (its path ends in ".linuxio-resize"), which has Remove only.
type SwapFileRow struct {
	Active              bool                 `json:"active"`
	BlocksSnapshots     bool                 `json:"blocks_snapshots,omitzero"`
	CanMove             bool                 `json:"can_move,omitzero"`
	DiskHint            SwapDiskHint         `json:"disk_hint,omitzero"`
	FreeSpace           uint64               `json:"free_space"`
	FSType              string               `json:"fs_type"`
	InFstab             bool                 `json:"in_fstab"`
	LockedReason        SwapFileLockedReason `json:"locked_reason"`
	MaxSize             uint64               `json:"max_size"`
	Path                string               `json:"path"`
	ResizeBlockedReason SwapFileResizeReason `json:"resize_blocked_reason"`
	Size                uint64               `json:"size"`
	Temporary           bool                 `json:"temporary,omitzero"`
	Used                uint64               `json:"used"`
}

// SwapFileCreateReason says why LinuxIO cannot create its swap file, or is
// empty when it can.
type SwapFileCreateReason string

const (
	// SwapFileCreateExists: LinuxIO's swap file is there already, as a row.
	SwapFileCreateExists                SwapFileCreateReason = "exists"
	SwapFileCreateUnsupportedFilesystem SwapFileCreateReason = "unsupported_filesystem"
	SwapFileCreateReadOnly              SwapFileCreateReason = "read_only"
	// SwapFileCreateBtrfsLayout: the btrfs filesystem has more than one
	// device or data that is not single.
	SwapFileCreateBtrfsLayout SwapFileCreateReason = "btrfs_layout"
	// SwapFileCreateNotEnoughSpace: the filesystem cannot hold MinSize and
	// still leave Reserve free.
	SwapFileCreateNotEnoughSpace SwapFileCreateReason = "not_enough_space"
	// SwapFileCreateFolderNotRootOnly: a folder on Path is a link or can be
	// changed by users other than root.
	SwapFileCreateFolderNotRootOnly SwapFileCreateReason = "folder_not_root_only"
	// SwapFileCreatePathTaken: something that is not LinuxIO's swap file is
	// at Path, or LinuxIO's folder holds something else.
	SwapFileCreatePathTaken SwapFileCreateReason = "path_taken"
	// SwapFileCreateHibernation: the host resumes at a file offset from the
	// device Path's filesystem is on, where every swap file is locked, so
	// the new file would be one LinuxIO cannot resize or remove.
	SwapFileCreateHibernation SwapFileCreateReason = "hibernation"
)

// SwapFileCreate is what the Memory dialog needs to offer "Create a swap
// file": the one path LinuxIO makes a swap file at, the sizes in bytes a
// request may ask for (MinSize to MaxSize, DefaultSize as the starting
// value), the filesystem's free space and the space a new file must leave
// free (Reserve), and why Create is not offered.
type SwapFileCreate struct {
	BlockedReason SwapFileCreateReason `json:"blocked_reason"`
	DefaultSize   uint64               `json:"default_size"`
	FreeSpace     uint64               `json:"free_space"`
	FSType        string               `json:"fs_type"`
	MaxSize       uint64               `json:"max_size"`
	MinSize       uint64               `json:"min_size"`
	Path          string               `json:"path"`
	Reserve       uint64               `json:"reserve"`
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
// its configuration files other than LinuxIO's define. Devices lists every
// active swap device; SwapFiles lists every swap file, active or named by
// /etc/fstab: the active ones in the order of Devices, then the others in
// fstab order.
type SwapStatus struct {
	CanDrain              bool                   `json:"can_drain"`
	Create                SwapFileCreate         `json:"create"`
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
	SwapFiles             []SwapFileRow          `json:"swap_files"`
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
