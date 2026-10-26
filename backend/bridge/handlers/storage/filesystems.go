package storage

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/shirou/gopsutil/v4/disk"
	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

var protectedMounts = []string{"/", "/boot", "/boot/efi", "/proc", "/sys", "/dev", "/run"}

func getPartitionByMountpoint(mountpoint string) (*disk.PartitionStat, error) {
	partitions, err := disk.Partitions(true)
	if err != nil {
		return nil, err
	}

	for _, partition := range partitions {
		if partition.Mountpoint == mountpoint {
			p := partition
			return &p, nil
		}
	}

	return nil, fmt.Errorf("mountpoint not found: %s", mountpoint)
}

func isProtectedMount(mountpoint string) bool {
	for _, protected := range protectedMounts {
		if mountpoint == protected || strings.HasPrefix(mountpoint, protected+"/") {
			return true
		}
	}

	return false
}

// mountedPartition and unmountPath are seams for tests.
var (
	mountedPartition = getPartitionByMountpoint
	unmountPath      = unix.Unmount
)

// UnmountFilesystem unmounts a mounted filesystem, refusing protected system
// mounts; removeFstab also drops its /etc/fstab line so it does not come back
// at boot.
func UnmountFilesystem(ctx context.Context, mountpoint string, removeFstab bool) (apischema.StorageMountResult, error) {
	slog.Debug("unmounting filesystem", "mountpoint", mountpoint)
	result, err := unmountFilesystem(ctx, mountpoint, removeFstab)
	if err != nil {
		return apischema.StorageMountResult{}, err
	}
	slog.Info("filesystem unmounted", "mountpoint", mountpoint, "removeFstab", removeFstab)
	return result, nil
}

func unmountFilesystem(ctx context.Context, mountpoint string, removeFstab bool) (apischema.StorageMountResult, error) {
	if !validPath.MatchString(mountpoint) {
		return apischema.StorageMountResult{}, fmt.Errorf("invalid mountpoint %q", mountpoint)
	}

	if isProtectedMount(mountpoint) {
		return apischema.StorageMountResult{}, fmt.Errorf("cannot unmount protected system mount: %s", mountpoint)
	}

	partition, err := mountedPartition(mountpoint)
	if err != nil {
		return apischema.StorageMountResult{}, err
	}

	if partition.Fstype == "nfs" || partition.Fstype == "nfs4" {
		unmounted, unmountErr := UnmountNFS(ctx, mountpoint, removeFstab)
		if unmountErr != nil {
			return apischema.StorageMountResult{}, unmountErr
		}
		// UnmountNFS never reported a mountpoint; keep that shape.
		return apischema.StorageMountResult{Success: unmounted.Success, Warning: unmounted.Warning}, nil
	}

	if err := unmountPath(mountpoint, 0); err != nil {
		return apischema.StorageMountResult{}, fmt.Errorf("umount %s: %w", mountpoint, err)
	}

	var warning string
	if removeFstab {
		if err := removeFromFstab(mountpoint); err != nil {
			slog.Warn("unmount succeeded but fstab update failed", "mountpoint", mountpoint, "error", err)
			warning = fmt.Sprintf("unmount succeeded but fstab update failed: %v", err)
		}
	}
	return mountResult(mountpoint, warning), nil
}
