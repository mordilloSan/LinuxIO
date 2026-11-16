package memory

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

var errNoBus = errors.New("no system bus")

// smallZramSwapsLine is a 256 MiB zram0 holding nothing.
const smallZramSwapsLine = "/dev/zram0                              partition\t262140\t\t0\t\t100\n"

// smallZramHost is diskHost just after zram-tools was installed: its service
// runs a 256 MiB zram0 beside the swap file, from the package's
// configuration.
func smallZramHost(t *testing.T) (fixture, *fakeSystem) {
	t.Helper()
	f, sys := diskHost(t)
	f.write(procSwaps, swapsHeader+smallZramSwapsLine+swapFileSwapsLine)
	f.write("/sys/block/zram0/disksize", "268435456\n")
	sys.devices[zramService] = smallZramSwapsLine
	sys.devices[zramGeneratorSwapUnit] = smallZramSwapsLine
	sys.activeStates[zramService] = "active"
	sys.fileStates[zramService] = "enabled"
	return f, sys
}

func plannedDisksize(t *testing.T, f fixture) bool {
	t.Helper()
	return strings.TrimSpace(f.read("/sys/block/zram0/disksize")) == "8349810688"
}

// zram that swaps at another size, as the one zram-tools starts when it is
// installed, is stopped and started again from the configuration just
// written, while the swap file is still on.
func TestSetSwapModeRestartsZramAtThePlannedSize(t *testing.T) {
	f, sys := smallZramHost(t)

	got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeZram)
	if err != nil {
		t.Fatal(err)
	}
	assertCalls(t, sys.calls, []string{
		"write " + zramswapConfigPath,
		"write " + sysctlDropInPath,
		"enable " + zramService,
		"stop " + zramService,
		"start " + zramService,
		"set " + swappinessParam + "=180",
		"set " + pageClusterParam + "=0",
		"set " + boostParam + "=0",
		"set " + scaleParam + "=125",
		"write " + zswapTmpfilesPath,
		"swapoff /swap.img",
		"mask swap.img.swap",
	})
	if got.Mode != apischema.SwapModeZram || !plannedDisksize(t, f) {
		t.Errorf("mode = %q, disksize = %s; want zram at the planned size", got.Mode, f.read("/sys/block/zram0/disksize"))
	}
	assertLeft(t, got.Left)
}

// zram-generator's device is restarted through its swap unit, when the
// generator made it.
func TestSetSwapModeRestartsTheGeneratorsZramAtThePlannedSize(t *testing.T) {
	f, sys := smallZramHost(t)
	sys.provider = apischema.ZramProviderGenerator
	sys.activeStates[zramGeneratorSetupUnit] = "active"

	got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeZram)
	if err != nil {
		t.Fatal(err)
	}
	stop := slices.Index(sys.calls, "stop "+zramGeneratorSwapUnit)
	if stop < 0 || !slices.Contains(sys.calls[stop:], "start "+zramGeneratorSwapUnit) {
		t.Fatalf("calls = %v, want the generator's swap unit stopped and started", sys.calls)
	}
	if got.Mode != apischema.SwapModeZram || !plannedDisksize(t, f) {
		t.Errorf("mode = %q, disksize = %s; want zram at the planned size", got.Mode, f.read("/sys/block/zram0/disksize"))
	}
	assertLeft(t, got.Left)
}

// zram at the planned size is left alone: a restart empties it.
func TestSetSwapModeLeavesZramAtThePlannedSizeAlone(t *testing.T) {
	_, sys := zramHost(t)

	got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeZram)
	if err != nil {
		t.Fatal(err)
	}
	for _, call := range sys.calls {
		if strings.HasSuffix(call, " "+zramService) && call != "enable "+zramService {
			t.Fatalf("calls = %v, want zramswap.service neither stopped nor started", sys.calls)
		}
	}
	assertLeft(t, got.Left)
}

// zram keeps its size, and the result says why, when stopping it is not
// safe or would not work.
func TestSetSwapModeKeepsZramAtItsSize(t *testing.T) {
	tests := map[string]struct {
		setup func(f fixture, sys *fakeSystem)
		want  []apischema.SwapLeft
	}{
		"its data does not fit in memory": {
			setup: func(f fixture, _ *fakeSystem) {
				// 200 MiB in zram, a full swap file and little free memory.
				f.write(procSwaps, swapsHeader+"/dev/zram0                              partition\t262140\t\t204800\t\t100\n"+
					"/swap.img                               file\t\t2097148\t\t2097148\t\t-2\n")
				f.setMemAvailable(600 * mib)
			},
			want: []apischema.SwapLeft{
				{What: apischema.SwapLeftZramSize, Reason: apischema.SwapLeftNotEnoughMemory},
				{What: apischema.SwapLeftSwapFile, Reason: apischema.SwapLeftNotEnoughMemory},
			},
		},
		"its service does not run it": {
			setup: func(_ fixture, sys *fakeSystem) { sys.activeStates[zramService] = "inactive" },
			want:  []apischema.SwapLeft{{What: apischema.SwapLeftZramSize, Reason: apischema.SwapLeftNotManaged}},
		},
		"no disk swap is active": {
			setup: func(f fixture, _ *fakeSystem) { f.write(procSwaps, swapsHeader+smallZramSwapsLine) },
			want:  []apischema.SwapLeft{{What: apischema.SwapLeftZramSize, Reason: apischema.SwapLeftNoOtherSwap}},
		},
		"a second zram device swaps": {
			setup: func(f fixture, _ *fakeSystem) {
				f.write(procSwaps, f.read(procSwaps)+zram1SwapsLine)
				f.write("/sys/block/zram1/disksize", "1073741824\n")
			},
			want: []apischema.SwapLeft{{What: apischema.SwapLeftZramSize, Reason: apischema.SwapLeftNotManaged}},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			f, sys := smallZramHost(t)
			test.setup(f, sys)

			got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeZram)
			if err != nil {
				t.Fatal(err)
			}
			if slices.Contains(sys.calls, "stop "+zramService) || slices.Contains(sys.calls, "restart "+zramService) {
				t.Fatalf("calls = %v, want zram left running", sys.calls)
			}
			if plannedDisksize(t, f) {
				t.Error("zram is at the planned size, want the size it had")
			}
			assertLeft(t, got.Left, test.want...)
		})
	}
}

