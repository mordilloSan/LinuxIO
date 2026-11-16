package memory

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

const (
	swappinessParam  = "/proc/sys/vm/swappiness"
	pageClusterParam = "/proc/sys/vm/page-cluster"
	boostParam       = "/proc/sys/vm/watermark_boost_factor"
	scaleParam       = "/proc/sys/vm/watermark_scale_factor"
)

// assertCalls compares the side effects in order, without the two stop
// timeout calls that wrap every stop of a swap unit;
// TestStopSwapUnitRaisesTheTimeoutForTheStop checks those.
func assertCalls(t *testing.T, got, want []string) {
	t.Helper()
	got = slices.DeleteFunc(slices.Clone(got), func(call string) bool {
		return strings.HasPrefix(call, raiseTimeoutCall) || strings.HasPrefix(call, restoreTimeoutCall)
	})
	if !slices.Equal(got, want) {
		t.Fatalf("calls =\n  %s\nwant\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

func TestSetSwapModeToZramFromDisk(t *testing.T) {
	f, sys := diskHost(t)
	var progress []apischema.SwapProgress

	got, err := sys.executor(&progress).setSwapMode(t.Context(), apischema.SwapModeZram)
	if err != nil {
		t.Fatal(err)
	}

	assertCalls(t, sys.calls, []string{
		"write " + zramswapConfigPath,
		"write " + sysctlDropInPath,
		"enable " + zramService,
		"start " + zramService,
		"set " + swappinessParam + "=180",
		"set " + pageClusterParam + "=0",
		"set " + boostParam + "=0",
		"set " + scaleParam + "=125",
		"write " + zswapTmpfilesPath,
		"stop swap.img.swap",
		"mask swap.img.swap",
	})
	if got.Mode != apischema.SwapModeZram || got.Swappiness != 180 {
		t.Errorf("status mode = %q swappiness = %d, want zram, 180", got.Mode, got.Swappiness)
	}
	if got := f.read(zramswapConfigPath); got != string(renderZramswapConfig(16308408*kib/2)) {
		t.Errorf("zramswap config = %q", got)
	}
	wantDropIn := "# Managed by LinuxIO (Dashboard, Memory); changes here are overwritten.\n" +
		"vm.swappiness = 180\nvm.page-cluster = 0\nvm.watermark_boost_factor = 0\nvm.watermark_scale_factor = 125\n"
	if got := f.read(sysctlDropInPath); got != wantDropIn {
		t.Errorf("drop-in = %q, want %q", got, wantDropIn)
	}
	wantProgress := []apischema.SwapProgress{
		{Step: apischema.SwapStepWriteConfig, Index: 1, Total: 5},
		{Step: apischema.SwapStepStartZram, Index: 2, Total: 5},
		{Step: apischema.SwapStepApplySysctls, Index: 3, Total: 5},
		{Step: apischema.SwapStepDisableZswap, Index: 4, Total: 5},
		{Step: apischema.SwapStepStopSwapFile, Index: 5, Total: 5},
	}
	if !slices.Equal(progress, wantProgress) {
		t.Errorf("progress = %+v, want %+v", progress, wantProgress)
	}
}

func TestSetSwapModeToZswapFromZram(t *testing.T) {
	f, sys := zramHost(t)

	got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeZswap)
	if err != nil {
		t.Fatal(err)
	}

	assertCalls(t, sys.calls, []string{
		"unmask swap.img.swap",
		"start swap.img.swap",
		"stop " + zramService,
		"disable " + zramService,
		"write " + sysctlDropInPath,
		"set " + swappinessParam + "=60",
		"set " + pageClusterParam + "=3",
		"set " + boostParam + "=15000",
		"set " + scaleParam + "=10",
		"set " + zswapCompressorParam + "=zstd",
		"set " + zswapEnabledParam + "=Y",
		"write " + zswapTmpfilesPath,
	})
	if got.Mode != apischema.SwapModeZswap {
		t.Errorf("status mode = %q, want zswap", got.Mode)
	}
	wantDropIn := "# Managed by LinuxIO (Dashboard, Memory); changes here are overwritten.\nvm.swappiness = 60\n"
	if got := f.read(sysctlDropInPath); got != wantDropIn {
		t.Errorf("drop-in = %q, want %q", got, wantDropIn)
	}
	if got, want := f.read(zswapTmpfilesPath), string(renderZswapTmpfiles("zstd")); got != want {
		t.Errorf("tmpfiles = %q, want %q", got, want)
	}
}

func TestSetSwapModeToDiskFromZswap(t *testing.T) {
	f, sys := diskHost(t)
	f.write(zswapEnabledParam, "Y\n")
	f.write(zswapCompressorParam, "zstd\n")
	f.write(zswapTmpfilesPath, string(renderZswapTmpfiles("zstd")))

	got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeDisk)
	if err != nil {
		t.Fatal(err)
	}

	// The host never ran zram's values, so the other sysctls stay as they are.
	assertCalls(t, sys.calls, []string{
		"disable " + zramService,
		"write " + sysctlDropInPath,
		"set " + swappinessParam + "=60",
		"set " + zswapEnabledParam + "=N",
		"write " + zswapTmpfilesPath,
	})
	if got.Mode != apischema.SwapModeDisk {
		t.Errorf("status mode = %q, want disk", got.Mode)
	}
	if got, want := f.read(zswapTmpfilesPath), string(renderZswapOffTmpfiles()); got != want {
		t.Errorf("tmpfiles = %q, want %q: zswap stays off at boot", got, want)
	}
}

// A kernel that starts with zswap on returns to it at boot unless a file
// turns it off, whether zswap is on now or was turned off before, so leaving
// zswap always writes that file.
func TestSetSwapModeKeepsZswapOffAtBoot(t *testing.T) {
	for name, enabled := range map[string]string{"zswap on": "Y\n", "zswap already off": "N\n"} {
		t.Run(name, func(t *testing.T) {
			f, sys := diskHost(t)
			f.write(zswapEnabledParam, enabled)

			if _, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeZram); err != nil {
				t.Fatal(err)
			}

			if got := f.param(zswapEnabledParam); got != "N" {
				t.Errorf("zswap enabled = %q, want N", got)
			}
			if got, want := f.read(zswapTmpfilesPath), string(renderZswapOffTmpfiles()); got != want {
				t.Errorf("tmpfiles = %q, want %q", got, want)
			}
		})
	}
}

