package btrfs

// Systemd units of timeline snapshots and the replication the snapshot pass
// starts when it ends. The two services, the health timer and the boot-time
// degraded mount are package units (packaging/systemd); the bridge writes
// only the snapshot timer, which holds a schedule and no command line, and
// enables it while a rule or replication job is enabled.
const (
	SnapshotServiceName  = "linuxio-btrfs-snapshot.service"
	SnapshotTimerName    = "linuxio-btrfs-snapshot.timer"
	ReplicateServiceName = "linuxio-btrfs-replicate.service"
	HealthTimerName      = "linuxio-btrfs-health.timer"
	DegradedServiceName  = "linuxio-btrfs-degraded.service"
	SnapshotUnitDir      = "/etc/systemd/system"
)

// RenderTimer returns the hourly timer; Persistent catches up missed runs.
func RenderTimer() []byte {
	return []byte("[Unit]\nDescription=Schedule LinuxIO btrfs timeline snapshots\n\n[Timer]\nOnCalendar=hourly\nPersistent=true\nUnit=" +
		SnapshotServiceName + "\n\n[Install]\nWantedBy=timers.target\n")
}
