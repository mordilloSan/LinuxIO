package memory

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

// writeSwapArea writes a file that starts as a swap area does: a page that
// ends in mkswap's signature.
func (f fixture) writeSwapArea(name string) {
	f.t.Helper()
	page := make([]byte, os.Getpagesize())
	copy(page[len(page)-10:], "SWAPSPACE2")
	f.write(name, string(page))
}

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
	f.writeSwapArea("/dev/zram0")
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

	got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram})
	if err != nil {
		t.Fatal(err)
	}
	assertCalls(t, sys.calls, []string{
		"write " + zramswapConfigPath,
		"write " + sysctlDropInPath,
		"enable " + zramService,
		"stop " + zramService,
		"reset " + zramDeviceName,
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

// zramswap stop leaves the device set up when it cannot unload the zram
// module, and the kernel refuses a new size for a device that is set up: one
// that does not swap is reset before the start, or zram would swap at the
// size it had.
func TestSetSwapModeResetsAZramDeviceLeftSetUp(t *testing.T) {
	f, sys := diskHost(t)
	f.write("/sys/block/zram0/disksize", "268435456\n")
	f.writeSwapArea("/dev/zram0")

	got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram})
	if err != nil {
		t.Fatal(err)
	}
	reset := slices.Index(sys.calls, "reset "+zramDeviceName)
	if reset < 0 || !slices.Contains(sys.calls[reset:], "start "+zramService) {
		t.Fatalf("calls = %v, want the device reset before the start", sys.calls)
	}
	if got.Mode != apischema.SwapModeZram || !plannedDisksize(t, f) {
		t.Errorf("mode = %q, disksize = %s; want zram at the planned size", got.Mode, f.read("/sys/block/zram0/disksize"))
	}
	assertLeft(t, got.Left)
}

// A device that cannot be reset, as one something holds open, comes up at
// the size it had: the run goes on and the result says so.
func TestSetSwapModeSaysWhenTheZramDeviceCouldNotBeReset(t *testing.T) {
	f, sys := diskHost(t)
	f.write("/sys/block/zram0/disksize", "268435456\n")
	f.writeSwapArea("/dev/zram0")
	sys.failures["reset "+zramDeviceName] = syscall.EBUSY

	got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram})
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != apischema.SwapModeZram || plannedDisksize(t, f) {
		t.Errorf("mode = %q, disksize = %s; want zram at the size the device had", got.Mode, f.read("/sys/block/zram0/disksize"))
	}
	assertLeft(t, got.Left, apischema.SwapLeft{What: apischema.SwapLeftZramSize, Reason: apischema.SwapLeftOtherSize})
}

// A zram0 that is set up and holds something other than swap is someone
// else's: it is not reset, and the step refuses before zramswap start would
// format it.
func TestSetSwapModeRefusesAZramDeviceThatHoldsNoSwap(t *testing.T) {
	f, sys := diskHost(t)
	f.write("/sys/block/zram0/disksize", "268435456\n")
	f.write("/dev/zram0", strings.Repeat("ext4 data ", 1024))

	_, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram})
	if !errors.Is(err, bridgeipc.ErrConflict) || !strings.Contains(err.Error(), "/dev/zram0") {
		t.Fatalf("err = %v, want a conflict naming the device", err)
	}
	for _, call := range sys.calls {
		if call == "reset "+zramDeviceName || call == "start "+zramService || call == "restart "+zramService {
			t.Fatalf("calls = %v, want the device neither reset nor started on", sys.calls)
		}
	}
}

// With the zram module unloaded there is no device to reset: zram is
// started as it is on a host that never ran it.
func TestSetSwapModeStartsZramWithoutTheModule(t *testing.T) {
	f, sys := diskHost(t)
	if err := os.RemoveAll(filepath.Join(f.root, "sys/block/zram0")); err != nil {
		t.Fatal(err)
	}

	got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram})
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(sys.calls, "reset "+zramDeviceName) || !slices.Contains(sys.calls, "start "+zramService) {
		t.Fatalf("calls = %v, want a start and no reset", sys.calls)
	}
	if got.Mode != apischema.SwapModeZram {
		t.Errorf("mode = %q, want zram", got.Mode)
	}
}

