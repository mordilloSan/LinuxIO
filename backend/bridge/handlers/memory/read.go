package memory

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

// readSwaps parses /proc/swaps: a header line, then one line per active
// device with its path, type, size and used in KiB, and priority.
func (h hostRoot) readSwaps() ([]apischema.SwapDevice, error) {
	data, err := os.ReadFile(h.path(procSwaps))
	if err != nil {
		return nil, fmt.Errorf("read swap devices: %w", err)
	}
	var devices []apischema.SwapDevice
	lines := strings.Split(string(data), "\n")
	for i, line := range lines[1:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		device, err := parseSwapLine(line)
		if err != nil {
			return nil, fmt.Errorf("parse %s line %d: %w", procSwaps, i+2, err)
		}
		devices = append(devices, device)
	}
	return devices, nil
}

func parseSwapLine(line string) (apischema.SwapDevice, error) {
	fields := strings.Fields(line)
	if len(fields) != 5 {
		return apischema.SwapDevice{}, fmt.Errorf("want 5 fields, got %d", len(fields))
	}
	device := apischema.SwapDevice{Path: unescapeOctal(fields[0])}
	switch fields[1] {
	case "file":
		device.Kind = apischema.SwapDeviceFile
	case "partition":
		device.Kind = apischema.SwapDevicePartition
		if isZramDevice(device.Path) {
			device.Kind = apischema.SwapDeviceZram
		}
	default:
		return apischema.SwapDevice{}, fmt.Errorf("unknown swap type %q", fields[1])
	}
	size, err := strconv.ParseUint(fields[2], 10, 64)
	if err != nil {
		return apischema.SwapDevice{}, fmt.Errorf("size: %w", err)
	}
	used, err := strconv.ParseUint(fields[3], 10, 64)
	if err != nil {
		return apischema.SwapDevice{}, fmt.Errorf("used: %w", err)
	}
	device.Priority, err = strconv.Atoi(fields[4])
	if err != nil {
		return apischema.SwapDevice{}, fmt.Errorf("priority: %w", err)
	}
	device.Size, device.Used = size*1024, used*1024
	return device, nil
}

// isZramDevice reports whether path names a zram block device, /dev/zram<N>.
func isZramDevice(path string) bool {
	name, ok := strings.CutPrefix(path, "/dev/")
	return ok && isZramName(name)
}

// isZramName reports whether name is a zram block device name, zram<N>.
func isZramName(name string) bool {
	n, ok := strings.CutPrefix(name, "zram")
	if !ok || n == "" {
		return false
	}
	_, err := strconv.ParseUint(n, 10, 32)
	return err == nil
}

// memInfo is what the status takes from /proc/meminfo, in bytes.
type memInfo struct {
	memTotal, memAvailable, swapTotal, swapFree uint64
}

// readMeminfo parses the fields of /proc/meminfo the status needs; each is
// required.
func (h hostRoot) readMeminfo() (memInfo, error) {
	data, err := os.ReadFile(h.path(procMeminfo))
	if err != nil {
		return memInfo{}, fmt.Errorf("read memory usage: %w", err)
	}
	var info memInfo
	missing := map[string]*uint64{
		"MemTotal": &info.memTotal, "MemAvailable": &info.memAvailable,
		"SwapTotal": &info.swapTotal, "SwapFree": &info.swapFree,
	}
	for line := range strings.Lines(string(data)) {
		key, rest, _ := strings.Cut(line, ":")
		dst, wanted := missing[key]
		if !wanted {
			continue
		}
		value := strings.Fields(rest)
		if len(value) != 2 || value[1] != "kB" {
			return memInfo{}, fmt.Errorf("parse %s %s: want a size in kB, got %q", procMeminfo, key, strings.TrimSpace(rest))
		}
		kb, err := strconv.ParseUint(value[0], 10, 64)
		if err != nil {
			return memInfo{}, fmt.Errorf("parse %s %s: %w", procMeminfo, key, err)
		}
		*dst = kb * 1024
		delete(missing, key)
	}
	if len(missing) > 0 {
		return memInfo{}, fmt.Errorf("parse %s: missing %s", procMeminfo, strings.Join(slices.Sorted(maps.Keys(missing)), ", "))
	}
	return info, nil
}