// A file that already turns zswap off is not written again, and a kernel
// without zswap gets none.
func TestSetSwapModeWritesNoZswapFileItDoesNotNeed(t *testing.T) {
	f, sys := diskHost(t)
	f.write(zswapTmpfilesPath, string(renderZswapOffTmpfiles()))
	if _, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeDisk); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(sys.calls, "write "+zswapTmpfilesPath) {
		t.Errorf("calls = %v, want the off file left alone", sys.calls)
	}

	f, sys = diskHost(t)
	if err := os.RemoveAll(filepath.Join(f.root, "sys/module/zswap")); err != nil {
		t.Fatal(err)
	}
	f.write(zswapTmpfilesPath, string(renderZswapTmpfiles("zstd")))
	if _, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeDisk); err != nil {
		t.Fatal(err)
	}
	if f.exists(zswapTmpfilesPath) {
		t.Error("tmpfiles file kept on a kernel without zswap")
	}
}

// A kernel that rejects zstd keeps its compressor: ENOENT without the
// compressor, EINVAL when no pool can be built with it. zswap is still
// enabled and the tmpfiles file persists what the kernel took.
func TestSetSwapModeToZswapKeepsTheCompressorTheKernelHas(t *testing.T) {
	for _, errno := range []syscall.Errno{syscall.EINVAL, syscall.ENOENT} {
		t.Run(errno.Error(), func(t *testing.T) {
			f, sys := diskHost(t)
			sys.failures["set "+zswapCompressorParam+"=zstd"] = &fs.PathError{Op: "write", Path: zswapCompressorParam, Err: errno}

			got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeZswap)
			if err != nil {
				t.Fatal(err)
			}
			if !got.Zswap.Enabled || got.Zswap.Compressor != "lzo" {
				t.Errorf("zswap = %+v, want enabled with lzo", got.Zswap)
			}
			if got, want := f.read(zswapTmpfilesPath), string(renderZswapTmpfiles("lzo")); got != want {
				t.Errorf("tmpfiles = %q, want %q", got, want)
			}
		})
	}
}

// Any other refusal of the compressor fails the step.
func TestSetSwapModeToZswapFailsOnAnotherCompressorError(t *testing.T) {
	f, sys := diskHost(t)
	sys.failures["set "+zswapCompressorParam+"=zstd"] = &fs.PathError{Op: "write", Path: zswapCompressorParam, Err: syscall.EIO}

	if _, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeZswap); err == nil || !strings.Contains(err.Error(), string(apischema.SwapStepEnableZswap)) {
		t.Fatalf("err = %v, want the enable_zswap step to fail", err)
	}
	if f.param(zswapEnabledParam) != "N" {
		t.Errorf("zswap enabled = %q, want N", f.param(zswapEnabledParam))
	}
}

// zram that never shows up in /proc/swaps fails its step within the bound,
// before any sysctl changes; the run puts back the files it wrote and
// stops and disables the service it enabled.
func TestSetSwapModeFailsWhenZramNeverAppears(t *testing.T) {
	f, sys := diskHost(t)
	sys.stuck[zramService] = true

	start := time.Now()
	_, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeZram)
	if err == nil || !strings.Contains(err.Error(), string(apischema.SwapStepStartZram)) || !errors.Is(err, errWaitTimedOut) {
		t.Fatalf("err = %v, want the start_zram step to time out", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("failed after %s, want within the bound", elapsed)
	}
	assertCalls(t, sys.calls, []string{
		"write " + zramswapConfigPath,
		"write " + sysctlDropInPath,
		"enable " + zramService,
		"start " + zramService,
		// rollback
		"stop " + zramService,
		"disable " + zramService,
		"remove " + sysctlDropInPath,
		"write " + zramswapConfigPath,
	})
	if got := f.read(zramswapConfigPath); got != zramswapPackageDefault {
		t.Errorf("zramswap config = %q, want the package default back", got)
	}
	if f.exists(sysctlDropInPath) {
		t.Error("drop-in left behind")
	}
}

// zramswap.service enabled before the run stays enabled when zram does not
// come up: the run only undoes what it did.
func TestSetSwapModeKeepsAZramServiceItDidNotEnable(t *testing.T) {
	_, sys := diskHost(t)
	sys.stuck[zramService] = true
	sys.fileStates[zramService] = "enabled"

	if _, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeZram); err == nil {
		t.Fatal("want an error")
	}
	if slices.Contains(sys.calls, "disable "+zramService) {
		t.Fatalf("calls = %v, want zramswap.service left enabled", sys.calls)
	}
}

