package memory

import (
	"bytes"
	"context"
	"errors"
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

	got, err := sys.executor(&progress).setSwapMode(t.Context(), zramRequest(t, sys))
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
		"fstab remove /swap.img",
		"reload",
		"delete /swap.img",
	})
	if got.Mode != apischema.SwapModeZram || got.Swappiness != 180 || len(got.SwapFiles) != 0 {
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
	// The swap file is gone with its fstab line.
	if f.exists("/swap.img") || strings.Contains(f.read(etcFstab), "/swap.img") {
		t.Errorf("fstab = %q, want /swap.img and its line removed", f.read(etcFstab))
	}
	wantProgress := []apischema.SwapProgress{
		{Step: apischema.SwapStepWriteConfig, Index: 1, Total: 5},
		{Step: apischema.SwapStepStartZram, Index: 2, Total: 5},
		{Step: apischema.SwapStepApplySysctls, Index: 3, Total: 5},
		{Step: apischema.SwapStepDisableZswap, Index: 4, Total: 5},
		{Step: apischema.SwapStepRemoveSwapFile, Index: 5, Total: 5, Device: "/swap.img"},
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
		if call == "delete /swap.img" {
			sys.statusFails = 2
		}
	}

	got, err := sys.executor(nil).setSwapMode(t.Context(), zramRequest(t, sys))
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
		if call == "delete /swap.img" {
			sys.statusFails = 1000
		}
	}

	_, err := sys.executor(nil).setSwapMode(t.Context(), zramRequest(t, sys))
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
		"make " + ownSwapFilePath,
		"fstab add " + ownSwapFilePath,
		"reload",
		"swapon " + ownSwapFilePath + " sw,nofail",
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
	// LinuxIO's swap file was made at the default size, half of RAM.
	row, ok := findRow(got.SwapStatus, ownSwapFilePath)
	if !ok || !row.Active || !row.InFstab || row.Size != 7963*mib-pageSize() {
		t.Errorf("swap files = %+v, want LinuxIO's swapping at 7963 MiB", got.SwapFiles)
	}
	if !strings.Contains(f.read(etcFstab), ownSwapFilePath+" none swap sw,nofail 0 0\n") {
		t.Errorf("fstab = %q, want LinuxIO's line", f.read(etcFstab))
	}
}

// A switch away from zram that fails after it made LinuxIO's swap file
// leaves the file on with its fstab line: adding swap is never undone.
func TestSetSwapModeRollbackKeepsTheSwapFileItMade(t *testing.T) {
	f, sys := zramHost(t)
	sys.failures["disable "+zramService] = errors.New("unit file is read-only")

	if _, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeDisk}); err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(f.read(procSwaps), ownSwapFilePath) || !strings.Contains(f.read(etcFstab), ownSwapFilePath) {
		t.Fatalf("swaps = %q, fstab = %q; want LinuxIO's swap file left on", f.read(procSwaps), f.read(etcFstab))
	}
	if slices.ContainsFunc(sys.calls, func(c string) bool { return strings.HasPrefix(c, "delete ") || strings.HasPrefix(c, "swapoff ") }) {
		t.Fatalf("calls = %v, want nothing taken away", sys.calls)
	}
}