// readVMSysctls reads the four vm sysctls into status.
func (h hostRoot) readVMSysctls(status *apischema.SwapStatus) error {
	for name, dst := range map[string]*int{
		sysctlSwappiness:           &status.Swappiness,
		sysctlPageCluster:          &status.PageCluster,
		sysctlWatermarkBoostFactor: &status.WatermarkBoostFactor,
		sysctlWatermarkScaleFactor: &status.WatermarkScaleFactor,
	} {
		value, err := h.readOptionalInt(filepath.Join(vmSysctlDir, name))
		if err != nil {
			return err
		}
		*dst = value
	}
	return nil
}

// readZswap reads the zswap module parameters. A kernel built without zswap
// has no parameters directory and reports zswap as not available.
func (h hostRoot) readZswap() (apischema.ZswapStatus, error) {
	if _, err := os.Stat(h.path(zswapParamDir)); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return apischema.ZswapStatus{}, nil
		}
		return apischema.ZswapStatus{}, err
	}
	status := apischema.ZswapStatus{Available: true}
	enabled, _, err := h.readOptional(zswapEnabledParam)
	if err != nil {
		return apischema.ZswapStatus{}, err
	}
	status.Enabled = strings.TrimSpace(enabled) == "Y"
	compressor, _, err := h.readOptional(zswapCompressorParam)
	if err != nil {
		return apischema.ZswapStatus{}, err
	}
	status.Compressor = strings.TrimSpace(compressor)
	status.MaxPoolPercent, err = h.readOptionalInt(filepath.Join(zswapParamDir, "max_pool_percent"))
	if err != nil {
		return apischema.ZswapStatus{}, err
	}
	return status, nil
}