// Without room for the swap file's data the last step is skipped: the run
// succeeds, the file stays active and the host reports mixed.
func TestSetSwapModeToZramLeavesTheSwapFileWithoutRoom(t *testing.T) {
	f, sys := diskHost(t)
	f.setMemAvailable(900 * mib)

	got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeZram)
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != apischema.SwapModeMixed {
		t.Errorf("mode = %q, want mixed", got.Mode)
	}
	assertLeft(t, got.Left, apischema.SwapLeft{What: apischema.SwapLeftSwapFile, Reason: apischema.SwapLeftNotEnoughMemory})
	if slices.Contains(sys.calls, "stop swap.img.swap") || slices.Contains(sys.calls, "mask swap.img.swap") {
		t.Fatalf("calls = %v, want the swap file left alone", sys.calls)
	}
}

// The room is read just before the swap file stops, not when the request
// arrived: memory taken while zram starts counts.
func TestSetSwapModeChecksRoomOnFreshStatus(t *testing.T) {
	f, sys := diskHost(t)
	sys.after = func(call string) {
		if call == "start "+zramService {
			f.setMemAvailable(900 * mib)
		}
	}

	got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeZram)
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != apischema.SwapModeMixed || slices.Contains(sys.calls, "stop swap.img.swap") {
		t.Fatalf("mode = %q, calls = %v; want the swap file left active", got.Mode, sys.calls)
	}
}

// A fstab swap file that is not active is masked so it stays off at boot;
// another active swap file fstab does not name is never stopped or masked.
func TestSetSwapModeToZramNeverTouchesAnUnmatchedSwapFile(t *testing.T) {
	f, sys := diskHost(t)
	f.write(procSwaps, swapsHeader+"/home/extra                             file\t\t1048572\t\t0\t\t-2\n")
	f.write("/home/extra", "")

	got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeZram)
	if err != nil {
		t.Fatal(err)
	}
	for _, call := range sys.calls {
		if strings.HasPrefix(call, "stop ") {
			t.Fatalf("calls = %v, want nothing stopped", sys.calls)
		}
	}
	if !slices.Contains(sys.calls, "mask swap.img.swap") || got.Mode != apischema.SwapModeMixed {
		t.Fatalf("mode = %q, calls = %v; want swap.img.swap masked and the extra file left", got.Mode, sys.calls)
	}
	assertLeft(t, got.Left, apischema.SwapLeft{What: apischema.SwapLeftSwapFile, Reason: apischema.SwapLeftNotManaged})
}

// An active swap file that cannot be resolved, such as one deleted while it
// swaps, may be the fstab one: its unit is neither stopped nor masked.
func TestSetSwapModeToZramMasksOnlyAProvenInactiveSwapFile(t *testing.T) {
	f, sys := diskHost(t)
	f.write(procSwaps, swapsHeader+"/swap.img\\040(deleted)                  file\t\t2097148\t\t0\t\t-2\n")

	got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeZram)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(sys.calls, "mask swap.img.swap") || slices.Contains(sys.calls, "stop swap.img.swap") {
		t.Fatalf("calls = %v, want swap.img.swap left alone", sys.calls)
	}
	if got.Mode != apischema.SwapModeMixed {
		t.Errorf("mode = %q, want mixed", got.Mode)
	}
	assertLeft(t, got.Left, apischema.SwapLeft{What: apischema.SwapLeftSwapFile, Reason: apischema.SwapLeftNotIdentified})
}

// A failure after zram is up keeps zram swapping, disables the service the
// run enabled, puts the files and the live values back and names the failed
// step.
func TestSetSwapModeRollsBackFilesAndLiveValues(t *testing.T) {
	f, sys := diskHost(t)
	f.write(zswapEnabledParam, "Y\n")
	sys.failures["set "+zswapEnabledParam+"=N"] = errors.New("device busy")

	_, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeZram)
	if err == nil || !strings.Contains(err.Error(), string(apischema.SwapStepDisableZswap)) || !strings.Contains(err.Error(), "device busy") {
		t.Fatalf("err = %v, want the disable_zswap step named", err)
	}
	if slices.Contains(sys.calls, "stop "+zramService) || sys.fileStates[zramService] != "disabled" {
		t.Fatalf("calls = %v, want zram kept and zramswap.service disabled again", sys.calls)
	}
	if !strings.Contains(f.read(procSwaps), "/dev/zram0") {
		t.Fatal("zram left /proc/swaps")
	}
	for param, want := range map[string]string{swappinessParam: "60", pageClusterParam: "3", boostParam: "15000", scaleParam: "10", zswapEnabledParam: "Y"} {
		if got := f.param(param); got != want {
			t.Errorf("%s = %q, want %q back", param, got, want)
		}
	}
	if got := f.read(zramswapConfigPath); got != zramswapPackageDefault {
		t.Errorf("zramswap config = %q, want the package default back", got)
	}
	if f.exists(sysctlDropInPath) {
		t.Error("drop-in left behind")
	}
}

// A swap file this run started stays on when a later step fails.
func TestSetSwapModeKeepsTheSwapFileItStarted(t *testing.T) {
	f, sys := zramHost(t)
	sys.failures["write "+sysctlDropInPath] = errors.New("read-only file system")

	_, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeDisk)
	if err == nil || !strings.Contains(err.Error(), string(apischema.SwapStepApplySysctls)) {
		t.Fatalf("err = %v, want the apply_sysctls step named", err)
	}
	if slices.Contains(sys.calls, "stop swap.img.swap") || !strings.Contains(f.read(procSwaps), "/swap.img") {
		t.Fatalf("calls = %v, want the swap file kept on", sys.calls)
	}
}

