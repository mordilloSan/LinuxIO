package btrfs

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"
)

// The request numbers below are the values strace prints for the kernel's
// own headers, so they pin every struct size in ioctl.go.
func TestIoctlNumbersMatchKernel(t *testing.T) {
	for name, c := range map[string]struct{ got, want uintptr }{
		"RESIZE":           {iocResize, 0x50009403},
		"SCAN_DEV":         {iocScanDev, 0x50009404},
		"FORGET_DEV":       {iocForgetDev, 0x50009405},
		"ADD_DEV":          {iocAddDev, 0x5000940A},
		"SUBVOL_CREATE":    {iocSubvolCreate, 0x5000940E},
		"SNAP_DESTROY":     {iocSnapDestroy, 0x5000940F},
		"DEFAULT_SUBVOL":   {iocDefaultSubvol, 0x40089413},
		"SNAP_CREATE_V2":   {iocSnapCreateV2, 0x50009417},
		"SEND":             {iocSend, 0x40489426},
		"QUOTA_CTL":        {iocQuotaCtl, 0xC0109428},
		"QGROUP_LIMIT":     {iocQgroupLimit, 0x8030942B},
		"TREE_SEARCH_V2":   {iocTreeSearchV2, 0xC0709411},
		"INO_LOOKUP":       {iocInoLookup, 0xD0009412},
		"SPACE_INFO":       {iocSpaceInfo, 0xC0109414},
		"SUBVOL_GETFLAGS":  {iocSubvolGetFlags, 0x80089419},
		"SUBVOL_SETFLAGS":  {iocSubvolSetFlags, 0x4008941A},
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

// The replace STATUS layout and states, from struct
// btrfs_ioctl_dev_replace_status_params and BTRFS_IOCTL_DEV_REPLACE_STATE_*.
func TestDevReplaceStatusMatchesKernel(t *testing.T) {
	var status struct {
		ReplaceState, Progress1000, TimeStarted, TimeStopped uint64
		NumWriteErrors, NumUncorrectableReadErrors           uint64
	}
	for name, c := range map[string]struct{ got, want uintptr }{
		"replace_state offset": {replaceStatusState, unsafe.Offsetof(status.ReplaceState)},
		"progress_1000 offset": {replaceStatusPerMill, unsafe.Offsetof(status.Progress1000)},
		"time_started offset":  {replaceStatusTimeStarted, unsafe.Offsetof(status.TimeStarted)},
		"STATE_STARTED":        {replaceStateStarted, 1},
		"STATE_FINISHED":       {replaceStateFinished, 2},
		"STATE_CANCELED":       {replaceStateCanceled, 3},
		"STATE_SUSPENDED":      {replaceStateSuspended, 4},
	} {
		if c.got != c.want {
			t.Errorf("%s = %d, want %d", name, c.got, c.want)
		}
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
	for name, p := range profiles {
		if name != "single" && profileName(bgData|p.flag) != name {
			t.Errorf("%s round trip failed", name)
		}
	}
}

// Since 6.19 any signal pending on the ioctl thread aborts a scrub or a
// replace, so the long ioctls run with every signal blocked but the faults
// a thread raises itself; SIGKILL and SIGSTOP cannot be blocked.
func TestBlockingIoctlBlocksAsyncSignals(t *testing.T) {
	stub := errors.New("stub ioctl")
	var blocked uint64
	var readErr error
	old := fsIoctl
	fsIoctl = func(string, uintptr, unsafe.Pointer) error {
		status, err := os.ReadFile("/proc/thread-self/status")
		if err != nil {
			readErr = err
			return stub
		}
		for line := range strings.Lines(string(status)) {
			if mask, ok := strings.CutPrefix(line, "SigBlk:"); ok {
				blocked, readErr = strconv.ParseUint(strings.TrimSpace(mask), 16, 64)
			}
		}
		return stub
	}
	t.Cleanup(func() { fsIoctl = old })

	if err := blockingIoctl("/data", iocScrub, nil); err != stub {
		t.Fatalf("blockingIoctl = %v, want the ioctl's own error", err)
	}
	if readErr != nil {
		t.Fatal(readErr)
	}
	want := ^uint64(0)
	for _, sig := range []unix.Signal{unix.SIGKILL, unix.SIGSTOP, unix.SIGSEGV, unix.SIGBUS, unix.SIGFPE, unix.SIGILL, unix.SIGTRAP, unix.SIGSYS} {
		want &^= 1 << (sig - 1)
	}
	if blocked != want {
		t.Fatalf("SigBlk = %016x, want %016x", blocked, want)
	}
}

// The texts are btrfs_err_str's for enum btrfs_err_code.
func TestKernelCodeMessages(t *testing.T) {
	if msg := kernelCode(8).Error(); !strings.Contains(msg, "operation in progress") {
		t.Errorf("kernelCode(8) = %q", msg)
	}
	if msg := kernelCode(99).Error(); msg != "kernel refused with code 99" {
		t.Errorf("kernelCode(99) = %q", msg)
	}
}