// readZramDevices reads every zram device under /sys/block, configured or
// not. A device that leaves while it is read, as when zram's unit stops and
// resets it, is skipped: a file it had open fails its read with ENODEV, and
// one opened after it left is missing, which reads as zero, so a device whose
// directory is gone once it was read is skipped too.
func (h hostRoot) readZramDevices() ([]apischema.ZramDevice, error) {
	entries, err := os.ReadDir(h.path(sysBlockDir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list block devices: %w", err)
	}
	var devices []apischema.ZramDevice
	for _, entry := range entries {
		if !isZramName(entry.Name()) {
			continue
		}
		device, err := h.readZramDevice(entry.Name())
		if errors.Is(err, syscall.ENODEV) || !h.exists(filepath.Join(sysBlockDir, entry.Name())) {
			continue
		}
		if err != nil {
			return nil, err
		}
		devices = append(devices, device)
	}
	return devices, nil
}

// readZramDevice reads one zram device's disksize and the first three fields
// of its mm_stat: orig_data_size, compr_data_size and mem_used_total.
func (h hostRoot) readZramDevice(name string) (apischema.ZramDevice, error) {
	dir := filepath.Join(sysBlockDir, name)
	disksize, err := h.readOptionalUint(filepath.Join(dir, "disksize"))
	if err != nil {
		return apischema.ZramDevice{}, err
	}
	device := apischema.ZramDevice{Name: name, Disksize: disksize}
	statPath := filepath.Join(dir, "mm_stat")
	text, ok, err := h.readOptional(statPath)
	if err != nil || !ok {
		return device, err
	}
	fields := strings.Fields(text)
	if len(fields) < 3 {
		return apischema.ZramDevice{}, fmt.Errorf("parse %s: want at least 3 fields, got %d", statPath, len(fields))
	}
	sizes := make([]uint64, 3)
	for i := range sizes {
		if sizes[i], err = strconv.ParseUint(fields[i], 10, 64); err != nil {
			return apischema.ZramDevice{}, fmt.Errorf("parse %s: %w", statPath, err)
		}
	}
	device.OriginalSize, device.CompressedSize, device.MemUsed = sizes[0], sizes[1], sizes[2]
	return device, nil
}

// fstabSwapFile returns the swap file among the fstab swap entries, one
// whose source is a path outside /dev, that LinuxIO manages: the first that
// is active or whose unit is held off at boot, or when there is none, the
// first. A held-off unit keeps the choice where it was after a switch to
// zram stopped the file and held it off, so the way back starts that one,
// and a later switch does not move on to another. Any other is left as it
// is.
func fstabSwapFile(entries []fstabSwap) (fstabSwap, bool) {
	first, found := fstabSwap{}, false
	for _, entry := range entries {
		if !isSwapFilePath(entry.path) {
			continue
		}
		if entry.device != "" || entry.heldOff {
			return entry, true
		}
		if !found {
			first, found = entry, true
		}
	}
	return first, found
}

// isSwapFilePath reports whether an fstab swap path names a file rather
// than a block device.
func isSwapFilePath(path string) bool {
	return strings.HasPrefix(path, "/") && !strings.HasPrefix(path, "/dev/")
}

// mountEntry is one mount from mountinfo.
type mountEntry struct {
	mountPoint, fsType, source string
}

// readMounts reads the bridge's mount table, or none when it is missing.
func (h hostRoot) readMounts() ([]mountEntry, error) {
	text, _, err := h.readOptional(procMountinfo)
	if err != nil {
		return nil, err
	}
	var mounts []mountEntry
	for line := range strings.Lines(text) {
		// The optional fields end at "-", followed by the filesystem type,
		// the source and the superblock options.
		fields := strings.Fields(line)
		sep := slices.Index(fields, "-")
		if sep < 6 || len(fields) < sep+3 {
			continue
		}
		mounts = append(mounts, mountEntry{
			mountPoint: unescapeOctal(fields[4]),
			fsType:     fields[sep+1],
			source:     unescapeOctal(fields[sep+2]),
		})
	}
	return mounts, nil
}

// mountHolding returns the mount that holds path: the one with the longest
// mount point above it, and of mounts stacked on that point the last, which
// is the one visible.
func mountHolding(mounts []mountEntry, path string) (mountEntry, bool) {
	var best mountEntry
	found := false
	for _, m := range mounts {
		if !underMountPoint(path, m.mountPoint) {
			continue
		}
		if !found || len(m.mountPoint) >= len(best.mountPoint) {
			best, found = m, true
		}
	}
	return best, found
}

func underMountPoint(path, mountPoint string) bool {
	if mountPoint == "/" {
		return strings.HasPrefix(path, "/")
	}
	rest, ok := strings.CutPrefix(path, mountPoint)
	return ok && (rest == "" || rest[0] == '/')
}

// diskHint flags a mount on a disk that swapping wears out, by its source: an
// SD card or eMMC, by its name, or a disk the kernel marks removable, which
// is what a USB stick or a card reader reports. A USB disk enclosure reports
// itself as fixed and is not flagged, and neither is a device-mapper volume.
func (h hostRoot) diskHint(mount mountEntry) apischema.SwapDiskHint {
	// Any user can make a FUSE mount through the setuid fusermount and name any
	// source for it, while fuseblk's source is the block device the kernel opens.
	if mount.fsType == "fuse" || strings.HasPrefix(mount.fsType, "fuse.") {
		return ""
	}
	name, ok := strings.CutPrefix(mount.source, "/dev/")
	switch {
	case !ok || strings.Contains(name, "/"):
		return ""
	case strings.HasPrefix(name, "mmcblk"):
		return apischema.SwapDiskHintMMC
	case h.removable(name):
		return apischema.SwapDiskHintRemovable
	}
	return ""
}

// removable reports whether the disk of block device name has the kernel's
// removable flag set. /sys/class/block/<name> links to the device's own
// directory, which for a partition lies inside its disk's.
func (h hostRoot) removable(name string) bool {
	dir, err := filepath.EvalSymlinks(h.path(filepath.Join(sysClassBlockDir, name)))
	if err != nil {
		return false
	}
	if _, statErr := os.Stat(filepath.Join(dir, "partition")); statErr == nil {
		dir = filepath.Dir(dir)
	}
	flag, err := os.ReadFile(filepath.Join(dir, "removable"))
	return err == nil && strings.TrimSpace(string(flag)) == "1"
}

// swapSignatures are what mkswap writes in the last ten bytes of a swap
// area's first page.
var swapSignatures = []string{"SWAPSPACE2", "SWAP-SPACE"}

// holdsSwap reports whether the block device or file name starts with a
// swap area, by its signature. One too short to hold a page does not.
func (h hostRoot) holdsSwap(name string) (bool, error) {
	f, err := os.Open(h.path(name))
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()
	signature := make([]byte, 10)
	if _, err := f.ReadAt(signature, int64(os.Getpagesize()-len(signature))); err != nil {
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		return false, err
	}
	return slices.Contains(swapSignatures, string(signature)), nil
}
