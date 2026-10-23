package btrfs

import "testing"

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
