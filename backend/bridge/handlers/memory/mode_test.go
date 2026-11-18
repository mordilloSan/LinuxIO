package memory

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
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

func assertCalls(t *testing.T, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("calls =\n  %s\nwant\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

func TestSetSwapModeToZramFromDisk(t *testing.T) {
	f, sys := diskHost(t)
	var progress []apischema.SwapProgress

	got, err := sys.executor(&progress).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram})
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
		"swapoff /swap.img",
		holdOffCall("swap.img.swap"),
		"reload",
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
	// The swap file's unit is held off at boot by a condition that never
	// holds, and not masked: swap.target requires the unit, and systemd
	// starts no target that requires a masked unit.
	wantHoldOff := "# Managed by LinuxIO (Dashboard, Memory); changes here are overwritten.\n" +
		"# Keeps this swap off at boot. LinuxIO removes this file to turn it on again.\n" +
		"[Unit]\nConditionPathExists=!/\n"
	if got := f.read("/etc/systemd/system/swap.img.swap.d/90-linuxio-memory.conf"); got != wantHoldOff {
		t.Errorf("hold-off drop-in = %q, want %q", got, wantHoldOff)
	}
	if f.exists("/etc/systemd/system/swap.img.swap") || sys.fileStates["swap.img.swap"] == "masked" {
		t.Errorf("swap.img.swap is masked, want it held off by the drop-in alone")
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

// The status after the last step is read again when a read fails, as one
// that races a zram device leaving: the change was made and the request
// succeeds.
func TestSetSwapModeReadsTheStatusAgainAfterTheChange(t *testing.T) {
	_, sys := diskHost(t)
	sys.after = func(call string) {
		if call == holdOffCall("swap.img.swap") {
			sys.statusFails = 2
		}
	}

	got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram})
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != apischema.SwapModeZram {
		t.Errorf("status mode = %q, want zram", got.Mode)
	}
}

// A status that still cannot be read after the last step fails the request
// with an error that says the change was made, and nothing is rolled back.
// The change is logged all the same: it is a completed host mutation.
func TestSetSwapModeSaysTheChangeWasMadeWhenTheStatusCannotBeRead(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	f, sys := diskHost(t)
	sys.after = func(call string) {
		if call == holdOffCall("swap.img.swap") {
			sys.statusFails = 1000
		}
	}

	_, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram})
	if !errors.Is(err, errStatusRace) || !strings.Contains(err.Error(), "was made") {
		t.Fatalf("err = %v, want the read error and that the change was made", err)
	}
	if slices.Contains(sys.calls, "disable "+zramService) || f.param(swappinessParam) != "180" {
		t.Fatalf("calls = %v, want nothing rolled back", sys.calls)
	}
	if !strings.Contains(logs.String(), `level=INFO msg="swap mode changed" mode=zram previous_mode=disk`) {
		t.Errorf("logs = %q, want the change logged at Info", logs.String())
	}
}

func TestSetSwapModeToZswapFromZram(t *testing.T) {
	f, sys := zramHost(t)

	got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZswap})
	if err != nil {
		t.Fatal(err)
	}

	assertCalls(t, sys.calls, []string{
		"remove " + holdOffDropInPath("swap.img.swap"),
		"reload",
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
	if f.exists(holdOffDropInPath("swap.img.swap")) {
		t.Errorf("the hold-off drop-in is still there after the swap file started")
	}
}

// A host an older LinuxIO switched to zram has the swap file's unit masked
// and no drop-in. The way back reloads systemd, unmasks the unit, which
// reloads it again, and starts it; with both a drop-in and a mask, both go.
func TestSetSwapModeToDiskUnmasksAUnitAMaskHeldOff(t *testing.T) {
	t.Run("masked", func(t *testing.T) {
		f, sys := maskedZramHost(t)
		got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeDisk})
		if err != nil {
			t.Fatal(err)
		}
		if got.Mode != apischema.SwapModeDisk || f.exists("/etc/systemd/system/swap.img.swap") {
			t.Fatalf("mode %s, mask still there %v; want disk and the unit unmasked", got.Mode, f.exists("/etc/systemd/system/swap.img.swap"))
		}
		assertCalls(t, sys.calls[:3], []string{"reload", "unmask swap.img.swap", "start swap.img.swap"})
	})
	t.Run("drop-in and mask", func(t *testing.T) {
		f, sys := maskedZramHost(t)
		f.write(holdOffDropInPath("swap.img.swap"), string(renderHoldOffDropIn()))
		got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeDisk})
		if err != nil {
			t.Fatal(err)
		}
		if got.Mode != apischema.SwapModeDisk || f.exists(holdOffDropInPath("swap.img.swap")) {
			t.Fatalf("mode %s; want disk and the drop-in gone", got.Mode)
		}
		assertCalls(t, sys.calls[:4], []string{"remove " + holdOffDropInPath("swap.img.swap"), "reload", "unmask swap.img.swap", "start swap.img.swap"})
	})
}