// A rollback action that fails is joined to the step's error.
func TestSetSwapModeJoinsAFailedRollback(t *testing.T) {
	f, sys := diskHost(t)
	f.write(zswapEnabledParam, "Y\n")
	sys.failures["set "+zswapEnabledParam+"=N"] = errors.New("device busy")
	sys.failures["set "+swappinessParam+"=60"] = errors.New("swappiness refused")

	_, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeZram)
	if err == nil || !strings.Contains(err.Error(), "device busy") || !strings.Contains(err.Error(), "swappiness refused") {
		t.Fatalf("err = %v, want the step error and the rollback error", err)
	}
	// The other rollback actions still ran.
	if got := f.param(pageClusterParam); got != "3" {
		t.Errorf("page-cluster = %q, want 3 back", got)
	}
	if got := f.read(zramswapConfigPath); got != zramswapPackageDefault {
		t.Errorf("zramswap config = %q, want the package default back", got)
	}
}

// Leaving zram with room stops zram once the swap file is back, and room
// counts the free space on disk swap.
func TestSetSwapModeStopsZramOnlyWithRoom(t *testing.T) {
	tests := []struct {
		name         string
		memAvailable uint64
		wantStopped  bool
	}{
		// 256 MiB in zram + 512 MiB margin; the swap file adds 1.5 GiB free.
		{name: "room in memory", memAvailable: 1 * gib, wantStopped: true},
		{name: "room with free disk swap", memAvailable: 100 * mib, wantStopped: true},
		{name: "no room", memAvailable: 10 * mib},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f, sys := zramHost(t)
			f.setMemAvailable(test.memAvailable)
			if test.name == "no room" {
				sys.devices["swap.img.swap"] = "/swap.img                               file\t\t1048572\t\t1048572\t\t-2\n"
			}

			got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeDisk)
			if err != nil {
				t.Fatal(err)
			}
			if stopped := slices.Contains(sys.calls, "stop "+zramService); stopped != test.wantStopped {
				t.Fatalf("zram stopped = %v, want %v (calls %v)", stopped, test.wantStopped, sys.calls)
			}
			if !test.wantStopped && got.Mode != apischema.SwapModeMixed {
				t.Errorf("mode = %q, want mixed", got.Mode)
			}
		})
	}
}

// Under TuneD a mode change writes no swappiness, live or in the drop-in,
// and keeps the drop-in's swappiness line; the other keys still apply.
func TestSetSwapModeUnderTunedLeavesSwappinessAlone(t *testing.T) {
	f, sys := diskHost(t)
	sys.tuned = true
	f.write(sysctlDropInPath, "vm.swappiness = 30\n")

	if _, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeZram); err != nil {
		t.Fatal(err)
	}
	for _, call := range sys.calls {
		if strings.HasPrefix(call, "set "+swappinessParam) {
			t.Fatalf("calls = %v, want swappiness untouched", sys.calls)
		}
	}
	if got := f.param(swappinessParam); got != "60" {
		t.Errorf("swappiness = %q, want 60", got)
	}
	wantDropIn := "# Managed by LinuxIO (Dashboard, Memory); changes here are overwritten.\n" +
		"vm.swappiness = 30\nvm.page-cluster = 0\nvm.watermark_boost_factor = 0\nvm.watermark_scale_factor = 125\n"
	if got := f.read(sysctlDropInPath); got != wantDropIn {
		t.Errorf("drop-in = %q, want %q", got, wantDropIn)
	}
}

func TestSetSwapModeRefusesBeforeChangingAnything(t *testing.T) {
	_, sys := diskHost(t)
	sys.provider = ""

	_, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeZram)
	if !errors.Is(err, bridgeipc.ErrConflict) || len(sys.calls) != 0 {
		t.Fatalf("err = %v, calls = %v; want a conflict and no change", err, sys.calls)
	}
	if _, err := sys.executor(nil).setSwapMode(t.Context(), "fast"); !errors.Is(err, bridgeipc.ErrInvalidArgs) {
		t.Fatalf("unknown mode err = %v, want invalid arguments", err)
	}
}

// One memory change runs at a time across the three routes.
func TestMemoryMutationsRunOneAtATime(t *testing.T) {
	f, sys := diskHost(t)
	started, release := make(chan struct{}), make(chan struct{})
	sys.after = func(call string) {
		if call == "start "+zramService {
			close(started)
			<-release
		}
	}
	done := make(chan error, 1)
	go func() {
		_, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeZram)
		done <- err
	}()
	<-started

	second := newFakeSystem(f)
	if _, err := second.executor(nil).setSwapMode(t.Context(), apischema.SwapModeDisk); !errors.Is(err, bridgeipc.ErrConflict) {
		t.Errorf("set_swap_mode err = %v, want a conflict", err)
	}
	if _, err := second.executor(nil).setSwappiness(t.Context(), 100); !errors.Is(err, bridgeipc.ErrConflict) {
		t.Errorf("set_swappiness err = %v, want a conflict", err)
	}
	if _, err := second.executor(nil).drainSwap(t.Context()); !errors.Is(err, bridgeipc.ErrConflict) {
		t.Errorf("drain_swap err = %v, want a conflict", err)
	}
	if len(second.calls) != 0 {
		t.Errorf("refused mutations made calls %v", second.calls)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("first mutation: %v", err)
	}
}

