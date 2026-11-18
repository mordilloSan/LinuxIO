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

	got, err := f.host().readSwapStatus("")
	if err != nil {
		t.Fatal(err)
	}

	wantDevices := []apischema.SwapDevice{{
		Path: "/swap.img", Kind: apischema.SwapDeviceFile, Size: 2097148 * kib, Used: 1 * gib, Priority: -2, FSType: "btrfs",
	}}
	if !slices.Equal(got.Devices, wantDevices) {
		t.Errorf("devices = %+v, want %+v", got.Devices, wantDevices)
	}
	wantFile := apischema.SwapFile{Path: "/swap.img", Active: true, FSType: "btrfs", Managed: true}
	if got.SwapFile == nil || *got.SwapFile != wantFile {
		t.Errorf("swap file = %+v, want %+v", got.SwapFile, wantFile)
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

	got, err := f.host().readSwapStatus(apischema.ZramProviderTools)
	if err != nil {
		t.Fatal(err)
	}
	wantFile := apischema.SwapFile{Path: "/mnt/sd/swapfile", FSType: "ext4", DiskHint: apischema.SwapDiskHintMMC, Managed: true}
	if got.SwapFile == nil || *got.SwapFile != wantFile {
		t.Errorf("swap file = %+v, want %+v", got.SwapFile, wantFile)
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

// With two swap files in fstab, the one that swaps is reported and managed
// though one that is off comes first: it is the one a switch to compressed
// memory takes away.
func TestReadSwapStatusReportsTheActiveOfTwoSwapFiles(t *testing.T) {
	f := newFixture(t)
	f.write("/proc/swaps", swapsHeader+swapFileSwapsLine)
	f.write("/proc/meminfo", meminfoText)
	f.write("/proc/self/mountinfo", mountinfoText)
	f.write("/etc/fstab", "/old.img none swap sw 0 0\n/swap.img none swap sw 0 0\n")

	got, err := f.host().readSwapStatus(apischema.ZramProviderTools)
	if err != nil {
		t.Fatal(err)
	}
	wantFile := apischema.SwapFile{Path: "/swap.img", Active: true, FSType: "btrfs", Managed: true}
	if got.SwapFile == nil || *got.SwapFile != wantFile {
		t.Errorf("swap file = %+v, want %+v", got.SwapFile, wantFile)
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

			got, err := f.host().readSwapStatus(apischema.ZramProviderTools)
			if err != nil {
				t.Fatal(err)
			}
			if got.SwapFile == nil || got.SwapFile.DiskHint != want || got.Devices[0].DiskHint != want {
				t.Errorf("swap file = %+v, device = %+v; want disk hint %q", got.SwapFile, got.Devices[0], want)
			}
		})
	}
}

// Without an fstab entry the active swap file is the one the status reports; a
// swap partition beside it keeps zram from being chosen.
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

	got, err := f.host().readSwapStatus(apischema.ZramProviderTools)
	if err != nil {
		t.Fatal(err)
	}
	// fstab does not name it, so it has no unit LinuxIO could stop.
	wantFile := apischema.SwapFile{Path: "/home/swapfile", Active: true, FSType: "ext4", UnmanagedReason: apischema.SwapFileNotInFstab}
	if got.SwapFile == nil || *got.SwapFile != wantFile {
		t.Errorf("swap file = %+v, want %+v", got.SwapFile, wantFile)
	}
	if got.Devices[0].FSType != "" || got.Devices[1].FSType != "ext4" {
		t.Errorf("devices = %+v, want a filesystem on the file only", got.Devices)
	}
	if got.Mode != apischema.SwapModeDisk || got.RecommendedMode != apischema.SwapModeZswap {
		t.Errorf("mode = %q, recommended = %q; want disk, zswap", got.Mode, got.RecommendedMode)
	}
	if got.ZramUnavailableReason != apischema.ZramSwapPartitionActive {
		t.Errorf("zram unavailable reason = %q, want swap_partition_active", got.ZramUnavailableReason)
	}
}