// The way back reloads systemd before it starts the swap file, whatever was
// held off: a swap file just added to /etc/fstab has no unit until a reload,
// and systemd keeps a hold-off condition whose file is gone until one.
func TestSetSwapModeToDiskReloadsBeforeItStartsTheFile(t *testing.T) {
	t.Run("unit not loaded yet", func(t *testing.T) {
		f, sys := zramHost(t)
		removeFixture(t, f, holdOffDropInPath("swap.img.swap"))
		delete(sys.heldOff, "swap.img.swap")
		sys.failures["state swap.img.swap"] = errors.New("unit swap.img.swap not found")
		sys.after = func(call string) {
			if call == "reload" {
				delete(sys.failures, "state swap.img.swap")
			}
		}
		got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeDisk})
		if err != nil || got.Mode != apischema.SwapModeDisk {
			t.Fatalf("mode %s, %v; want disk", got.Mode, err)
		}
		assertCalls(t, sys.calls[:2], []string{"reload", "start swap.img.swap"})
	})
	t.Run("condition still loaded", func(t *testing.T) {
		f, sys := zramHost(t)
		removeFixture(t, f, holdOffDropInPath("swap.img.swap"))
		got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeDisk})
		if err != nil || got.Mode != apischema.SwapModeDisk {
			t.Fatalf("mode %s, %v; want disk", got.Mode, err)
		}
		assertCalls(t, sys.calls[:2], []string{"reload", "start swap.img.swap"})
	})
}

// A host an older LinuxIO switched to zram with a mask boots without
// swap.target on zram-generator, so with no swap: the status reads none. A
// switch to zram from there holds the file off with the drop-in and unmasks
// the unit, which reloads systemd, so the next boot starts swap.target.
func TestSetSwapModeToZramClearsAMask(t *testing.T) {
	f, sys := diskHost(t)
	f.write(procSwaps, swapsHeader)
	sys.fileStates["swap.img.swap"] = "masked"
	f.maskInFixture("swap.img.swap")

	got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram})
	if err != nil || got.Mode != apischema.SwapModeZram {
		t.Fatalf("mode %s, %v; want zram", got.Mode, err)
	}
	if !f.exists(holdOffDropInPath("swap.img.swap")) || f.exists("/etc/systemd/system/swap.img.swap") || sys.fileStates["swap.img.swap"] == "masked" {
		t.Fatalf("calls = %v; want the drop-in written and the unit unmasked", sys.calls)
	}
	held := slices.Index(sys.calls, holdOffCall("swap.img.swap"))
	if held < 0 || !slices.Equal(sys.calls[held:], []string{holdOffCall("swap.img.swap"), "reload", "unmask swap.img.swap"}) {
		t.Fatalf("calls = %v; want the drop-in written, a reload, then the unmask", sys.calls)
	}
}

// The job host runs with umask 077, which would make the drop-in's folder
// 0700 and hide the drop-in from a session without sudo, whose status would
// then not see the file held off. The folder is made 0755, also when an
// earlier run left it 0700 holding nothing but LinuxIO's file; an admin's
// folder with drop-ins of its own keeps its mode.
func TestSetSwapModeMakesTheDropInFolderReadable(t *testing.T) {
	previous := syscall.Umask(0o077)
	t.Cleanup(func() { syscall.Umask(previous) })
	dir := filepath.Dir(holdOffDropInPath("swap.img.swap"))
	tests := []struct {
		name  string
		setup func(f fixture)
		want  fs.FileMode
	}{
		{name: "new", want: 0o755},
		{name: "left 0700 by an earlier run", setup: func(f fixture) {
			f.write(holdOffDropInPath("swap.img.swap"), string(renderHoldOffDropIn()))
			if err := os.Chmod(filepath.Join(f.root, dir), 0o700); err != nil {
				t.Fatal(err)
			}
		}, want: 0o755},
		{name: "an admin's", setup: func(f fixture) {
			f.write(dir+"/50-admin.conf", "[Swap]\nPriority=5\n")
			if err := os.Chmod(filepath.Join(f.root, dir), 0o700); err != nil {
				t.Fatal(err)
			}
		}, want: 0o700},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f, sys := diskHost(t)
			if test.setup != nil {
				test.setup(f)
			}
			if _, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram}); err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(filepath.Join(f.root, dir))
			if err != nil || info.Mode().Perm() != test.want {
				t.Fatalf("%s mode = %v, %v; want %v", dir, info.Mode().Perm(), err, test.want)
			}
			if info, err := os.Stat(filepath.Join(f.root, holdOffDropInPath("swap.img.swap"))); err != nil || info.Mode().Perm() != 0o644 {
				t.Fatalf("drop-in mode = %v, %v; want 0644", info.Mode().Perm(), err)
			}
		})
	}
}

