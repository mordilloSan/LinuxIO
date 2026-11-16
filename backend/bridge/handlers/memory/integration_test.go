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
// systemd, the kernel and /etc. It needs root, zram-tools and a swap file in
// /etc/fstab, so it runs only in the disposable test VM:
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
}

func switchTo(t *testing.T, e *executor, mode apischema.SwapMode) apischema.SwapStatus {
	t.Helper()
	got, err := e.setSwapMode(t.Context(), mode)
	if err != nil {
		t.Fatalf("switch to %s: %v", mode, err)
	}
	if got.Mode != mode {
		t.Fatalf("mode after switching to %s = %s", mode, got.Mode)
	}
	return got
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
