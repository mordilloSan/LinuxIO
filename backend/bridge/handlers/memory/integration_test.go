package memory

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/systemd"
)

// TestMemoryIntegration switches the real host to zram and back through
// systemd, the kernel and /etc, then again from zram running at another size
// beside the swap file, and checks that systemd takes the raised stop
// timeout of the swap file's unit. It needs root, a zram provider and a swap
// file in /etc/fstab, so it runs only in the disposable test VM:
// `make vm-test-memory-integration`. Cleanup puts the starting mode back.
func TestMemoryIntegration(t *testing.T) {
	if os.Getenv("LINUXIO_RUN_MEMORY_INTEGRATION") != "1" {
		t.Skip("set LINUXIO_RUN_MEMORY_INTEGRATION=1 and run as root in the test VM")
	}
	if os.Geteuid() != 0 {
		t.Skip("needs root")
	}
	if host.zramProvider() == "" {
		t.Skip("neither zram-tools nor zram-generator is installed")
	}
	t.Logf("zram provider: %s", host.zramProvider())
	ctx := t.Context()
	e := newExecutor(func(p apischema.SwapProgress) { t.Logf("step %d/%d %s %s", p.Index, p.Total, p.Step, p.Device) })
	view, err := e.readView(ctx)
	if err != nil {
		t.Fatal(err)
	}
	file, ok := view.managedSwapFile()
	if !ok {
		t.Skip("no swap file in /etc/fstab")
	}
	start := view.status.Mode
	back := start
	switch start {
	case apischema.SwapModeDisk, apischema.SwapModeZswap:
	case apischema.SwapModeZram:
		back = apischema.SwapModeDisk
	default:
		t.Skipf("start from disk, zswap or zram, not %s", start)
	}
	t.Cleanup(func() {
		if _, err := e.setSwapMode(context.WithoutCancel(ctx), start); err != nil {
			t.Errorf("restore %s: %v", start, err)
		}
	})

	if start != apischema.SwapModeZram {
		switchTo(t, e, apischema.SwapModeZram)
	}
	assertZramMode(t, e, file)

	assertDiskMode(t, file, switchTo(t, e, back))

	assertStopTimeoutTaken(t, e, file)

	// zram that swaps at another size beside the swap file, as the one
	// zram-tools starts when it is installed, ends at the planned size.
	startSmallZram(t, e)
	switchTo(t, e, apischema.SwapModeZram)
	assertZramMode(t, e, file)

	assertDiskMode(t, file, switchTo(t, e, back))
}

// startSmallZram makes the provider swap on a 64 MiB zram device.
func startSmallZram(t *testing.T, e *executor) {
	t.Helper()
	ctx := t.Context()
	unit, path, config := zramService, zramswapConfigPath, renderZramswapConfig(64<<20)
	if host.zramProvider() == apischema.ZramProviderGenerator {
		unit, path, config = zramGeneratorSwapUnit, zramGeneratorConfigPath, renderZramGeneratorConfig(64<<20)
	}
	if err := e.sys.writeFile(path, config, managedFileMode); err != nil {
		t.Fatal(err)
	}
	if err := e.sys.reload(ctx); err != nil {
		t.Fatal(err)
	}
	if err := e.sys.restartUnit(ctx, unit); err != nil {
		t.Fatal(err)
	}
	if err := waitUntil(ctx, e.waits.swapOn, e.waits.poll, func() (bool, error) {
		status, err := e.sys.status(ctx)
		return err == nil && hasDevice(status.Devices, apischema.SwapDeviceZram) && zramSized(status, 64<<20), err
	}); err != nil {
		t.Fatalf("start a 64 MiB zram device: %v", err)
	}
}

// assertStopTimeoutTaken checks that systemd reads the raised timeout for
// the swap file's unit, which fstab generates, and the unit's own again
// once it is put back.
func assertStopTimeoutTaken(t *testing.T, e *executor, file fstabSwap) {
	t.Helper()
	ctx := t.Context()
	timeout := func() string {
		out, err := exec.CommandContext(ctx, "systemctl", "show", "--property=TimeoutUSec", "--value", file.unit).Output()
		if err != nil {
			t.Fatalf("read the timeout of %s: %v", file.unit, err)
		}
		return strings.TrimSpace(string(out))
	}
	before := timeout()
	if err := e.sys.extendStopTimeout(ctx, file.unit, 47*time.Minute); err != nil {
		t.Fatal(err)
	}
	raised := timeout()
	if err := e.sys.restoreStopTimeout(ctx, file.unit); err != nil {
		t.Fatal(err)
	}
	if raised != "47min" {
		t.Errorf("timeout of %s while raised = %q, want 47min", file.unit, raised)
	}
	if after := timeout(); after != before {
		t.Errorf("timeout of %s after it was put back = %q, want %q as before", file.unit, after, before)
	}
}