// Only /proc/swaps and /proc/meminfo are required: a host without zswap,
// zram, fstab, mountinfo or the vm sysctls still reports its status.
func TestReadSwapStatusOfAMinimalHost(t *testing.T) {
	f := newFixture(t)
	f.write("/proc/swaps", swapsHeader)
	f.write("/proc/meminfo", "MemTotal: 2097152 kB\nMemAvailable: 1048576 kB\nSwapTotal: 0 kB\nSwapFree: 0 kB\n")

	got, err := f.host().readSwapStatus(apischema.ZramProviderTools)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Devices) != 0 || got.SwapFile != nil || len(got.ZramDevices) != 0 || got.Zswap != (apischema.ZswapStatus{}) {
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
			if got, err := f.host().readSwapStatus(apischema.ZramProviderTools); err == nil {
				t.Fatalf("status = %+v, want an error", got)
			}
		})
	}
}

// A unit held off at boot, by LinuxIO's drop-in for it in
// /etc/systemd/system or by a link to /dev/null there or in
// /run/systemd/system, could not start its device again after a drain, so
// the status says unit_held_off without asking systemd. A link elsewhere, a
// drop-in of another name, or a held-off unit whose device holds no data,
// does not block.
func TestReadSwapStatusReportsAHeldOffDrainUnit(t *testing.T) {
	tests := []struct {
		name   string
		link   string
		to     string
		dropIn string
		used   string
		want   apischema.SwapDrainBlockedReason
	}{
		{name: "LinuxIO's drop-in", dropIn: "/etc/systemd/system/swap.img.swap.d/90-linuxio-memory.conf", used: "524288", want: apischema.SwapDrainUnitHeldOff},
		{name: "another drop-in", dropIn: "/etc/systemd/system/swap.img.swap.d/50-admin.conf", used: "524288"},
		{name: "masked in etc", link: "/etc/systemd/system/swap.img.swap", to: "/dev/null", used: "524288", want: apischema.SwapDrainUnitHeldOff},
		{name: "masked at runtime", link: "/run/systemd/system/swap.img.swap", to: "/dev/null", used: "524288", want: apischema.SwapDrainUnitHeldOff},
		{name: "linked to a unit file", link: "/etc/systemd/system/swap.img.swap", to: "/lib/systemd/system/other.swap", used: "524288"},
		{name: "masked without parked data", link: "/etc/systemd/system/swap.img.swap", to: "/dev/null", used: "0", want: apischema.SwapDrainNothingParked},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newFixture(t)
			f.write("/proc/swaps", swapsHeader+"/swap.img                               file\t\t2097148\t\t"+test.used+"\t\t-2\n")
			f.write("/proc/meminfo", meminfoText)
			f.write("/etc/fstab", "/swap.img none swap sw 0 0\n")
			f.write("/swap.img", "")
			if test.link != "" {
				f.symlink(test.to, test.link)
			}
			if test.dropIn != "" {
				f.write(test.dropIn, "[Unit]\nConditionPathExists=!/\n")
			}

			got, err := f.host().readSwapStatus(apischema.ZramProviderTools)
			if err != nil {
				t.Fatal(err)
			}
			if got.DrainBlockedReason != test.want || got.CanDrain != (test.want == "") {
				t.Fatalf("can drain = %v, reason = %q; want reason %q", got.CanDrain, got.DrainBlockedReason, test.want)
			}
		})
	}
}

const openSwapsLine = "/home/open/swapfile                      file\t\t1048572\t\t131072\t\t-2\n"

// openSwapFileHost has fstab name a swap file in a folder everyone can
// write.
func openSwapFileHost(t *testing.T, swaps string) fixture {
	t.Helper()
	f := newFixture(t)
	f.write("/proc/swaps", swapsHeader+swaps)
	f.write("/proc/meminfo", meminfoText)
	f.write("/proc/self/mountinfo", mountinfoText)
	f.write("/etc/fstab", "/home/open/swapfile none swap sw 0 0\n")
	f.othersCanWrite("/home/open")
	f.write("/home/open/swapfile", "")
	f.write("/sys/module/zswap/parameters/enabled", "N\n")
	f.write("/sys/module/zswap/parameters/compressor", "lzo\n")
	f.write("/sys/module/zswap/parameters/max_pool_percent", "20\n")
	return f
}

