package memory

import (
	"slices"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

// writeVMSysctls writes the four vm sysctls the status reads.
func (f fixture) writeVMSysctls(swappiness, pageCluster, boost, scale string) {
	f.t.Helper()
	f.write("/proc/sys/vm/swappiness", swappiness+"\n")
	f.write("/proc/sys/vm/page-cluster", pageCluster+"\n")
	f.write("/proc/sys/vm/watermark_boost_factor", boost+"\n")
	f.write("/proc/sys/vm/watermark_scale_factor", scale+"\n")
}

// A desktop with an active swap file on a btrfs root, zswap on, a zram device
// configured but not swapping, and zram-tools missing.
func TestReadSwapStatusOfASwapFileOnBtrfs(t *testing.T) {
	f := newFixture(t)
	f.write("/proc/swaps", swapsHeader+"/swap.img                               file\t\t2097148\t\t1048576\t\t-2\n")
	f.write("/proc/meminfo", meminfoText)
	f.write("/proc/self/mountinfo", mountinfoText)
	f.write("/etc/fstab", "UUID=0a1b2c3d / btrfs defaults,subvol=@ 0 1\n/swap.img none swap sw 0 0\n")
	f.writeVMSysctls("60", "3", "15000", "10")
	f.write("/sys/module/zswap/parameters/enabled", "Y\n")
	f.write("/sys/module/zswap/parameters/compressor", "zstd\n")
	f.write("/sys/module/zswap/parameters/max_pool_percent", "20\n")
	f.write("/sys/block/zram0/disksize", "0\n")
	f.write("/sys/block/zram0/mm_stat", "0 0 0 0 0 0 0 0 0\n")

	got, _, err := f.host().readSwapStatus("")
	if err != nil {
		t.Fatal(err)
	}

	wantDevices := []apischema.SwapDevice{{
		Path: "/swap.img", Kind: apischema.SwapDeviceFile, Size: 2097148 * kib, Used: 1 * gib, Priority: -2, FSType: "btrfs",
	}}
	if !slices.Equal(got.Devices, wantDevices) {
		t.Errorf("devices = %+v, want %+v", got.Devices, wantDevices)
	}
	f.useFolderFS(folderFS{kind: "btrfs", btrfsLayout: true, total: 100 * gib, free: 40 * gib})
	f.writeSwapFile("/swap.img", 2*gib)
	got, _, err = f.host().readSwapStatus("")
	if err != nil {
		t.Fatal(err)
	}
	// The only swap device: a resize makes a temporary file of the data
	// plus a quarter, 1.25 GiB, so the largest size is the file's 2 GiB and
	// the 40 GiB free less that and the 5 GiB reserve. It is on the
	// filesystem LinuxIO's folder would be on, outside a subvolume of its
	// own, so it can move there.
	wantFiles := []apischema.SwapFileRow{{
		Path: "/swap.img", Active: true, InFstab: true, FSType: "btrfs", Size: 2097148 * kib, Used: 1 * gib,
		FreeSpace: 40 * gib, MaxSize: 35*gib + 768*mib, BlocksSnapshots: true, CanMove: true,
	}}
	if !slices.Equal(got.SwapFiles, wantFiles) {
		t.Errorf("swap files = %+v, want %+v", got.SwapFiles, wantFiles)
	}
	if got.MemTotal != 16308408*kib || got.MemAvailable != 8154204*kib || got.SwapTotal != 2097148*kib || got.SwapUsed != 1048576*kib {
		t.Errorf("memory = total %d available %d swap total %d used %d", got.MemTotal, got.MemAvailable, got.SwapTotal, got.SwapUsed)
	}
	if got.Swappiness != 60 || got.PageCluster != 3 || got.WatermarkBoostFactor != 15000 || got.WatermarkScaleFactor != 10 {
		t.Errorf("sysctls = swappiness %d page-cluster %d boost %d scale %d", got.Swappiness, got.PageCluster, got.WatermarkBoostFactor, got.WatermarkScaleFactor)
	}
	wantZswap := apischema.ZswapStatus{Available: true, Enabled: true, Compressor: "zstd", MaxPoolPercent: 20}
	if got.Zswap != wantZswap {
		t.Errorf("zswap = %+v, want %+v", got.Zswap, wantZswap)
	}
	if want := []apischema.ZramDevice{{Name: "zram0"}}; !slices.Equal(got.ZramDevices, want) {
		t.Errorf("zram devices = %+v, want %+v", got.ZramDevices, want)
	}
	if got.Mode != apischema.SwapModeZswap || got.RecommendedMode != apischema.SwapModeZram || got.RecommendedSwappiness != 60 {
		t.Errorf("mode = %q, recommended = %q with swappiness %d; want zswap, zram, 60", got.Mode, got.RecommendedMode, got.RecommendedSwappiness)
	}
	if got.ZramAvailable || got.ZramUnavailableReason != apischema.ZramPackageMissing {
		t.Errorf("zram available = %v, reason = %q; want false, package_missing", got.ZramAvailable, got.ZramUnavailableReason)
	}
	if want := uint64(16308408 * kib / 2); got.ZramPlannedSize != want {
		t.Errorf("zram planned size = %d, want %d", got.ZramPlannedSize, want)
	}
	if !got.CanDrain || got.DrainBlockedReason != "" {
		t.Errorf("can drain = %v, reason = %q; want true with no reason", got.CanDrain, got.DrainBlockedReason)
	}
	if len(got.Presets) != 3 {
		t.Errorf("presets = %+v, want the three selectable modes", got.Presets)
	}
}

// Data parked in zram is already in memory, so only disk swap counts as
// parked; the swap file fstab names is reported though it is not active.
func TestReadSwapStatusOfZramWithAnInactiveSwapFile(t *testing.T) {
	f := newFixture(t)
	f.write("/proc/swaps", swapsHeader+"/dev/zram0                              partition\t4194300\t\t2097152\t\t100\n")
	f.write("/proc/meminfo", meminfoText)
	f.write("/proc/self/mountinfo", mountinfoText)
	f.write("/etc/fstab", "/mnt/sd/swapfile none swap sw 0 0\n")
	f.writeVMSysctls("180", "0", "0", "125")
	f.write("/sys/block/zram0/disksize", "4294967296\n")
	f.write("/sys/block/zram0/mm_stat", "2147483648 715827882 760000000 0 760000000 0 0 0 0\n")

	got, _, err := f.host().readSwapStatus(apischema.ZramProviderTools)
	if err != nil {
		t.Fatal(err)
	}
	wantFiles := []apischema.SwapFileRow{{
		Path: "/mnt/sd/swapfile", InFstab: true, FSType: "ext4", DiskHint: apischema.SwapDiskHintMMC,
		FreeSpace: 50 * gib, ResizeBlockedReason: apischema.SwapFileResizeMissing,
	}}
	if !slices.Equal(got.SwapFiles, wantFiles) {
		t.Errorf("swap files = %+v, want %+v", got.SwapFiles, wantFiles)
	}
	if got.Devices[0].FSType != "" || got.Devices[0].DiskHint != "" {
		t.Errorf("zram device = %+v, want no filesystem or disk hint", got.Devices[0])
	}
	if got.Mode != apischema.SwapModeZram || got.RecommendedMode != apischema.SwapModeZram || got.RecommendedSwappiness != 180 {
		t.Errorf("mode = %q, recommended = %q with swappiness %d; want zram, zram, 180", got.Mode, got.RecommendedMode, got.RecommendedSwappiness)
	}
	if got.CanDrain || got.DrainBlockedReason != apischema.SwapDrainNothingParked {
		t.Errorf("can drain = %v, reason = %q; want false, nothing_parked", got.CanDrain, got.DrainBlockedReason)
	}
	if !got.ZramAvailable || got.ZramUnavailableReason != "" {
		t.Errorf("zram available = %v, reason = %q; want true with no reason", got.ZramAvailable, got.ZramUnavailableReason)
	}
	want := apischema.ZramDevice{Name: "zram0", Disksize: 4 * gib, OriginalSize: 2 * gib, CompressedSize: 715827882, MemUsed: 760000000}
	if len(got.ZramDevices) != 1 || got.ZramDevices[0] != want {
		t.Errorf("zram devices = %+v, want %+v", got.ZramDevices, want)
	}
}

// Every swap file is a row: the active ones in the order of /proc/swaps,
// then the ones fstab names that are off, in fstab order.
func TestReadSwapStatusListsEverySwapFile(t *testing.T) {
	f := newFixture(t)
	f.write("/proc/swaps", swapsHeader+swapsLine("/swap2.img", gib, 0)+swapFileSwapsLine)
	f.write("/proc/meminfo", meminfoText)
	f.write("/etc/fstab", "/old.img none swap sw 0 0\n/swap.img none swap sw 0 0\n/swap2.img none swap sw 0 0\n")
	f.writeSwapFile("/swap.img", 2*gib)
	f.writeSwapFile("/swap2.img", gib)
	f.writeSwapFile("/old.img", gib)

	got, _, err := f.host().readSwapStatus(apischema.ZramProviderTools)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, row := range got.SwapFiles {
		paths = append(paths, row.Path)
	}
	if want := []string{"/swap2.img", "/swap.img", "/old.img"}; !slices.Equal(paths, want) {
		t.Fatalf("swap files = %v, want %v", paths, want)
	}
	if off := got.SwapFiles[2]; off.Active || off.Size != gib || off.ResizeBlockedReason != apischema.SwapFileResizeOff || off.LockedReason != "" {
		t.Errorf("off file = %+v, want off, 1 GiB, removable", off)
	}
}

// Any user can make a FUSE mount through the setuid fusermount and name any
// source, so one gives no disk hint. fuseblk's source is the block device
// the kernel opens, so it does.
func TestReadSwapStatusGivesNoDiskHintUnderAFuseMount(t *testing.T) {
	for fsType, want := range map[string]apischema.SwapDiskHint{
		"fuse":        "",
		"fuse.sshfs":  "",
		"fuseblk":     apischema.SwapDiskHintMMC,
		"fuseblk.foo": apischema.SwapDiskHintMMC,
	} {
		t.Run(fsType, func(t *testing.T) {
			f := newFixture(t)
			f.write("/proc/swaps", swapsHeader+"/mnt/user/swapfile                      file\t\t1048572\t\t0\t\t-2\n")
			f.write("/proc/meminfo", meminfoText)
			f.write("/proc/self/mountinfo", mountinfoText+"40 22 0:50 / /mnt/user rw,nosuid,nodev,relatime shared:9 - "+fsType+" /dev/mmcblk0p2 rw\n")
			f.write("/etc/fstab", "/mnt/user/swapfile none swap sw 0 0\n")

			got, _, err := f.host().readSwapStatus(apischema.ZramProviderTools)
			if err != nil {
				t.Fatal(err)
			}
			if got.SwapFiles[0].DiskHint != want || got.Devices[0].DiskHint != want {
				t.Errorf("swap file = %+v, device = %+v; want disk hint %q", got.SwapFiles[0], got.Devices[0], want)
			}
		})
	}
}

// An active swap file fstab does not name is a row LinuxIO does not change;
// a swap partition is no row at all, and zram can be chosen beside it.
func TestReadSwapStatusFallsBackToTheActiveSwapFile(t *testing.T) {
	f := newFixture(t)
	f.write("/proc/swaps", swapsHeader+
		"/dev/sda3                               partition\t8388604\t\t0\t\t-3\n"+
		"/home/swapfile                          file\t\t1048572\t\t0\t\t-2\n")
	f.write("/proc/meminfo", meminfoText)
	f.write("/proc/self/mountinfo", mountinfoText)
	f.write("/etc/fstab", "/dev/sda3 none swap sw 0 0\n")
	f.write("/sys/module/zswap/parameters/enabled", "N\n")
	f.write("/sys/module/zswap/parameters/compressor", "lzo\n")
	f.write("/sys/module/zswap/parameters/max_pool_percent", "20\n")

	got, _, err := f.host().readSwapStatus(apischema.ZramProviderTools)
	if err != nil {
		t.Fatal(err)
	}
	wantFiles := []apischema.SwapFileRow{{
		Path: "/home/swapfile", Active: true, FSType: "ext4", Size: 1048572 * kib, FreeSpace: 50 * gib,
		LockedReason: apischema.SwapFileNotInFstab,
	}}
	if !slices.Equal(got.SwapFiles, wantFiles) {
		t.Errorf("swap files = %+v, want %+v", got.SwapFiles, wantFiles)
	}
	if got.Devices[0].FSType != "" || got.Devices[1].FSType != "ext4" {
		t.Errorf("devices = %+v, want a filesystem on the file only", got.Devices)
	}
	if got.Mode != apischema.SwapModeDisk || got.RecommendedMode != apischema.SwapModeZswap {
		t.Errorf("mode = %q, recommended = %q; want disk, zswap", got.Mode, got.RecommendedMode)
	}
	if got.ZramUnavailableReason != "" {
		t.Errorf("zram unavailable reason = %q, want none", got.ZramUnavailableReason)
	}
	if got.DrainBlockedReason != apischema.SwapDrainNothingParked {
		t.Errorf("drain reason = %q, want nothing_parked", got.DrainBlockedReason)
	}
}

// Only /proc/swaps and /proc/meminfo are required: a host without zswap,
// zram, fstab, mountinfo or the vm sysctls still reports its status.
func TestReadSwapStatusOfAMinimalHost(t *testing.T) {
	f := newFixture(t)
	f.write("/proc/swaps", swapsHeader)
	f.write("/proc/meminfo", "MemTotal: 2097152 kB\nMemAvailable: 1048576 kB\nSwapTotal: 0 kB\nSwapFree: 0 kB\n")

	got, _, err := f.host().readSwapStatus(apischema.ZramProviderTools)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Devices) != 0 || len(got.ZramDevices) != 0 || got.Zswap != (apischema.ZswapStatus{}) {
		t.Errorf("status = %+v, want no devices, swap file, zram or zswap", got)
	}
	if got.Swappiness != 0 || got.PageCluster != 0 {
		t.Errorf("sysctls = swappiness %d page-cluster %d, want zero when missing", got.Swappiness, got.PageCluster)
	}
	if got.Mode != apischema.SwapModeNone || got.RecommendedMode != apischema.SwapModeZram || got.RecommendedSwappiness != 180 {
		t.Errorf("mode = %q, recommended = %q with swappiness %d; want none, zram, 180", got.Mode, got.RecommendedMode, got.RecommendedSwappiness)
	}
	if got.ZramPlannedSize != 1*gib || got.DrainBlockedReason != apischema.SwapDrainNothingParked {
		t.Errorf("zram planned size = %d, drain reason = %q; want 1 GiB, nothing_parked", got.ZramPlannedSize, got.DrainBlockedReason)
	}
	// An empty list, not null on the wire.
	if got.SwapFiles == nil || len(got.SwapFiles) != 0 {
		t.Errorf("swap files = %#v, want an empty list", got.SwapFiles)
	}
	want := apischema.SwapFileCreate{
		Path: ownSwapFilePath, FSType: "ext4", MinSize: 256 * mib, DefaultSize: gib,
		FreeSpace: 50 * gib, Reserve: 5 * gib, MaxSize: 45 * gib,
	}
	if got.Create != want {
		t.Errorf("create = %+v, want %+v", got.Create, want)
	}
}