func switchTo(t *testing.T, e *executor, mode apischema.SwapMode) apischema.SwapStatus {
	t.Helper()
	got, err := e.setSwapMode(t.Context(), mode)
	if err != nil {
		t.Fatalf("switch to %s: %v", mode, err)
	}
	if got.Mode != mode || len(got.Left) > 0 {
		t.Fatalf("after switching to %s: mode %s, left as it was %+v", mode, got.Mode, got.Left)
	}
	return got.SwapStatus
}

func assertZramMode(t *testing.T, e *executor, file fstabSwap) {
	t.Helper()
	got, err := e.sys.status(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != apischema.SwapModeZram || got.Zswap.Enabled {
		t.Fatalf("status = mode %s zswap %v, want zram without zswap", got.Mode, got.Zswap.Enabled)
	}
	if !got.TunedActive && got.Swappiness != 180 {
		t.Errorf("swappiness = %d, want 180", got.Swappiness)
	}
	if got.PageCluster != 0 || got.WatermarkBoostFactor != 0 || got.WatermarkScaleFactor != 125 {
		t.Errorf("sysctls = page-cluster %d boost %d scale %d, want 0, 0, 125", got.PageCluster, got.WatermarkBoostFactor, got.WatermarkScaleFactor)
	}
	if state := unitFileState(t, file.unit); state != "masked" {
		t.Errorf("%s file state = %q, want masked", file.unit, state)
	}
	if !zramSized(got, got.ZramPlannedSize) {
		t.Errorf("zram devices = %+v, want the planned %d MiB", got.ZramDevices, got.ZramPlannedSize>>20)
	}
	assertNoStopTimeoutLeft(t, file.unit)
	if got.ZramProvider == apischema.ZramProviderGenerator {
		config, readErr := os.ReadFile(zramGeneratorConfigPath)
		if readErr != nil || !strings.Contains(string(config), "[zram0]\n") || !strings.Contains(string(config), "compression-algorithm = zstd\n") {
			t.Errorf("%s = %q, %v; want a zram0 device with zstd", zramGeneratorConfigPath, config, readErr)
		}
		if state, stateErr := e.sys.activeState(t.Context(), zramGeneratorSetupUnit); stateErr != nil || state != "active" {
			t.Errorf("%s = %q, %v; want active", zramGeneratorSetupUnit, state, stateErr)
		}
		return
	}
	if state := unitFileState(t, zramService); state != "enabled" {
		t.Errorf("%s file state = %q, want enabled", zramService, state)
	}
	config, err := os.ReadFile(zramswapConfigPath)
	if err != nil || !strings.Contains(string(config), "ALGO=zstd\n") {
		t.Errorf("%s = %q, %v; want ALGO=zstd", zramswapConfigPath, config, err)
	}
}

func assertNoStopTimeoutLeft(t *testing.T, unit string) {
	t.Helper()
	name, err := stopTimeoutPath(unit)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(name); err == nil {
		t.Errorf("%s left behind after the stop", name)
	}
}

func assertDiskMode(t *testing.T, file fstabSwap, got apischema.SwapStatus) {
	t.Helper()
	if got.SwapFile == nil || !got.SwapFile.Active {
		t.Fatalf("swap file = %+v, want it active", got.SwapFile)
	}
	for _, device := range got.Devices {
		if device.Kind == apischema.SwapDeviceZram {
			t.Fatalf("devices = %+v, want no zram swapping", got.Devices)
		}
	}
	if state := unitFileState(t, file.unit); state == "masked" {
		t.Errorf("%s file state = %q, want unmasked", file.unit, state)
	}
	if got.ZramProvider == apischema.ZramProviderGenerator {
		config, err := os.ReadFile(zramGeneratorConfigPath)
		if err != nil || string(config) != string(renderZramGeneratorOff()) {
			t.Errorf("%s = %q, %v; want the configuration with no device", zramGeneratorConfigPath, config, err)
		}
		return
	}
	if state := unitFileState(t, zramService); state != "disabled" {
		t.Errorf("%s file state = %q, want disabled", zramService, state)
	}
}

func unitFileState(t *testing.T, unit string) string {
	t.Helper()
	state, err := systemd.GetUnitFileState(t.Context(), unit)
	if err != nil {
		t.Fatalf("read the file state of %s: %v", unit, err)
	}
	return state
}
