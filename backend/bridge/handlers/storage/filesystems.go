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

// UnmountFilesystem unmounts a mounted filesystem, refusing protected system
// mounts.
func UnmountFilesystem(ctx context.Context, mountpoint string) (apischema.StorageMountResult, error) {
	slog.Info("unmounting filesystem", "mountpoint", mountpoint)
	result, err := unmountFilesystem(ctx, mountpoint)
	if err != nil {
		slog.Error("failed to unmount filesystem", "mountpoint", mountpoint, "error", err)
		return apischema.StorageMountResult{}, err
	}
	slog.Info("filesystem unmounted", "mountpoint", mountpoint)
	return result, nil
}

func unmountFilesystem(ctx context.Context, mountpoint string) (apischema.StorageMountResult, error) {
	if !validPath.MatchString(mountpoint) {
		return apischema.StorageMountResult{}, fmt.Errorf("invalid mountpoint")
	}

	if isProtectedMount(mountpoint) {
		return apischema.StorageMountResult{}, fmt.Errorf("cannot unmount protected system mount: %s", mountpoint)
	}

	partition, err := getPartitionByMountpoint(mountpoint)
	if err != nil {
		return apischema.StorageMountResult{}, err
	}

	if partition.Fstype == "nfs" || partition.Fstype == "nfs4" {
		unmounted, unmountErr := UnmountNFS(ctx, mountpoint, false)
		if unmountErr != nil {
			return apischema.StorageMountResult{}, unmountErr
		}
		// UnmountNFS never reported a mountpoint; keep that shape.
		return apischema.StorageMountResult{Success: unmounted.Success, Warning: unmounted.Warning}, nil
	}

	if err := unix.Unmount(mountpoint, 0); err != nil {
		return apischema.StorageMountResult{}, fmt.Errorf("umount %s: %w", mountpoint, err)
	}

	return mountResult(mountpoint, ""), nil
}