func TestReadSwapStatusRefusesBrokenRequiredFiles(t *testing.T) {
	tests := map[string]func(f fixture){
		"missing meminfo": func(f fixture) {
			f.write("/proc/swaps", swapsHeader)
		},
		"missing swaps": func(f fixture) {
			f.write("/proc/meminfo", meminfoText)
		},
		"malformed swaps": func(f fixture) {
			f.write("/proc/swaps", swapsHeader+"/swap.img file 1024\n")
			f.write("/proc/meminfo", meminfoText)
		},
		"malformed sysctl": func(f fixture) {
			f.write("/proc/swaps", swapsHeader)
			f.write("/proc/meminfo", meminfoText)
			f.write("/proc/sys/vm/swappiness", "sixty\n")
		},
	}
	for name, setup := range tests {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			setup(f)
			if got, _, err := f.host().readSwapStatus(apischema.ZramProviderTools); err == nil {
				t.Fatalf("status = %+v, want an error", got)
			}
		})
	}
}

// The mode is what the host does now. Whether the next start does the same
// is read from LinuxIO's zswap file and, without one, the command line.
func TestReadSwapStatusSaysWhetherTheModeIsSavedForBoot(t *testing.T) {
	tests := []struct {
		name     string
		enabled  string
		tmpfiles string
		cmdline  string
		mode     apischema.SwapMode
		saved    bool
	}{
		{name: "disk on a host LinuxIO never changed", enabled: "N", mode: apischema.SwapModeDisk, saved: true},
		{name: "zswap turned on by hand", enabled: "Y", mode: apischema.SwapModeZswap},
		{name: "zswap LinuxIO applied", enabled: "Y", tmpfiles: string(renderZswapTmpfiles("zstd")), mode: apischema.SwapModeZswap, saved: true},
		{name: "zswap from the command line", enabled: "Y", cmdline: "ro zswap.enabled=1\n", mode: apischema.SwapModeZswap, saved: true},
		{name: "zswap on over LinuxIO's file that keeps it off", enabled: "Y", tmpfiles: string(renderZswapOffTmpfiles()), cmdline: "ro zswap.enabled=1\n", mode: apischema.SwapModeZswap},
		{name: "zswap turned off by hand", enabled: "N", tmpfiles: string(renderZswapTmpfiles("zstd")), mode: apischema.SwapModeDisk},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f, sys := diskHost(t)
			f.write(zswapEnabledParam, test.enabled+"\n")
			if test.tmpfiles != "" {
				f.write(zswapTmpfilesPath, test.tmpfiles)
			}
			if test.cmdline != "" {
				f.write(kernelCmdlinePath, test.cmdline)
			}
			status, _, err := sys.status(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if status.Mode != test.mode || status.ModeSavedForBoot != test.saved {
				t.Errorf("mode %q, saved for boot %v, want %q, %v", status.Mode, status.ModeSavedForBoot, test.mode, test.saved)
			}
		})
	}
}