// A swap file that swaps while its unit is held off, as a failed switch away
// from zram leaves it, is let go by a switch to disk, so the next boot does
// not hold it off; it swaps already, so it is not started or waited for.
func TestSetSwapModeToDiskLetsGoASwappingFileHeldOff(t *testing.T) {
	f, sys := diskHost(t)
	f.write(holdOffDropInPath("swap.img.swap"), string(renderHoldOffDropIn()))

	got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeDisk})
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != apischema.SwapModeDisk || f.exists(holdOffDropInPath("swap.img.swap")) {
		t.Fatalf("mode %s, drop-in still there %v; want disk and the drop-in gone", got.Mode, f.exists(holdOffDropInPath("swap.img.swap")))
	}
	if len(sys.calls) < 2 || !slices.Equal(sys.calls[:2], []string{"remove " + holdOffDropInPath("swap.img.swap"), "reload"}) ||
		slices.Contains(sys.calls, "start swap.img.swap") {
		t.Fatalf("calls = %v, want the drop-in removed and a reload, and no start", sys.calls)
	}
}

// A switch away from zram that fails after the swap file started leaves the
// file let go: starting it added swap, which a rollback never undoes, so the
// drop-in it removed does not come back.
func TestSetSwapModeRollbackLeavesTheSwapFileLetGo(t *testing.T) {
	f, sys := zramHost(t)
	sys.failures["disable "+zramService] = errors.New("unit file is read-only")

	if _, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeDisk}); err == nil {
		t.Fatal("want an error")
	}
	if f.exists(holdOffDropInPath("swap.img.swap")) || slices.Contains(sys.calls, holdOffCall("swap.img.swap")) {
		t.Fatalf("calls = %v; want the hold-off drop-in left removed", sys.calls)
	}
	if !strings.Contains(f.read(procSwaps), "/swap.img") {
		t.Fatalf("swaps = %q, want the swap file left on", f.read(procSwaps))
	}
}

// A switch to zram that fails after it held the swap file off takes its
// drop-in away again, with a reload, so the next boot is no worse than
// before the run: the file comes back.
func TestSetSwapModeRollbackRemovesTheDropInItWrote(t *testing.T) {
	f, sys := diskHost(t)
	failed := false
	sys.after = func(call string) {
		if call == holdOffCall("swap.img.swap") && !failed {
			failed = true
			sys.failures["reload"] = errors.New("manager busy")
		} else if call == "remove "+holdOffDropInPath("swap.img.swap") {
			delete(sys.failures, "reload")
		}
	}

	if _, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram}); err == nil {
		t.Fatal("want an error")
	}
	if f.exists(holdOffDropInPath("swap.img.swap")) {
		t.Fatalf("calls = %v; want the drop-in the run wrote removed", sys.calls)
	}
	removed := slices.Index(sys.calls, "remove "+holdOffDropInPath("swap.img.swap"))
	if removed < 0 || !slices.Contains(sys.calls[removed:], "reload") {
		t.Fatalf("calls = %v; want the drop-in removed and a reload after it", sys.calls)
	}
}

func TestSetSwapModeToDiskFromZswap(t *testing.T) {
	f, sys := diskHost(t)
	f.write(zswapEnabledParam, "Y\n")
	f.write(zswapCompressorParam, "zstd\n")
	f.write(zswapTmpfilesPath, string(renderZswapTmpfiles("zstd")))

	got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeDisk})
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

// On a host that runs zram, zram's values are zram's even where the drop-in
// does not hold them, as after a run whose rollback put the drop-in back but
// left zram's values live: leaving zram writes the kernel defaults.
func TestSetSwapModeFromZramResetsZramSysctlsTheDropInDoesNotHold(t *testing.T) {
	f, sys := zramHost(t)
	removeFixture(t, f, sysctlDropInPath)

	if _, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeDisk}); err != nil {
		t.Fatal(err)
	}
	assertDiskSysctls(t, f)
}

