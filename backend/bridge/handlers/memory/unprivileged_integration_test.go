package memory

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

// TestMemoryStatusWithoutRootIntegration reads the status as a user without
// sudo, as the bridge of such a session does, with no sbin folder in PATH.
// TuneD, which the rig starts, must read as running with the profile
// LINUXIO_MEMORY_TUNED_PROFILE names, and the zram provider
// LINUXIO_MEMORY_ZRAM_PROVIDER names must be found, zram-tools' zramswap
// through the /usr/sbin fallback. With LINUXIO_MEMORY_EXPECT_HELD_OFF=1 the
// host is in zram mode and the swap file must read as held off. The rig runs
// it in the test VM: `make vm-test-memory-integration`.
func TestMemoryStatusWithoutRootIntegration(t *testing.T) {
	if os.Getenv("LINUXIO_RUN_MEMORY_STATUS_WITHOUT_ROOT") != "1" {
		t.Skip("set LINUXIO_RUN_MEMORY_STATUS_WITHOUT_ROOT=1 and run as a user without sudo in the test VM")
	}
	if os.Geteuid() == 0 {
		t.Skip("runs as a user without sudo")
	}
	path := filepath.SplitList(os.Getenv("PATH"))
	if slices.Contains(path, "/usr/sbin") || slices.Contains(path, "/sbin") {
		t.Fatalf("PATH %v has an sbin folder, so the /usr/sbin fallback would not be what finds zramswap", path)
	}
	status, err := GetSwapStatus(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("as uid %d: TuneD %v with %q, zram provider %q, mode %s", os.Geteuid(), status.TunedActive, status.TunedProfile, status.ZramProvider, status.Mode)
	if profile := os.Getenv("LINUXIO_MEMORY_TUNED_PROFILE"); !status.TunedActive || status.TunedProfile != profile {
		t.Errorf("TuneD running %v with %q, want running with %q", status.TunedActive, status.TunedProfile, profile)
	}
	provider := apischema.ZramProvider(os.Getenv("LINUXIO_MEMORY_ZRAM_PROVIDER"))
	if status.ZramProvider != provider || !status.ZramAvailable {
		t.Errorf("zram provider %q, available %v; want %q", status.ZramProvider, status.ZramAvailable, provider)
	}
	if os.Getenv("LINUXIO_MEMORY_EXPECT_HELD_OFF") == "1" {
		assertSeesTheSwapFileHeldOff(t, status)
	}
}

// assertSeesTheSwapFileHeldOff checks, with the status's own reading of
// /etc/fstab and the units, that a user without sudo sees the swap file a
// switch to zram held off as held off: LinuxIO's drop-in sits in a folder
// such a user can look into.
func assertSeesTheSwapFileHeldOff(t *testing.T, status apischema.SwapStatus) {
	t.Helper()
	entries, _, err := host.fstabSwaps()
	if err != nil {
		t.Fatal(err)
	}
	file, ok := fstabSwapFile(host.matchFstabSwaps(entries, status.Devices))
	t.Logf("mode %s, swap file %+v", status.Mode, file)
	if status.Mode != apischema.SwapModeZram || !ok || !file.heldOff {
		t.Errorf("mode %s, swap file %+v (found %v); want zram and the swap file held off", status.Mode, file, ok)
	}
}

// TestMemorySwitchIntegration switches the host to LINUXIO_MEMORY_SWITCH_TO,
// as root with the job host's umask, for the rig's steps around
// TestMemoryStatusWithoutRootIntegration.
func TestMemorySwitchIntegration(t *testing.T) {
	mode := apischema.SwapMode(os.Getenv("LINUXIO_MEMORY_SWITCH_TO"))
	if os.Getenv("LINUXIO_RUN_MEMORY_INTEGRATION") != "1" || mode == "" {
		t.Skip("set LINUXIO_RUN_MEMORY_INTEGRATION=1 and LINUXIO_MEMORY_SWITCH_TO, and run as root in the test VM")
	}
	if os.Geteuid() != 0 {
		t.Skip("needs root")
	}
	asJobHost(t)
	e := newExecutor(func(p apischema.SwapProgress) { t.Logf("step %d/%d %s %s", p.Index, p.Total, p.Step, p.Device) })
	switchTo(t, e, mode)
}
