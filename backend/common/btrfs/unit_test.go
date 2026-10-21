package btrfs

import "testing"

func TestRenderTimerHourlyPersistent(t *testing.T) {
	const timer = "[Unit]\nDescription=Schedule LinuxIO btrfs timeline snapshots\n\n[Timer]\nOnCalendar=hourly\nPersistent=true\nUnit=linuxio-btrfs-snapshot.service\n\n[Install]\nWantedBy=timers.target\n"
	if got := string(RenderTimer()); got != timer {
		t.Errorf("RenderTimer =\n%q\nwant\n%q", got, timer)
	}
	const unit = "[Unit]\nDescription=LinuxIO btrfs timeline snapshots\n\n[Service]\nType=oneshot\nExecStart=/usr/local/bin/linuxio btrfs-snapshot run\n"
	if got := string(RenderUnit()); got != unit {
		t.Errorf("RenderUnit =\n%q\nwant\n%q", got, unit)
	}

	const healthTimer = "[Unit]\nDescription=Schedule LinuxIO btrfs health checks\n\n[Timer]\nOnBootSec=2min\nOnUnitActiveSec=10min\nUnit=linuxio-btrfs-health.service\n\n[Install]\nWantedBy=timers.target\n"
	if got := string(RenderHealthTimer()); got != healthTimer {
		t.Errorf("RenderHealthTimer =\n%q\nwant\n%q", got, healthTimer)
	}
	const healthUnit = "[Unit]\nDescription=LinuxIO btrfs health check\n\n[Service]\nType=oneshot\nExecStart=/usr/local/bin/linuxio btrfs-health run\n"
	if got := string(RenderHealthUnit()); got != healthUnit {
		t.Errorf("RenderHealthUnit =\n%q\nwant\n%q", got, healthUnit)
	}
}