// Live values that equal zram's preset on a host whose drop-in never held
// them and that does not run zram are an admin's: leaving them is not
// leaving zram, so no kernel default is written over them.
func TestSetSwapModeLeavesAnAdminsSysctlsThatMatchZram(t *testing.T) {
	f, sys := diskHost(t)
	f.writeVMSysctls("60", "0", "0", "125")

	if _, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZswap}); err != nil {
		t.Fatal(err)
	}
	for param, want := range map[string]string{pageClusterParam: "0", boostParam: "0", scaleParam: "125"} {
		if got := f.param(param); got != want {
			t.Errorf("%s = %q, want the admin's %q kept", param, got, want)
		}
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

			if _, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram}); err != nil {
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
	if _, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeDisk}); err != nil {
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
	if _, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeDisk}); err != nil {
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

			got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZswap})
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

	if _, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZswap}); err == nil || !strings.Contains(err.Error(), string(apischema.SwapStepEnableZswap)) {
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
	_, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram})
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

	if _, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram}); err == nil {
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

	got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram})
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != apischema.SwapModeMixed {
		t.Errorf("mode = %q, want mixed", got.Mode)
	}
	assertLeft(t, got.Left, apischema.SwapLeft{What: apischema.SwapLeftSwapFile, Reason: apischema.SwapLeftNotEnoughMemory})
	if slices.Contains(sys.calls, "swapoff /swap.img") || slices.Contains(sys.calls, holdOffCall("swap.img.swap")) {
		t.Fatalf("calls = %v, want the swap file left alone", sys.calls)
	}
}

// A swap file in a folder someone other than root can write is never given
// to swapoff, which could be sent to another device there: the run succeeds,
// the file stays active and the host reports mixed.
func TestSetSwapModeToZramLeavesASwapFileOthersCanRedirect(t *testing.T) {
	_, sys := diskHost(t)
	sys.refused = map[string]error{"/swap.img": fmt.Errorf("%w: /swap.img: %w", bridgeipc.ErrConflict, errOthersCanRedirect)}

	got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram})
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != apischema.SwapModeMixed {
		t.Errorf("mode = %q, want mixed", got.Mode)
	}
	assertLeft(t, got.Left, apischema.SwapLeft{What: apischema.SwapLeftSwapFile, Reason: apischema.SwapLeftOthersCanWrite})
	if slices.Contains(sys.calls, holdOffCall("swap.img.swap")) {
		t.Fatalf("calls = %v, want the swap file's unit left alone", sys.calls)
	}
}

// A swap file the status reports as not managed, since others can change
// its folder, gets no step: zram is set up beside it and the result says
// why the file was left.
func TestSetSwapModeToZramLeavesASwapFileItDoesNotManage(t *testing.T) {
	f, sys := diskHost(t)
	f.write(etcFstab, "/home/open/swapfile none swap sw 0 0\n")
	f.othersCanWrite("/home/open")
	f.write("/home/open/swapfile", "")
	f.write(procSwaps, swapsHeader+openSwapsLine)

	got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram})
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != apischema.SwapModeMixed {
		t.Errorf("mode = %q, want mixed", got.Mode)
	}
	assertLeft(t, got.Left, apischema.SwapLeft{What: apischema.SwapLeftSwapFile, Reason: apischema.SwapLeftOthersCanWrite})
	if slices.ContainsFunc(sys.calls, func(call string) bool { return strings.HasPrefix(call, "swapoff") || strings.HasPrefix(call, "mask") }) {
		t.Fatalf("calls = %v, want the swap file left alone", sys.calls)
	}
}

// With two swap files in fstab, the one that swaps is the one a switch to
// zram stops and masks, though one that is off comes first; that one is
// left as it is.
func TestSetSwapModeToZramStopsTheActiveOfTwoSwapFiles(t *testing.T) {
	f, sys := diskHost(t)
	f.write(etcFstab, "/old.img none swap sw 0 0\n/swap.img none swap sw 0 0\n")

	got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram})
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != apischema.SwapModeZram || !slices.Contains(sys.calls, "swapoff /swap.img") ||
		!slices.Contains(sys.calls, holdOffCall("swap.img.swap")) || slices.Contains(sys.calls, holdOffCall("old.img.swap")) {
		t.Fatalf("mode = %q, calls = %v; want zram with /swap.img stopped and masked", got.Mode, sys.calls)
	}
	assertLeft(t, got.Left)
}

// The managed swap file keeps its identity both ways: with one that is off
// before it in fstab, the file a switch to zram masked is the one the way
// back starts.
func TestSetSwapModeBackFromZramStartsTheSwapFileItMasked(t *testing.T) {
	f, sys := diskHost(t)
	f.write(etcFstab, "/old.img none swap sw 0 0\n/swap.img none swap sw 0 0\n")
	if _, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram}); err != nil {
		t.Fatal(err)
	}
	sys.calls = nil

	got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeDisk})
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != apischema.SwapModeDisk || !slices.Contains(sys.calls, "start swap.img.swap") ||
		slices.ContainsFunc(sys.calls, func(call string) bool { return strings.Contains(call, "old.img") }) {
		t.Fatalf("mode = %q, calls = %v; want disk with /swap.img started", got.Mode, sys.calls)
	}
	if f.host().unitHeldOff("swap.img.swap") {
		t.Error("swap.img.swap is still masked")
	}
}

