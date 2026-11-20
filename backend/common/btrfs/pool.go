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
	// superMagicTemporary ("!BHRfS_M") is what mkfs.btrfs writes until its
	// last step; blkid and the kernel ignore it.
	superMagicTemporary = 0x4D5F536652484221
	labelSize           = 256 // BTRFS_LABEL_SIZE, including the terminating NUL

	// Field offsets in struct btrfs_super_block.
	superFSID       = 32
	superMagicAt    = 64
	superGeneration = 72
	superTotalBytes = 112
	superNumDevices = 136
	superDevID      = 201 // dev_item.devid
	superDevUUID    = 267 // dev_item.uuid
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
	UUID  string
	Label string
	DevID uint64
	// DevUUID is the device's own UUID (dev_item.uuid), which DEV_INFO
	// reports for the member the device is.
	DevUUID    string
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
		DevUUID:    formatUUID(b[superDevUUID : superDevUUID+16]),
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
// overwrites foreign signatures. check, if not nil, runs right before each
// device's trim and, since mkfs.btrfs starts only after the last trim, for
// every device again right before it; its error stops the create there.
// progress, if not nil, follows the trim before mkfs (see trim). Once
// mkfs.btrfs ran, an error comes with the UUID too, so the caller can wipe
// what it wrote (see WipePoolSuperblocks).
func CreatePool(ctx context.Context, label, data, meta string, devices []string, force bool, check func(device string) error, progress func(pct float64)) (string, error) {
	if err := ValidateLabel(label); err != nil {
		return "", err
	}
	if len(devices) == 0 {
		return "", errors.New("at least one device is required")
	}
	if err := ValidateProfiles(&data, &meta, len(devices)); err != nil {
		return "", err
	}
	if err := trim(ctx, devices, check, progress); err != nil {
		return "", err
	}
	for _, d := range devices {
		if check == nil {
			break
		}
		if err := check(d); err != nil {
			return "", err
		}
	}
	uuid := newUUID()
	args := []string{"-L", label, "-U", uuid, "-d", data, "-m", meta, "-K"}
	if force {
		args = append(args, "-f")
	}
	if _, err := runCommand(ctx, "mkfs.btrfs", append(args, devices...)...); err != nil {
		return uuid, err
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
// mounts it at mountpoint with options. It registers nothing, with
// ErrUnclearDisks, when no disk is present or it cannot tell that each
// device is a disk of the pool (unclearDisks), reading each device's md and
// LVM signature through an exclusive open, which also refuses a device
// something claims. That open refuses the disks of a pool already mounted
// too, so a further mount of a pool goes through MountPool. A leg something
// else registered before is forgotten first: the kernel keeps one device
// for each devid (volumes.c device_list_add), and with its array not
// registered the mount would open the leg.
//
// With an automatic rebuild of the pool on record and not verified, the
// disk it wrote is erased first (eraseUnverifiedTarget), and the mount is
// degraded when that disk had become a member; once mounted, the replace
// the kernel suspended or continued is canceled (settleUnverifiedRebuild).
// It erases a disk that became a member only when every other member is
// present and none is older, and leaves it otherwise; when a disk of the
// rebuild cannot be told, or its target is the only device present,
// nothing is erased or mounted (ErrUnclearDisks, with a way out).
func MountOfflinePool(ctx context.Context, pool BtrfsOfflinePool, mountpoint string, options []string, degraded bool) error {
	if len(pool.Devices) == 0 {
		return fmt.Errorf("%w: no disk of the pool is present", ErrUnclearDisks)
	}
	reason, err := unclearDisks(pool, memberSignature)
	if err != nil {
		return err
	}
	if reason != "" {
		return fmt.Errorf("%w: %s", ErrUnclearDisks, reason)
	}
	missing := missingDisks(pool)
	erased, plan, err := eraseUnverifiedTarget(ctx, pool)
	if err != nil {
		return err
	}
	if plan.reason != "" {
		return fmt.Errorf("%w: %s. %s", ErrUnclearDisks, plan.reason, plan.wayOut)
	}
	pool = erased
	degraded = degraded || missingDisks(pool) > missing
	for _, l := range pool.legs {
		// ENOENT: the kernel never registered it. A path is never empty
		// here; an empty one would forget every unmounted btrfs device.
		if err := controlIoctl(iocForgetDev, l.device); err != nil && !errors.Is(err, unix.ENOENT) {
			return fmt.Errorf("forget %s: %w", l.device, err)
		}
	}
	for _, d := range pool.Devices {
		if err := RegisterDevice(d); err != nil {
			return err
		}
	}
	if err := MountPool(ctx, pool.Devices[0], mountpoint, options, degraded); err != nil {
		return err
	}
	settleUnverifiedRebuild(ctx, pool.UUID, mountpoint)
	return nil
}

// WipeDevice zeroes the primary superblock magic of an unmounted btrfs
// device, so it no longer probes as btrfs, and unregisters it from the
// kernel. A device without the magic is refused untouched, and so, with
// EBUSY, is one still mounted, here or in another mount namespace, or
// claimed since the caller looked: the open is exclusive, which a mounted
// btrfs refuses (it holds its devices) and a merely registered one does not
// (btrfs_scan_one_device in fs/btrfs/volumes.c opens without a holder).
func WipeDevice(device string) error {
	f, err := openExclusive(device, os.O_RDWR)
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

// superblockOffsets lists the superblock copies of a device of size bytes:
// 64 KiB, 64 MiB and 256 GiB, each one that fits.
func superblockOffsets(size uint64) []int64 {
	var offsets []int64
	for _, off := range []int64{superOffset, 64 << 20, 256 << 30} {
		if uint64(off)+superSize <= size {
			offsets = append(offsets, off)
		}
	}
	return offsets
}

// WipePoolSuperblocks undoes a pool create on device: it zeroes the magic of
// every superblock copy whose fsid is uuid, real or the temporary one a
// failed mkfs.btrfs leaves, then unregisters the device. Copies of any other
// filesystem are left alone. It reports whether it wiped any. Like
// WipeDevice it opens device exclusively, so a device that is mounted or was
// claimed since the create is refused with EBUSY and left as it is.
func WipePoolSuperblocks(device, uuid string) (bool, error) {
	f, err := openExclusive(device, os.O_RDWR)
	if err != nil {
		return false, err
	}
	defer f.Close()
	return wipePoolSuperblocksOf(f, device, uuid)
}

// wipePoolSuperblocksOf is WipePoolSuperblocks through f, device opened
// exclusively for writing by the caller, which may check what the device
// carries first through the same open.
func wipePoolSuperblocksOf(f *os.File, device, uuid string) (bool, error) {
	if device == "" {
		// The forget below would drop every unmounted btrfs device.
		return false, errors.New("no device to erase")
	}
	size, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return false, fmt.Errorf("size of %s: %w", device, err)
	}
	wiped := false
	sb := make([]byte, superSize)
	for _, off := range superblockOffsets(uint64(size)) {
		if _, err = f.ReadAt(sb, off); err != nil {
			return wiped, fmt.Errorf("read superblock of %s at %d: %w", device, off, err)
		}
		magic := binary.LittleEndian.Uint64(sb[superMagicAt:])
		if magic != superMagic && magic != superMagicTemporary || formatUUID(sb[superFSID:superFSID+16]) != uuid {
			continue
		}
		if _, err = f.WriteAt(make([]byte, 8), off+superMagicAt); err != nil {
			return wiped, fmt.Errorf("wipe %s: %w", device, err)
		}
		wiped = true
	}
	if !wiped {
		return false, nil
	}
	if err = f.Sync(); err != nil {
		return true, fmt.Errorf("wipe %s: %w", device, err)
	}
	// ENOENT: the kernel never registered the device, nothing to forget.
	if err = controlIoctl(iocForgetDev, device); err != nil && !errors.Is(err, unix.ENOENT) {
		return true, fmt.Errorf("forget %s: %w", device, err)
	}
	return true, nil
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
	// Missing counts the members absent; a device replace's target, which
	// the kernel scans as devid 0, is no member.
	Missing int `json:"missing"`
	// devids and generations hold each of Devices' superblock devid and
	// generation.
	devids, generations []uint64
	// legs are the devices that show the pool's superblock as part of
	// another device (see partOfAnother), which are none of its disks.
	legs []poolLeg
}

// poolLeg is a device that shows a pool's superblock, carrying devid, as
// part of another device: a leg of an md array with its metadata at the end,
// a path of a multipath disk or an image of an LVM mirror. Why says what
// makes it one, such as "is held by /dev/md127".
type poolLeg struct {
	device string
	devid  uint64
	why    string
}

// ErrUnclearDisks refuses to register the devices of a pool when LinuxIO
// cannot tell that each is a disk of the pool (see unclearDisks).
var ErrUnclearDisks = errors.New("LinuxIO cannot tell which devices are the pool's disks")

// FindOfflinePools reads the superblocks of candidates and groups the
// devices of every filesystem that is not mounted, in first-seen order. A
// device that shows the superblock as part of another device is kept apart
// from the disks (partOfAnother), and a pool that shows only on such devices
// is returned without disks. A candidate that cannot be read is logged and
// skipped.
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
			pools = append(pools, BtrfsOfflinePool{UUID: sb.UUID})
		}
		p := &pools[i]
		why := partOfAnother(device)
		// The label and size are a disk's: a leg is read through its own
		// page cache, which the array's writes bypass.
		if !ok || (why == "" && len(p.Devices) == 0) {
			p.Label, p.NumDevices, p.TotalBytes = sb.Label, int(sb.NumDevices), sb.TotalBytes
		}
		if why != "" {
			p.legs = append(p.legs, poolLeg{device, sb.DevID, why})
			continue
		}
		p.Devices = append(p.Devices, device)
		p.devids = append(p.devids, sb.DevID)
		p.generations = append(p.generations, sb.Generation)
	}
	for i := range pools {
		pools[i].Missing = missingDisks(pools[i])
	}
	return pools, nil
}
