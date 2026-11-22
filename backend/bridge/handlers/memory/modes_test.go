package memory

import (
	"slices"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

var (
	zramDevice      = apischema.SwapDevice{Path: "/dev/zram0", Kind: apischema.SwapDeviceZram}
	fileDevice      = apischema.SwapDevice{Path: "/swap.img", Kind: apischema.SwapDeviceFile}
	partitionDevice = apischema.SwapDevice{Path: "/dev/sda3", Kind: apischema.SwapDevicePartition}
)

func TestDeriveMode(t *testing.T) {
	removable := []string{"/swap.img"}
	lockedFile := apischema.SwapDevice{Path: "/home/swapfile", Kind: apischema.SwapDeviceFile}
	tests := []struct {
		name         string
		devices      []apischema.SwapDevice
		zswapEnabled bool
		want         apischema.SwapMode
	}{
		{name: "no swap", want: apischema.SwapModeNone},
		{name: "no swap with zswap enabled", zswapEnabled: true, want: apischema.SwapModeNone},
		{name: "zram only", devices: []apischema.SwapDevice{zramDevice}, want: apischema.SwapModeZram},
		{name: "zram only with zswap enabled", devices: []apischema.SwapDevice{zramDevice}, zswapEnabled: true, want: apischema.SwapModeZram},
		{name: "zram and a removable swap file", devices: []apischema.SwapDevice{zramDevice, fileDevice}, want: apischema.SwapModeMixed},
		{name: "zram and a swap file LinuxIO does not change", devices: []apischema.SwapDevice{zramDevice, lockedFile}, want: apischema.SwapModeZram},
		{name: "zram and partition", devices: []apischema.SwapDevice{partitionDevice, zramDevice}, zswapEnabled: true, want: apischema.SwapModeZram},
		{name: "swap file with zswap", devices: []apischema.SwapDevice{fileDevice}, zswapEnabled: true, want: apischema.SwapModeZswap},
		{name: "partition with zswap", devices: []apischema.SwapDevice{partitionDevice}, zswapEnabled: true, want: apischema.SwapModeZswap},
		{name: "swap file alone", devices: []apischema.SwapDevice{fileDevice}, want: apischema.SwapModeDisk},
		{name: "partition alone", devices: []apischema.SwapDevice{partitionDevice}, want: apischema.SwapModeDisk},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := deriveMode(test.devices, removable, test.zswapEnabled); got != test.want {
				t.Fatalf("mode = %q, want %q", got, test.want)
			}
		})
	}
}

func TestRecommendMode(t *testing.T) {
	onBtrfs := apischema.SwapFileRow{Path: "/swap.img", Active: true, FSType: "btrfs", BlocksSnapshots: true}
	inOwnFolder := apischema.SwapFileRow{Path: ownSwapFilePath, Active: true, FSType: "btrfs"}
	offOnBtrfs := apischema.SwapFileRow{Path: "/old.img", FSType: "btrfs", BlocksSnapshots: true}
	mmcFile := apischema.SwapDevice{Path: "/swapfile", Kind: apischema.SwapDeviceFile, DiskHint: apischema.SwapDiskHintMMC}
	usbFile := apischema.SwapDevice{Path: "/mnt/usb/swapfile", Kind: apischema.SwapDeviceFile, DiskHint: apischema.SwapDiskHintRemovable}
	tests := []struct {
		name    string
		devices []apischema.SwapDevice
		rows    []apischema.SwapFileRow
		noZswap bool
		want    apischema.SwapMode
	}{
		{name: "no disk swap", want: apischema.SwapModeZram},
		{name: "zram only", devices: []apischema.SwapDevice{zramDevice}, want: apischema.SwapModeZram},
		{name: "an off swap file only", rows: []apischema.SwapFileRow{offOnBtrfs}, want: apischema.SwapModeZram},
		{name: "swap file on an SD card or eMMC", devices: []apischema.SwapDevice{mmcFile}, want: apischema.SwapModeZram},
		{name: "swap file on a removable drive", devices: []apischema.SwapDevice{usbFile}, want: apischema.SwapModeZram},
		{name: "swap file that blocks snapshots", devices: []apischema.SwapDevice{fileDevice}, rows: []apischema.SwapFileRow{onBtrfs}, want: apischema.SwapModeZram},
		{name: "swap file in LinuxIO's subvolume", devices: []apischema.SwapDevice{fileDevice}, rows: []apischema.SwapFileRow{inOwnFolder}, want: apischema.SwapModeZswap},
		{name: "swap file anywhere else", devices: []apischema.SwapDevice{fileDevice}, want: apischema.SwapModeZswap},
		{name: "swap partition", devices: []apischema.SwapDevice{partitionDevice}, want: apischema.SwapModeZswap},
		{name: "swap file on a kernel without zswap", devices: []apischema.SwapDevice{fileDevice}, noZswap: true, want: apischema.SwapModeDisk},
		{name: "no disk swap on a kernel without zswap", noZswap: true, want: apischema.SwapModeZram},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := recommendMode(test.devices, test.rows, !test.noZswap); got != test.want {
				t.Fatalf("recommended mode = %q, want %q", got, test.want)
			}
		})
	}
}