// With two swap files on, the first is the managed one. A second switch to
// zram, after the first masked it, still leaves the other on and never stops
// or masks it.
func TestSetSwapModeToZramAgainLeavesTheSecondSwapFile(t *testing.T) {
	f, sys := diskHost(t)
	f.write(etcFstab, "/swap.img none swap sw 0 0\n/home/extra none swap sw 0 0\n")
	f.write("/home/extra", "")
	f.write(procSwaps, swapsHeader+swapFileSwapsLine+unmanagedSwapsLine)
	if _, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram}); err != nil {
		t.Fatal(err)
	}
	sys.calls = nil

	got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram})
	if err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(sys.calls, func(call string) bool { return call == "swapoff /home/extra" || call == holdOffCall("home-extra.swap") }) {
		t.Fatalf("calls = %v, want /home/extra left alone", sys.calls)
	}
	assertLeft(t, got.Left, apischema.SwapLeft{What: apischema.SwapLeftSwapFile, Reason: apischema.SwapLeftNotManaged})
}

// A host that is mixed only on the read after the settle still says why:
// here zswap goes off behind the run, so the host ends in disk mode and is
// settled, and zram appears meanwhile.
func TestSetSwapModeExplainsAMixedHostFoundAfterTheSettle(t *testing.T) {
	f, sys := diskHost(t)
	settling := false
	sys.after = func(call string) {
		switch {
		case call == "write "+zswapTmpfilesPath:
			f.write(zswapEnabledParam, "N\n")
			settling = true
		case settling && call == "set "+swappinessParam+"=60":
			f.write(procSwaps, f.read(procSwaps)+zramSwapsLine)
		}
	}

	got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZswap})
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != apischema.SwapModeMixed {
		t.Fatalf("mode = %q, want mixed", got.Mode)
	}
	assertLeft(t, got.Left, apischema.SwapLeft{What: apischema.SwapLeftZram, Reason: apischema.SwapLeftNotManaged})
}

// One swap file is managed: a second one that swaps is left on, and the
// mixed result says it is not managed.
func TestSetSwapModeToZramLeavesASecondActiveSwapFile(t *testing.T) {
	f, sys := diskHost(t)
	f.write(etcFstab, "/swap.img none swap sw 0 0\n/home/extra none swap sw 0 0\n")
	f.write("/home/extra", "")
	f.write(procSwaps, swapsHeader+swapFileSwapsLine+unmanagedSwapsLine)

	got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram})
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != apischema.SwapModeMixed || !slices.Contains(sys.calls, "swapoff /swap.img") || slices.Contains(sys.calls, "swapoff /home/extra") {
		t.Fatalf("mode = %q, calls = %v; want mixed with /home/extra left on", got.Mode, sys.calls)
	}
	assertLeft(t, got.Left, apischema.SwapLeft{What: apischema.SwapLeftSwapFile, Reason: apischema.SwapLeftNotManaged})
}

// The reason names the file that stays: when the fstab file others can
// redirect is off and another swap file swaps on, that one is not managed.
func TestSetSwapModeToZramExplainsTheSwapFileThatStays(t *testing.T) {
	f, sys := diskHost(t)
	f.write(etcFstab, "/home/open/swapfile none swap sw 0 0\n")
	f.othersCanWrite("/home/open")
	f.write("/home/open/swapfile", "")
	f.write("/home/extra", "")
	f.write(procSwaps, swapsHeader+unmanagedSwapsLine)

	got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram})
	if err != nil {
		t.Fatal(err)
	}
	assertLeft(t, got.Left, apischema.SwapLeft{What: apischema.SwapLeftSwapFile, Reason: apischema.SwapLeftNotManaged})
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

	got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram})
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != apischema.SwapModeMixed || slices.Contains(sys.calls, "swapoff /swap.img") {
		t.Fatalf("mode = %q, calls = %v; want the swap file left active", got.Mode, sys.calls)
	}
}

// A fstab swap file that is not active is masked so it stays off at boot;
// another active swap file fstab does not name is never stopped or masked.
func TestSetSwapModeToZramNeverTouchesAnUnmatchedSwapFile(t *testing.T) {
	f, sys := diskHost(t)
	f.write(procSwaps, swapsHeader+"/home/extra                             file\t\t1048572\t\t0\t\t-2\n")
	f.write("/home/extra", "")

	got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram})
	if err != nil {
		t.Fatal(err)
	}
	for _, call := range sys.calls {
		if strings.HasPrefix(call, "stop ") {
			t.Fatalf("calls = %v, want nothing stopped", sys.calls)
		}
	}
	if !slices.Contains(sys.calls, holdOffCall("swap.img.swap")) || got.Mode != apischema.SwapModeMixed {
		t.Fatalf("mode = %q, calls = %v; want swap.img.swap masked and the extra file left", got.Mode, sys.calls)
	}
	assertLeft(t, got.Left, apischema.SwapLeft{What: apischema.SwapLeftSwapFile, Reason: apischema.SwapLeftNotManaged})
}

