package btrfs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"runtime"
	"slices"
	"unsafe"

	"golang.org/x/sys/unix"
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
// ErrDeviceHasData; with it, mkfs gets its force flag. progress, if not
// nil, follows the trim before mkfs (see trim). Callers check that device
// is not in use.
func Format(ctx context.Context, device, fsType, label string, overwrite bool, progress func(pct float64)) (string, error) {
	if err := ValidateFormat(fsType, label); err != nil {
		return "", err
	}
	// mkfs.ext4 without a terminal overwrites silently; every start-of-device signature lies in the first 1 MiB, which CreatePartition zeroes.
	if !overwrite {
		if err := checkBlank(device); err != nil {
			return "", err
		}
	}
	if err := trim(ctx, []string{device}, progress); err != nil {
		return "", err
	}
	uuid := newUUID()
	// -K and -E nodiscard: trim has discarded the device already.
	var args []string
	switch fsType {
	case "ext4":
		args = []string{"-U", uuid, "-E", "nodiscard"}
	case "xfs":
		args = []string{"-m", "uuid=" + uuid, "-K"}
	default:
		args = []string{"-U", uuid, "-K"}
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

// trimChunk is how much one BLKDISCARD covers: btrfs-progs' discard_blocks
// granularity, small enough to report progress and notice cancellation.
const trimChunk = 1 << 30

// trim discards every block of devices, the full-device TRIM mkfs would
// otherwise run silently, so callers run mkfs with its nodiscard flag.
// progress, if not nil, gets the whole percent done whenever it changes,
// ending at 100. A device without discard, such as a hard disk, is skipped.
func trim(ctx context.Context, devices []string, progress func(pct float64)) error {
	sizes := make([]uint64, len(devices))
	var total, done uint64
	for i, d := range devices {
		sizes[i], _ = blockDeviceSize(d) // 0, and skipped, for a regular file
		total += sizes[i]
	}
	last := -1.0
	report := func() {
		pct := 100.0
		if done < total {
			pct = math.Floor(float64(done) * 100 / float64(total))
		}
		if pct != last && progress != nil {
			last = pct
			progress(pct)
		}
	}
	report()
	for i, d := range devices {
		if sizes[i] == 0 {
			continue
		}
		if err := discardDevice(ctx, d, sizes[i], func(n uint64) { done += n; report() }); err != nil {
			return err
		}
	}
	done = total
	report()
	return nil
}

// discardDevice discards size bytes of device in trimChunk pieces, calling
// advance after each. Like mkfs it opens the device exclusively, so one
// mounted or claimed meanwhile is refused with EBUSY, and like mkfs it
// treats a failed discard as the end of the trim, not an error. A var so
// tests can stub it.
var discardDevice = func(ctx context.Context, device string, size uint64, advance func(uint64)) error {
	f, err := os.OpenFile(device, os.O_WRONLY|os.O_EXCL, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	for off := uint64(0); off < size; off += trimChunk {
		if err := ctx.Err(); err != nil {
			return err
		}
		r := [2]uint64{off, min(trimChunk, size-off)}
		_, _, errno := unix.Syscall(unix.SYS_IOCTL, f.Fd(), unix.BLKDISCARD, uintptr(unsafe.Pointer(&r)))
		runtime.KeepAlive(&r)
		if errno != 0 {
			if errno != unix.EOPNOTSUPP {
				slog.Warn("trim stopped, formatting without it", "device", device, "error", errno)
			}
			return nil
		}
		advance(r[1])
	}
	return nil
}
