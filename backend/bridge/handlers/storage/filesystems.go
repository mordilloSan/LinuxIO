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

func UnmountFilesystem(ctx context.Context, mountpoint string) (map[string]any, error) {
	if !validPath.MatchString(mountpoint) {
		return nil, fmt.Errorf("invalid mountpoint")
	}

	if isProtectedMount(mountpoint) {
		return nil, fmt.Errorf("cannot unmount protected system mount: %s", mountpoint)
	}

	partition, err := getPartitionByMountpoint(mountpoint)
	if err != nil {
		return nil, err
	}

	if partition.Fstype == "nfs" || partition.Fstype == "nfs4" {
		return UnmountNFS(ctx, mountpoint, false)
	}

	cmd := exec.Command("umount", mountpoint)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("umount failed: %s", strings.TrimSpace(string(out)))
	}

	return map[string]any{
		"success":    true,
		"mountpoint": mountpoint,
	}, nil
}

func CreateBtrfsSubvolume(mountpoint, name string) (map[string]any, error) {
	if !validPath.MatchString(mountpoint) {
		return nil, fmt.Errorf("invalid mountpoint")
	}

	name = strings.TrimSpace(name)
	if !validSubvolumeName.MatchString(name) {
		return nil, fmt.Errorf("invalid subvolume name")
	}

	partition, err := getPartitionByMountpoint(mountpoint)
	if err != nil {
		return nil, err
	}

	if partition.Fstype != "btrfs" {
		return nil, fmt.Errorf("%s is not a btrfs filesystem", mountpoint)
	}

	if slices.Contains(partition.Opts, "ro") {
		return nil, fmt.Errorf("%s is mounted read-only", mountpoint)
	}

	targetPath := filepath.Join(mountpoint, name)
	if !strings.HasPrefix(targetPath, filepath.Clean(mountpoint)+string(os.PathSeparator)) &&
		filepath.Clean(mountpoint) != "/" {
		return nil, fmt.Errorf("invalid subvolume path")
	}
	if filepath.Clean(mountpoint) == "/" {
		targetPath = filepath.Join("/", name)
	}

	if _, statErr := os.Stat(targetPath); statErr == nil {
		return nil, fmt.Errorf("path already exists: %s", targetPath)
	} else if !os.IsNotExist(statErr) {
		return nil, fmt.Errorf("failed to inspect target path: %w", statErr)
	}

	cmd := exec.Command("btrfs", "subvolume", "create", targetPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("btrfs subvolume create failed: %s", strings.TrimSpace(string(out)))
	}

	return map[string]any{
		"success":    true,
		"mountpoint": mountpoint,
		"path":       targetPath,
	}, nil
}