// An active swap file that cannot be resolved, such as one deleted while it
// swaps, may be the fstab one: its unit is neither stopped nor masked.
func TestSetSwapModeToZramMasksOnlyAProvenInactiveSwapFile(t *testing.T) {
	f, sys := diskHost(t)
	f.write(procSwaps, swapsHeader+"/swap.img\\040(deleted)                  file\t\t2097148\t\t0\t\t-2\n")

	got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram})
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(sys.calls, holdOffCall("swap.img.swap")) || slices.Contains(sys.calls, "swapoff /swap.img") {
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

	_, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram})
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

	_, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeDisk})
	if err == nil || !strings.Contains(err.Error(), string(apischema.SwapStepApplySysctls)) {
		t.Fatalf("err = %v, want the apply_sysctls step named", err)
	}
	if slices.Contains(sys.calls, "swapoff /swap.img") || !strings.Contains(f.read(procSwaps), "/swap.img") {
		t.Fatalf("calls = %v, want the swap file kept on", sys.calls)
	}
}

// A rollback action that fails is joined to the step's error.
func TestSetSwapModeJoinsAFailedRollback(t *testing.T) {
	f, sys := diskHost(t)
	f.write(zswapEnabledParam, "Y\n")
	sys.failures["set "+zswapEnabledParam+"=N"] = errors.New("device busy")
	sys.failures["set "+swappinessParam+"=60"] = errors.New("swappiness refused")

	_, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram})
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

			got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeDisk})
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

	if _, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram}); err != nil {
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

	_, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram})
	if !errors.Is(err, bridgeipc.ErrConflict) || len(sys.calls) != 0 {
		t.Fatalf("err = %v, calls = %v; want a conflict and no change", err, sys.calls)
	}
	if _, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: "fast"}); !errors.Is(err, bridgeipc.ErrInvalidArgs) {
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
		_, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram})
		done <- err
	}()
	<-started

	second := newFakeSystem(f)
	if _, err := second.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeDisk}); !errors.Is(err, bridgeipc.ErrConflict) {
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

// A swapoff the kernel refuses fails the step: nothing is masked, zram and
// the swap file both stay, and the run's files and live values are put back.
func TestSetSwapModeFailsWhenSwapoffFails(t *testing.T) {
	f, sys := diskHost(t)
	sys.failures["swapoff /swap.img"] = syscall.EBUSY

	_, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram})
	if !errors.Is(err, syscall.EBUSY) || !strings.Contains(err.Error(), string(apischema.SwapStepStopSwapFile)) ||
		!strings.Contains(err.Error(), "swapoff /swap.img") {
		t.Fatalf("err = %v, want the stop_swap_file step to report the swapoff", err)
	}
	if slices.Contains(sys.calls, holdOffCall("swap.img.swap")) {
		t.Fatalf("calls = %v, want nothing masked", sys.calls)
	}
	swaps := f.read(procSwaps)
	if !strings.Contains(swaps, "/dev/zram0") || !strings.Contains(swaps, "/swap.img") {
		t.Fatalf("/proc/swaps = %q, want zram and the swap file both kept", swaps)
	}
	assertRolledBackToDisk(t, f)
}

// A request that ends in the moment the swapoff finishes still masks the
// unit: the file is off, and unmasked it would return at the next boot.
func TestSetSwapModeMasksTheSwapFileAfterACancelAtTheEndOfSwapoff(t *testing.T) {
	f, sys := diskHost(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	sys.after = func(call string) {
		if call == "swapoff /swap.img" {
			cancel()
		}
	}

	_, _ = sys.executor(nil).setSwapMode(ctx, apischema.SwapModeRequest{Mode: apischema.SwapModeZram})
	if strings.Contains(f.read(procSwaps), "/swap.img") || !slices.Contains(sys.calls, holdOffCall("swap.img.swap")) {
		t.Fatalf("calls = %v, want the file off and its unit masked", sys.calls)
	}
}

// A request cancelled while swapoff reads the file back ends the swapoff
// with the file still swapping; nothing is masked and the run is rolled
// back.
func TestSetSwapModeLeavesTheSwapFileOnWhenCancelledDuringSwapoff(t *testing.T) {
	f, sys := diskHost(t)
	sys.blocked["/swap.img"] = true
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	_, err := sys.executor(nil).setSwapMode(ctx, apischema.SwapModeRequest{Mode: apischema.SwapModeZram})
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), string(apischema.SwapStepStopSwapFile)) {
		t.Fatalf("err = %v, want the stop_swap_file step cancelled", err)
	}
	if slices.Contains(sys.calls, holdOffCall("swap.img.swap")) || !strings.Contains(f.read(procSwaps), "/swap.img") {
		t.Fatalf("calls = %v, want nothing masked and the file on", sys.calls)
	}
	assertRolledBackToDisk(t, f)
}

