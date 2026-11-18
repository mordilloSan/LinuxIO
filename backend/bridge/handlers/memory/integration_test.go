package memory

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/common/systemd"
)

// TestMemoryIntegration switches the real host to zram and back through
// systemd, the kernel and /etc, drains the swap file, which takes it away
// with swapoff and has systemd start it again, and switches to zram once
// more from zram running at another size beside the swap file. It then asks
// before replacing an admin's zram configuration and keeps its copy, refuses
// to leave zram while another zram-generator file defines a swap device, and
// switches to zswap and back with its tmpfiles entry. It needs root, a zram
// provider and a swap file in /etc/fstab, so it runs only in the disposable
// test VM: `make vm-test-memory-integration`. A host that starts mixed, as a
// fresh zram-generator install leaves it, goes to disk first. Cleanup puts
// the host back as it was: its mode, the files LinuxIO manages and the
// copies it made, zram-tools' start at boot, and the live sysctls and zswap.
func TestMemoryIntegration(t *testing.T) {
	if os.Getenv("LINUXIO_RUN_MEMORY_INTEGRATION") != "1" {
		t.Skip("set LINUXIO_RUN_MEMORY_INTEGRATION=1 and run as root in the test VM")
	}
	if os.Geteuid() != 0 {
		t.Skip("needs root")
	}
	ctx := t.Context()
	provider := host.zramProvider(ctx, systemd.GetActiveState)
	if provider == "" {
		t.Skip("neither zram-tools nor zram-generator is installed")
	}
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
	t.Logf("zram provider %s, mode %s, configuration not LinuxIO's %+v, other generator devices %+v, main generator files %v",
		provider, start, view.status.ForeignZramConfig, view.status.ZramGeneratorDevices, existingGeneratorMainFiles())
	if start == apischema.SwapModeNone {
		t.Skip("start from disk, zswap, zram or mixed, not none")
	}
	snapshot := takeHostSnapshot(t, view.status)
	t.Cleanup(func() { snapshot.restore(t, e, provider) })
	// Runs before the restore: what systemd and the kernel say about zram
	// when a step failed.
	t.Cleanup(func() {
		if t.Failed() {
			logZramState(t)
		}
	})
	back := start
	if start == apischema.SwapModeZram || start == apischema.SwapModeMixed {
		back = apischema.SwapModeDisk
	}
	if start == apischema.SwapModeMixed {
		switchTo(t, e, back)
	}

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

	assertStartsZramAStopLeftSetUp(t, e, provider, file, back)

	assertAsksBeforeReplacingAnAdminsFile(t, e, provider, file, back)
	if provider == apischema.ZramProviderGenerator {
		assertListsADropInDeviceAndRefusesToLeaveZram(t, e)
	}
	assertZswapRoundTrip(t, e)
}

// logZramState logs what systemd and the kernel say about zram0 and its
// units, for a failed run.
func logZramState(t *testing.T) {
	t.Helper()
	for _, args := range [][]string{
		{"systemctl", "--no-pager", "status", zramGeneratorSwapUnit, zramGeneratorSetupUnit, "dev-zram0.device", zramService},
		{"journalctl", "--no-pager", "-n", "80", "-u", zramGeneratorSwapUnit, "-u", zramGeneratorSetupUnit, "-u", zramService},
		{"systemctl", "--no-pager", "cat", zramGeneratorSwapUnit, zramGeneratorSetupUnit},
		{"ls", "-l", "/dev/zram0", "/sys/class/zram-control"},
		{"cat", "/proc/swaps", "/sys/block/zram0/disksize", "/sys/block/zram0/comp_algorithm"},
	} {
		out, err := exec.CommandContext(context.WithoutCancel(t.Context()), args[0], args[1:]...).CombinedOutput()
		t.Logf("$ %s (%v)\n%s", strings.Join(args, " "), err, out)
	}
}

// testDropIn is the zram-generator drop-in the test writes, and removes.
const testDropIn = zramGeneratorEtcDropIns + "/90-linuxio-integration-test.conf"

// existingGeneratorMainFiles names the zram-generator main files on the host,
// for the log: the first of them in /run, /etc, /usr/local/lib, /usr/lib is
// the one the generator reads.
func existingGeneratorMainFiles() []string {
	var found []string
	for _, dir := range zramGeneratorDirs {
		if name := dir + "/zram-generator.conf"; host.exists(name) {
			found = append(found, name)
		}
	}
	return found
}

// hostSnapshot is the host as TestMemoryIntegration found it.
type hostSnapshot struct {
	status apischema.SwapStatus
	// files are the files LinuxIO manages and the copies it may make.
	files []savedFile
	// toolsState is zramswap.service's file state, or empty without
	// zram-tools.
	toolsState    string
	dropInDirSeen bool
}