func TestRecommendedSwappiness(t *testing.T) {
	tests := []struct {
		mode, recommended apischema.SwapMode
		want              int
	}{
		{mode: apischema.SwapModeZram, recommended: apischema.SwapModeZswap, want: 180},
		{mode: apischema.SwapModeZswap, recommended: apischema.SwapModeZram, want: 60},
		{mode: apischema.SwapModeDisk, recommended: apischema.SwapModeZram, want: 60},
		{mode: apischema.SwapModeNone, recommended: apischema.SwapModeZram, want: 180},
		{mode: apischema.SwapModeMixed, recommended: apischema.SwapModeZswap, want: 60},
		{mode: apischema.SwapModeMixed, recommended: apischema.SwapModeZram, want: 180},
	}
	for _, test := range tests {
		if got := recommendedSwappiness(test.mode, test.recommended); got != test.want {
			t.Errorf("recommendedSwappiness(%q, %q) = %d, want %d", test.mode, test.recommended, got, test.want)
		}
	}
}

// The preset table is what the mode change writes, so its values are pinned
// here: zram also sets the three other vm sysctls, zswap and disk leave them
// at the kernel default.
func TestModePresets(t *testing.T) {
	want := map[apischema.SwapMode]modePreset{
		apischema.SwapModeZram: {mode: apischema.SwapModeZram, swappiness: 180, sysctls: []sysctlSetting{
			{name: "page-cluster", value: 0}, {name: "watermark_boost_factor", value: 0}, {name: "watermark_scale_factor", value: 125},
		}},
		apischema.SwapModeZswap: {mode: apischema.SwapModeZswap, swappiness: 60},
		apischema.SwapModeDisk:  {mode: apischema.SwapModeDisk, swappiness: 60},
	}
	if len(modePresets) != len(want) {
		t.Fatalf("modePresets has %d entries, want %d", len(modePresets), len(want))
	}
	for mode, preset := range want {
		got, ok := presetFor(mode)
		if !ok || got.mode != preset.mode || got.swappiness != preset.swappiness || !slices.Equal(got.sysctls, preset.sysctls) {
			t.Errorf("preset for %q = %+v (found %v), want %+v", mode, got, ok, preset)
		}
	}
	for _, mode := range []apischema.SwapMode{apischema.SwapModeNone, apischema.SwapModeMixed} {
		if _, ok := presetFor(mode); ok {
			t.Errorf("mode %q has a preset; only selectable modes do", mode)
		}
	}
	wantList := []apischema.SwapModePreset{
		{Mode: apischema.SwapModeZram, Swappiness: 180},
		{Mode: apischema.SwapModeZswap, Swappiness: 60},
		{Mode: apischema.SwapModeDisk, Swappiness: 60},
	}
	if got := presetList(); !slices.Equal(got, wantList) {
		t.Fatalf("presetList = %+v, want %+v", got, wantList)
	}
}

