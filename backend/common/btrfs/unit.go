package btrfs

import "github.com/mordilloSan/LinuxIO/backend/common/version"

// Systemd units that run timeline snapshots hourly via the linuxio CLI. The
// health timer is packaged in packaging/systemd and only enabled here.
const (
	SnapshotServiceName = "linuxio-btrfs-snapshot.service"
	SnapshotTimerName   = "linuxio-btrfs-snapshot.timer"
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