// assertRolledBackToDisk checks that diskHost's files and live values are as
// they were before a failed switch to zram.
func assertRolledBackToDisk(t *testing.T, f fixture) {
	t.Helper()
	for param, want := range map[string]string{swappinessParam: "60", pageClusterParam: "3", boostParam: "15000", scaleParam: "10"} {
		if got := f.param(param); got != want {
			t.Errorf("%s = %q, want %q back", param, got, want)
		}
	}
	if got := f.read(zramswapConfigPath); got != zramswapPackageDefault {
		t.Errorf("zramswap config = %q, want the package default back", got)
	}
	if f.exists(sysctlDropInPath) {
		t.Error("drop-in left behind")
	}
}

// systemd kills a swapoff that outlives the unit's timeout and keeps the
// swap unit active: the step fails as soon as systemd gives up, not at the
// route's own bound, and nothing is masked; zram stays and the run's files
// and live values are put back.
func TestSetSwapModeFailsFastWhenSystemdGivesUpStoppingTheSwapFile(t *testing.T) {
	f, sys := diskHost(t)
	sys.stops["swap.img.swap"] = &fakeStop{polls: 3, ends: "active"}
	e := sys.executor(nil)
	e.waits.swapOff = time.Minute

	start := time.Now()
	_, err := e.setSwapMode(t.Context(), apischema.SwapModeZram)
	if !errors.Is(err, errStopAbandoned) || !strings.Contains(err.Error(), string(apischema.SwapStepStopSwapFile)) ||
		!strings.Contains(err.Error(), "still active") {
		t.Fatalf("err = %v, want the stop_swap_file step to report systemd giving up", err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("failed after %s, want as soon as systemd gave up", elapsed)
	}
	if slices.Contains(sys.calls, "mask swap.img.swap") {
		t.Fatalf("calls = %v, want nothing masked", sys.calls)
	}
	swaps := f.read(procSwaps)
	if !strings.Contains(swaps, "/dev/zram0") || !strings.Contains(swaps, "/swap.img") {
		t.Fatalf("/proc/swaps = %q, want zram and the swap file both kept", swaps)
	}
	assertRolledBackToDisk(t, f)
}

// A zramswap.service whose stop timed out or failed ends failed with zram
// still swapping: the step fails at once, the service stays enabled and the
// swap file the run started stays on.
func TestSetSwapModeFailsFastWhenSystemdGivesUpStoppingZram(t *testing.T) {
	f, sys := zramHost(t)
	sys.stops[zramService] = &fakeStop{polls: 3, ends: "failed"}
	e := sys.executor(nil)
	e.waits.swapOff = time.Minute

	start := time.Now()
	_, err := e.setSwapMode(t.Context(), apischema.SwapModeDisk)
	if !errors.Is(err, errStopAbandoned) || !strings.Contains(err.Error(), string(apischema.SwapStepStopZram)) {
		t.Fatalf("err = %v, want the stop_zram step to report systemd giving up", err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("failed after %s, want as soon as systemd gave up", elapsed)
	}
	if slices.Contains(sys.calls, "disable "+zramService) {
		t.Fatalf("calls = %v, want zramswap.service left enabled", sys.calls)
	}
	if !strings.Contains(f.read(procSwaps), "/swap.img") {
		t.Fatal("the swap file the run started is off")
	}
	assertZramHostUnchanged(t, f)
}

// assertZramHostUnchanged checks that zramHost's files and live values are
// as they were before a failed switch away from zram.
// assertLeft checks what a mode change reports it left as it was.
func assertLeft(t *testing.T, got []apischema.SwapLeft, want ...apischema.SwapLeft) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("left = %+v, want %+v", got, want)
	}
}

func assertZramHostUnchanged(t *testing.T, f fixture) {
	t.Helper()
	for param, want := range map[string]string{swappinessParam: "180", pageClusterParam: "0", boostParam: "0", scaleParam: "125", zswapEnabledParam: "N", zswapCompressorParam: "lzo"} {
		if got := f.param(param); got != want {
			t.Errorf("%s = %q, want %q back", param, got, want)
		}
	}
	wantDropIn := string(renderSysctlDropIn([]sysctlEntry{
		{key: "vm.swappiness", value: "180"},
		{key: "vm.page-cluster", value: "0"},
		{key: "vm.watermark_boost_factor", value: "0"},
		{key: "vm.watermark_scale_factor", value: "125"},
	}))
	if got := f.read(sysctlDropInPath); got != wantDropIn {
		t.Errorf("drop-in = %q, want %q back", got, wantDropIn)
	}
	if got, want := f.read(zramswapConfigPath), string(renderZramswapConfig(7963*mib)); got != want {
		t.Errorf("zramswap config = %q, want %q", got, want)
	}
	if f.exists(zswapTmpfilesPath) {
		t.Error("zswap tmpfiles file left behind")
	}
}

// zramDropIn is the sysctl drop-in of the zram mode.
var zramDropIn = string(renderSysctlDropIn([]sysctlEntry{
	{key: "vm.swappiness", value: "180"},
	{key: "vm.page-cluster", value: "0"},
	{key: "vm.watermark_boost_factor", value: "0"},
	{key: "vm.watermark_scale_factor", value: "125"},
}))

// assertDiskSysctls checks that the live sysctls are the disk mode's:
// swappiness 60 and the kernel's own readahead and watermarks.
func assertDiskSysctls(t *testing.T, f fixture) {
	t.Helper()
	for param, want := range map[string]string{swappinessParam: "60", pageClusterParam: "3", boostParam: "15000", scaleParam: "10"} {
		if got := f.param(param); got != want {
			t.Errorf("%s = %q, want %q", param, got, want)
		}
	}
}