// A mode that does not set a sysctl zram sets writes its kernel default live
// only where LinuxIO's drop-in held the key, so the value is LinuxIO's, and
// the live value is still zram's.
func TestLiveSysctls(t *testing.T) {
	zramLive := apischema.SwapStatus{WatermarkScaleFactor: 125}
	held := map[string]bool{sysctlPageCluster: true, sysctlWatermarkBoostFactor: true, sysctlWatermarkScaleFactor: true}
	defaults := []sysctlSetting{{name: sysctlSwappiness, value: 60}, {name: sysctlPageCluster, value: 3},
		{name: sysctlWatermarkBoostFactor, value: 15000}, {name: sysctlWatermarkScaleFactor, value: 10}}
	tests := []struct {
		name   string
		mode   apischema.SwapMode
		status apischema.SwapStatus
		held   map[string]bool
		want   []sysctlSetting
	}{
		{name: "leaving LinuxIO's zram values", mode: apischema.SwapModeDisk, status: zramLive, held: held, want: defaults},
		{name: "an admin's values that match zram", mode: apischema.SwapModeZswap, status: zramLive,
			want: defaults[:1]},
		{name: "one key held", mode: apischema.SwapModeDisk, status: zramLive, held: map[string]bool{sysctlPageCluster: true},
			want: defaults[:2]},
		{name: "a held key the admin changed live", mode: apischema.SwapModeDisk,
			status: apischema.SwapStatus{PageCluster: 5, WatermarkScaleFactor: 125}, held: held,
			want: []sysctlSetting{defaults[0], defaults[2], defaults[3]}},
		{name: "zram sets its own", mode: apischema.SwapModeZram, status: zramLive, held: held,
			want: []sysctlSetting{{name: sysctlSwappiness, value: 180}, {name: sysctlPageCluster, value: 0},
				{name: sysctlWatermarkBoostFactor, value: 0}, {name: sysctlWatermarkScaleFactor, value: 125}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := liveSysctls(test.mode, test.status, test.held); !slices.Equal(got, test.want) {
				t.Errorf("liveSysctls = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestDrainBlockedReason(t *testing.T) {
	file := func(path string, used uint64) apischema.SwapDevice {
		return apischema.SwapDevice{Path: path, Kind: apischema.SwapDeviceFile, Size: 8 * gib, Used: used}
	}
	row := func(path string, used uint64, locked apischema.SwapFileLockedReason) apischema.SwapFileRow {
		return apischema.SwapFileRow{Path: path, Active: true, Used: used, LockedReason: locked}
	}
	partition := apischema.SwapDevice{Path: "/dev/sda3", Kind: apischema.SwapDevicePartition, Size: 8 * gib, Used: gib}
	tests := []struct {
		name         string
		devices      []apischema.SwapDevice
		rows         []apischema.SwapFileRow
		memAvailable uint64
		want         apischema.SwapDrainBlockedReason
	}{
		{name: "nothing parked", memAvailable: 8 * gib, want: apischema.SwapDrainNothingParked},
		{name: "nothing parked and little memory", devices: []apischema.SwapDevice{file("/swap.img", 0)}, rows: []apischema.SwapFileRow{row("/swap.img", 0, "")},
			memAvailable: 100 * mib, want: apischema.SwapDrainNothingParked},
		{name: "room", devices: []apischema.SwapDevice{file("/swap.img", gib)}, rows: []apischema.SwapFileRow{row("/swap.img", gib, "")}, memAvailable: 4 * gib},
		{name: "no room", devices: []apischema.SwapDevice{file("/swap.img", 3900*mib)}, rows: []apischema.SwapFileRow{row("/swap.img", 3900*mib, "")},
			memAvailable: 4 * gib, want: apischema.SwapDrainNotEnoughMemory},
		{name: "exactly at the margin", devices: []apischema.SwapDevice{file("/swap.img", 4*gib-512*mib)}, rows: []apischema.SwapFileRow{row("/swap.img", 4*gib-512*mib, "")},
			memAvailable: 4 * gib, want: apischema.SwapDrainNotEnoughMemory},
		{name: "one byte inside the margin", devices: []apischema.SwapDevice{file("/swap.img", 4*gib-512*mib-1)}, rows: []apischema.SwapFileRow{row("/swap.img", 4*gib-512*mib-1, "")},
			memAvailable: 4 * gib},
		{name: "free space on a partition counts as room", devices: []apischema.SwapDevice{file("/swap.img", 6*gib), partition},
			rows: []apischema.SwapFileRow{row("/swap.img", 6*gib, "")}, memAvailable: 4 * gib},
		{name: "parked only on a partition", devices: []apischema.SwapDevice{partition}, memAvailable: 8 * gib, want: apischema.SwapDrainOtherSwap},
		{name: "parked only on a locked swap file", devices: []apischema.SwapDevice{file("/home/swapfile", gib)},
			rows: []apischema.SwapFileRow{row("/home/swapfile", gib, apischema.SwapFileNotInFstab)}, memAvailable: 8 * gib, want: apischema.SwapDrainOtherSwap},
		{name: "zram data is in memory already", devices: []apischema.SwapDevice{{Path: "/dev/zram0", Kind: apischema.SwapDeviceZram, Used: gib}},
			memAvailable: 8 * gib, want: apischema.SwapDrainNothingParked},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			status := apischema.SwapStatus{Devices: test.devices, SwapFiles: test.rows, MemAvailable: test.memAvailable}
			if got := drainBlockedReason(status); got != test.want {
				t.Fatalf("drain blocked reason = %q, want %q", got, test.want)
			}
		})
	}
}

// The room check before a swap file is turned off counts the free space on
// the other disk swap devices, not on zram.
func TestRoomToTurnOff(t *testing.T) {
	status := apischema.SwapStatus{MemAvailable: gib, Devices: []apischema.SwapDevice{
		{Path: "/swap.img", Kind: apischema.SwapDeviceFile, Size: 4 * gib, Used: 2 * gib},
		{Path: "/dev/zram0", Kind: apischema.SwapDeviceZram, Size: 8 * gib},
	}}
	if roomToTurnOff(status, "/swap.img") {
		t.Fatal("room with 1 GiB free for 2 GiB of data, want none")
	}
	status.Devices = append(status.Devices, apischema.SwapDevice{Path: "/swap.img.linuxio-resize", Kind: apischema.SwapDeviceFile, Size: 2 * gib})
	if !roomToTurnOff(status, "/swap.img") {
		t.Fatal("no room with 1 GiB free and 2 GiB free on another swap file for 2 GiB of data")
	}
	if !roomToTurnOff(status, "/gone.img") {
		t.Fatal("a device that is not active needs room")
	}
}

func TestZramPlannedSize(t *testing.T) {
	for _, test := range []struct {
		memTotal, want uint64
	}{
		{memTotal: 4 * gib, want: 2 * gib},
		{memTotal: 15 * gib, want: 7*gib + 512*mib},
		{memTotal: 16 * gib, want: 8 * gib},
		{memTotal: 64 * gib, want: 8 * gib},
	} {
		if got := zramPlannedSize(test.memTotal); got != test.want {
			t.Errorf("zramPlannedSize(%d) = %d, want %d", test.memTotal, got, test.want)
		}
	}
}

func TestZramUnavailableReason(t *testing.T) {
	if got := zramUnavailableReason(true); got != "" {
		t.Errorf("zram unavailable reason with a tool = %q, want none", got)
	}
	if got := zramUnavailableReason(false); got != apischema.ZramPackageMissing {
		t.Errorf("zram unavailable reason without a tool = %q, want package_missing", got)
	}
}
