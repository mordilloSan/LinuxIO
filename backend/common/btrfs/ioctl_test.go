package btrfs

import (
	"testing"
	"unsafe"
)

// The request numbers below are the values strace prints for the kernel's
// own headers, so they pin every struct size in ioctl.go.
func TestIoctlNumbersMatchKernel(t *testing.T) {
	for name, c := range map[string]struct{ got, want uintptr }{
		"RESIZE":           {iocResize, 0x50009403},
		"SCAN_DEV":         {iocScanDev, 0x50009404},
		"FORGET_DEV":       {iocForgetDev, 0x50009405},
		"ADD_DEV":          {iocAddDev, 0x5000940A},
		"SPACE_INFO":       {iocSpaceInfo, 0xC0109414},
		"SCRUB":            {iocScrub, 0xC400941B},
		"SCRUB_CANCEL":     {iocScrubCancel, 0x941C},
		"SCRUB_PROGRESS":   {iocScrubProgress, 0xC400941D},
		"DEV_INFO":         {iocDevInfo, 0xD000941E},
		"FS_INFO":          {iocFSInfo, 0x8400941F},
		"BALANCE_V2":       {iocBalanceV2, 0xC4009420},
		"BALANCE_CTL":      {iocBalanceCtl, 0x40049421},
		"BALANCE_PROGRESS": {iocBalanceProgress, 0x84009422},
		"GET_FSLABEL":      {iocGetFSLabel, 0x81009431},
		"SET_FSLABEL":      {iocSetFSLabel, 0x41009432},
		"GET_DEV_STATS":    {iocGetDevStats, 0xC4089434},
		"DEV_REPLACE":      {iocDevReplace, 0xCA289435},
		"RM_DEV_V2":        {iocRmDevV2, 0x5000943A},
	} {
		if c.got != c.want {
			t.Errorf("%s = %#x, want %#x", name, c.got, c.want)
		}
	}
}

func TestKernelStructOffsets(t *testing.T) {
	var d devInfoArgs
	if off := unsafe.Offsetof(d.Path); off != 3072 {
		t.Errorf("dev_info_args.path offset = %d, want 3072", off)
	}
	var b balanceArgs
	if off := unsafe.Offsetof(b.Stat); off != 424 || unsafe.Sizeof(b.Data) != 136 {
		t.Errorf("balance_args.stat offset = %d, data size = %d", off, unsafe.Sizeof(b.Data))
	}
	var s scrubArgs
	if off := unsafe.Offsetof(s.Progress); off != 32 || unsafe.Sizeof(s.Progress) != 120 {
		t.Errorf("scrub_args.progress offset = %d, size = %d", off, unsafe.Sizeof(s.Progress))
	}
	var r devReplaceArgs
	if off := unsafe.Offsetof(r.Spare); off != 2088 {
		t.Errorf("dev_replace_args.spare offset = %d, want 2088", off)
	}
	var v volArgsV2
	if off := unsafe.Offsetof(v.Name); off != 56 {
		t.Errorf("vol_args_v2.name offset = %d, want 56", off)
	}
}

func TestProfileAndKindNames(t *testing.T) {
	cases := map[uint64][2]string{
		bgData | bgRaid1:               {"data", "raid1"},
		bgMetadata | bgDup:             {"metadata", "dup"},
		bgSystem:                       {"system", "single"},
		bgData | bgMetadata | bgRaid10: {"data+metadata", "raid10"},
		bgData | bgRaid1c3:             {"data", "raid1c3"},
	}
	for flags, want := range cases {
		if kindName(flags) != want[0] || profileName(flags) != want[1] {
			t.Errorf("%#x: %s/%s, want %s/%s", flags, kindName(flags), profileName(flags), want[0], want[1])
		}
	}
	for name, bit := range profileFlags {
		if name != "single" && profileName(bgData|bit) != name {
			t.Errorf("%s round trip failed", name)
		}
	}
}
