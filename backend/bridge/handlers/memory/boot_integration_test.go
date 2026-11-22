package memory

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/systemd"
)

// vendorGeneratorConfig is the main zram-generator file a package installs,
// as Fedora's zram-generator-defaults does. The generator reads it while no
// main file in /run, /etc or /usr/local/lib takes its place.
const vendorGeneratorConfig = "/usr/lib/systemd/zram-generator.conf"

// TestMemoryDefaultZramIntegration starts from the zram a distribution turns
// on with zram-generator, as Fedora does: the vendor file and zram0 swapping
// at the size it sets from boot. The status must read zram, or mixed beside
// an active swap file, with zram-generator as the provider and no other file
// of its listed. Without disk swap a switch to zram writes LinuxIO's
// configuration for the next boot and keeps zram at its size, since a
// restart would leave the host without swap meanwhile, and a switch to
// zswap makes LinuxIO's swap file at the size asked before it stops zram.
// Beside a swap file, a switch to zram removes the file and restarts zram
// at the planned size, and a switch to disk makes LinuxIO's file and stops
// zram. Cleanup puts the default back. The rig runs it on such a host twice,
// without and then with a swap file: `make vm-test-memory-integration
// DISTRO=fedora`.
func TestMemoryDefaultZramIntegration(t *testing.T) {
	needRoot(t)
	if main := existingGeneratorMainFiles(); !slices.Equal(main, []string{vendorGeneratorConfig}) {
		t.Skipf("zram-generator main files %v: the default is the vendor's %s alone", main, vendorGeneratorConfig)
	}
	asJobHost(t)
	ctx := t.Context()
	e := itExecutor(t)
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
	snapshot := takeHostSnapshot(t, status)
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
	if len(zramRemovals(status.SwapFiles)) == 0 {
		t.Fatalf("disk swap %+v swaps, but no swap file LinuxIO can remove does", status.Devices)
	}
	switchTo(t, apischema.SwapModeZram)
	assertZramMode(t, e)
	got := switchTo(t, apischema.SwapModeDisk)
	assertDiskMode(t, got)
	assertSwapsAt(t, ownSwapFilePath, got.Create.DefaultSize)
}

// assertDefaultZramAlone checks a host whose only swap is the default zram:
// nothing to recommend but zram, a switch to zram that writes LinuxIO's
// configuration and keeps zram at its size, and a switch to zswap that makes
// LinuxIO's swap file at the size asked and only then stops zram.
func assertDefaultZramAlone(t *testing.T, e *executor, status apischema.SwapStatus) {
	t.Helper()
	ctx := t.Context()
	if len(status.SwapFiles) != 0 || status.RecommendedMode != apischema.SwapModeZram {
		t.Errorf("swap files %+v, recommended %s; want none and zram", status.SwapFiles, status.RecommendedMode)
	}
	got, err := SetSwapMode(ctx, apischema.SwapModeRequest{Mode: apischema.SwapModeZram}, logProgress(t))
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
	if !status.Zswap.Available {
		t.Log("the kernel has no zswap: the switch from the default zram to zswap is not checked")
		return
	}
	log := &stepLog{t: t}
	zswap, err := SetSwapMode(ctx, apischema.SwapModeRequest{Mode: apischema.SwapModeZswap, SwapFileSize: 512 * mib}, log.report)
	if err != nil || zswap.Mode != apischema.SwapModeZswap || len(zswap.Left) > 0 {
		t.Fatalf("switch to zswap: mode %s, left %+v, %v", zswap.Mode, zswap.Left, err)
	}
	if create, stop := slices.Index(log.steps, apischema.SwapStepCreateSwapFile), slices.Index(log.steps, apischema.SwapStepStopZram); create < 0 || stop < create {
		t.Errorf("steps %v, want create_swap_file before stop_zram", log.steps)
	}
	assertSwapsAt(t, ownSwapFilePath, 512*mib)
	assertDiskMode(t, zswap.SwapStatus)
}

// The reboot tests run in phases around reboots the rig makes, named by
// LINUXIO_MEMORY_REBOOT_PHASE; each phase checks what the boot before it
// brought and changes what the next boot must bring.
func rebootPhase(t *testing.T, phases ...string) string {
	t.Helper()
	needRoot(t)
	phase := os.Getenv("LINUXIO_MEMORY_REBOOT_PHASE")
	if !slices.Contains(phases, phase) {
		t.Skipf("set LINUXIO_MEMORY_REBOOT_PHASE to one of %v, around the reboots the rig makes", phases)
	}
	asJobHost(t)
	t.Logf("phase %s", phase)
	return phase
}