// A switch to zram that fails after it removed a swap file keeps zram on at
// boot: the file is not made again, so undoing zram at boot would leave the
// next boot without swap. The host swaps on zram only by then, so the live
// sysctls end as zram's.
func TestSetSwapModeRollbackKeepsZramAtBootOnceAFileIsRemoved(t *testing.T) {
	f, sys := diskHost(t)
	f.write(etcFstab, "/swap.img none swap sw 0 0\n/swap2.img none swap sw 0 0\n")
	f.writeSwapFile("/swap2.img", gib)
	f.write(procSwaps, swapsHeader+swapFileSwapsLine+swapsLine("/swap2.img", gib, 0))
	sys.failures["delete /swap2.img"] = errors.New("read-only filesystem")

	_, err := sys.executor(nil).setSwapMode(t.Context(), zramRequest(t, sys))
	if err == nil || !strings.Contains(err.Error(), "/swap2.img") {
		t.Fatalf("err = %v, want the failed delete", err)
	}
	if f.exists("/swap.img") || slices.Contains(sys.calls, "disable "+zramService) {
		t.Fatalf("calls = %v; want /swap.img removed and zram left on at boot", sys.calls)
	}
	if got := f.read(zramswapConfigPath); got != string(renderZramswapConfig(16308408*kib/2)) {
		t.Errorf("zramswap config = %q, want LinuxIO's kept", got)
	}
	if f.param(swappinessParam) != "180" {
		t.Errorf("swappiness = %s, want zram's 180", f.param(swappinessParam))
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

			if _, err := sys.executor(nil).setSwapMode(t.Context(), zramRequest(t, sys)); err != nil {
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
	_, err := sys.executor(nil).setSwapMode(t.Context(), zramRequest(t, sys))
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

	if _, err := sys.executor(nil).setSwapMode(t.Context(), zramRequest(t, sys)); err == nil {
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

	got, err := sys.executor(nil).setSwapMode(t.Context(), zramRequest(t, sys))
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != apischema.SwapModeMixed {
		t.Errorf("mode = %q, want mixed", got.Mode)
	}
	assertLeft(t, got.Left, apischema.SwapLeft{What: apischema.SwapLeftSwapFile, Reason: apischema.SwapLeftNotEnoughMemory, Path: "/swap.img"})
	if slices.ContainsFunc(sys.calls, func(c string) bool { return strings.Contains(c, "/swap.img") }) || !f.exists("/swap.img") {
		t.Fatalf("calls = %v, want the swap file left alone", sys.calls)
	}
}

// A swap file that fails the strict check gets no step: zram is set up
// beside it, the host reads zram, and nothing says it was left, since no
// switch would remove it.
func TestSetSwapModeToZramLeavesALockedSwapFile(t *testing.T) {
	for name, setup := range map[string]func(f fixture){
		"not in fstab": func(f fixture) {
			f.write(etcFstab, "")
		},
		"in a folder others can change": func(f fixture) {
			f.write(etcFstab, "/home/open/swapfile none swap sw 0 0\n")
			f.othersCanWrite("/home/open")
		},
	} {
		t.Run(name, func(t *testing.T) {
			f, sys := diskHost(t)
			f.writeSwapFile("/home/open/swapfile", gib)
			f.write(procSwaps, swapsHeader+swapsLine("/home/open/swapfile", gib, 128*mib))
			setup(f)

			got, err := sys.executor(nil).setSwapMode(t.Context(), zramRequest(t, sys))
			if err != nil {
				t.Fatal(err)
			}
			if got.Mode != apischema.SwapModeZram {
				t.Errorf("mode = %q, want zram", got.Mode)
			}
			assertLeft(t, got.Left)
			if slices.ContainsFunc(sys.calls, func(call string) bool { return strings.Contains(call, "/home/open") }) {
				t.Fatalf("calls = %v, want the swap file left alone", sys.calls)
			}
		})
	}
}

// A switch to zram removes every swap file that passes the strict check:
// one that swaps is turned off first, one that is off only loses its fstab
// line and its file, and a missing one its line.
func TestSetSwapModeToZramRemovesEverySwapFile(t *testing.T) {
	f, sys := diskHost(t)
	f.write(etcFstab, "/old.img none swap sw 0 0\n/swap.img none swap sw 0 0\n/gone.img none swap sw 0 0\n")
	f.writeSwapFile("/old.img", gib)

	got, err := sys.executor(nil).setSwapMode(t.Context(), zramRequest(t, sys))
	if err != nil {
		t.Fatal(err)
	}
	removal := sys.calls[slices.Index(sys.calls, "write "+zswapTmpfilesPath)+1:]
	assertCalls(t, removal, []string{
		"swapoff /swap.img", "fstab remove /swap.img", "reload", "delete /swap.img",
		"fstab remove /old.img", "reload", "delete /old.img",
		"fstab remove /gone.img", "reload",
	})
	if got.Mode != apischema.SwapModeZram || len(got.SwapFiles) != 0 || f.read(etcFstab) != "" {
		t.Fatalf("mode = %q, swap files = %+v, fstab = %q; want zram and every file gone", got.Mode, got.SwapFiles, f.read(etcFstab))
	}
	assertLeft(t, got.Left)
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

// zram that swaps beside a swap partition reads zram, not mixed, and a host
// without a zram provider gets no step that stops it: a switch to disk still
// says why zram is on.
func TestSetSwapModeExplainsZramLeftBesideASwapPartition(t *testing.T) {
	f, sys := diskHost(t)
	sys.provider = ""
	f.write(procSwaps, swapsHeader+"/dev/sda3                               partition\t8388604\t\t0\t\t-3\n"+zramSwapsLine)
	f.write(etcFstab, "/dev/sda3 none swap sw 0 0\n")

	got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeDisk})
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != apischema.SwapModeZram {
		t.Fatalf("mode = %q, want zram", got.Mode)
	}
	assertLeft(t, got.Left, apischema.SwapLeft{What: apischema.SwapLeftZram, Reason: apischema.SwapLeftNotManaged})
}

// A swap file is removed only when a fresh status shows it as one that
// passes the strict check: one fstab stops naming after the plan is left.
func TestSetSwapModeRemovesOnlyWhatAFreshStatusAllows(t *testing.T) {
	f, sys := diskHost(t)
	req := zramRequest(t, sys)
	sys.after = func(call string) {
		if call == "start "+zramService {
			f.write(etcFstab, "UUID=0a1b2c3d / ext4 defaults 0 1\n")
		}
	}

	_, err := sys.executor(nil).setSwapMode(t.Context(), req)
	if !errors.Is(err, bridgeipc.ErrConflict) {
		t.Fatalf("err = %v, want a conflict", err)
	}
	if slices.ContainsFunc(sys.calls, func(call string) bool { return strings.Contains(call, "swap.img") }) || !f.exists("/swap.img") {
		t.Fatalf("calls = %v, want /swap.img left alone", sys.calls)
	}
}

// The success line says what stays active after the change, read from the
// host: also when the host did not end mixed, as here, where the other kind
// of swap goes away behind the run.
func TestSetSwapModeLogsWhatStaysActive(t *testing.T) {
	t.Run("a swap file after a switch to zram", func(t *testing.T) {
		logs := captureLogs(t)
		f, sys := diskHost(t)
		sys.after = func(call string) {
			if call == "write "+zswapTmpfilesPath {
				f.write(procSwaps, swapsHeader+swapFileSwapsLine)
			}
		}

		got, err := sys.executor(nil).setSwapMode(t.Context(), zramRequest(t, sys))
		if err != nil {
			t.Fatal(err)
		}
		if got.Mode != apischema.SwapModeDisk {
			t.Fatalf("mode = %q, want disk", got.Mode)
		}
		if want := "swap_file_left_active=true zram_left_active=false"; !strings.Contains(logs.String(), want) {
			t.Errorf("logs = %q, want %q", logs.String(), want)
		}
	})
	t.Run("zram after a switch to disk", func(t *testing.T) {
		logs := captureLogs(t)
		f, sys := zramHost(t)
		sys.provider = ""
		sys.after = func(call string) {
			if call == "set "+swappinessParam+"=60" {
				f.removeSwapsLine(ownSwapFilePath)
			}
		}

		got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeDisk})
		if err != nil {
			t.Fatal(err)
		}
		if got.Mode != apischema.SwapModeZram {
			t.Fatalf("mode = %q, want zram", got.Mode)
		}
		if want := "swap_file_left_active=false zram_left_active=true"; !strings.Contains(logs.String(), want) {
			t.Errorf("logs = %q, want %q", logs.String(), want)
		}
	})
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

	got, err := sys.executor(nil).setSwapMode(t.Context(), zramRequest(t, sys))
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != apischema.SwapModeMixed || slices.Contains(sys.calls, "swapoff /swap.img") {
		t.Fatalf("mode = %q, calls = %v; want the swap file left active", got.Mode, sys.calls)
	}
}

