package btrfs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/common/scheduledrun"
	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

func TestRenderTimerHourlyPersistent(t *testing.T) {
	const timer = "[Unit]\nDescription=Schedule LinuxIO btrfs timeline snapshots\n\n[Timer]\nOnCalendar=hourly\nPersistent=true\nUnit=linuxio-btrfs-snapshot.service\n\n[Install]\nWantedBy=timers.target\n"
	if got := string(RenderTimer()); got != timer {
		t.Errorf("RenderTimer =\n%q\nwant\n%q", got, timer)
	}
}

// The two services are package units (packaging/systemd). Each runs its
// worker word as a oneshot; the replication unit has no [Install] section,
// since only the snapshot pass starts it, and the run hooks come from the
// packaged drop-in, never from the unit.
func TestPackagedSnapshotUnits(t *testing.T) {
	for name, word := range map[string]string{SnapshotServiceName: "snapshot", ReplicateServiceName: "replicate"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "..", "packaging", "systemd", name))
		if err != nil {
			t.Fatal(err)
		}
		unit := string(data)
		for _, want := range []string{"\nType=oneshot\n", "\nExecStart=" + version.LibexecDir + "/linuxio-worker " + word + "\n"} {
			if !strings.Contains(unit, want) {
				t.Errorf("%s is missing %q:\n%s", name, want, unit)
			}
		}
		if strings.Contains(unit, "scheduled-run") {
			t.Errorf("%s carries a run hook:\n%s", name, unit)
		}
		if name == ReplicateServiceName && strings.Contains(unit, "\n[Install]\n") {
			t.Errorf("%s has an [Install] section:\n%s", name, unit)
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
	kind, ok = scheduledrun.KindOf(RebuildUnitName(pool), "")
	if !ok || kind.Route != scheduledrun.RouteRebuild || kind.Instance != pool {
		t.Errorf("KindOf(%q) = %+v, %v", RebuildUnitName(pool), kind, ok)
	}
}