// The sizes the reboot tests use.
const (
	rebootITOwnSize   = 512 * mib
	rebootITGrown     = 1536 * mib
	rebootITZswapSize = 768 * mib
)

// TestSwapFilesRebootIntegration checks across two reboots what LinuxIO's
// swap file operations leave for systemd to bring at boot. Phase create
// makes LinuxIO's swap file and grows the rig's /swap.img; after the reboot
// (phase resized) LinuxIO's file swaps through its nofail line, which
// swap.target wants and does not require, swap.target is active, and
// /swap.img swaps at its new size; on btrfs LinuxIO's folder is still a
// subvolume labelled like /var/lib/linuxio, the file NOCOW and swapfile_t
// where SELinux runs, and a snapshot of the subvolume holding
// /var/lib/linuxio succeeds while it swaps. The phase then removes
// /swap.img, and after the next reboot (phase removed) it is still gone:
// not in /proc/swaps, /etc/fstab or on disk, and its unit not active. The
// phase removes LinuxIO's file last; the rig makes /swap.img again.
func TestSwapFilesRebootIntegration(t *testing.T) {
	switch rebootPhase(t, "create", "resized", "removed") {
	case "create":
		onlyDiskSwap(t, rigSwapFile)
		makeStateDir(t)
		fstab := readFile(t, etcFstab)
		if _, err := CreateSwapFile(t.Context(), apischema.SwapFileCreateRequest{Size: rebootITOwnSize}, logProgress(t)); err != nil {
			t.Fatal(err)
		}
		if want := fstab + ownSwapFilePath + " none swap sw,nofail 0 0\n"; readFile(t, etcFstab) != want {
			t.Errorf("/etc/fstab = %q, want %q", readFile(t, etcFstab), want)
		}
		assertOwnSwapFile(t, rebootITOwnSize)
		fstab, id := readFile(t, etcFstab), fileIdentity(t, rigSwapFile)
		steps := resizeIT(t, rigSwapFile, rebootITGrown)
		if slices.Contains(steps, apischema.SwapStepMakeTemporarySwap) {
			t.Errorf("steps %v, want no temporary file beside LinuxIO's file", steps)
		}
		assertResizedInPlace(t, rigSwapFile, rebootITGrown, fstab, id)
	case "resized":
		assertSwapTargetActive(t)
		assertOwnSwapFile(t, rebootITOwnSize)
		wanted := "/run/systemd/generator/swap.target.wants/" + swapUnit(t, ownSwapFilePath)
		if _, err := os.Lstat(wanted); err != nil {
			t.Errorf("%s: %v; want swap.target to want LinuxIO's file, as nofail makes it", wanted, err)
		}
		assertSwapsAt(t, rigSwapFile, rebootITGrown)
		if _, err := RemoveSwapFile(t.Context(), apischema.SwapFilePathRequest{Path: rigSwapFile}, logProgress(t)); err != nil {
			t.Fatal(err)
		}
		assertSwapFileGone(t, rigSwapFile)
	case "removed":
		assertSwapTargetActive(t)
		assertSwapFileGone(t, rigSwapFile)
		assertSwapsAt(t, ownSwapFilePath, rebootITOwnSize)
		if _, err := RemoveSwapFile(t.Context(), apischema.SwapFilePathRequest{Path: ownSwapFilePath}, logProgress(t)); err != nil {
			t.Fatal(err)
		}
		assertSwapFileGone(t, ownSwapFilePath)
		removeOwnSwapDir(t, true)
	}
}

// assertOwnSwapFile checks LinuxIO's swap file: swapping at size through its
// unit, a regular file of root's, 0600, in a folder 0755; on btrfs the
// folder a subvolume labelled like /var/lib/linuxio and the file NOCOW,
// labelled swapfile_t where SELinux runs, and the subvolume holding
// /var/lib/linuxio can be snapshotted while it swaps.
func assertOwnSwapFile(t *testing.T, size uint64) {
	t.Helper()
	assertSwapsAt(t, ownSwapFilePath, size)
	if info, err := os.Lstat(ownSwapFilePath); err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Errorf("%s: %v, %v; want a regular file 0600", ownSwapFilePath, info, err)
	}
	if dir, err := os.Stat(ownSwapDir); err != nil || dir.Mode().Perm() != 0o755 {
		t.Errorf("%s: %v, %v; want a folder 0755", ownSwapDir, dir, err)
	}
	folder, err := readFolderFS(ownSwapDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s on %+v", ownSwapDir, folder)
	if folder.kind != "btrfs" {
		return
	}
	if !folder.subvolumeRoot {
		t.Errorf("%s is not a subvolume", ownSwapDir)
	}
	checkLabelledLike(t, ownSwapDir, filepath.Dir(ownSwapDir))
	checkNoCowOn(t, ownSwapFilePath)
	if label := selinuxLabel(t, ownSwapFilePath); label != "" && !strings.Contains(label, ":swapfile_t:") {
		t.Errorf("%s is labelled %s, want swapfile_t", ownSwapFilePath, label)
	}
	subvolume := subvolumeHolding(t, filepath.Dir(ownSwapDir))
	if out, err := snapshotIT(t, subvolume); err != nil {
		t.Errorf("snapshot of %s, which holds %s, while %s swaps: %v, %s", subvolume, filepath.Dir(ownSwapDir), ownSwapFilePath, err, out)
	}
}

