package btrfs

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/scheduledrun"
	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

func TestRenderTimerHourlyPersistent(t *testing.T) {
	const timer = "[Unit]\nDescription=Schedule LinuxIO btrfs timeline snapshots\n\n[Timer]\nOnCalendar=hourly\nPersistent=true\nUnit=linuxio-btrfs-snapshot.service\n\n[Install]\nWantedBy=timers.target\n"
	if got := string(RenderTimer()); got != timer {
		t.Errorf("RenderTimer =\n%q\nwant\n%q", got, timer)
	}
	const unit = "[Unit]\nDescription=LinuxIO btrfs timeline snapshots\n\n[Service]\nType=oneshot\nExecStart=/usr/local/bin/linuxio-btrfs-timer snapshot run\n"
	if got := string(RenderUnit()); got != unit {
		t.Errorf("RenderUnit =\n%q\nwant\n%q", got, unit)
	}
}

// The replication unit is a oneshot with no [Install] section: nothing but
// the snapshot pass starts it, and its hooks come from the packaged drop-in.
func TestRenderReplicateUnit(t *testing.T) {
	const unit = "[Unit]\nDescription=LinuxIO btrfs replication\n\n[Service]\nType=oneshot\nExecStart=/usr/local/bin/linuxio-btrfs-timer replicate run\n"
	if got := string(RenderReplicateUnit()); got != unit {
		t.Errorf("RenderReplicateUnit =\n%q\nwant\n%q", got, unit)
	}
	u := ReplicateUnit()
	if u.Name != ReplicateServiceName || u.Path != "/etc/systemd/system/linuxio-btrfs-replicate.service" || string(u.Content) != unit {
		t.Errorf("ReplicateUnit = %+v", u)
	}
}

// systemd's own parser checks the rendered services. The binary is swapped
// for one that exists, since verify fails on a missing ExecStart program.
func TestRenderedServicesVerify(t *testing.T) {
	if _, err := exec.LookPath("systemd-analyze"); err != nil {
		t.Skip("systemd-analyze unavailable")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, content := range map[string][]byte{SnapshotServiceName: RenderUnit(), ReplicateServiceName: RenderReplicateUnit()} {
		unit := strings.Replace(string(content), version.BinDir+"/linuxio-btrfs-timer", exe, 1)
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(unit), 0o600); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		out, err := exec.CommandContext(ctx, "systemd-analyze", "verify", path).CombinedOutput()
		cancel()
		if err != nil {
			t.Errorf("systemd-analyze verify %s: %v\n%s", name, err, out)
		}
	}
}

func TestScheduledRunKnowsTheBtrfsUnits(t *testing.T) {
	const pool = "0a1b2c3d-1111-2222-3333-444455556666"
	if kind, ok := scheduledrun.KindOf(SnapshotServiceName, ""); !ok || kind.Route != scheduledrun.RouteSnapshot {
		t.Errorf("KindOf(%q) = %+v, %v", SnapshotServiceName, kind, ok)
	}
	if kind, ok := scheduledrun.KindOf(ReplicateServiceName, ""); !ok || kind.Route != scheduledrun.RouteReplicate {
		t.Errorf("KindOf(%q) = %+v, %v", ReplicateServiceName, kind, ok)
	}
	kind, ok := scheduledrun.KindOf(ResyncUnitName(pool), "")
	if !ok || kind.Route != scheduledrun.RouteResync || kind.Instance != pool {
		t.Errorf("KindOf(%q) = %+v, %v", ResyncUnitName(pool), kind, ok)
	}
}

// stubUnitManager records the manager calls of UnitFile.Start; startErr is
// what the start returns.
func stubUnitManager(t *testing.T, startErr error) *[]string {
	t.Helper()
	oldReload, oldStart := unitDaemonReload, unitStart
	var calls []string
	unitDaemonReload = func(context.Context) error {
		calls = append(calls, "reload")
		return nil
	}
	unitStart = func(_ context.Context, name string) error {
		calls = append(calls, "start:"+name)
		return startErr
	}
	t.Cleanup(func() { unitDaemonReload, unitStart = oldReload, oldStart })
	return &calls
}

func testReplicateUnit(t *testing.T) UnitFile {
	t.Helper()
	u := ReplicateUnit()
	u.Path = filepath.Join(t.TempDir(), u.Name)
	return u
}

// An install upgraded from the release that replicated inside the snapshot
// unit has no replication unit: the snapshot pass writes it, reloads systemd
// and starts it.
func TestUnitStartWritesAMissingUnit(t *testing.T) {
	calls := stubUnitManager(t, nil)
	u := testReplicateUnit(t)
	if err := u.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got, err := os.ReadFile(u.Path); err != nil || string(got) != string(RenderReplicateUnit()) {
		t.Fatalf("unit file = %q, %v", got, err)
	}
	if want := []string{"reload", "start:" + ReplicateServiceName}; !reflect.DeepEqual(*calls, want) {
		t.Fatalf("calls = %v, want %v", *calls, want)
	}
	info, err := os.Stat(u.Path)
	if err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("unit file mode = %v, %v; want 0644", info, err)
	}
}

// A unit as this build renders it is started without a reload; one an older
// build wrote is rewritten first.
func TestUnitStartReloadsOnlyAfterAWrite(t *testing.T) {
	calls := stubUnitManager(t, nil)
	u := testReplicateUnit(t)
	if err := os.WriteFile(u.Path, u.Content, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := u.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if want := []string{"start:" + ReplicateServiceName}; !reflect.DeepEqual(*calls, want) {
		t.Fatalf("current unit: calls = %v, want %v", *calls, want)
	}

	*calls = nil
	if err := os.WriteFile(u.Path, []byte("[Service]\nExecStart=/bin/old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := u.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got, _ := os.ReadFile(u.Path); string(got) != string(u.Content) {
		t.Fatalf("stale unit not rewritten: %q", got)
	}
	if want := []string{"reload", "start:" + ReplicateServiceName}; !reflect.DeepEqual(*calls, want) {
		t.Fatalf("stale unit: calls = %v, want %v", *calls, want)
	}
}

func TestUnitStartReturnsTheStartError(t *testing.T) {
	boom := errors.New("boom")
	stubUnitManager(t, boom)
	if err := testReplicateUnit(t).Start(t.Context()); !errors.Is(err, boom) {
		t.Fatalf("Start = %v, want %v", err, boom)
	}
}
