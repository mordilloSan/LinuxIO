package memory

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

// fixture is a host root, with /proc, /sys and /etc, built in a temporary
// directory.
type fixture struct {
	t    *testing.T
	root string
}

// newFixture makes the root a folder only its owner can write, as "/" is,
// whatever the umask gave the temporary directory.
func newFixture(t *testing.T) fixture {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	f := fixture{t: t, root: root}
	f.useFolderFS(roomyExt4)
	return f
}

func (f fixture) write(name, content string) {
	f.t.Helper()
	path := filepath.Join(f.root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f fixture) mkdir(name string) {
	f.t.Helper()
	if err := os.MkdirAll(filepath.Join(f.root, name), 0o755); err != nil {
		f.t.Fatal(err)
	}
}

func (f fixture) host() hostRoot { return hostRoot{root: f.root, owner: uint32(os.Getuid())} }

// othersCanWrite makes the folder name one that users other than its owner
// can change.
func (f fixture) othersCanWrite(name string) {
	f.t.Helper()
	f.mkdir(name)
	if err := os.Chmod(filepath.Join(f.root, name), 0o777); err != nil {
		f.t.Fatal(err)
	}
}

const (
	kib = 1 << 10
	mib = 1 << 20
	gib = 1 << 30

	swapsHeader = "Filename\t\t\t\tType\t\tSize\t\tUsed\t\tPriority\n"
)

func TestReadSwaps(t *testing.T) {
	tests := []struct {
		name  string
		swaps string
		want  []apischema.SwapDevice
	}{
		{
			name:  "file",
			swaps: swapsHeader + "/swap.img                               file\t\t2097148\t\t1024\t\t-2\n",
			want:  []apischema.SwapDevice{{Path: "/swap.img", Kind: apischema.SwapDeviceFile, Size: 2097148 * kib, Used: 1024 * kib, Priority: -2}},
		},
		{
			name:  "partition",
			swaps: swapsHeader + "/dev/sda3                               partition\t8388604\t\t0\t\t-3\n",
			want:  []apischema.SwapDevice{{Path: "/dev/sda3", Kind: apischema.SwapDevicePartition, Size: 8388604 * kib, Priority: -3}},
		},
		{
			name:  "zram",
			swaps: swapsHeader + "/dev/zram0                              partition\t4194300\t\t512\t\t100\n",
			want:  []apischema.SwapDevice{{Path: "/dev/zram0", Kind: apischema.SwapDeviceZram, Size: 4194300 * kib, Used: 512 * kib, Priority: 100}},
		},
		{
			name:  "header only",
			swaps: swapsHeader,
		},
		{
			name:  "escaped space",
			swaps: swapsHeader + "/mnt/my\\040swap/file                    file\t\t1024\t\t0\t\t-2\n",
			want:  []apischema.SwapDevice{{Path: "/mnt/my swap/file", Kind: apischema.SwapDeviceFile, Size: 1024 * kib, Priority: -2}},
		},
		{
			name: "several",
			swaps: swapsHeader +
				"/dev/zram0                              partition\t4194300\t\t0\t\t100\n" +
				"/swap.img                               file\t\t2097148\t\t0\t\t-2\n",
			want: []apischema.SwapDevice{
				{Path: "/dev/zram0", Kind: apischema.SwapDeviceZram, Size: 4194300 * kib, Priority: 100},
				{Path: "/swap.img", Kind: apischema.SwapDeviceFile, Size: 2097148 * kib, Priority: -2},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newFixture(t)
			f.write("/proc/swaps", test.swaps)
			got, err := f.host().readSwaps()
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, test.want) {
				t.Fatalf("devices = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestReadSwapsRefusesAMalformedTable(t *testing.T) {
	for name, swaps := range map[string]string{
		"size not a number": swapsHeader + "/swap.img file big 0 -2\n",
		"used not a number": swapsHeader + "/swap.img file 1024 some -2\n",
		"bad priority":      swapsHeader + "/swap.img file 1024 0 high\n",
		"missing field":     swapsHeader + "/swap.img file 1024 0\n",
		"unknown type":      swapsHeader + "/swap.img tape 1024 0 -2\n",
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.write("/proc/swaps", swaps)
			if got, err := f.host().readSwaps(); err == nil {
				t.Fatalf("devices = %+v, want an error", got)
			}
		})
	}
	t.Run("missing", func(t *testing.T) {
		if _, err := newFixture(t).host().readSwaps(); err == nil {
			t.Fatal("want an error for a missing /proc/swaps")
		}
	})
}

const meminfoText = `MemTotal:       16308408 kB
MemFree:         1203840 kB
MemAvailable:    8154204 kB
Buffers:          402116 kB
SwapCached:        10240 kB
SwapTotal:       2097148 kB
SwapFree:        1048572 kB
HugePages_Total:       0
`

func TestReadMeminfo(t *testing.T) {
	f := newFixture(t)
	f.write("/proc/meminfo", meminfoText)
	got, err := f.host().readMeminfo()
	if err != nil {
		t.Fatal(err)
	}
	want := memInfo{memTotal: 16308408 * kib, memAvailable: 8154204 * kib, swapTotal: 2097148 * kib, swapFree: 1048572 * kib}
	if got != want {
		t.Fatalf("meminfo = %+v, want %+v", got, want)
	}
}

func TestReadMeminfoRefusesAMalformedFile(t *testing.T) {
	for name, text := range map[string]string{
		"missing MemAvailable": "MemTotal: 16308408 kB\nSwapTotal: 0 kB\nSwapFree: 0 kB\n",
		"value not a number":   "MemTotal: lots kB\nMemAvailable: 1 kB\nSwapTotal: 0 kB\nSwapFree: 0 kB\n",
		"unit missing":         "MemTotal: 16308408\nMemAvailable: 1 kB\nSwapTotal: 0 kB\nSwapFree: 0 kB\n",
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.write("/proc/meminfo", text)
			if got, err := f.host().readMeminfo(); err == nil {
				t.Fatalf("meminfo = %+v, want an error", got)
			}
		})
	}
	t.Run("missing", func(t *testing.T) {
		if _, err := newFixture(t).host().readMeminfo(); err == nil {
			t.Fatal("want an error for a missing /proc/meminfo")
		}
	})
}

func TestReadZswap(t *testing.T) {
	tests := []struct {
		name   string
		params map[string]string
		want   apischema.ZswapStatus
	}{
		{
			name:   "enabled",
			params: map[string]string{"enabled": "Y\n", "compressor": "zstd\n", "max_pool_percent": "20\n"},
			want:   apischema.ZswapStatus{Available: true, Enabled: true, Compressor: "zstd", MaxPoolPercent: 20},
		},
		{
			name:   "disabled",
			params: map[string]string{"enabled": "N\n", "compressor": "lzo-rle\n", "max_pool_percent": "25\n"},
			want:   apischema.ZswapStatus{Available: true, Compressor: "lzo-rle", MaxPoolPercent: 25},
		},
		{
			name: "kernel without zswap",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newFixture(t)
			for name, value := range test.params {
				f.write("/sys/module/zswap/parameters/"+name, value)
			}
			got, err := f.host().readZswap()
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("zswap = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestReadZramDevices(t *testing.T) {
	f := newFixture(t)
	f.write("/sys/block/zram0/disksize", "8589934592\n")
	f.write("/sys/block/zram0/mm_stat", " 3145728  1048576  1179648        0  1179648       10        0        0        0\n")
	f.write("/sys/block/zram1/disksize", "0\n")
	f.write("/sys/block/zram1/mm_stat", "       0        0        0        0        0        0        0        0        0\n")
	f.write("/sys/block/sda/size", "1953525168\n")
	f.mkdir("/sys/block/zramx")

	got, err := f.host().readZramDevices()
	if err != nil {
		t.Fatal(err)
	}
	want := []apischema.ZramDevice{
		{Name: "zram0", Disksize: 8 * gib, OriginalSize: 3 * mib, CompressedSize: 1 * mib, MemUsed: 1152 * kib},
		{Name: "zram1"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("zram devices = %+v, want %+v", got, want)
	}
}

// A device that leaves while it is read, as zram's unit resets it, is
// skipped: here its link in /sys/block leads nowhere.
func TestReadZramDevicesSkipsADeviceThatLeft(t *testing.T) {
	f := newFixture(t)
	f.write("/sys/block/zram0/disksize", "8589934592\n")
	f.symlink("../devices/virtual/block/zram1", "/sys/block/zram1")

	got, err := f.host().readZramDevices()
	if err != nil {
		t.Fatal(err)
	}
	if want := []apischema.ZramDevice{{Name: "zram0", Disksize: 8 * gib}}; !slices.Equal(got, want) {
		t.Fatalf("zram devices = %+v, want %+v", got, want)
	}
}

func TestReadZramDevicesWithoutZram(t *testing.T) {
	f := newFixture(t)
	if got, err := f.host().readZramDevices(); err != nil || len(got) != 0 {
		t.Fatalf("no /sys/block: devices = %+v, err = %v; want none", got, err)
	}
	f.write("/sys/block/sda/size", "1953525168\n")
	if got, err := f.host().readZramDevices(); err != nil || len(got) != 0 {
		t.Fatalf("no zram device: devices = %+v, err = %v; want none", got, err)
	}
}

func TestReadZramDevicesRefusesAShortMMStat(t *testing.T) {
	f := newFixture(t)
	f.write("/sys/block/zram0/disksize", "8589934592\n")
	f.write("/sys/block/zram0/mm_stat", "3145728 1048576\n")
	if got, err := f.host().readZramDevices(); err == nil {
		t.Fatalf("zram devices = %+v, want an error", got)
	}
}

const mountinfoText = `22 1 0:21 /@ / rw,relatime shared:1 - btrfs /dev/nvme0n1p2 rw,ssd,space_cache=v2,subvolid=256,subvol=/@
23 22 0:22 / /proc rw,nosuid,nodev,noexec,relatime shared:2 - proc proc rw
30 22 179:1 / /boot/firmware rw,relatime shared:3 - vfat /dev/mmcblk0p1 rw
31 22 179:2 / /mnt/sd rw,relatime shared:4 - ext4 /dev/mmcblk0p2 rw
32 22 8:17 / /mnt/sd\040card rw,relatime shared:5 - ext4 /dev/sdb1 rw
33 22 0:21 /@swap /swap rw,relatime shared:6 - btrfs /dev/nvme0n1p2 rw,subvolid=260,subvol=/@swap
34 22 8:1 / /home rw,relatime shared:7 - xfs /dev/sda1 rw
35 34 8:2 / /home rw,relatime shared:8 - ext4 /dev/sda2 rw
`

func TestMountHolding(t *testing.T) {
	f := newFixture(t)
	f.write("/proc/self/mountinfo", mountinfoText)
	mounts, err := f.host().readMounts()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		path   string
		fsType string
		source string
	}{
		{path: "/swap.img", fsType: "btrfs", source: "/dev/nvme0n1p2"},
		{path: "/swap/swapfile", fsType: "btrfs", source: "/dev/nvme0n1p2"},
		{path: "/mnt/sd/swapfile", fsType: "ext4", source: "/dev/mmcblk0p2"},
		{path: "/mnt/sdx/swapfile", fsType: "btrfs", source: "/dev/nvme0n1p2"},
		{path: "/mnt/sd card/swapfile", fsType: "ext4", source: "/dev/sdb1"},
		{path: "/homes/swapfile", fsType: "btrfs", source: "/dev/nvme0n1p2"},
		{path: "/home/swapfile", fsType: "ext4", source: "/dev/sda2"},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			got, ok := mountHolding(mounts, test.path)
			if !ok || got.fsType != test.fsType || got.source != test.source {
				t.Fatalf("mount = %+v (found %v), want %s on %s", got, ok, test.fsType, test.source)
			}
		})
	}
}

func TestDiskHint(t *testing.T) {
	f := newFixture(t)
	// A USB stick with one partition, a fixed disk with one, and a card in a
	// USB reader used whole: /sys/class/block links to each device's directory.
	f.write("/sys/devices/usb1/block/sdb/removable", "1\n")
	f.write("/sys/devices/usb1/block/sdb/sdb1/partition", "1\n")
	f.write("/sys/devices/ata1/block/sda/removable", "0\n")
	f.write("/sys/devices/ata1/block/sda/sda1/partition", "1\n")
	f.write("/sys/devices/usb2/block/sdc/removable", "1\n")
	f.symlink("../../devices/usb1/block/sdb", "/sys/class/block/sdb")
	f.symlink("../../devices/usb1/block/sdb/sdb1", "/sys/class/block/sdb1")
	f.symlink("../../devices/ata1/block/sda", "/sys/class/block/sda")
	f.symlink("../../devices/ata1/block/sda/sda1", "/sys/class/block/sda1")
	f.symlink("../../devices/usb2/block/sdc", "/sys/class/block/sdc")

	for source, want := range map[string]apischema.SwapDiskHint{
		"/dev/mmcblk0p2":   apischema.SwapDiskHintMMC,
		"/dev/mmcblk1":     apischema.SwapDiskHintMMC,
		"/dev/sdb1":        apischema.SwapDiskHintRemovable,
		"/dev/sdc":         apischema.SwapDiskHintRemovable,
		"/dev/sda1":        "",
		"/dev/nvme0n1p2":   "",
		"/dev/mapper/root": "",
		"/dev/../sdb1":     "",
		"tmpfs":            "",
		"":                 "",
	} {
		if got := f.host().diskHint(mountEntry{fsType: "ext4", source: source}); got != want {
			t.Errorf("diskHint(%q) = %q, want %q", source, got, want)
		}
	}
}