// A zramswap.service whose stop timed out or failed ends failed with zram
// still swapping: the step fails at once, the service stays enabled and the
// swap file the run started stays on. The stop is over, so nothing can take
// zram away later and no start is queued.
func TestSetSwapModeFailsFastWhenSystemdGivesUpStoppingZram(t *testing.T) {
	f, sys := zramHost(t)
	sys.stops[zramService] = &fakeStop{polls: 3, ends: "failed"}
	e := sys.executor(nil)
	e.waits.zramOff = time.Minute

	start := time.Now()
	_, err := e.setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeDisk})
	if !errors.Is(err, errStopAbandoned) || !strings.Contains(err.Error(), string(apischema.SwapStepStopZram)) {
		t.Fatalf("err = %v, want the stop_zram step to report systemd giving up", err)
	}
	if slices.Contains(sys.calls, "start "+zramService) || strings.Contains(err.Error(), "a start was queued") {
		t.Fatalf("err = %v, calls = %v; want no start queued after a stop that is over", err, sys.calls)
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

	_, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZswap})
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
	// The swap file the run started, and let go of, is added swap, which a
	// rollback never undoes: holding it off again would leave the next boot
	// with no swap, since zram stays off at boot.
	if f.exists(holdOffDropInPath("swap.img.swap")) || sys.fileStates[zramService] != "disabled" {
		t.Errorf("drop-in there %v, %s %q; want the swap file let go and zram off at boot",
			f.exists(holdOffDropInPath("swap.img.swap")), zramService, sys.fileStates[zramService])
	}
}

// A stop whose command removed zram and then exited non-zero leaves the unit
// failed in the same moment the device left: the device is gone, so the step
// succeeds rather than reporting that systemd gave up.
func TestSetSwapModeTakesADeviceGoneAsStoppedWhateverTheUnitSays(t *testing.T) {
	_, sys := zramHost(t)
	sys.stops[zramService] = &fakeStop{polls: 2, ends: "failed", leaves: true}

	got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeDisk})
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != apischema.SwapModeDisk || !slices.Contains(sys.calls, "disable "+zramService) {
		t.Fatalf("mode = %q, calls = %v; want disk with zramswap.service disabled", got.Mode, sys.calls)
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

	_, err := sys.executor(nil).setSwapMode(ctx, apischema.SwapModeRequest{Mode: apischema.SwapModeZram})
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
	sys.failures[holdOffCall("swap.img.swap")] = errors.New("unit file is read-only")

	if _, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram}); err == nil {
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

	got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram})
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(sys.calls, "swapoff /swap.img") || slices.Contains(sys.calls, holdOffCall("swap.img.swap")) {
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
	sys.failures[holdOffCall("swap.img.swap")] = errors.New("unit file is read-only")

	if _, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram}); err == nil {
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

	if _, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram}); err == nil {
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

			if _, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram}); err == nil {
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

	if _, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram}); err != nil {
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

			got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeDisk})
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
// swapoff takes the swap file away whatever systemd says of its unit, as
// when it tracks the file under another unit name: the file is stopped and
// the unit fstab gives it is masked.
func TestSetSwapModeStopsASwapFileWhateverItsUnitReads(t *testing.T) {
	for _, state := range []string{"inactive", "failed"} {
		t.Run(state, func(t *testing.T) {
			f, sys := diskHost(t)
			sys.activeStates["swap.img.swap"] = state

			got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram})
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(sys.calls, "swapoff /swap.img") || !slices.Contains(sys.calls, holdOffCall("swap.img.swap")) {
				t.Fatalf("calls = %v, want the swap file stopped and masked", sys.calls)
			}
			if got.Mode != apischema.SwapModeZram || strings.Contains(f.read(procSwaps), "/swap.img") {
				t.Fatalf("mode = %q, want zram with the swap file off", got.Mode)
			}
			assertLeft(t, got.Left)
		})
	}
}