// A switch from zram that fails after zram stopped leaves the host on the
// swap file the run started, since swap is never undone. The rollback puts
// the files and zswap back, and the live sysctls end as disk's and not as
// the zram ones the rollback restored: the host swaps to disk only now. The
// drop-in stays zram's, like the zram configuration the next boot starts.
func TestSetSwapModeFromZramThatFailsEndsWithDiskSysctls(t *testing.T) {
	f, sys := zramHost(t)
	sys.failures["set "+zswapCompressorParam+"=zstd"] = errors.New("device busy")

	_, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeZswap)
	if err == nil || !strings.Contains(err.Error(), string(apischema.SwapStepEnableZswap)) {
		t.Fatalf("err = %v, want the enable_zswap step named", err)
	}
	if swaps := f.read(procSwaps); !strings.Contains(swaps, "/swap.img") || strings.Contains(swaps, "/dev/zram0") {
		t.Fatalf("swaps = %q, want the swap file on and zram stopped", swaps)
	}
	assertDiskSysctls(t, f)
	if got := f.read(sysctlDropInPath); got != zramDropIn {
		t.Errorf("drop-in = %q, want zram's %q back", got, zramDropIn)
	}
	if got, want := f.read(zramswapConfigPath), string(renderZramswapConfig(7963*mib)); got != want {
		t.Errorf("zramswap config = %q, want %q back", got, want)
	}
	if got := f.param(zswapEnabledParam); got != "N" {
		t.Errorf("zswap enabled = %q, want N back", got)
	}
}

// A stop whose command removed zram and then exited non-zero leaves the unit
// failed in the same moment the device left: the device is gone, so the step
// succeeds rather than reporting that systemd gave up.
func TestSetSwapModeTakesADeviceGoneAsStoppedWhateverTheUnitSays(t *testing.T) {
	_, sys := zramHost(t)
	sys.stops[zramService] = &fakeStop{polls: 2, ends: "failed", leaves: true}

	got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeDisk)
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != apischema.SwapModeDisk || !slices.Contains(sys.calls, "disable "+zramService) {
		t.Fatalf("mode = %q, calls = %v; want disk with zramswap.service disabled", got.Mode, sys.calls)
	}
}

// A swapoff that takes a while is waited for: the unit stays active while
// its job waits, then deactivating, and the step succeeds once the device
// leaves.
func TestSetSwapModeWaitsForASlowSwapoff(t *testing.T) {
	_, sys := diskHost(t)
	sys.stops["swap.img.swap"] = &fakeStop{queued: 3, polls: 5}

	got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeZram)
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != apischema.SwapModeZram || !slices.Contains(sys.calls, "mask swap.img.swap") {
		t.Fatalf("mode = %q, calls = %v; want zram with the swap file masked", got.Mode, sys.calls)
	}
}

// A request cancelled while zram is starting still rolls back: the undo runs
// on a context the cancel does not reach, so systemd is still asked to stop
// and disable the service this run enabled.
func TestSetSwapModeRollsBackOnADetachedContextAfterACancel(t *testing.T) {
	f, sys := diskHost(t)
	sys.stuck[zramService] = true
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	sys.after = func(call string) {
		if call == "start "+zramService {
			cancel()
		}
	}

	_, err := sys.executor(nil).setSwapMode(ctx, apischema.SwapModeZram)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want the cancel", err)
	}
	if sys.fileStates[zramService] != "disabled" || !slices.Contains(sys.calls, "stop "+zramService) {
		t.Fatalf("calls = %v, file state = %q; want zramswap.service stopped and disabled", sys.calls, sys.fileStates[zramService])
	}
	assertRolledBackToDisk(t, f)
}

// From zswap, a failed switch to zram puts back the tmpfiles file it replaced
// and turns zswap on again.
func TestSetSwapModeRestoresTheZswapTmpfilesAndSwitch(t *testing.T) {
	f, sys := diskHost(t)
	f.write(zswapEnabledParam, "Y\n")
	f.write(zswapCompressorParam, "zstd\n")
	tmpfiles := string(renderZswapTmpfiles("zstd"))
	f.write(zswapTmpfilesPath, tmpfiles)
	sys.failures["mask swap.img.swap"] = errors.New("unit file is read-only")

	if _, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeZram); err == nil {
		t.Fatal("want an error")
	}
	if !f.exists(zswapTmpfilesPath) || f.read(zswapTmpfilesPath) != tmpfiles {
		t.Error("tmpfiles file not restored")
	}
	if got := f.param(zswapEnabledParam); got != "Y" {
		t.Errorf("zswap enabled = %q, want Y back", got)
	}
}

// zram that swapped when the run started it but is gone before the swap file
// would stop leaves the swap file on: stopping it would leave no swap. The
// host swaps to disk only, so the live sysctls end as disk's and not as the
// zram ones the run applied. The drop-in stays zram's, like the zram service
// the run enabled for the next boot.
func TestSetSwapModeLeavesTheSwapFileWhenZramVanished(t *testing.T) {
	f, sys := diskHost(t)
	sys.after = func(call string) {
		if call == "set "+scaleParam+"=125" {
			f.write(procSwaps, swapsHeader+swapFileSwapsLine)
		}
	}

	got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeZram)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(sys.calls, "stop swap.img.swap") || slices.Contains(sys.calls, "mask swap.img.swap") {
		t.Fatalf("calls = %v, want the swap file left on", sys.calls)
	}
	if got.Mode != apischema.SwapModeDisk || got.Swappiness != 60 {
		t.Errorf("status = mode %q swappiness %d, want disk at 60", got.Mode, got.Swappiness)
	}
	assertLeft(t, got.Left, apischema.SwapLeft{What: apischema.SwapLeftSwapFile, Reason: apischema.SwapLeftZramNotSwapping})
	assertDiskSysctls(t, f)
	if got := f.read(sysctlDropInPath); got != zramDropIn {
		t.Errorf("drop-in = %q, want zram's %q", got, zramDropIn)
	}
}

