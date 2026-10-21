package btrfs

import "github.com/mordilloSan/LinuxIO/backend/common/version"

// Systemd units that run timeline snapshots hourly and the health check
// every ten minutes via the linuxio CLI.
const (
	SnapshotServiceName = "linuxio-btrfs-snapshot.service"
	SnapshotTimerName   = "linuxio-btrfs-snapshot.timer"
	HealthServiceName   = "linuxio-btrfs-health.service"
	HealthTimerName     = "linuxio-btrfs-health.timer"
	SnapshotUnitDir     = "/etc/systemd/system"
)

// RenderUnit returns the oneshot service the snapshot timer starts.
func RenderUnit() []byte {
	return []byte("[Unit]\nDescription=LinuxIO btrfs timeline snapshots\n\n[Service]\nType=oneshot\nExecStart=" +
		version.BinDir + "/linuxio btrfs-snapshot run\n")
}

// RenderTimer returns the hourly timer; Persistent catches up missed runs.
func RenderTimer() []byte {
	return []byte("[Unit]\nDescription=Schedule LinuxIO btrfs timeline snapshots\n\n[Timer]\nOnCalendar=hourly\nPersistent=true\nUnit=" +
		SnapshotServiceName + "\n\n[Install]\nWantedBy=timers.target\n")
}

// RenderHealthUnit returns the oneshot service the health timer starts.
func RenderHealthUnit() []byte {
	return []byte("[Unit]\nDescription=LinuxIO btrfs health check\n\n[Service]\nType=oneshot\nExecStart=" +
		version.BinDir + "/linuxio btrfs-health run\n")
}

// RenderHealthTimer returns the timer: shortly after boot, then every ten
// minutes, so a failed disk is noticed without anyone logged in.
func RenderHealthTimer() []byte {
	return []byte("[Unit]\nDescription=Schedule LinuxIO btrfs health checks\n\n[Timer]\nOnBootSec=2min\nOnUnitActiveSec=10min\nUnit=" +
		HealthServiceName + "\n\n[Install]\nWantedBy=timers.target\n")
}