// zramswap.service stopping cleanly while a second zram device, set up by
// another tool, still swaps: the unit is inactive with zram still listed, so
// the stop ended without taking it away. The step fails at once, not at the
// route's bound, and the service stays enabled. The stop is over, so no
// start is queued.
func TestSetSwapModeFailsFastWhenTheStopEndsWithZramStillSwapping(t *testing.T) {
	f, sys := zramHost(t)
	other := "/dev/zram1                              partition\t1048572\t\t0\t\t50\n"
	f.write(procSwaps, f.read(procSwaps)+other)
	e := sys.executor(nil)
	e.waits.zramOff = time.Minute

	start := time.Now()
	_, err := e.setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeDisk})
	if !errors.Is(err, errStopEnded) || !strings.Contains(err.Error(), string(apischema.SwapStepStopZram)) || !strings.Contains(err.Error(), "inactive") {
		t.Fatalf("err = %v, want the stop_zram step to say the stop ended with zram still active", err)
	}
	if slices.Contains(sys.calls, "start "+zramService) || strings.Contains(err.Error(), "a start was queued") {
		t.Fatalf("err = %v, calls = %v; want no start queued after a stop that is over", err, sys.calls)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("failed after %s, want as soon as the unit was inactive", elapsed)
	}
	if slices.Contains(sys.calls, "disable "+zramService) || !strings.Contains(f.read(procSwaps), "/dev/zram1") {
		t.Fatalf("calls = %v, want zramswap.service left enabled and zram1 swapping", sys.calls)
	}
}

// A stop of zram that is not waited out, as when the request is cancelled,
// can still take zram away after the route gave up, with its service still
// enabled: a start is queued behind it, past the cancel, so zram comes back.
func TestSetSwapModeQueuesAStartWhenTheZramStopIsNotWaitedOut(t *testing.T) {
	f, sys := zramHost(t)
	sys.stops[zramService] = &fakeStop{queued: 1 << 30}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	sys.after = func(call string) {
		if call == "stop "+zramService {
			cancel()
		}
	}

	_, err := sys.executor(nil).setSwapMode(ctx, apischema.SwapModeRequest{Mode: apischema.SwapModeDisk})
	if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "a start was queued") {
		t.Fatalf("err = %v, want the cancel and that a start was queued", err)
	}
	sys.finishStop(zramService)
	if !strings.Contains(f.read(procSwaps), "/dev/zram0") || sys.fileStates[zramService] != "enabled" {
		t.Fatalf("calls = %v, want zram back with its service enabled", sys.calls)
	}
}

// The same with zram-generator: its swap unit is started again.
func TestSetSwapModeWithTheGeneratorQueuesAStartWhenTheZramStopFails(t *testing.T) {
	f, sys := generatorZramHost(t)
	sys.failAfter["stop "+zramGeneratorSwapUnit] = context.Canceled

	_, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeDisk})
	if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "a start was queued") {
		t.Fatalf("err = %v, want the cancel and that a start was queued", err)
	}
	if !slices.Contains(sys.calls, "start "+zramGeneratorSwapUnit) || !strings.Contains(f.read(procSwaps), "/dev/zram0") {
		t.Fatalf("calls = %v, want dev-zram0.swap started again", sys.calls)
	}
}

// A stop systemd never starts to carry out, the unit staying active without
// ever deactivating, fails after the short grace, not at the route's bound;
// zram stays and its service stays enabled.
func TestSetSwapModeFailsWhenSystemdNeverCarriesOutTheStop(t *testing.T) {
	f, sys := zramHost(t)
	sys.stops[zramService] = &fakeStop{queued: 1 << 30}
	e := sys.executor(nil)
	e.waits.zramOff = time.Minute
	e.waits.stopGrace = 20 * time.Millisecond

	start := time.Now()
	_, err := e.setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeDisk})
	if !errors.Is(err, errStopNotCarriedOut) || !strings.Contains(err.Error(), string(apischema.SwapStepStopZram)) || !strings.Contains(err.Error(), "still active") {
		t.Fatalf("err = %v, want the stop_zram step to say systemd did not carry out the stop", err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("failed after %s, want after the grace", elapsed)
	}
	if slices.Contains(sys.calls, "disable "+zramService) || !strings.Contains(f.read(procSwaps), "/dev/zram0") {
		t.Fatalf("calls = %v, want zram on and its service enabled", sys.calls)
	}
}

// A unit this run enabled is disabled again by rollback even when zram
// swaps, so the next boot does not start zram with the old configuration;
// zram that swaps is not stopped.
func TestSetSwapModeRollbackDisablesTheServiceItEnabledWhileZramSwaps(t *testing.T) {
	f, sys := diskHost(t)
	f.write(procSwaps, swapsHeader+zramSwapsLine+swapFileSwapsLine)
	sys.failures[holdOffCall("swap.img.swap")] = errors.New("unit file is read-only")

	if _, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram}); err == nil {
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

		if _, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram}); err != nil {
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

		if _, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeDisk}); err != nil {
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