// A swap file in fstab that is off is removed by a switch to zram; another
// active swap file fstab does not name is never turned off.
func TestSetSwapModeToZramNeverTouchesAnUnmatchedSwapFile(t *testing.T) {
	f, sys := diskHost(t)
	f.write(procSwaps, swapsHeader+"/home/extra                             file\t\t1048572\t\t0\t\t-2\n")
	f.write("/home/extra", "")

	got, err := sys.executor(nil).setSwapMode(t.Context(), zramRequest(t, sys))
	if err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(sys.calls, func(call string) bool {
		return strings.Contains(call, "/home/extra") || strings.HasPrefix(call, "stop ")
	}) {
		t.Fatalf("calls = %v, want /home/extra and the units left alone", sys.calls)
	}
	if f.exists("/swap.img") || got.Mode != apischema.SwapModeZram {
		t.Fatalf("mode = %q, calls = %v; want /swap.img removed and zram", got.Mode, sys.calls)
	}
	assertLeft(t, got.Left)
}

// A failure after zram is up keeps zram swapping, disables the service the
// run enabled, puts the files and the live values back and names the failed
// step.
func TestSetSwapModeRollsBackFilesAndLiveValues(t *testing.T) {
	f, sys := diskHost(t)
	f.write(zswapEnabledParam, "Y\n")
	sys.failures["set "+zswapEnabledParam+"=N"] = errors.New("device busy")

	_, err := sys.executor(nil).setSwapMode(t.Context(), zramRequest(t, sys))
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

// A swap file this run made stays on when a later step fails.
func TestSetSwapModeKeepsTheSwapFileItMade(t *testing.T) {
	f, sys := zramHost(t)
	sys.failures["write "+sysctlDropInPath] = errors.New("read-only file system")

	_, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeDisk})
	if err == nil || !strings.Contains(err.Error(), string(apischema.SwapStepApplySysctls)) {
		t.Fatalf("err = %v, want the apply_sysctls step named", err)
	}
	if !strings.Contains(f.read(procSwaps), ownSwapFilePath) {
		t.Fatalf("calls = %v, want the swap file kept on", sys.calls)
	}
}