func takeHostSnapshot(t *testing.T, status apischema.SwapStatus) hostSnapshot {
	t.Helper()
	snapshot := hostSnapshot{status: status, dropInDirSeen: host.exists(zramGeneratorEtcDropIns)}
	for _, name := range []string{sysctlDropInPath, zswapTmpfilesPath, zramswapConfigPath, zramGeneratorConfigPath,
		zramswapConfigPath + zramConfigCopySuffix, zramGeneratorConfigPath + zramConfigCopySuffix} {
		saved, err := host.saveFile(name)
		if err != nil {
			t.Fatalf("save %s: %v", name, err)
		}
		snapshot.files = append(snapshot.files, saved)
	}
	if tools, _ := host.zramToolsInstalled(); tools {
		snapshot.toolsState = unitFileState(t, zramService)
	}
	return snapshot
}

// restore puts the host back as the snapshot found it. zram that swapped
// beside the swap file, which no mode reaches, is started again through its
// provider's unit once the files are back.
func (s hostSnapshot) restore(t *testing.T, e *executor, provider apischema.ZramProvider) {
	ctx := context.WithoutCancel(t.Context())
	for _, name := range []string{zramswapConfigPath + zramConfigCopySuffix, zramGeneratorConfigPath + zramConfigCopySuffix} {
		if info, err := os.Lstat(name); err == nil && info.IsDir() {
			_ = os.Remove(name)
		}
	}
	if err := os.Remove(testDropIn); err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("remove %s: %v", testDropIn, err)
	}
	if !s.dropInDirSeen {
		_ = os.Remove(zramGeneratorEtcDropIns)
	}
	mode := s.status.Mode
	if mode == apischema.SwapModeMixed {
		mode = apischema.SwapModeDisk
	}
	// After a failure the admin's test file can still be in place: the
	// files are put back after the switch anyway.
	if _, err := e.setSwapMode(ctx, apischema.SwapModeRequest{Mode: mode, ConfirmReplace: zramConfigPath(provider)}); err != nil {
		t.Errorf("restore %s: %v", mode, err)
	}
	for _, f := range s.files {
		if err := f.restore(e.sys); err != nil {
			t.Errorf("restore: %v", err)
		}
	}
	if err := e.sys.reload(ctx); err != nil {
		t.Errorf("reload: %v", err)
	}
	s.restoreUnitsAndKernel(t, e, provider)
}

// restoreUnitsAndKernel puts back zram-tools' start at boot, zram beside the
// swap file for a host that started mixed, and the live sysctls and zswap.
func (s hostSnapshot) restoreUnitsAndKernel(t *testing.T, e *executor, provider apischema.ZramProvider) {
	ctx := context.WithoutCancel(t.Context())
	switch s.toolsState {
	case "":
	case "enabled":
		if err := e.sys.enableUnit(ctx, zramService); err != nil {
			t.Errorf("enable %s: %v", zramService, err)
		}
	default:
		if err := e.sys.disableUnit(ctx, zramService); err != nil {
			t.Errorf("disable %s: %v", zramService, err)
		}
	}
	if s.status.Mode == apischema.SwapModeMixed {
		unit := zramService
		if provider == apischema.ZramProviderGenerator {
			unit = zramGeneratorSwapUnit
		}
		if err := resetLeftZram(ctx, e, provider); err != nil {
			t.Errorf("start zram beside the swap file again: %v", err)
		} else if err := e.startUntilBack(ctx, unit, e.zramIs(true)); err != nil {
			t.Errorf("start zram beside the swap file again: %v", err)
		}
	}
	params := map[string]int{sysctlSwappiness: s.status.Swappiness, sysctlPageCluster: s.status.PageCluster,
		sysctlWatermarkBoostFactor: s.status.WatermarkBoostFactor, sysctlWatermarkScaleFactor: s.status.WatermarkScaleFactor}
	for name, value := range params {
		if err := e.sys.writeParam(vmSysctlDir+"/"+name, strconv.Itoa(value)); err != nil {
			t.Errorf("restore vm.%s: %v", name, err)
		}
	}
	if !s.status.Zswap.Available {
		return
	}
	if err := e.sys.writeParam(zswapCompressorParam, s.status.Zswap.Compressor); err != nil {
		t.Errorf("restore the zswap compressor: %v", err)
	}
	if err := e.sys.writeParam(zswapEnabledParam, zswapSwitch(s.status.Zswap.Enabled)); err != nil {
		t.Errorf("restore zswap: %v", err)
	}
}