// A swap file in a folder users other than root can change is reported, and
// reported as not managed with the reason, before anything is asked of it.
func TestReadSwapStatusOfASwapFileOthersCanRedirect(t *testing.T) {
	f := openSwapFileHost(t, openSwapsLine)

	got, err := f.host().readSwapStatus(apischema.ZramProviderTools)
	if err != nil {
		t.Fatal(err)
	}
	wantFile := apischema.SwapFile{Path: "/home/open/swapfile", Active: true, FSType: "ext4", UnmanagedReason: apischema.SwapFileOthersCanWrite}
	if got.SwapFile == nil || *got.SwapFile != wantFile {
		t.Errorf("swap file = %+v, want %+v", got.SwapFile, wantFile)
	}
	if got.CanDrain || got.DrainBlockedReason != apischema.SwapDrainOthersCanWrite {
		t.Errorf("can drain = %v, reason = %q; want false, others_can_write", got.CanDrain, got.DrainBlockedReason)
	}
}

// Such a file that is off cannot be started from here, so no mode that needs
// it is recommended.
func TestReadSwapStatusDoesNotCountASwapFileItWouldNotStart(t *testing.T) {
	f := openSwapFileHost(t, "")

	got, err := f.host().readSwapStatus(apischema.ZramProviderTools)
	if err != nil {
		t.Fatal(err)
	}
	wantFile := apischema.SwapFile{Path: "/home/open/swapfile", FSType: "ext4", UnmanagedReason: apischema.SwapFileOthersCanWrite}
	if got.SwapFile == nil || *got.SwapFile != wantFile {
		t.Errorf("swap file = %+v, want %+v", got.SwapFile, wantFile)
	}
	if got.RecommendedMode != apischema.SwapModeZram {
		t.Errorf("recommended = %q, want zram", got.RecommendedMode)
	}
}

// Beside such a file that is off, another swap file that is on is the disk
// swap the recommendation goes by: here ext4, where the fstab file's btrfs
// would have called for zram.
func TestReadSwapStatusRecommendsByAnActiveSwapFile(t *testing.T) {
	f := newFixture(t)
	f.write("/proc/swaps", swapsHeader+"/home/other                             file\t\t1048572\t\t0\t\t-2\n")
	f.write("/proc/meminfo", meminfoText)
	f.write("/proc/self/mountinfo", mountinfoText)
	f.write("/etc/fstab", "/srv/open/swapfile none swap sw 0 0\n")
	f.othersCanWrite("/srv/open")
	f.write("/srv/open/swapfile", "")
	f.write("/home/other", "")
	f.write("/sys/module/zswap/parameters/enabled", "N\n")
	f.write("/sys/module/zswap/parameters/compressor", "lzo\n")
	f.write("/sys/module/zswap/parameters/max_pool_percent", "20\n")

	got, err := f.host().readSwapStatus(apischema.ZramProviderTools)
	if err != nil {
		t.Fatal(err)
	}
	if got.SwapFile == nil || got.SwapFile.Path != "/srv/open/swapfile" || got.SwapFile.Active || got.SwapFile.FSType != "btrfs" {
		t.Errorf("swap file = %+v, want the fstab file on btrfs, off", got.SwapFile)
	}
	if got.RecommendedMode != apischema.SwapModeZswap {
		t.Errorf("recommended = %q, want zswap", got.RecommendedMode)
	}
}

// A swap file that is on and not managed stays on whatever is chosen, so
// zram alone is not recommended, as beside a swap partition.
func TestReadSwapStatusDoesNotRecommendZramBesideASwapFileThatStays(t *testing.T) {
	f := newFixture(t)
	f.write("/proc/swaps", swapsHeader+"/srv/open/swapfile                      file\t\t1048572\t\t0\t\t-2\n")
	f.write("/proc/meminfo", meminfoText)
	f.write("/proc/self/mountinfo", mountinfoText)
	f.write("/etc/fstab", "/srv/open/swapfile none swap sw 0 0\n")
	f.othersCanWrite("/srv/open")
	f.write("/srv/open/swapfile", "")
	f.write("/sys/module/zswap/parameters/enabled", "N\n")
	f.write("/sys/module/zswap/parameters/compressor", "lzo\n")
	f.write("/sys/module/zswap/parameters/max_pool_percent", "20\n")

	got, err := f.host().readSwapStatus(apischema.ZramProviderTools)
	if err != nil {
		t.Fatal(err)
	}
	if got.SwapFile == nil || got.SwapFile.FSType != "btrfs" || got.SwapFile.Managed {
		t.Errorf("swap file = %+v, want one on btrfs that is not managed", got.SwapFile)
	}
	if got.RecommendedMode != apischema.SwapModeZswap {
		t.Errorf("recommended = %q, want zswap", got.RecommendedMode)
	}
}
