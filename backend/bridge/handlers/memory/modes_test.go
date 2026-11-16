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
		{name: "zram and swap file", devices: []apischema.SwapDevice{zramDevice, fileDevice}, want: apischema.SwapModeMixed},
		{name: "zram and partition", devices: []apischema.SwapDevice{partitionDevice, zramDevice}, zswapEnabled: true, want: apischema.SwapModeMixed},
		{name: "swap file with zswap", devices: []apischema.SwapDevice{fileDevice}, zswapEnabled: true, want: apischema.SwapModeZswap},
		{name: "partition with zswap", devices: []apischema.SwapDevice{partitionDevice}, zswapEnabled: true, want: apischema.SwapModeZswap},
		{name: "swap file alone", devices: []apischema.SwapDevice{fileDevice}, want: apischema.SwapModeDisk},
		{name: "partition alone", devices: []apischema.SwapDevice{partitionDevice}, want: apischema.SwapModeDisk},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := deriveMode(test.devices, test.zswapEnabled); got != test.want {
				t.Fatalf("mode = %q, want %q", got, test.want)
			}
		})
	}
}

func TestRecommendMode(t *testing.T) {
	tests := []struct {
		name            string
		swapFile        *apischema.SwapFile
		partitionActive bool
		noZswap         bool
		want            apischema.SwapMode
	}{
		{name: "no swap file", want: apischema.SwapModeZram},
		{
			name:     "swap file on an SD card or eMMC",
			swapFile: &apischema.SwapFile{Path: "/swapfile", FSType: "ext4", DiskHint: apischema.SwapDiskHintMMC},
			want:     apischema.SwapModeZram,
		},
		{
			name:     "swap file on a removable drive",
			swapFile: &apischema.SwapFile{Path: "/mnt/usb/swapfile", Active: true, FSType: "ext4", DiskHint: apischema.SwapDiskHintRemovable},
			want:     apischema.SwapModeZram,
		},
		{
			name:     "swap file on btrfs",
			swapFile: &apischema.SwapFile{Path: "/swap/swapfile", Active: true, FSType: "btrfs"},
			want:     apischema.SwapModeZram,
		},
		{
			name:     "swap file anywhere else",
			swapFile: &apischema.SwapFile{Path: "/swap.img", Active: true, FSType: "ext4"},
			want:     apischema.SwapModeZswap,
		},
		{
			name:     "inactive swap file anywhere else",
			swapFile: &apischema.SwapFile{Path: "/swap.img", FSType: "xfs"},
			want:     apischema.SwapModeZswap,
		},
		{name: "swap partition and no swap file", partitionActive: true, want: apischema.SwapModeZswap},
		{
			name:            "swap partition beside a swap file on btrfs",
			swapFile:        &apischema.SwapFile{Path: "/swap/swapfile", Active: true, FSType: "btrfs"},
			partitionActive: true,
			want:            apischema.SwapModeZswap,
		},
		{
			name:     "swap file anywhere else on a kernel without zswap",
			swapFile: &apischema.SwapFile{Path: "/swap.img", Active: true, FSType: "ext4"},
			noZswap:  true,
			want:     apischema.SwapModeZram,
		},
		{name: "swap partition on a kernel without zswap", partitionActive: true, noZswap: true, want: apischema.SwapModeDisk},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := recommendMode(test.swapFile, test.partitionActive, !test.noZswap); got != test.want {
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

func TestDrainBlockedReason(t *testing.T) {
	tests := []struct {
		name         string
		diskUsed     uint64
		managedUsed  uint64
		memAvailable uint64
		unitMasked   bool
		want         apischema.SwapDrainBlockedReason
	}{
		{name: "nothing parked", memAvailable: 8 * gib, want: apischema.SwapDrainNothingParked},
		{name: "nothing parked and little memory", memAvailable: 100 * mib, want: apischema.SwapDrainNothingParked},
		{name: "room", diskUsed: 1 * gib, managedUsed: 1 * gib, memAvailable: 4 * gib},
		{name: "no room", diskUsed: 3900 * mib, managedUsed: 3900 * mib, memAvailable: 4 * gib, want: apischema.SwapDrainNotEnoughMemory},
		{name: "more parked than available", diskUsed: 6 * gib, managedUsed: 6 * gib, memAvailable: 4 * gib, want: apischema.SwapDrainNotEnoughMemory},
		{name: "available below the margin", diskUsed: 1 * mib, managedUsed: 1 * mib, memAvailable: 256 * mib, want: apischema.SwapDrainNotEnoughMemory},
		{name: "exactly at the margin", diskUsed: 4*gib - 512*mib, managedUsed: 4*gib - 512*mib, memAvailable: 4 * gib, want: apischema.SwapDrainNotEnoughMemory},
		{name: "one byte inside the margin", diskUsed: 4*gib - 512*mib - 1, managedUsed: 4*gib - 512*mib - 1, memAvailable: 4 * gib},
		{name: "parked only on unmanaged devices", diskUsed: 1 * gib, memAvailable: 8 * gib, want: apischema.SwapDrainUnmanagedDevice},
		{name: "room for the managed share", diskUsed: 6 * gib, managedUsed: 1 * gib, memAvailable: 4 * gib},
		{name: "a masked unit", diskUsed: 1 * gib, managedUsed: 1 * gib, memAvailable: 8 * gib, unitMasked: true, want: apischema.SwapDrainUnitMasked},
		{name: "a masked unit without room", diskUsed: 6 * gib, managedUsed: 6 * gib, memAvailable: 4 * gib, unitMasked: true, want: apischema.SwapDrainUnitMasked},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := drainBlockedReason(test.diskUsed, test.managedUsed, test.memAvailable, test.unitMasked); got != test.want {
				t.Fatalf("drain blocked reason = %q, want %q", got, test.want)
			}
		})
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
	tests := []struct {
		name            string
		toolsInstalled  bool
		partitionActive bool
		want            apischema.ZramUnavailableReason
	}{
		{name: "available", toolsInstalled: true},
		{name: "package missing", want: apischema.ZramPackageMissing},
		{name: "swap partition active", toolsInstalled: true, partitionActive: true, want: apischema.ZramSwapPartitionActive},
		{name: "package missing wins", partitionActive: true, want: apischema.ZramPackageMissing},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := zramUnavailableReason(test.toolsInstalled, test.partitionActive); got != test.want {
				t.Fatalf("zram unavailable reason = %q, want %q", got, test.want)
			}
		})
	}
}
