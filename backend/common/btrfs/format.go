package btrfs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
)

// ErrDeviceHasData is Format, without overwrite, refusing a device whose
// first MiB is not all zeros.
var ErrDeviceHasData = errors.New("device holds data")

// labelLimits is the longest label, in bytes, of each filesystem Format makes.
var labelLimits = map[string]int{"ext4": 16, "xfs": 12, "btrfs": labelSize - 1}

// MinFormatSize is the smallest device, in bytes, Format makes fsType on, or
// 0 for a type it does not make. xfs and btrfs are what mkfs.xfs 6.18.0
// (xfsprogs validate_datadev, XFS_MIN_DATA_BLOCKS since 5.19) and mkfs.btrfs
// 6.17.1 (btrfs_min_dev_size for Format's defaults) accept at the least.
func MinFormatSize(fsType string) uint64 {
	switch fsType {
	case "ext4":
		return MinUserPartition
	case "xfs":
		return 300 << 20
	case "btrfs":
		return 109 << 20
	}
	return 0
}

// ValidateFormat checks that Format makes fsType and that label fits it.
func ValidateFormat(fsType, label string) error {
	limit, ok := labelLimits[fsType]
	if !ok {
		return fmt.Errorf("unsupported filesystem %q: use ext4, xfs or btrfs", fsType)
	}
	if len(label) > limit {
		return fmt.Errorf("%s labels are at most %d bytes", fsType, limit)
	}
	if label == "" {
		return nil
	}
	return ValidateLabel(label)
}

// Format makes an fsType filesystem on device and returns its UUID, which it
// chooses so callers need not wait for udev to probe the device. Without
// overwrite, a device whose first MiB is not all zeros is refused with
// ErrDeviceHasData; with it, mkfs gets its force flag. Callers check that
// device is not in use.
func Format(ctx context.Context, device, fsType, label string, overwrite bool) (string, error) {
	if err := ValidateFormat(fsType, label); err != nil {
		return "", err
	}
	// mkfs.ext4 without a terminal overwrites silently; every start-of-device signature lies in the first 1 MiB, which CreatePartition zeroes.
	if !overwrite {
		if err := checkBlank(device); err != nil {
			return "", err
		}
	}
	uuid := newUUID()
	args := []string{"-U", uuid}
	if fsType == "xfs" {
		args = []string{"-m", "uuid=" + uuid}
	}
	if label != "" {
		args = append(args, "-L", label)
	}
	switch {
	case overwrite && fsType == "ext4":
		args = append(args, "-F") // once: a second -F formats a device in use
	case overwrite:
		args = append(args, "-f")
	}
	if _, err := runCommand(ctx, "mkfs."+fsType, append(args, device)...); err != nil {
		return "", err
	}
	return uuid, nil
}

// checkBlank refuses device unless its first MiB, or all of it when it is
// smaller, is zeros.
func checkBlank(device string) error {
	f, err := os.Open(device)
	if err != nil {
		return err
	}
	defer f.Close()
	head := make([]byte, 1<<20)
	n, err := f.ReadAt(head, 0)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if slices.ContainsFunc(head[:n], func(b byte) bool { return b != 0 }) {
		return fmt.Errorf("%s: %w; overwrite to replace it", device, ErrDeviceHasData)
	}
	return nil
}