// zram-generator's device is restarted through its swap unit, when the
// generator made it.
func TestSetSwapModeRestartsTheGeneratorsZramAtThePlannedSize(t *testing.T) {
	f, sys := smallZramHost(t)
	sys.provider = apischema.ZramProviderGenerator
	sys.activeStates[zramGeneratorSetupUnit] = "active"

	got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram})
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

	got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram})
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

			got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram})
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

	if _, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram}); err == nil {
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

	got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram})
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

	got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram})
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

	_, _ = sys.executor(nil).setSwapMode(ctx, apischema.SwapModeRequest{Mode: apischema.SwapModeZram})
	if !slices.Contains(sys.calls, "start "+zramService) || !strings.Contains(f.read(procSwaps), "/dev/zram0") {
		t.Fatalf("calls = %v, want zram started again", sys.calls)
	}
}

// A stop for the restart that fails may still have reached systemd: a start
// is queued behind it, and the swap file is never stopped.
func TestSetSwapModeQueuesAStartWhenTheZramStopFails(t *testing.T) {
	f, sys := smallZramHost(t)
	sys.failAfter["stop "+zramService] = context.Canceled

	_, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram})
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

	got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram})
	if err != nil {
		t.Fatal(err)
	}
	assertLeft(t, got.Left, apischema.SwapLeft{What: apischema.SwapLeftZramSize, Reason: apischema.SwapLeftOtherSize})
}

// generatorStaleHost is a disk host with zram-generator whose zram0 was left
// set up, holding a swap area, by a reset that failed when its swap unit
// stopped: the setup service is failed and nothing swaps on the device.
func generatorStaleHost(t *testing.T) (fixture, *fakeSystem) {
	t.Helper()
	f, sys := generatorDiskHost(t)
	f.write("/sys/block/zram0/disksize", "268435456\n")
	f.writeSwapArea("/dev/zram0")
	sys.activeStates[zramGeneratorSetupUnit] = "failed"
	sys.activeStates[zramGeneratorSwapUnit] = "inactive"
	return f, sys
}

// zram-generator cannot set up a device that is set up already, so a zram0
// its own reset left that way is reset before its swap unit starts.
func TestSetSwapModeResetsTheGeneratorsZramLeftSetUp(t *testing.T) {
	f, sys := generatorStaleHost(t)

	got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram})
	if err != nil {
		t.Fatal(err)
	}
	reset := slices.Index(sys.calls, "reset "+zramDeviceName)
	if reset < 0 || !slices.Contains(sys.calls[reset:], "start "+zramGeneratorSwapUnit) {
		t.Fatalf("calls = %v, want zram0 reset before the swap unit starts", sys.calls)
	}
	if got.Mode != apischema.SwapModeZram || !plannedDisksize(t, f) {
		t.Errorf("mode = %q, disksize = %s; want zram at the planned size", got.Mode, f.read("/sys/block/zram0/disksize"))
	}
}

// The restart at the planned size stops the swap unit, whose reset can fail
// and leave zram0 set up: the device is reset before the start, and zram
// comes back at the planned size.
func TestSetSwapModeRestartsTheGeneratorsZramWhoseResetFailed(t *testing.T) {
	f, sys := smallZramHost(t)
	sys.provider = apischema.ZramProviderGenerator
	f.write(ZramGeneratorPath, "")
	sys.activeStates[zramGeneratorSetupUnit] = "active"
	sys.generatorResetFails = true

	got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram})
	if err != nil {
		t.Fatal(err)
	}
	stop := slices.Index(sys.calls, "stop "+zramGeneratorSwapUnit)
	if stop < 0 || !slices.Contains(sys.calls[stop:], "reset "+zramDeviceName) {
		t.Fatalf("calls = %v, want zram0 reset after the stop", sys.calls)
	}
	if got.Mode != apischema.SwapModeZram || !plannedDisksize(t, f) {
		t.Errorf("mode = %q, disksize = %s; want zram at the planned size", got.Mode, f.read("/sys/block/zram0/disksize"))
	}
}

