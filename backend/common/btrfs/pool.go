package btrfs

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"golang.org/x/sys/unix"
)

// Superblock constants from linux/btrfs_tree.h.
const (
	controlDevice = "/dev/btrfs-control"
	superOffset   = 65536 // BTRFS_SUPER_INFO_OFFSET: the primary superblock
	superSize     = 4096  // BTRFS_SUPER_INFO_SIZE
	superMagic    = 0x4D5F53665248425F
	labelSize     = 256 // BTRFS_LABEL_SIZE, including the terminating NUL

	// Field offsets in struct btrfs_super_block.
	superFSID       = 32
	superMagicAt    = 64
	superGeneration = 72
	superTotalBytes = 112
	superNumDevices = 136
	superDevID      = 201 // dev_item.devid
	superLabel      = 299
)

// sysBlockDir lists the host's block devices; a var so tests can point it
// at a fixture tree.
var sysBlockDir = "/sys/class/block"

// BlockDevices returns the /dev path of every block device with a size,
// skipping RAM, floppy and optical devices whose superblocks are not worth
// reading.
func BlockDevices() []string {
	entries, err := os.ReadDir(sysBlockDir)
	if err != nil {
		slog.Warn("list block devices", "error", err)
		return nil
	}
	var paths []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "ram") || strings.HasPrefix(name, "zram") || strings.HasPrefix(name, "sr") || strings.HasPrefix(name, "fd") {
			continue
		}
		size, err := os.ReadFile(filepath.Join(sysBlockDir, name, "size"))
		if err != nil || strings.TrimSpace(string(size)) == "0" {
			continue
		}
		paths = append(paths, "/dev/"+name)
	}
	return paths
}

// RegisterDevice makes the kernel read device's superblock, as udev does
// when a device appears, so a later mount finds every member of its
// filesystem.
func RegisterDevice(device string) error {
	if err := controlIoctl(iocScanDev, device); err != nil {
		return fmt.Errorf("register %s: %w", device, err)
	}
	return nil
}

// Superblock is the part of a device's primary btrfs superblock LinuxIO reads.
type Superblock struct {
	UUID       string
	Label      string
	DevID      uint64
	NumDevices uint64
	TotalBytes uint64
	// Generation is the last transaction written to this device; every
	// commit stamps it on each device it writes.
	Generation uint64
}

var errNotBtrfs = errors.New("not a btrfs device")

// readSuperblock is a var so tests can stub device reads.
var readSuperblock = ReadSuperblock

// ReadSuperblock reads the primary superblock of device.
func ReadSuperblock(device string) (Superblock, error) {
	f, err := os.Open(device)
	if err != nil {
		return Superblock{}, err
	}
	defer f.Close()
	buf := make([]byte, superSize)
	if _, err = f.ReadAt(buf, superOffset); errors.Is(err, io.EOF) {
		return Superblock{}, fmt.Errorf("%s: %w", device, errNotBtrfs) // smaller than a superblock
	} else if err != nil {
		return Superblock{}, fmt.Errorf("read superblock of %s: %w", device, err)
	}
	sb, err := parseSuperblock(buf)
	if err != nil {
		return Superblock{}, fmt.Errorf("%s: %w", device, err)
	}
	return sb, nil
}

func parseSuperblock(b []byte) (Superblock, error) {
	if len(b) < superSize {
		return Superblock{}, errors.New("short superblock")
	}
	if binary.LittleEndian.Uint64(b[superMagicAt:]) != superMagic {
		return Superblock{}, errNotBtrfs
	}
	label, _, _ := strings.Cut(string(b[superLabel:superLabel+labelSize]), "\x00")
	return Superblock{
		UUID:       formatUUID(b[superFSID : superFSID+16]),
		Label:      label,
		DevID:      binary.LittleEndian.Uint64(b[superDevID:]),
		NumDevices: binary.LittleEndian.Uint64(b[superNumDevices:]),
		TotalBytes: binary.LittleEndian.Uint64(b[superTotalBytes:]),
		Generation: binary.LittleEndian.Uint64(b[superGeneration:]),
	}, nil
}

func formatUUID(b []byte) string {
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// newUUID returns a random version 4 UUID for mkfs.btrfs -U.
func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:]) // never fails since Go 1.24
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return formatUUID(b[:])
}

// ValidateLabel checks a filesystem label for mkfs.btrfs.
func ValidateLabel(label string) error {
	if label == "" || len(label) >= labelSize {
		return fmt.Errorf("label must be 1 to %d bytes", labelSize-1)
	}
	if strings.ContainsRune(label, '/') || strings.ContainsFunc(label, unicode.IsControl) {
		return errors.New("label must not contain slashes or control characters")
	}
	return nil
}

