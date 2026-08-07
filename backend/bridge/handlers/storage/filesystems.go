package storage

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/shirou/gopsutil/v4/disk"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

var (
	validSubvolumeName = regexp.MustCompile(`^[A-Za-z0-9._@+-]+$`)
	protectedMounts    = []string{"/", "/boot", "/boot/efi", "/proc", "/sys", "/dev", "/run"}
)

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

	cmd := exec.CommandContext(ctx, "umount", mountpoint)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return apischema.StorageMountResult{}, fmt.Errorf("umount failed: %s", strings.TrimSpace(string(out)))
	}

	return mountResult(mountpoint, ""), nil
}

func CreateBtrfsSubvolume(ctx context.Context, mountpoint, name string) (apischema.StoragePathResult, error) {
	if !validPath.MatchString(mountpoint) {
		return apischema.StoragePathResult{}, fmt.Errorf("invalid mountpoint")
	}

	name = strings.TrimSpace(name)
	if !validSubvolumeName.MatchString(name) {
		return apischema.StoragePathResult{}, fmt.Errorf("invalid subvolume name")
	}

	partition, err := getPartitionByMountpoint(mountpoint)
	if err != nil {
		return apischema.StoragePathResult{}, err
	}

	if partition.Fstype != "btrfs" {
		return apischema.StoragePathResult{}, fmt.Errorf("%s is not a btrfs filesystem", mountpoint)
	}

	if slices.Contains(partition.Opts, "ro") {
		return apischema.StoragePathResult{}, fmt.Errorf("%s is mounted read-only", mountpoint)
	}

	targetPath := filepath.Join(mountpoint, name)
	if !strings.HasPrefix(targetPath, filepath.Clean(mountpoint)+string(os.PathSeparator)) &&
		filepath.Clean(mountpoint) != "/" {
		return apischema.StoragePathResult{}, fmt.Errorf("invalid subvolume path")
	}
	if filepath.Clean(mountpoint) == "/" {
		targetPath = filepath.Join("/", name)
	}

	if _, statErr := os.Stat(targetPath); statErr == nil {
		return apischema.StoragePathResult{}, fmt.Errorf("path already exists: %s", targetPath)
	} else if !os.IsNotExist(statErr) {
		return apischema.StoragePathResult{}, fmt.Errorf("failed to inspect target path: %w", statErr)
	}

	cmd := exec.CommandContext(ctx, "btrfs", "subvolume", "create", targetPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return apischema.StoragePathResult{}, fmt.Errorf("btrfs subvolume create failed: %s", strings.TrimSpace(string(out)))
	}

	return apischema.StoragePathResult{Success: true, Mountpoint: &mountpoint, Path: &targetPath}, nil
}
