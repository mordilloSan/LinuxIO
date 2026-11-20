package memory

import (
	"context"
	"errors"
	"os"
	"slices"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

// vendorGeneratorConfig is the main zram-generator file a package installs,
// as Fedora's zram-generator-defaults does. The generator reads it while no
// main file in /run, /etc or /usr/local/lib takes its place.
const vendorGeneratorConfig = "/usr/lib/systemd/zram-generator.conf"

// TestMemoryDefaultZramIntegration starts from the zram a distribution turns
// on with zram-generator, as Fedora does: the vendor file and zram0 swapping
// at the size it sets from boot. The status must read zram, or mixed beside
// an active swap file, with zram-generator as the provider and no other file
// of its listed. Without disk swap, disk is refused and a switch to zram
// writes LinuxIO's configuration for the next boot and keeps zram at its
// size, since a restart would leave the host without swap meanwhile. Beside
// a swap file, a switch to zram restarts it at the planned size, and a switch
// to disk stops it. Cleanup puts the default back. The rig runs it on such a
// host twice, without and then with a swap file:
// `make vm-test-memory-integration DISTRO=fedora`.
func TestMemoryDefaultZramIntegration(t *testing.T) {
	if os.Getenv("LINUXIO_RUN_MEMORY_INTEGRATION") != "1" {
		t.Skip("set LINUXIO_RUN_MEMORY_INTEGRATION=1 and run as root in the test VM")
	}
	if os.Geteuid() != 0 {
		t.Skip("needs root")
	}
	if main := existingGeneratorMainFiles(); !slices.Equal(main, []string{vendorGeneratorConfig}) {
		t.Skipf("zram-generator main files %v: the default is the vendor's %s alone", main, vendorGeneratorConfig)
	}
	asJobHost(t)
	ctx := t.Context()
	e := newExecutor(func(p apischema.SwapProgress) { t.Logf("step %d/%d %s %s", p.Index, p.Total, p.Step, p.Device) })
	view, err := e.readView(ctx)
	if err != nil {
		t.Fatal(err)
	}
	status := view.status
	t.Logf("default zram: mode %s, provider %s, devices %+v, zram %+v, planned %d MiB",
		status.Mode, status.ZramProvider, status.Devices, status.ZramDevices, status.ZramPlannedSize>>20)
	if status.ZramProvider != apischema.ZramProviderGenerator || !status.ZramAvailable {
		t.Fatalf("zram provider %q, available %v; want zram-generator", status.ZramProvider, status.ZramAvailable)
	}
	if !slices.ContainsFunc(status.Devices, func(d apischema.SwapDevice) bool { return d.Path == zramGeneratorDevice }) {
		t.Fatalf("devices = %+v, want %s swapping as %s sets it up", status.Devices, zramGeneratorDevice, vendorGeneratorConfig)
	}
	if len(status.ZramGeneratorDevices) != 0 || status.ForeignZramConfig != nil {
		t.Errorf("generator devices %+v, configuration not LinuxIO's %+v; want neither for the vendor file", status.ZramGeneratorDevices, status.ForeignZramConfig)
	}
	disk := hasDevice(status.Devices, apischema.SwapDeviceFile) || hasDevice(status.Devices, apischema.SwapDevicePartition)
	want := apischema.SwapModeZram
	if disk {
		want = apischema.SwapModeMixed
	}
	if status.Mode != want {
		t.Fatalf("mode = %s, want %s", status.Mode, want)
	}
	file, hasFile := view.managedSwapFile()
	snapshot := takeHostSnapshot(t, status, file.unit)
	t.Cleanup(func() { snapshot.restore(t, e, apischema.ZramProviderGenerator) })
	t.Cleanup(func() {
		if t.Failed() {
			logZramState(t)
		}
	})
	if !disk {
		assertDefaultZramAlone(t, e, status)
		return
	}
	if !hasFile {
		t.Fatalf("disk swap %+v swaps, but /etc/fstab names no swap file", status.Devices)
	}
	switchTo(t, e, apischema.SwapModeZram)
	assertZramMode(t, e, file)
	assertDiskMode(t, file, switchTo(t, e, apischema.SwapModeDisk))
}

// assertDefaultZramAlone checks a host whose only swap is the default zram:
// nothing to recommend but zram, disk refused, and a switch to zram that
// writes LinuxIO's configuration and keeps zram at its size.
func assertDefaultZramAlone(t *testing.T, e *executor, status apischema.SwapStatus) {
	t.Helper()
	ctx := t.Context()
	if status.SwapFile != nil || status.RecommendedMode != apischema.SwapModeZram {
		t.Errorf("swap file %+v, recommended %s; want none and zram", status.SwapFile, status.RecommendedMode)
	}
	if _, err := e.setSwapMode(ctx, apischema.SwapModeRequest{Mode: apischema.SwapModeDisk}); !errors.Is(err, bridgeipc.ErrConflict) {
		t.Errorf("switch to disk without disk swap: %v, want a conflict", err)
	}
	got, err := e.setSwapMode(ctx, apischema.SwapModeRequest{Mode: apischema.SwapModeZram})
	if err != nil || got.Mode != apischema.SwapModeZram {
		t.Fatalf("switch to zram: mode %s, %v", got.Mode, err)
	}
	left := []apischema.SwapLeft{}
	if !zramSized(status, status.ZramPlannedSize) {
		left = append(left, apischema.SwapLeft{What: apischema.SwapLeftZramSize, Reason: apischema.SwapLeftNoOtherSwap})
	}
	if !slices.Equal(got.Left, left) {
		t.Errorf("left as it was %+v, want %+v", got.Left, left)
	}
	if config, readErr := os.ReadFile(zramGeneratorConfigPath); readErr != nil || string(config) != string(renderZramGeneratorConfig(status.ZramPlannedSize)) {
		t.Errorf("%s = %q, %v; want LinuxIO's configuration for the next boot", zramGeneratorConfigPath, config, readErr)
	}
}

// TestMemoryRebootIntegration checks across a reboot that the drop-in a
// switch to zram puts on the swap file's unit keeps the file off at boot
// while swap.target, which requires that unit, still starts, and with it
// zram with either tool; and that a switch back removes the drop-in, starts
// the file and drains it through that unit. It runs in two phases around a
// reboot the rig makes, LINUXIO_MEMORY_REBOOT_PHASE=before and then after,
// once with the swap file fstab names by its own path and once through a
// link to it.
func TestMemoryRebootIntegration(t *testing.T) {
	if os.Getenv("LINUXIO_RUN_MEMORY_INTEGRATION") != "1" {
		t.Skip("set LINUXIO_RUN_MEMORY_INTEGRATION=1 and run as root in the test VM")
	}
	if os.Geteuid() != 0 {
		t.Skip("needs root")
	}
	phase := os.Getenv("LINUXIO_MEMORY_REBOOT_PHASE")
	if phase != "before" && phase != "after" {
		t.Skip("set LINUXIO_MEMORY_REBOOT_PHASE to before or after a reboot")
	}
	asJobHost(t)
	ctx := t.Context()
	e := newExecutor(func(p apischema.SwapProgress) { t.Logf("step %d/%d %s %s", p.Index, p.Total, p.Step, p.Device) })
	view, err := e.readView(ctx)
	if err != nil {
		t.Fatal(err)
	}
	file, ok := view.managedSwapFile()
	if !ok {
		t.Fatal("no swap file in /etc/fstab")
	}
	t.Logf("%s the reboot: mode %s, swap file %s, its unit %s", phase, view.status.Mode, file.path, file.unit)
	if phase == "before" {
		if view.status.Mode != apischema.SwapModeDisk {
			switchTo(t, e, apischema.SwapModeDisk)
		}
		switchTo(t, e, apischema.SwapModeZram)
		assertZramMode(t, e, file)
		return
	}
	// A failure here leaves the host on disk swap for the steps after it.
	t.Cleanup(func() {
		if t.Failed() {
			_, _ = e.setSwapMode(context.WithoutCancel(ctx), apischema.SwapModeRequest{Mode: apischema.SwapModeDisk})
		}
	})
	assertHeldOffAtBoot(t, e, view, file)
	got := switchTo(t, e, apischema.SwapModeDisk)
	assertDiskMode(t, file, got)
	// systemd ties the state of a unit named after the file's own path to
	// /proc/swaps. A unit named after a link to it, systemd 259 reads
	// inactive as soon as another swap device comes or goes, here zram
	// leaving, while the file swaps on; LinuxIO reads no state of it but
	// before a start, once the file left.
	state, err := e.sys.activeState(ctx, file.unit)
	t.Logf("after the switch back: %s %s (%v)", file.unit, state, err)
	ownPath := slices.ContainsFunc(got.Devices, func(d apischema.SwapDevice) bool { return d.Path == file.path })
	if ownPath && (err != nil || state != "active") {
		t.Errorf("%s = %q, %v; want the unit fstab makes back and active", file.unit, state, err)
	}
	assertDrainsTheSwapFile(t, e)
}

// assertHeldOffAtBoot checks a host booted with the swap file's unit held
// off: the file is off, its unit held off by LinuxIO's drop-in and not
// active, swap.target active, which the fstab generator makes require the
// file's unit and zram-generator's swap unit is wanted by, and zram, which
// the provider starts at boot, the only swap.
func assertHeldOffAtBoot(t *testing.T, e *executor, view swapView, file fstabSwap) {
	t.Helper()
	target, err := e.sys.activeState(t.Context(), "swap.target")
	t.Logf("after the reboot: swap.target %s (%v), devices %+v", target, err, view.status.Devices)
	if view.status.Mode != apischema.SwapModeZram || file.device != "" {
		t.Fatalf("after the reboot: mode %s, devices %+v, swap.target %s; want zram alone and %s off", view.status.Mode, view.status.Devices, target, file.path)
	}
	if err != nil || target != "active" {
		t.Errorf("swap.target = %q, %v; want active", target, err)
	}
	assertHeldOff(t, file.unit, true)
	if state, err := e.sys.activeState(t.Context(), file.unit); err != nil || state == "active" {
		t.Errorf("%s = %q, %v; want it not active", file.unit, state, err)
	}
}