// resetLeftZram resets a zram0 that a stop left set up, as a change does
// before it starts zram (resetStaleZram): the tests start zram themselves.
func resetLeftZram(ctx context.Context, e *executor, provider apischema.ZramProvider) error {
	run := &modeRun{executor: e, before: apischema.SwapStatus{ZramProvider: provider}}
	return run.resetStaleZram(ctx)
}

// zramIs returns a check that a zram device swaps, or does not.
func (e *executor) zramIs(on bool) func() (bool, error) {
	return func() (bool, error) {
		devices, err := e.root.readSwaps()
		return err == nil && hasDevice(devices, apischema.SwapDeviceZram) == on, err
	}
}

// assertAsksBeforeReplacingAnAdminsFile writes an admin's configuration for
// the provider, from a mode without zram, and checks that a switch to zram
// is refused without the confirmation, that a folder at the copy's name
// stops a confirmed one before the file changes, and that a confirmed
// switch keeps a copy with the file's mode before it replaces the file.
func assertAsksBeforeReplacingAnAdminsFile(t *testing.T, e *executor, provider apischema.ZramProvider, file fstabSwap, back apischema.SwapMode) {
	t.Helper()
	ctx := t.Context()
	path := zramConfigPath(provider)
	admins := "# The admin's own\nSIZE=256\nPRIORITY=10\n"
	if provider == apischema.ZramProviderGenerator {
		admins = "# The admin's own\n[zram0]\nzram-size = 256\n"
	}
	copyPath := path + zramConfigCopySuffix
	writeAdminsConfig(t, path, copyPath, admins)

	_, err := e.setSwapMode(ctx, apischema.SwapModeRequest{Mode: apischema.SwapModeZram})
	if !errors.Is(err, bridgeipc.ErrConflict) || !strings.Contains(err.Error(), path) {
		t.Fatalf("switch to zram over an admin's %s: %v, want a conflict naming it", path, err)
	}
	assertCopyInTheWayStopsTheSwitch(t, e, path, copyPath, admins)

	got, err := e.setSwapMode(ctx, apischema.SwapModeRequest{Mode: apischema.SwapModeZram, ConfirmReplace: path})
	if err != nil || got.Mode != apischema.SwapModeZram {
		t.Fatalf("confirmed switch to zram: mode %s, %v", got.Mode, err)
	}
	saved, err := host.saveFile(copyPath)
	if err != nil || !saved.existed || string(saved.data) != admins || saved.mode != 0o640 {
		t.Fatalf("copy %s = %q mode %v, %v; want the admin's file with mode 0640", copyPath, saved.data, saved.mode, err)
	}
	if config, readErr := os.ReadFile(path); readErr != nil || !strings.HasPrefix(string(config), managedHeader) {
		t.Fatalf("%s = %q, %v; want LinuxIO's", path, config, readErr)
	}
	assertZramMode(t, e, file)
	assertDiskMode(t, file, switchTo(t, e, back))
}