// A start queued behind a stop that may still take effect finds zram0 set
// up when the generator's reset failed: once zram no longer swaps, the
// device is reset before the start.
func TestSetSwapModeResetsTheGeneratorsZramBeforeAQueuedStart(t *testing.T) {
	f, sys := smallZramHost(t)
	sys.provider = apischema.ZramProviderGenerator
	f.write(ZramGeneratorPath, "")
	sys.activeStates[zramGeneratorSetupUnit] = "active"
	sys.generatorResetFails = true
	sys.failAfter["stop "+zramGeneratorSwapUnit] = context.Canceled

	_, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram})
	if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "a start was queued") {
		t.Fatalf("err = %v, want the cancel and that a start was queued", err)
	}
	reset := slices.Index(sys.calls, "reset "+zramDeviceName)
	if reset < 0 || !slices.Contains(sys.calls[reset:], "start "+zramGeneratorSwapUnit) {
		t.Fatalf("calls = %v, want zram0 reset before the queued start", sys.calls)
	}
	if !strings.Contains(f.read(procSwaps), "/dev/zram0") {
		t.Fatalf("swaps = %q, want zram back", f.read(procSwaps))
	}
}

// A zram0 that swaps is never reset, whichever tool runs it.
func TestResetStaleZramLeavesAZramThatSwaps(t *testing.T) {
	for _, provider := range []apischema.ZramProvider{apischema.ZramProviderTools, apischema.ZramProviderGenerator} {
		t.Run(string(provider), func(t *testing.T) {
			f, sys := smallZramHost(t)
			e := sys.executor(nil)
			r := &modeRun{executor: e, before: apischema.SwapStatus{ZramProvider: provider}}

			if err := r.resetStaleZram(t.Context()); err != nil {
				t.Fatal(err)
			}
			if slices.Contains(sys.calls, "reset "+zramDeviceName) || !strings.Contains(f.read(procSwaps), "/dev/zram0") {
				t.Fatalf("calls = %v, want zram0 left swapping", sys.calls)
			}
		})
	}
}

// The kernel refuses to reset a device something still holds, as udev does
// for a moment after a swapoff: the reset is tried again, and a device that
// stays held fails the step with zram-generator, which cannot set it up,
// naming it, before any start.
func TestSetSwapModeRetriesTheGeneratorsZramReset(t *testing.T) {
	t.Run("freed", func(t *testing.T) {
		_, sys := generatorStaleHost(t)
		sys.busyResets = 3

		if _, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram}); err != nil {
			t.Fatal(err)
		}
		resets := 0
		for _, call := range sys.calls {
			if call == "reset "+zramDeviceName {
				resets++
			}
		}
		if resets != 4 {
			t.Errorf("calls = %v, want three refused resets and one that lands", sys.calls)
		}
	})
	t.Run("still held", func(t *testing.T) {
		f, sys := generatorStaleHost(t)
		sys.busyResets = 1 << 30

		_, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram})
		if !errors.Is(err, syscall.EBUSY) || !strings.Contains(err.Error(), "/dev/zram0") {
			t.Fatalf("err = %v, want the busy device named", err)
		}
		if slices.Contains(sys.calls, "start "+zramGeneratorSwapUnit) || !strings.Contains(f.read(procSwaps), "/swap.img") {
			t.Fatalf("calls = %v, want no start and the swap file on", sys.calls)
		}
	})
}

// While a refused reset is tried again, another tool can start swapping on
// zram0 or format it: what the device holds is read again before each
// attempt, and the reset stops, with no error for a device that now swaps
// and the refusal for one that holds something else.
func TestResetStaleZramChecksTheDeviceBeforeEachAttempt(t *testing.T) {
	t.Run("now swaps", func(t *testing.T) {
		f, sys := generatorStaleHost(t)
		sys.busyResets = 1
		sys.onBusyReset = func() { f.write(procSwaps, f.read(procSwaps)+smallZramSwapsLine) }
		r := &modeRun{executor: sys.executor(nil), before: apischema.SwapStatus{ZramProvider: apischema.ZramProviderGenerator}}

		if err := r.resetStaleZram(t.Context()); err != nil {
			t.Fatal(err)
		}
		assertCalls(t, sys.calls, []string{"reset " + zramDeviceName})
	})
	t.Run("formatted", func(t *testing.T) {
		f, sys := generatorStaleHost(t)
		sys.busyResets = 1
		sys.onBusyReset = func() { f.write("/dev/zram0", strings.Repeat("ext4 data ", 1024)) }
		r := &modeRun{executor: sys.executor(nil), before: apischema.SwapStatus{ZramProvider: apischema.ZramProviderGenerator}}

		err := r.resetStaleZram(t.Context())
		if !errors.Is(err, bridgeipc.ErrConflict) || !strings.Contains(err.Error(), "/dev/zram0") {
			t.Fatalf("err = %v, want a conflict naming the device", err)
		}
		assertCalls(t, sys.calls, []string{"reset " + zramDeviceName})
	})
}
