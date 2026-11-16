package memory

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/systemd"
)

// TestMemoryIntegration switches the real host to zram and back through
// systemd, the kernel and /etc, drains the swap file, which takes it away
// with swapoff and has systemd start it again, and switches to zram once
// more from zram running at another size beside the swap file. It needs
// root, a zram provider and a swap file in /etc/fstab, so it runs only in
// the disposable test VM: `make vm-test-memory-integration`. Cleanup puts
// the starting mode back.
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

	assertDrainsTheSwapFile(t, e)

	// zram that swaps at another size beside the swap file, as the one
	// zram-tools starts when it is installed, ends at the planned size.
	startSmallZram(t, e)
	switchTo(t, e, apischema.SwapModeZram)
	assertZramMode(t, e, file)

	assertDiskMode(t, file, switchTo(t, e, back))
}

// startSmallZram makes the provider swap on a 64 MiB zram device. zram is
// not swapping; zram-tools' device may still be set up from the stop before,
// and is then reset first.
func startSmallZram(t *testing.T, e *executor) {
	t.Helper()
	ctx := t.Context()
	unit, path, config := zramService, zramswapConfigPath, renderZramswapConfig(64<<20)
	if host.zramProvider() == apischema.ZramProviderGenerator {
		unit, path, config = zramGeneratorSwapUnit, zramGeneratorConfigPath, renderZramGeneratorConfig(64<<20)
	} else if err := e.sys.resetZram(ctx, zramToolsDevice); err != nil {
		t.Fatalf("reset %s: %v", zramToolsDevice, err)
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

// assertDrainsTheSwapFile takes the active swap file away with swapoff
// and has its unit start it again, as a drain does for a device that holds
// data. It checks what the unit tests only assume: that systemd sees the
// device leave behind its back, reads the unit inactive and then starts it.
func assertDrainsTheSwapFile(t *testing.T, e *executor) {
	t.Helper()
	ctx := t.Context()
	view, err := e.readView(ctx)
	if err != nil {
		t.Fatal(err)
	}
	file, ok := view.managedSwapFile()
	if !ok || file.device == "" {
		t.Fatalf("swap file = %+v, want it active before the drain", file)
	}
	if err := e.drainDevice(ctx, file, 0); err != nil {
		t.Fatalf("drain %s: %v", file.device, err)
	}
	if on, err := e.deviceIs(file.device, true)(); err != nil || !on {
		t.Fatalf("%s active after the drain = %v, %v; want it back", file.device, on, err)
	}
	if state, err := e.sys.activeState(ctx, file.unit); err != nil || state != "active" {
		t.Errorf("%s = %q, %v; want active after the drain", file.unit, state, err)
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
	assertZramSizedAndFileSeenOff(t, e, file, got)
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

// assertZramSizedAndFileSeenOff checks that zram has the planned size and
// that systemd saw the swap file go: it left through swapoff, not a stop
// of its unit.
func assertZramSizedAndFileSeenOff(t *testing.T, e *executor, file fstabSwap, got apischema.SwapStatus) {
	t.Helper()
	if !zramSized(got, got.ZramPlannedSize) {
		t.Errorf("zram devices = %+v, want the planned %d MiB", got.ZramDevices, got.ZramPlannedSize>>20)
	}
	if state, err := e.sys.activeState(t.Context(), file.unit); err != nil || state == "active" {
		t.Errorf("%s = %q, %v; want it no longer active", file.unit, state, err)
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