// writeAdminsConfig puts an admin's file, mode 0640, at path, with no copy
// beside it.
func writeAdminsConfig(t *testing.T, path, copyPath, admins string) {
	t.Helper()
	for _, name := range []string{path, copyPath} {
		if err := os.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(path, []byte(admins), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
}

// assertCopyInTheWayStopsTheSwitch puts a folder at the copy's name and
// checks that a confirmed switch to zram fails, naming it, with the admin's
// file as it was.
func assertCopyInTheWayStopsTheSwitch(t *testing.T, e *executor, path, copyPath, admins string) {
	t.Helper()
	if err := os.Mkdir(copyPath, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := e.setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram, ConfirmReplace: path})
	if err == nil || !strings.Contains(err.Error(), copyPath) {
		t.Fatalf("switch to zram with a folder at %s: %v, want it named", copyPath, err)
	}
	if got, readErr := os.ReadFile(path); readErr != nil || string(got) != admins {
		t.Fatalf("%s = %q, %v; want the admin's file left as it was", path, got, readErr)
	}
	if err := os.Remove(copyPath); err != nil {
		t.Fatal(err)
	}
}

// assertListsADropInDeviceAndRefusesToLeaveZram writes a drop-in that
// defines a zram swap device and checks that the status lists it and that a
// switch away from zram is refused, naming it, before anything changes; it
// is removed before any reload makes a unit from it.
func assertListsADropInDeviceAndRefusesToLeaveZram(t *testing.T, e *executor) {
	t.Helper()
	ctx := t.Context()
	if err := os.MkdirAll(zramGeneratorEtcDropIns, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(testDropIn, []byte("[zram1]\nzram-size = 64\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(testDropIn) }()
	status, err := e.sys.status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := apischema.ZramGeneratorDevice{Path: testDropIn, Device: "zram1", ZramSize: "64"}
	if !slices.Contains(status.ZramGeneratorDevices, want) {
		t.Errorf("generator devices = %+v, want %+v listed", status.ZramGeneratorDevices, want)
	}
	_, err = e.setSwapMode(ctx, apischema.SwapModeRequest{Mode: apischema.SwapModeDisk})
	if !errors.Is(err, bridgeipc.ErrConflict) || !strings.Contains(err.Error(), testDropIn) {
		t.Errorf("switch to disk with %s: %v, want a conflict naming it", testDropIn, err)
	}
}

// assertZswapRoundTrip switches to zswap and back to disk, and checks zswap
// live and its tmpfiles entry, which sets it at boot, each way.
func assertZswapRoundTrip(t *testing.T, e *executor) {
	t.Helper()
	status, err := e.sys.status(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !status.Zswap.Available {
		t.Log("the kernel has no zswap: the zswap round trip is not checked")
		return
	}
	got := switchTo(t, e, apischema.SwapModeZswap)
	if !got.Zswap.Enabled {
		t.Errorf("zswap = %+v after switching to it, want enabled", got.Zswap)
	}
	if entry, readErr := os.ReadFile(zswapTmpfilesPath); readErr != nil || string(entry) != string(renderZswapTmpfiles(got.Zswap.Compressor)) {
		t.Errorf("%s = %q, %v; want zswap on at boot with %s", zswapTmpfilesPath, entry, readErr, got.Zswap.Compressor)
	}
	got = switchTo(t, e, apischema.SwapModeDisk)
	if got.Zswap.Enabled {
		t.Errorf("zswap = %+v after switching to disk, want off", got.Zswap)
	}
	if entry, readErr := os.ReadFile(zswapTmpfilesPath); readErr != nil || string(entry) != string(renderZswapOffTmpfiles()) {
		t.Errorf("%s = %q, %v; want zswap off at boot", zswapTmpfilesPath, entry, readErr)
	}
}

// assertStartsZramAStopLeftSetUp has a stop of the provider's unit leave
// zram0 set up, as the test VM saw zram-generator's reset fail with EBUSY
// right after a swapoff: zram0 is held open while the unit stops, so the
// reset meets it busy. A switch to zram must then reset the device and
// start zram at the planned size.
func assertStartsZramAStopLeftSetUp(t *testing.T, e *executor, provider apischema.ZramProvider, file fstabSwap, back apischema.SwapMode) {
	t.Helper()
	ctx := t.Context()
	unit := zramService
	if provider == apischema.ZramProviderGenerator {
		unit = zramGeneratorSwapUnit
	}
	startSmallZram(t, e)
	held, err := os.Open(zramGeneratorDevice)
	if err != nil {
		t.Fatal(err)
	}
	stopErr := e.sys.stopUnit(ctx, unit)
	if stopErr == nil {
		stopErr = waitUntil(ctx, e.waits.swapOn, e.waits.poll, func() (bool, error) {
			off, readErr := e.zramIs(false)()
			if readErr != nil || !off {
				return false, readErr
			}
			state, stateErr := e.sys.activeState(ctx, unit)
			return stateErr == nil && state != "deactivating", stateErr
		})
	}
	if closeErr := held.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if stopErr != nil {
		t.Fatalf("stop %s with zram0 held open: %v", unit, stopErr)
	}
	size, err := host.readOptionalUint("/sys/block/zram0/disksize")
	if err != nil {
		t.Fatal(err)
	}
	setup, _ := e.sys.activeState(ctx, zramGeneratorSetupUnit)
	t.Logf("with zram0 held open, the stop of %s left disksize %d, %s %s", unit, size, zramGeneratorSetupUnit, setup)
	if size == 0 {
		t.Fatalf("zram0 was reset although it was held open: the case this checks did not happen")
	}
	switchTo(t, e, apischema.SwapModeZram)
	assertZramMode(t, e, file)
	assertDiskMode(t, file, switchTo(t, e, back))
}

// startSmallZram makes the provider swap on a 64 MiB zram device. zram is
// not swapping; its device may still be set up from the stop before, and is
// then reset first.
func startSmallZram(t *testing.T, e *executor) {
	t.Helper()
	ctx := t.Context()
	provider := host.zramProvider(ctx, systemd.GetActiveState)
	unit, path, config := zramService, zramswapConfigPath, renderZramswapConfig(64<<20)
	if provider == apischema.ZramProviderGenerator {
		unit, path, config = zramGeneratorSwapUnit, zramGeneratorConfigPath, renderZramGeneratorConfig(64<<20)
	}
	if err := resetLeftZram(ctx, e, provider); err != nil {
		t.Fatal(err)
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
	got, err := e.setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: mode})
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