// A rollback action that fails is joined to the step's error.
func TestSetSwapModeJoinsAFailedRollback(t *testing.T) {
	f, sys := diskHost(t)
	f.write(zswapEnabledParam, "Y\n")
	sys.failures["set "+zswapEnabledParam+"=N"] = errors.New("device busy")
	sys.failures["set "+swappinessParam+"=60"] = errors.New("swappiness refused")

	_, err := sys.executor(nil).setSwapMode(t.Context(), zramRequest(t, sys))
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

// Leaving zram with room stops zram once LinuxIO's swap file swaps, and room
// counts the free space on disk swap.
func TestSetSwapModeStopsZramOnlyWithRoom(t *testing.T) {
	tests := []struct {
		name         string
		memAvailable uint64
		wantStopped  bool
	}{
		// 256 MiB in zram + 512 MiB margin; the new swap file adds 256 MiB
		// free.
		{name: "room in memory", memAvailable: 1 * gib, wantStopped: true},
		{name: "room with free disk swap", memAvailable: 600 * mib, wantStopped: true},
		{name: "no room", memAvailable: 10 * mib},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f, sys := zramHost(t)
			f.setMemAvailable(test.memAvailable)

			got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeDisk, SwapFileSize: 256 * mib})
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

	if _, err := sys.executor(nil).setSwapMode(t.Context(), zramRequest(t, sys)); err != nil {
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

	_, err := sys.executor(nil).setSwapMode(t.Context(), zramRequest(t, sys))
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
	req := zramRequest(t, sys)
	go func() {
		_, err := sys.executor(nil).setSwapMode(t.Context(), req)
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
	if _, err := second.executor(nil).createSwapFileRoute(t.Context(), apischema.SwapFileCreateRequest{Size: gib}); !errors.Is(err, bridgeipc.ErrConflict) {
		t.Errorf("create_swap_file err = %v, want a conflict", err)
	}
	if _, err := second.executor(nil).resizeSwapFile(t.Context(), apischema.SwapFileResizeRequest{Path: "/swap.img", Size: gib}); !errors.Is(err, bridgeipc.ErrConflict) {
		t.Errorf("resize_swap_file err = %v, want a conflict", err)
	}
	if _, err := second.executor(nil).removeSwapFile(t.Context(), apischema.SwapFilePathRequest{Path: "/swap.img"}); !errors.Is(err, bridgeipc.ErrConflict) {
		t.Errorf("remove_swap_file err = %v, want a conflict", err)
	}
	if _, err := second.executor(nil).moveSwapFile(t.Context(), apischema.SwapFilePathRequest{Path: "/swap.img"}); !errors.Is(err, bridgeipc.ErrConflict) {
		t.Errorf("move_swap_file err = %v, want a conflict", err)
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

// A swapoff the kernel refuses fails the step: nothing is removed, zram and
// the swap file both stay, and the run's files and live values are put back,
// zram at boot too, since the file keeps its fstab line.
func TestSetSwapModeFailsWhenSwapoffFails(t *testing.T) {
	f, sys := diskHost(t)
	sys.failures["swapoff /swap.img"] = syscall.EBUSY

	_, err := sys.executor(nil).setSwapMode(t.Context(), zramRequest(t, sys))
	if !errors.Is(err, syscall.EBUSY) || !strings.Contains(err.Error(), string(apischema.SwapStepRemoveSwapFile)) ||
		!strings.Contains(err.Error(), "swapoff /swap.img") {
		t.Fatalf("err = %v, want the remove_swap_file step to report the swapoff", err)
	}
	if slices.Contains(sys.calls, "fstab remove /swap.img") || !f.exists("/swap.img") {
		t.Fatalf("calls = %v, want nothing removed", sys.calls)
	}
	swaps := f.read(procSwaps)
	if !strings.Contains(swaps, "/dev/zram0") || !strings.Contains(swaps, "/swap.img") {
		t.Fatalf("/proc/swaps = %q, want zram and the swap file both kept", swaps)
	}
	assertRolledBackToDisk(t, f)
}

// A request that ends in the moment the swapoff finishes still removes the
// file: it is off, and removing its line and the file is not cancelled.
func TestSetSwapModeRemovesTheSwapFileAfterACancelAtTheEndOfSwapoff(t *testing.T) {
	f, sys := diskHost(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	sys.after = func(call string) {
		if call == "swapoff /swap.img" {
			cancel()
		}
	}

	_, _ = sys.executor(nil).setSwapMode(ctx, zramRequest(t, sys))
	if strings.Contains(f.read(procSwaps), "/swap.img") || strings.Contains(f.read(etcFstab), "/swap.img") || f.exists("/swap.img") {
		t.Fatalf("calls = %v, want the file off, its line gone and the file deleted", sys.calls)
	}
}

// A request cancelled while swapoff reads the file back ends the swapoff
// with the file still swapping; nothing is removed and the run is rolled
// back.
func TestSetSwapModeLeavesTheSwapFileOnWhenCancelledDuringSwapoff(t *testing.T) {
	f, sys := diskHost(t)
	sys.blocked["/swap.img"] = true
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	_, err := sys.executor(nil).setSwapMode(ctx, zramRequest(t, sys))
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), string(apischema.SwapStepRemoveSwapFile)) {
		t.Fatalf("err = %v, want the remove_swap_file step cancelled", err)
	}
	if slices.Contains(sys.calls, "fstab remove /swap.img") || !strings.Contains(f.read(procSwaps), "/swap.img") {
		t.Fatalf("calls = %v, want nothing removed and the file on", sys.calls)
	}
	assertRolledBackToDisk(t, f)
}

// A zramswap.service whose stop timed out or failed ends failed with zram
// still swapping: the step fails at once, the service stays enabled and the
// swap file the run made stays on. The stop is over, so nothing can take
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
	if !strings.Contains(f.read(procSwaps), ownSwapFilePath) {
		t.Fatal("the swap file the run made is off")
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
// swap file the run made, since swap is never undone. The rollback puts
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
	if swaps := f.read(procSwaps); !strings.Contains(swaps, ownSwapFilePath) || strings.Contains(swaps, "/dev/zram0") {
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
	// The swap file the run made is added swap, which a rollback never
	// undoes, and zram stays off at boot.
	if !strings.Contains(f.read(etcFstab), ownSwapFilePath) || sys.fileStates[zramService] != "disabled" {
		t.Errorf("fstab = %q, %s %q; want the swap file's line kept and zram off at boot",
			f.read(etcFstab), zramService, sys.fileStates[zramService])
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

	_, err := sys.executor(nil).setSwapMode(ctx, zramRequest(t, sys))
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
	sys.failures["swapoff /swap.img"] = syscall.EBUSY

	if _, err := sys.executor(nil).setSwapMode(t.Context(), zramRequest(t, sys)); err == nil {
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

	got, err := sys.executor(nil).setSwapMode(t.Context(), zramRequest(t, sys))
	if err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(sys.calls, func(c string) bool { return strings.Contains(c, "/swap.img") }) {
		t.Fatalf("calls = %v, want the swap file left on", sys.calls)
	}
	if got.Mode != apischema.SwapModeDisk || got.Swappiness != 60 {
		t.Errorf("status = mode %q swappiness %d, want disk at 60", got.Mode, got.Swappiness)
	}
	assertLeft(t, got.Left, apischema.SwapLeft{What: apischema.SwapLeftSwapFile, Reason: apischema.SwapLeftZramNotSwapping, Path: "/swap.img"})
	assertDiskSysctls(t, f)
	if got := f.read(sysctlDropInPath); got != zramDropIn {
		t.Errorf("drop-in = %q, want zram's %q", got, zramDropIn)
	}
}

// The reverse: a switch to zram that fails after the swap file went off
// leaves the host on zram only, and the sysctls end as zram's and not as the
// disk ones the rollback restored.
func TestSetSwapModeToZramThatFailsEndsWithZramSysctls(t *testing.T) {
	f, sys := diskHost(t)
	sys.failures["fstab remove /swap.img"] = errors.New("fstab is locked")

	if _, err := sys.executor(nil).setSwapMode(t.Context(), zramRequest(t, sys)); err == nil {
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

	if _, err := sys.executor(nil).setSwapMode(t.Context(), zramRequest(t, sys)); err == nil {
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

			if _, err := sys.executor(nil).setSwapMode(t.Context(), zramRequest(t, sys)); err == nil {
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

	if _, err := sys.executor(nil).setSwapMode(t.Context(), zramRequest(t, sys)); err != nil {
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
	sys.failures["swapoff /swap.img"] = syscall.EBUSY

	if _, err := sys.executor(nil).setSwapMode(t.Context(), zramRequest(t, sys)); err == nil {
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

		if _, err := sys.executor(nil).setSwapMode(t.Context(), zramRequest(t, sys)); err != nil {
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

// Removing a temporary swap file a resize left, which never had an fstab
// line, does not keep zram at boot out of the rollback: when the file it
// stood in for then fails to go, zram at boot is put back with the rest.
func TestSetSwapModeRemovingATemporaryFileKeepsZramAtBootInTheRollback(t *testing.T) {
	f, sys := diskHost(t)
	f.writeSwapFile(tempPath, 256*mib)
	f.write(procSwaps, f.read(procSwaps)+swapsLine(tempPath, 256*mib, 0))
	sys.failures["swapoff /swap.img"] = syscall.EBUSY

	_, err := sys.executor(nil).setSwapMode(t.Context(), zramRequest(t, sys))
	if !errors.Is(err, syscall.EBUSY) {
		t.Fatalf("err = %v, want the swapoff of /swap.img to fail the run", err)
	}
	if f.exists(tempPath) || !strings.Contains(f.read(etcFstab), "/swap.img ") {
		t.Fatalf("calls = %v, want the temporary file removed and /swap.img kept with its line", sys.calls)
	}
	assertRolledBackToDisk(t, f)
}

// A temporary swap file a resize left whose data does not fit in memory
// stays, and so does the file it stood in for, whose own data would fit:
// the run ends with both named, and nothing is rolled back.
func TestSetSwapModeToZramLeavesAFileWhoseTemporaryFileStays(t *testing.T) {
	for name, baseActive := range map[string]bool{"both swap": true, "only the temporary file swaps": false} {
		t.Run(name, func(t *testing.T) {
			f, sys := diskHost(t)
			f.writeSwapFile(tempPath, 4*gib)
			swaps := swapsHeader + swapsLine(tempPath, 4*gib, 3*gib)
			if baseActive {
				swaps += swapsLine("/swap.img", 2*gib, 0)
			}
			f.write(procSwaps, swaps)
			f.setMemAvailable(900 * mib)

			got, err := sys.executor(nil).setSwapMode(t.Context(), zramRequest(t, sys))
			if err != nil {
				t.Fatal(err)
			}
			assertLeft(t, got.Left,
				apischema.SwapLeft{What: apischema.SwapLeftSwapFile, Reason: apischema.SwapLeftNotEnoughMemory, Path: tempPath},
				apischema.SwapLeft{What: apischema.SwapLeftSwapFile, Reason: apischema.SwapLeftNotEnoughMemory, Path: "/swap.img"})
			if slices.ContainsFunc(sys.calls, func(c string) bool { return strings.Contains(c, "/swap.img") }) {
				t.Fatalf("calls = %v, want both files left alone", sys.calls)
			}
			if !f.exists(tempPath) || !f.exists("/swap.img") || !strings.Contains(f.read(etcFstab), "/swap.img ") {
				t.Fatalf("fstab = %q, want both files and the line kept", f.read(etcFstab))
			}
			if got := f.read(sysctlDropInPath); got != zramDropIn {
				t.Errorf("drop-in = %q, want zram's %q: the run is not rolled back", got, zramDropIn)
			}
		})
	}
}

// zswap turned on by hand reads as the mode, and nothing turns it on at the
// next start. Applying the mode the host is in writes what does, and keeps
// the swappiness the admin set: it is no choice of a preset.
func TestSetSwapModeAgainSavesTheModeForBootAndKeepsSwappiness(t *testing.T) {
	f, sys := diskHost(t)
	f.write(zswapEnabledParam, "Y\n")
	f.write(zswapCompressorParam, "zstd\n")
	f.writeVMSysctls("10", "3", "15000", "10")
	dropIn := managedHeader + "vm.swappiness = 10\n"
	f.write(sysctlDropInPath, dropIn)
	before, _, err := sys.status(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if before.Mode != apischema.SwapModeZswap || before.ModeSavedForBoot {
		t.Fatalf("before: mode %q, saved for boot %v, want zswap that is not saved", before.Mode, before.ModeSavedForBoot)
	}

	got, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZswap})
	if err != nil {
		t.Fatal(err)
	}

	assertCalls(t, sys.calls, []string{
		"disable " + zramService,
		"write " + sysctlDropInPath,
		"set " + zswapCompressorParam + "=zstd",
		"set " + zswapEnabledParam + "=Y",
		"write " + zswapTmpfilesPath,
	})
	if got.Mode != apischema.SwapModeZswap || !got.ModeSavedForBoot {
		t.Errorf("after: mode %q, saved for boot %v, want zswap that is saved", got.Mode, got.ModeSavedForBoot)
	}
	if got, want := f.read(zswapTmpfilesPath), string(renderZswapTmpfiles("zstd")); got != want {
		t.Errorf("tmpfiles = %q, want %q", got, want)
	}
	if got := f.param(swappinessParam); got != "10" {
		t.Errorf("swappiness = %q, want the admin's 10", got)
	}
	if got := f.read(sysctlDropInPath); got != dropIn {
		t.Errorf("drop-in = %q, want %q", got, dropIn)
	}
}

// The drop-in can hold another mode's swappiness, as after a switch that
// left the host where it was: applying the mode again saves the swappiness
// the host runs with, so the next start does not bring the other one.
func TestSetSwapModeAgainSavesTheSwappinessTheHostRunsWith(t *testing.T) {
	f, sys := diskHost(t)
	f.write(zswapEnabledParam, "Y\n")
	f.writeVMSysctls("45", "3", "15000", "10")
	f.write(sysctlDropInPath, managedHeader+"vm.swappiness = 180\n")

	if _, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZswap}); err != nil {
		t.Fatal(err)
	}
	for _, call := range sys.calls {
		if strings.HasPrefix(call, "set "+swappinessParam) {
			t.Fatalf("calls = %v, want the live swappiness untouched", sys.calls)
		}
	}
	if got, want := f.read(sysctlDropInPath), managedHeader+"vm.swappiness = 45\n"; got != want {
		t.Errorf("drop-in = %q, want %q", got, want)
	}
}

// Under TuneD the drop-in's swappiness is left alone on such a run too.
func TestSetSwapModeAgainUnderTunedLeavesTheDropInSwappiness(t *testing.T) {
	f, sys := diskHost(t)
	sys.tuned = true
	f.write(zswapEnabledParam, "Y\n")
	f.writeVMSysctls("45", "3", "15000", "10")
	dropIn := managedHeader + "vm.swappiness = 30\n"
	f.write(sysctlDropInPath, dropIn)

	if _, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZswap}); err != nil {
		t.Fatal(err)
	}
	if got := f.read(sysctlDropInPath); got != dropIn {
		t.Errorf("drop-in = %q, want %q", got, dropIn)
	}
}

// A switch to another mode still applies that mode's preset.
func TestSetSwapModeToAnotherModeAppliesItsSwappiness(t *testing.T) {
	f, sys := diskHost(t)
	f.writeVMSysctls("10", "3", "15000", "10")
	f.write(sysctlDropInPath, managedHeader+"vm.swappiness = 10\n")

	if _, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZswap}); err != nil {
		t.Fatal(err)
	}
	if got := f.param(swappinessParam); got != "60" {
		t.Errorf("swappiness = %q, want zswap's 60", got)
	}
}
