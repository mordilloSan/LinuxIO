package btrfs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/mordilloSan/LinuxIO/backend/common/atomicfile"
	"github.com/mordilloSan/LinuxIO/backend/common/systemd"
	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

// Systemd units that run timeline snapshots hourly via linuxio-btrfs-timer,
// and the replication the snapshot pass starts when it ends. The health timer
// and the boot-time degraded mount are packaged in packaging/systemd and only
// enabled here.
const (
	SnapshotServiceName  = "linuxio-btrfs-snapshot.service"
	SnapshotTimerName    = "linuxio-btrfs-snapshot.timer"
	ReplicateServiceName = "linuxio-btrfs-replicate.service"
	HealthTimerName      = "linuxio-btrfs-health.timer"
	DegradedServiceName  = "linuxio-btrfs-degraded.service"
	SnapshotUnitDir      = "/etc/systemd/system"
)

// RenderUnit returns the oneshot service the snapshot timer starts.
func RenderUnit() []byte {
	return []byte("[Unit]\nDescription=LinuxIO btrfs timeline snapshots\n\n[Service]\nType=oneshot\nExecStart=" +
		version.BinDir + "/linuxio-btrfs-timer snapshot run\n")
}

// RenderTimer returns the hourly timer; Persistent catches up missed runs.
func RenderTimer() []byte {
	return []byte("[Unit]\nDescription=Schedule LinuxIO btrfs timeline snapshots\n\n[Timer]\nOnCalendar=hourly\nPersistent=true\nUnit=" +
		SnapshotServiceName + "\n\n[Install]\nWantedBy=timers.target\n")
}

// RenderReplicateUnit returns the oneshot service that runs the replication
// jobs. It has no [Install] section: only the snapshot pass starts it.
func RenderReplicateUnit() []byte {
	return []byte("[Unit]\nDescription=LinuxIO btrfs replication\n\n[Service]\nType=oneshot\nExecStart=" +
		version.BinDir + "/linuxio-btrfs-timer replicate run\n")
}

// UnitFile is a unit the snapshot pass starts: its name, the file it is
// written to and what the file holds.
type UnitFile struct {
	Name    string
	Path    string
	Content []byte
}

// ReplicateUnit is the replication unit as this build renders it.
func ReplicateUnit() UnitFile {
	return UnitFile{
		Name:    ReplicateServiceName,
		Path:    filepath.Join(SnapshotUnitDir, ReplicateServiceName),
		Content: RenderReplicateUnit(),
	}
}

// The manager calls of UnitFile.Start, swapped by tests.
var (
	unitDaemonReload = systemd.DaemonReload
	unitStart        = systemd.StartUnit
)

// Start writes the unit file when it is missing or differs from Content,
// reloads systemd after such a write, and asks systemd to start the unit
// without waiting for its run: the manager's StartUnit call returns once the
// start job is queued. A start while the unit still runs merges into its
// running start job, so runs never pile up behind a long one.
//
// Writing the file here is what keeps an upgraded install replicating: the
// release that split replication out of the snapshot unit runs under a
// snapshot unit written before it, with no replication unit on disk.
func (u UnitFile) Start(ctx context.Context) error {
	have, err := os.ReadFile(u.Path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("read %s: %w", u.Path, err)
	}
	if err != nil || !bytes.Equal(have, u.Content) {
		if err := atomicfile.Write(u.Path, u.Content, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", u.Path, err)
		}
		if err := unitDaemonReload(ctx); err != nil {
			return fmt.Errorf("reload systemd: %w", err)
		}
	}
	return unitStart(ctx, u.Name)
}
