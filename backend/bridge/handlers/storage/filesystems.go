package storage

import (
	"context"
	"fmt"
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

func UnmountFilesystem(ctx context.Context, mountpoint string) (apischema.StorageMountResult, error) {
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
