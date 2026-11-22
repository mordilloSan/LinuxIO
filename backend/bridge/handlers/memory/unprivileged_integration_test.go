package memory

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

// TestMemoryStatusWithoutRootIntegration reads the status as a user without
// sudo, as the bridge of such a session does, with no sbin folder in PATH,
// and compares it with the status root read just before
// (TestMemoryStatusAsRootIntegration, in the file
// LINUXIO_MEMORY_ROOT_STATUS). The swap file rows must be root's: the same
// files, active or not, in fstab or not, locked for the same reason, at the
// same size; a 0600 file whose header the user may not read is not judged,
// so where root reads no_swap_header the user reads off, and the rig puts
// one such file in fstab for that. Create must be offered or refused as
// root sees it, with the same path, sizes and filesystem. TuneD, which the
// rig starts, must read as running with the profile
// LINUXIO_MEMORY_TUNED_PROFILE names, and the zram provider
// LINUXIO_MEMORY_ZRAM_PROVIDER names must be found, zram-tools' zramswap
// through the /usr/sbin fallback. The rig runs it in the test VM:
// `make vm-test-memory-integration`.
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
	data, err := os.ReadFile(os.Getenv("LINUXIO_MEMORY_ROOT_STATUS"))
	if err != nil {
		t.Fatalf("read the status root read (LINUXIO_MEMORY_ROOT_STATUS): %v", err)
	}
	var root apischema.SwapStatus
	if err = json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
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
	assertRowsAsRootSeesThem(t, root.SwapFiles, status.SwapFiles)
	assertCreateAsRootSeesIt(t, root.Create, status.Create)
	if status.Mode != root.Mode || status.RecommendedMode != root.RecommendedMode {
		t.Errorf("mode %s, recommended %s; root reads %s and %s", status.Mode, status.RecommendedMode, root.Mode, root.RecommendedMode)
	}
}

// assertRowsAsRootSeesThem compares the rows a user without sudo reads with
// root's: what can change between the two reads (used, free space, the
// largest size) is left out, and a header the user may not read is off.
func assertRowsAsRootSeesThem(t *testing.T, root, got []apischema.SwapFileRow) {
	t.Helper()
	unread := 0
	for _, want := range root {
		row, ok := findRow(apischema.SwapStatus{SwapFiles: got}, want.Path)
		if !ok {
			t.Errorf("no row for %s, which root lists: %+v", want.Path, want)
			continue
		}
		t.Logf("%s: root %+v, without sudo %+v", want.Path, want, row)
		if want.ResizeBlockedReason == apischema.SwapFileResizeNoHeader {
			unread++
			want.ResizeBlockedReason = apischema.SwapFileResizeOff
		}
		if row.Active != want.Active || row.InFstab != want.InFstab || row.LockedReason != want.LockedReason ||
			row.Temporary != want.Temporary || row.Size != want.Size || row.FSType != want.FSType ||
			row.BlocksSnapshots != want.BlocksSnapshots || row.CanMove != want.CanMove || row.ResizeBlockedReason != want.ResizeBlockedReason {
			t.Errorf("%s = %+v, want it as root reads it: %+v", want.Path, row, want)
		}
	}
	if len(got) != len(root) {
		t.Errorf("%d rows, root reads %d", len(got), len(root))
	}
	if unread == 0 {
		t.Error("root reads no file without a swap header: the rig puts one in /etc/fstab for this test")
	}
}

// assertCreateAsRootSeesIt compares what the user reads of Create with
// root's; the free space may have moved a little between the two reads.
func assertCreateAsRootSeesIt(t *testing.T, root, got apischema.SwapFileCreate) {
	t.Helper()
	t.Logf("create: root %+v, without sudo %+v", root, got)
	const drift = 256 << 20
	if got.BlockedReason != root.BlockedReason || got.Path != root.Path || got.FSType != root.FSType ||
		got.MinSize != root.MinSize || got.Reserve != root.Reserve || got.DefaultSize != root.DefaultSize ||
		max(got.MaxSize, root.MaxSize)-min(got.MaxSize, root.MaxSize) > drift {
		t.Errorf("create = %+v, want it as root reads it: %+v", got, root)
	}
}

// TestMemoryStatusAsRootIntegration writes the status root reads, as JSON,
// to the file LINUXIO_MEMORY_STATUS_OUT names, mode 0644, for
// TestMemoryStatusWithoutRootIntegration to compare with.
func TestMemoryStatusAsRootIntegration(t *testing.T) {
	needRoot(t)
	out := os.Getenv("LINUXIO_MEMORY_STATUS_OUT")
	if out == "" {
		t.Skip("set LINUXIO_MEMORY_STATUS_OUT to the file to write the status to")
	}
	data, err := json.Marshal(freshStatus(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(out, 0o644); err != nil {
		t.Fatal(err)
	}
}
