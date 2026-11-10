package btrfs

import (
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/common/scheduledrun"
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

func TestScheduledRunKnowsTheBtrfsUnits(t *testing.T) {
	const pool = "0a1b2c3d-1111-2222-3333-444455556666"
	if kind, ok := scheduledrun.KindOf(SnapshotServiceName, ""); !ok || kind.Route != scheduledrun.RouteSnapshot {
		t.Errorf("KindOf(%q) = %+v, %v", SnapshotServiceName, kind, ok)
	}
	kind, ok := scheduledrun.KindOf(ResyncUnitName(pool), "")
	if !ok || kind.Route != scheduledrun.RouteResync || kind.Instance != pool {
		t.Errorf("KindOf(%q) = %+v, %v", ResyncUnitName(pool), kind, ok)
	}
}