func assertSwapTargetActive(t *testing.T) {
	t.Helper()
	if state, err := systemd.GetActiveState(t.Context(), "swap.target"); err != nil || state != "active" {
		t.Errorf("swap.target = %q, %v; want active", state, err)
	}
}

// TestSwapModesRebootIntegration checks the modes across two reboots. Phase
// zram switches from disk to zram, which removes the swap files; after the
// reboot (phase zswap) zram swaps at the planned size, no swap file is
// left, swap.target is active, and a switch to zswap makes LinuxIO's swap
// file at the size asked and stops zram; after the next reboot (phase
// disk) LinuxIO's file swaps at that size, zram stays off and zswap on,
// and the phase switches to disk and removes LinuxIO's file. The rig makes
// /swap.img again.
func TestSwapModesRebootIntegration(t *testing.T) {
	phase := rebootPhase(t, "zram", "zswap", "disk")
	e := itExecutor(t)
	status := freshStatus(t)
	t.Logf("mode %s, devices %+v", status.Mode, status.Devices)
	switch phase {
	case "zram":
		if status.ZramProvider == "" {
			t.Skip("neither zram-tools nor zram-generator is installed")
		}
		if len(zramRemovals(status.SwapFiles)) == 0 || (status.Mode != apischema.SwapModeDisk && status.Mode != apischema.SwapModeZswap) {
			t.Skipf("mode %s with swap files %+v: start from disk or zswap with a swap file the rig adds", status.Mode, status.SwapFiles)
		}
		makeStateDir(t)
		switchTo(t, apischema.SwapModeZram)
		assertZramMode(t, e)
	case "zswap":
		swapModesAfterZram(t, e, status)
	case "disk":
		swapModesAfterZswap(t, status)
	}
}

// swapModesAfterZram checks the boot after the switch to zram, then
// switches to zswap, or disk on a kernel without zswap, with LinuxIO's file.
func swapModesAfterZram(t *testing.T, e *executor, status apischema.SwapStatus) {
	t.Helper()
	assertSwapTargetActive(t)
	assertZramMode(t, e)
	if len(status.SwapFiles) != 0 {
		t.Errorf("swap files %+v after the boot, want none", status.SwapFiles)
	}
	mode := apischema.SwapModeZswap
	if !status.Zswap.Available {
		mode = apischema.SwapModeDisk
	}
	got, err := SetSwapMode(t.Context(), apischema.SwapModeRequest{Mode: mode, SwapFileSize: rebootITZswapSize}, logProgress(t))
	if err != nil || got.Mode != mode || len(got.Left) > 0 {
		t.Fatalf("switch to %s: mode %s, left %+v, %v", mode, got.Mode, got.Left, err)
	}
	assertSwapsAt(t, ownSwapFilePath, rebootITZswapSize)
	assertDiskMode(t, got.SwapStatus)
}

// swapModesAfterZswap checks the boot after the switch to zswap, then goes
// to disk and removes LinuxIO's file.
func swapModesAfterZswap(t *testing.T, status apischema.SwapStatus) {
	t.Helper()
	assertSwapTargetActive(t)
	assertSwapsAt(t, ownSwapFilePath, rebootITZswapSize)
	if hasDevice(status.Devices, apischema.SwapDeviceZram) {
		t.Errorf("devices %+v after the boot, want no zram", status.Devices)
	}
	if status.Zswap.Available && (status.Mode != apischema.SwapModeZswap || !status.Zswap.Enabled) {
		t.Errorf("mode %s, zswap %+v after the boot; want zswap on", status.Mode, status.Zswap)
	}
	switchTo(t, apischema.SwapModeDisk)
	if _, err := RemoveSwapFile(t.Context(), apischema.SwapFilePathRequest{Path: ownSwapFilePath}, logProgress(t)); err != nil {
		t.Fatal(err)
	}
	assertSwapFileGone(t, ownSwapFilePath)
	removeOwnSwapDir(t, true)
}