// CreatePool formats devices as one btrfs filesystem with the given data
// and metadata profiles, registers them with the kernel and returns the new
// filesystem's UUID. Callers check that the devices are free; force
// overwrites foreign signatures.
func CreatePool(ctx context.Context, label, data, meta string, devices []string, force bool) (string, error) {
	if err := ValidateLabel(label); err != nil {
		return "", err
	}
	if len(devices) == 0 {
		return "", errors.New("at least one device is required")
	}
	if err := ValidateProfiles(&data, &meta, len(devices)); err != nil {
		return "", err
	}
	uuid := newUUID()
	args := []string{"-L", label, "-U", uuid, "-d", data, "-m", meta}
	if force {
		args = append(args, "-f")
	}
	if _, err := runCommand(ctx, "mkfs.btrfs", append(args, devices...)...); err != nil {
		return "", err
	}
	for _, d := range devices {
		if err := RegisterDevice(d); err != nil {
			return uuid, err
		}
	}
	return uuid, nil
}

// mountFS is unix.Mount, a var so tests can stub it.
var mountFS = unix.Mount

// MountPool mounts the filesystem device belongs to at mountpoint with its
// fstab options, creating the directory, and records the pool for the boot
// check (see MountDegradedPools). Degraded lets a filesystem with a missing
// device mount. Devices must be registered first.
func MountPool(ctx context.Context, device, mountpoint string, options []string, degraded bool) error {
	if err := os.MkdirAll(mountpoint, 0o755); err != nil {
		return fmt.Errorf("create mountpoint: %w", err)
	}
	if degraded {
		options = append(slices.Clone(options), "degraded")
	}
	flags, data := splitMountOptions(kernelMountOptions(options))
	if err := mountFS(device, mountpoint, "btrfs", flags, data); err != nil {
		return fmt.Errorf("mount %s at %s: %w", device, mountpoint, err)
	}
	recordMount(ctx, mountpoint, degraded)
	return nil
}

// MountOfflinePool registers the present devices of pool with the kernel and
// mounts it at mountpoint with options.
func MountOfflinePool(ctx context.Context, pool BtrfsOfflinePool, mountpoint string, options []string, degraded bool) error {
	for _, d := range pool.Devices {
		if err := RegisterDevice(d); err != nil {
			return err
		}
	}
	return MountPool(ctx, pool.Devices[0], mountpoint, options, degraded)
}

// WipeDevice zeroes the primary superblock magic of an unmounted btrfs
// device, so it no longer probes as btrfs, and unregisters it from the
// kernel. A device without the magic is refused untouched.
func WipeDevice(device string) error {
	f, err := os.OpenFile(device, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	var magic [8]byte
	if _, err := f.ReadAt(magic[:], superOffset+superMagicAt); err != nil {
		return fmt.Errorf("read superblock of %s: %w", device, err)
	}
	if binary.LittleEndian.Uint64(magic[:]) != superMagic {
		return fmt.Errorf("%s: %w", device, errNotBtrfs)
	}
	if _, err := f.WriteAt(make([]byte, len(magic)), superOffset+superMagicAt); err != nil {
		return fmt.Errorf("wipe %s: %w", device, err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("wipe %s: %w", device, err)
	}
	// ENOENT: the kernel never registered the device, nothing to forget.
	if err := controlIoctl(iocForgetDev, device); err != nil && !errors.Is(err, unix.ENOENT) {
		return fmt.Errorf("forget %s: %w", device, err)
	}
	return nil
}

// BtrfsOfflinePool is an unmounted btrfs filesystem assembled from the
// superblocks of its present devices.
type BtrfsOfflinePool struct {
	UUID       string   `json:"uuid"`
	Label      string   `json:"label"`
	NumDevices int      `json:"numDevices"`
	Devices    []string `json:"devices"`
	TotalBytes uint64   `json:"totalBytes"`
	// Mountpoint is where /etc/fstab mounts the filesystem; the caller that
	// knows fstab fills it.
	Mountpoint string `json:"mountpoint"`
	// devids and generations hold each of Devices' superblock devid and
	// generation.
	devids, generations []uint64
}

// FindOfflinePools reads the superblocks of candidates and groups the
// devices of every filesystem that is not mounted, in first-seen order. A
// candidate that cannot be read is logged and skipped.
func FindOfflinePools(ctx context.Context, candidates []string) ([]BtrfsOfflinePool, error) {
	mounts, err := listMounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("list btrfs mounts: %w", err)
	}
	mounted := map[string]bool{}
	for _, m := range mounts {
		mounted[m.UUID] = true
	}
	byUUID := map[string]int{}
	pools := make([]BtrfsOfflinePool, 0)
	for _, device := range candidates {
		sb, err := readSuperblock(device)
		if errors.Is(err, errNotBtrfs) {
			continue // most devices are not btrfs; that is not worth a log line
		}
		if err != nil {
			slog.Warn("btrfs superblock", "device", device, "error", err)
			continue
		}
		if mounted[sb.UUID] {
			continue
		}
		i, ok := byUUID[sb.UUID]
		if !ok {
			i = len(pools)
			byUUID[sb.UUID] = i
			pools = append(pools, BtrfsOfflinePool{UUID: sb.UUID, Label: sb.Label, NumDevices: int(sb.NumDevices), TotalBytes: sb.TotalBytes})
		}
		pools[i].Devices = append(pools[i].Devices, device)
		pools[i].devids = append(pools[i].devids, sb.DevID)
		pools[i].generations = append(pools[i].generations, sb.Generation)
	}
	return pools, nil
}