// The reverse: a switch to zram that fails after the swap file stopped
// leaves the host on zram only, and the sysctls end as zram's and not as the
// disk ones the rollback restored.
func TestSetSwapModeToZramThatFailsEndsWithZramSysctls(t *testing.T) {
	f, sys := diskHost(t)
	sys.failures["mask swap.img.swap"] = errors.New("unit file is read-only")

	if _, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeZram); err == nil {
		t.Fatal("want an error")
	}
	if swaps := f.read(procSwaps); strings.Contains(swaps, "/swap.img") || !strings.Contains(swaps, "/dev/zram0") {
		t.Fatalf("swaps = %q, want zram on and the swap file stopped", swaps)
	}
	for param, want := range map[string]string{swappinessParam: "180", pageClusterParam: "0", boostParam: "0", scaleParam: "125"} {
		if got := f.param(param); got != want {
			t.Errorf("%s = %q, want %q", param, got, want)
		}
	}
}

// A run that fails with the host still in the mode it started in keeps the
// sysctls the rollback put back.
func TestSetSwapModeThatFailsInTheSameModeWritesNoSysctlsAgain(t *testing.T) {
	f, sys := diskHost(t)
	sys.failures["enable "+zramService] = errors.New("no such unit")

	if _, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeZram); err == nil {
		t.Fatal("want an error")
	}
	for _, call := range sys.calls {
		if strings.HasPrefix(call, "set ") {
			t.Fatalf("calls = %v, want no kernel value written", sys.calls)
		}
	}
	if f.exists(sysctlDropInPath) {
		t.Error("drop-in written, want none as before the run")
	}
}

// A write that lands and still reports an error (a failed directory fsync
// after the rename, a failed close after a kernel write) is undone too.
func TestSetSwapModeUndoesAWriteThatFailedAfterLanding(t *testing.T) {
	tests := map[string]string{
		"file":   "write " + sysctlDropInPath,
		"kernel": "set " + swappinessParam + "=180",
	}
	for name, call := range tests {
		t.Run(name, func(t *testing.T) {
			f, sys := diskHost(t)
			sys.failAfter[call] = errors.New("input/output error")

			if _, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeZram); err == nil {
				t.Fatal("want an error")
			}
			assertRolledBackToDisk(t, f)
		})
	}
}

// zswap already off is not written again: on a kernel where zswap failed to
// start, that write fails and would sink the whole change.
func TestSetSwapModeSkipsTurningOffZswapThatIsOff(t *testing.T) {
	_, sys := diskHost(t)
	sys.failures["set "+zswapEnabledParam+"=N"] = errors.New("no such device")

	if _, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeZram); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(sys.calls, "set "+zswapEnabledParam+"=N") {
		t.Fatalf("calls = %v, want zswap left as it is", sys.calls)
	}
}

// zram set up by another tool, or under a zramswap.service that is not
// running, would not stop with the service: zram stays, the service is not
// disabled and the result is mixed.
func TestSetSwapModeLeavesZramItsServiceDoesNotRun(t *testing.T) {
	for _, state := range []string{"inactive", "failed"} {
		t.Run(state, func(t *testing.T) {
			f, sys := zramHost(t)
			sys.activeStates[zramService] = state

			got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeDisk)
			if err != nil {
				t.Fatal(err)
			}
			if slices.Contains(sys.calls, "stop "+zramService) || slices.Contains(sys.calls, "disable "+zramService) {
				t.Fatalf("calls = %v, want zramswap.service neither stopped nor disabled", sys.calls)
			}
			if got.Mode != apischema.SwapModeMixed || !strings.Contains(f.read(procSwaps), "/dev/zram0") {
				t.Fatalf("mode = %q, want mixed with zram still swapping", got.Mode)
			}
			assertLeft(t, got.Left, apischema.SwapLeft{What: apischema.SwapLeftZram, Reason: apischema.SwapLeftNotManaged})
		})
	}
}

// A swap file whose fstab unit systemd does not report active, as when it
// tracks the file under another name, is left on and not masked.
func TestSetSwapModeLeavesASwapFileItsUnitDoesNotRun(t *testing.T) {
	for _, state := range []string{"inactive", "failed"} {
		t.Run(state, func(t *testing.T) {
			f, sys := diskHost(t)
			sys.activeStates["swap.img.swap"] = state

			got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeZram)
			if err != nil {
				t.Fatal(err)
			}
			if slices.Contains(sys.calls, "stop swap.img.swap") || slices.Contains(sys.calls, "mask swap.img.swap") {
				t.Fatalf("calls = %v, want swap.img.swap neither stopped nor masked", sys.calls)
			}
			if got.Mode != apischema.SwapModeMixed || !strings.Contains(f.read(procSwaps), "/swap.img") {
				t.Fatalf("mode = %q, want mixed with the swap file on", got.Mode)
			}
			assertLeft(t, got.Left, apischema.SwapLeft{What: apischema.SwapLeftSwapFile, Reason: apischema.SwapLeftUnitNotRunning})
		})
	}
}