// A restart that cannot bring zram back fails the run with the host on its
// swap file, and the sysctls end as disk's.
func TestSetSwapModeFailsWhenZramDoesNotComeBack(t *testing.T) {
	f, sys := smallZramHost(t)
	sys.failures["start "+zramService] = errNoBus

	if _, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeZram); err == nil {
		t.Fatal("want an error")
	}
	if swaps := f.read(procSwaps); !strings.Contains(swaps, "/swap.img") || strings.Contains(swaps, "/dev/zram0") {
		t.Fatalf("swaps = %q, want the swap file on and zram stopped", swaps)
	}
	if slices.Contains(sys.calls, "swapoff /swap.img") {
		t.Fatalf("calls = %v, want the swap file never stopped", sys.calls)
	}
	if got := f.param(swappinessParam); got != "60" {
		t.Errorf("swappiness = %s, want 60", got)
	}
}

// zramswap.service that reads active with nothing swapping, as after a
// swapoff by hand, would do nothing on a start: it is restarted.
func TestSetSwapModeRestartsAZramServiceThatSwapsNothing(t *testing.T) {
	_, sys := diskHost(t)
	sys.activeStates[zramService] = "active"

	got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeZram)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(sys.calls, "restart "+zramService) || slices.Contains(sys.calls, "start "+zramService) {
		t.Fatalf("calls = %v, want zramswap.service restarted", sys.calls)
	}
	if got.Mode != apischema.SwapModeZram {
		t.Errorf("mode = %q, want zram", got.Mode)
	}
}

// A zram device whose size cannot be read is not emptied to find out.
func TestSetSwapModeLeavesZramOfUnknownSizeAlone(t *testing.T) {
	f, sys := smallZramHost(t)
	if err := os.RemoveAll(filepath.Join(f.root, "sys/block/zram0")); err != nil {
		t.Fatal(err)
	}

	got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeZram)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(sys.calls, "stop "+zramService) {
		t.Fatalf("calls = %v, want zram left running", sys.calls)
	}
	assertLeft(t, got.Left)
}

// Once zram is stopped for the restart, bringing it back is not cancelled
// with the request.
func TestSetSwapModeBringsZramBackAfterACancel(t *testing.T) {
	f, sys := smallZramHost(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	sys.after = func(call string) {
		if call == "stop "+zramService {
			cancel()
		}
	}

	_, _ = sys.executor(nil).setSwapMode(ctx, apischema.SwapModeZram)
	if !slices.Contains(sys.calls, "start "+zramService) || !strings.Contains(f.read(procSwaps), "/dev/zram0") {
		t.Fatalf("calls = %v, want zram started again", sys.calls)
	}
}

// A stop for the restart that fails may still have reached systemd: a start
// is queued behind it, and the swap file is never stopped.
func TestSetSwapModeQueuesAStartWhenTheZramStopFails(t *testing.T) {
	f, sys := smallZramHost(t)
	sys.failAfter["stop "+zramService] = context.Canceled

	_, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeZram)
	if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "a start was queued") {
		t.Fatalf("err = %v, want the cancel and that a start was queued", err)
	}
	swaps := f.read(procSwaps)
	if !strings.Contains(swaps, "/dev/zram0") || !strings.Contains(swaps, "/swap.img") || slices.Contains(sys.calls, "swapoff /swap.img") {
		t.Fatalf("swaps = %q, calls = %v; want zram back and the swap file on", swaps, sys.calls)
	}
}

// A provider that brings zram back at another size than planned, as one a
// drop-in overrides, is reported: every later change would restart it.
func TestSetSwapModeSaysWhenZramComesBackAtAnotherSize(t *testing.T) {
	f, sys := smallZramHost(t)
	sys.after = func(call string) {
		if call == "start "+zramService {
			f.write("/sys/block/zram0/disksize", "4294967296\n")
		}
	}

	got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeZram)
	if err != nil {
		t.Fatal(err)
	}
	assertLeft(t, got.Left, apischema.SwapLeft{What: apischema.SwapLeftZramSize, Reason: apischema.SwapLeftOtherSize})
}