// A unit state that cannot be read does not hold the stop back: the wait
// that follows decides.
func TestSetSwapModeStopsWhenTheUnitStateCannotBeRead(t *testing.T) {
	_, sys := diskHost(t)
	sys.failures["active swap.img.swap"] = errors.New("connection refused")

	got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeZram)
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != apischema.SwapModeZram || !slices.Contains(sys.calls, "mask swap.img.swap") {
		t.Fatalf("mode = %q, calls = %v; want the swap file stopped and masked", got.Mode, sys.calls)
	}
}

// zramswap.service stopping cleanly while a second zram device, set up by
// another tool, still swaps: the unit is inactive with zram still listed, so
// the stop ended without taking it away. The step fails at once, not at the
// route's bound, and the service stays enabled.
func TestSetSwapModeFailsFastWhenTheStopEndsWithZramStillSwapping(t *testing.T) {
	f, sys := zramHost(t)
	other := "/dev/zram1                              partition\t1048572\t\t0\t\t50\n"
	f.write(procSwaps, f.read(procSwaps)+other)
	e := sys.executor(nil)
	e.waits.swapOff = time.Minute

	start := time.Now()
	_, err := e.setSwapMode(t.Context(), apischema.SwapModeDisk)
	if !errors.Is(err, errStopEnded) || !strings.Contains(err.Error(), string(apischema.SwapStepStopZram)) || !strings.Contains(err.Error(), "inactive") {
		t.Fatalf("err = %v, want the stop_zram step to say the stop ended with zram still active", err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("failed after %s, want as soon as the unit was inactive", elapsed)
	}
	if slices.Contains(sys.calls, "disable "+zramService) || !strings.Contains(f.read(procSwaps), "/dev/zram1") {
		t.Fatalf("calls = %v, want zramswap.service left enabled and zram1 swapping", sys.calls)
	}
}

// A stop systemd never starts to carry out, the unit staying active without
// ever deactivating (as when the kernel refuses the swapoff at once), fails
// after the short grace, not at the route's bound; nothing is masked.
func TestSetSwapModeFailsWhenSystemdNeverCarriesOutTheStop(t *testing.T) {
	f, sys := diskHost(t)
	sys.stops["swap.img.swap"] = &fakeStop{queued: 1 << 30}
	e := sys.executor(nil)
	e.waits.swapOff = time.Minute
	e.waits.stopGrace = 20 * time.Millisecond

	start := time.Now()
	_, err := e.setSwapMode(t.Context(), apischema.SwapModeZram)
	if !errors.Is(err, errStopNotCarriedOut) || !strings.Contains(err.Error(), string(apischema.SwapStepStopSwapFile)) || !strings.Contains(err.Error(), "still active") {
		t.Fatalf("err = %v, want the stop_swap_file step to say systemd did not carry out the stop", err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("failed after %s, want after the grace", elapsed)
	}
	if slices.Contains(sys.calls, "mask swap.img.swap") || !strings.Contains(f.read(procSwaps), "/swap.img") {
		t.Fatalf("calls = %v, want nothing masked and the file on", sys.calls)
	}
	assertRolledBackToDisk(t, f)
}

// A unit this run enabled is disabled again by rollback even when zram
// swaps, so the next boot does not start zram with the old configuration;
// zram that swaps is not stopped.
func TestSetSwapModeRollbackDisablesTheServiceItEnabledWhileZramSwaps(t *testing.T) {
	f, sys := diskHost(t)
	f.write(procSwaps, swapsHeader+zramSwapsLine+swapFileSwapsLine)
	sys.failures["mask swap.img.swap"] = errors.New("unit file is read-only")

	if _, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeZram); err == nil {
		t.Fatal("want an error")
	}
	if sys.fileStates[zramService] != "disabled" || !slices.Contains(sys.calls, "disable "+zramService) {
		t.Fatalf("calls = %v, file state = %q; want zramswap.service disabled again", sys.calls, sys.fileStates[zramService])
	}
	if slices.Contains(sys.calls, "stop "+zramService) || !strings.Contains(f.read(procSwaps), "/dev/zram0") {
		t.Fatalf("calls = %v, want zram left swapping", sys.calls)
	}
}

// A vm sysctl the kernel does not have (watermark_boost_factor came in 5.0)
// is not written live, as a preset or as a kernel default; the drop-in keeps
// its key.
func TestSetSwapModeSkipsAVMSysctlTheKernelLacks(t *testing.T) {
	t.Run("zram preset", func(t *testing.T) {
		f, sys := diskHost(t)
		removeFixture(t, f, boostParam)

		if _, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeZram); err != nil {
			t.Fatal(err)
		}
		if slices.ContainsFunc(sys.calls, func(c string) bool { return strings.HasPrefix(c, "set "+boostParam) }) {
			t.Fatalf("calls = %v, want watermark_boost_factor not written", sys.calls)
		}
		if !strings.Contains(f.read(sysctlDropInPath), "vm.watermark_boost_factor = 0\n") {
			t.Errorf("drop-in = %q, want the key kept", f.read(sysctlDropInPath))
		}
	})
	t.Run("kernel default", func(t *testing.T) {
		f, sys := zramHost(t)
		removeFixture(t, f, boostParam)

		if _, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeDisk); err != nil {
			t.Fatal(err)
		}
		if slices.ContainsFunc(sys.calls, func(c string) bool { return strings.HasPrefix(c, "set "+boostParam) }) {
			t.Fatalf("calls = %v, want watermark_boost_factor not written", sys.calls)
		}
	})
}

func removeFixture(t *testing.T, f fixture, name string) {
	t.Helper()
	if err := os.Remove(filepath.Join(f.root, name)); err != nil {
		t.Fatal(err)
	}
}
