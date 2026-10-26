package btrfs

import (
	"bytes"
	"cmp"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	gptEntries   = 128
	gptEntrySize = 128
	gptHeaderLen = 92
	// partitionAlign is where partitions start and end, as every tool does.
	partitionAlign = 1 << 20
	// MinUserPartition is the smallest partition CreatePartition makes.
	MinUserPartition = partitionAlign
	// PartitionSlack is left unused at the end of a partitioned disk, so a
	// replacement disk of the same nominal size that is a little smaller fits.
	PartitionSlack = 1 << 30
	// minPartition refuses disks too small to be worth a pool member.
	minPartition = 256 << 20
)

// tableMu serializes partition table changes, so two of them on one disk
// cannot read the same table and lose each other's entry.
var tableMu sync.Mutex

// ErrStaleTable is DeletePartition refusing a partition whose number the
// kernel gives to another range, after a table edit it has not re-read.
var ErrStaleTable = errors.New("the kernel's partition table is out of date; re-read it first")

// linuxFilesystemType is the GPT type GUID for Linux filesystem data.
var linuxFilesystemType = mixedGUID("0FC63DAF-8483-4772-8E79-3D69D8477DE4")

// mixedGUID encodes a GUID string the way GPT stores it: the first three
// fields little-endian, the rest as written.
func mixedGUID(s string) [16]byte {
	raw, err := hex.DecodeString(strings.ReplaceAll(s, "-", ""))
	if err != nil || len(raw) != 16 {
		panic("invalid GUID " + s)
	}
	var g [16]byte
	for i, j := range []int{3, 2, 1, 0, 5, 4, 7, 6} {
		g[i] = raw[j]
	}
	copy(g[8:], raw[8:])
	return g
}

func randomGUID() [16]byte {
	var g [16]byte
	_, _ = rand.Read(g[:])
	g[7] = g[7]&0x0f | 0x40 // version 4, in the little-endian third field
	g[8] = g[8]&0x3f | 0x80
	return g
}

// gptLayout is one Linux partition on a disk of sectors logical sectors.
type gptLayout struct {
	sectorSize, sectors uint64
	first, last         uint64 // partition LBAs, inclusive
	disk, part          [16]byte
}

// newGPTLayout places one partition from 1 MiB to the last aligned sector
// before the slack.
func newGPTLayout(sizeBytes, sectorSize uint64) (gptLayout, error) {
	l := gptLayout{sectorSize: sectorSize, sectors: sizeBytes / sectorSize, disk: randomGUID(), part: randomGUID()}
	align := partitionAlign / sectorSize
	l.first = align
	// Checked before any subtraction: these are unsigned.
	if sizeBytes < partitionAlign+minPartition+PartitionSlack {
		return l, fmt.Errorf("disk of %d bytes is too small to partition", sizeBytes)
	}
	end := (sizeBytes - PartitionSlack) / sectorSize / align * align // first LBA past the partition
	l.last = min(end-1, l.sectors-2-l.entrySectors())
	return l, nil
}

func (l gptLayout) entrySectors() uint64 { return gptEntries * gptEntrySize / l.sectorSize }

func (l gptLayout) entries() []byte {
	b := make([]byte, gptEntries*gptEntrySize)
	if l.last == 0 { // no partition: an empty table
		return b
	}
	copy(b, linuxFilesystemType[:])
	copy(b[16:], l.part[:])
	binary.LittleEndian.PutUint64(b[32:], l.first)
	binary.LittleEndian.PutUint64(b[40:], l.last)
	for i, r := range "linuxio" {
		binary.LittleEndian.PutUint16(b[56+2*i:], uint16(r))
	}
	return b
}

// header is the GPT header at lba, whose entries start at entriesLBA.
func (l gptLayout) header(lba, alternate, entriesLBA uint64, entriesCRC uint32) []byte {
	b := make([]byte, l.sectorSize)
	copy(b, "EFI PART")
	binary.LittleEndian.PutUint32(b[8:], 0x00010000)
	binary.LittleEndian.PutUint32(b[12:], gptHeaderLen)
	binary.LittleEndian.PutUint64(b[24:], lba)
	binary.LittleEndian.PutUint64(b[32:], alternate)
	binary.LittleEndian.PutUint64(b[40:], 2+l.entrySectors())
	binary.LittleEndian.PutUint64(b[48:], l.sectors-2-l.entrySectors())
	copy(b[56:], l.disk[:])
	binary.LittleEndian.PutUint64(b[72:], entriesLBA)
	binary.LittleEndian.PutUint32(b[80:], gptEntries)
	binary.LittleEndian.PutUint32(b[84:], gptEntrySize)
	binary.LittleEndian.PutUint32(b[88:], entriesCRC)
	binary.LittleEndian.PutUint32(b[16:], crc32.ChecksumIEEE(b[:gptHeaderLen]))
	return b
}

// head is the protective MBR, primary header and entries; tail is the
// backup entries and header that end the disk.
func (l gptLayout) build() (head, tail []byte) {
	entries := l.entries()
	crc := crc32.ChecksumIEEE(entries)
	last := l.sectors - 1
	head = make([]byte, (2+l.entrySectors())*l.sectorSize)
	mbr := head[446:]
	mbr[2] = 0x02 // CHS start 0/0/2
	mbr[4] = 0xEE
	mbr[5], mbr[6], mbr[7] = 0xFF, 0xFF, 0xFF
	binary.LittleEndian.PutUint32(mbr[8:], 1)
	binary.LittleEndian.PutUint32(mbr[12:], uint32(min(last, 0xFFFFFFFF)))
	head[510], head[511] = 0x55, 0xAA
	copy(head[l.sectorSize:], l.header(1, last, 2, crc))
	copy(head[2*l.sectorSize:], entries)
	tail = make([]byte, (l.entrySectors()+1)*l.sectorSize)
	copy(tail, entries)
	copy(tail[l.entrySectors()*l.sectorSize:], l.header(last, 1, last-l.entrySectors(), crc))
	return head, tail
}

// sectorSize reads a device's logical sector size; a var so tests can stub it.
var sectorSize = func(device string) (uint64, error) {
	f, err := os.Open(device)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	n, err := unix.IoctlGetInt(int(f.Fd()), unix.BLKSSZGET)
	if err != nil {
		return 0, fmt.Errorf("sector size of %s: %w", device, err)
	}
	return uint64(n), nil
}

// rereadPartitions asks the kernel to read device's new partition table; a
// var so tests can stub it.
var rereadPartitions = func(device string) error {
	f, err := os.Open(device)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := unix.IoctlSetInt(int(f.Fd()), unix.BLKRRPART, 0); err != nil {
		return fmt.Errorf("re-read partitions of %s: %w", device, err)
	}
	return nil
}

// blkpg sends device one BLKPG request about part.
func blkpg(device string, op int32, part unix.BlkpgPartition) error {
	f, err := os.Open(device)
	if err != nil {
		return err
	}
	defer f.Close()
	arg := unix.BlkpgIoctlArg{Op: op, Datalen: int32(unsafe.Sizeof(part)), Data: (*byte)(unsafe.Pointer(&part))}
	// x/sys has no BLKPG request number: _IO(0x12, 105), ten past BLKRRPART's
	// _IO(0x12, 95), which keeps the per-architecture _IO encoding.
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, f.Fd(), unix.BLKRRPART+10, uintptr(unsafe.Pointer(&arg)))
	runtime.KeepAlive(&part)
	if errno != 0 {
		return errno
	}
	return nil
}

// addKernelPartition tells the kernel about one new partition of device
// (start and length in bytes) without rereading the table, which fails while
// other partitions are busy; a var so tests can stub it.
var addKernelPartition = func(device string, number int, start, length uint64) error {
	if err := blkpg(device, unix.BLKPG_ADD_PARTITION, unix.BlkpgPartition{Start: int64(start), Length: int64(length), Pno: int32(number)}); err != nil {
		return fmt.Errorf("add partition %d to %s: %w", number, device, err)
	}
	return nil
}

// delKernelPartition tells the kernel to forget partition number of device,
// which it refuses with EBUSY while anything has the partition open; a var
// so tests can stub it.
var delKernelPartition = func(device string, number int) error {
	if err := blkpg(device, unix.BLKPG_DEL_PARTITION, unix.BlkpgPartition{Pno: int32(number)}); err != nil {
		return fmt.Errorf("delete partition %d from %s: %w", number, device, err)
	}
	return nil
}

// dropKernelPartition is delKernelPartition retried for about a second while
// the partition is busy, since a udev or blkid probe holds it open briefly.
func dropKernelPartition(ctx context.Context, device string, number int) error {
	for try := 1; ; try++ {
		err := delKernelPartition(device, number)
		if try == 10 || !errors.Is(err, unix.EBUSY) {
			return err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w: %w", ctx.Err(), err)
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// kernelPartition is a partition the kernel knows, in bytes.
type kernelPartition struct {
	number        int
	start, length uint64
}

// kernelPartitions lists the partitions the kernel knows on device: the
// subdirectories of its sysfs directory that have a partition file.
func kernelPartitions(device string) ([]kernelPartition, error) {
	dir := filepath.Join(sysBlockDir, filepath.Base(device))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var parts []kernelPartition
	for _, e := range entries {
		if !e.IsDir() {
			continue // an attribute of the disk itself
		}
		sub := filepath.Join(dir, e.Name())
		number, err := sysNumber(filepath.Join(sub, "partition"))
		if errors.Is(err, os.ErrNotExist) {
			continue // queue, holders and other attribute groups
		}
		if err != nil {
			return nil, err
		}
		start, err := sysNumber(filepath.Join(sub, "start"))
		if err != nil {
			return nil, err
		}
		size, err := sysNumber(filepath.Join(sub, "size"))
		if err != nil {
			return nil, err
		}
		// sysfs counts 512-byte units whatever the logical sector size.
		parts = append(parts, kernelPartition{int(number), start * 512, size * 512})
	}
	return parts, nil
}

// sysNumber reads the number in a sysfs attribute file.
func sysNumber(path string) (uint64, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	n, err := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", path, err)
	}
	return n, nil
}

// CheckNotBusy refuses device while it is mounted, an active swap or
// claimed by a holder such as LVM, md or dm-crypt, all of which make the
// kernel refuse to open it exclusively.
func CheckNotBusy(device string) error {
	f, err := os.OpenFile(device, os.O_RDONLY|os.O_EXCL, 0)
	if errors.Is(err, unix.EBUSY) {
		return fmt.Errorf("%s is in use: %w", device, unix.EBUSY)
	}
	if err != nil {
		return err
	}
	return f.Close()
}

// partitionPath names partition number of device: sdb3, nvme0n1p3.
func partitionPath(device string, number int) string {
	if r := device[len(device)-1]; unicode.IsDigit(rune(r)) {
		return device + "p" + strconv.Itoa(number)
	}
	return device + strconv.Itoa(number)
}

// diskGeometry reads device's size and logical sector size.
func diskGeometry(device string) (size, ss uint64, err error) {
	if size, err = blockDeviceSize(device); err != nil {
		return 0, 0, err
	}
	if ss, err = sectorSize(device); err != nil {
		return 0, 0, err
	}
	if ss == 0 || partitionAlign%ss != 0 {
		return 0, 0, fmt.Errorf("unsupported sector size %d on %s", ss, device)
	}
	return size, ss, nil
}

// waitForNode waits for udev to create the device node of part.
func waitForNode(ctx context.Context, part string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, statErr := os.Stat(part); statErr == nil {
			return part, nil
		}
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("partition %s did not appear: %w", filepath.Base(part), ctx.Err())
		case <-ticker.C:
		}
	}
}

// PartitionDisk replaces everything on device with a GPT holding one Linux
// partition that leaves PartitionSlack free at the end, and returns the
// partition once udev created its device node. Callers check device is free.
func PartitionDisk(ctx context.Context, device string) (string, error) {
	tableMu.Lock()
	defer tableMu.Unlock()
	if err := checkWholeDisk(device); err != nil {
		return "", err
	}
	size, ss, err := diskGeometry(device)
	if err != nil {
		return "", err
	}
	layout, err := newGPTLayout(size, ss)
	if err != nil {
		return "", fmt.Errorf("%s: %w", device, err)
	}
	if err = writeTable(device, layout); err == nil {
		err = rereadPartitions(device)
	}
	if err != nil {
		return "", err
	}
	return waitForNode(ctx, partitionPath(device, 1))
}

// checkWholeDisk refuses device when sysfs lists it as a partition, whose
// own partition table the kernel would never read.
func checkWholeDisk(device string) error {
	if _, err := os.Stat(filepath.Join(sysBlockDir, filepath.Base(device), "partition")); err == nil {
		return fmt.Errorf("%s is a partition, not a whole disk", device)
	}
	return nil
}

// InitializeDisk replaces everything on device with an empty GPT. Every
// partition is dropped from the kernel first, which it refuses while one is
// in use; then the ones already dropped are restored and nothing is written.
// They are restored too when writing the table fails.
func InitializeDisk(ctx context.Context, device string) error {
	tableMu.Lock()
	defer tableMu.Unlock()
	if err := checkWholeDisk(device); err != nil {
		return err
	}
	size, ss, err := diskGeometry(device)
	if err != nil {
		return err
	}
	if size < 2*partitionAlign {
		return fmt.Errorf("%s is too small to partition", device)
	}
	// A filesystem or holder on the whole disk has no partition to drop.
	if err = CheckNotBusy(device); err != nil {
		return err
	}
	parts, err := kernelPartitions(device)
	if err != nil {
		return err
	}
	// restore gives the kernel back dropped, which the old table may still list.
	restore := func(err error, dropped []kernelPartition) error {
		for _, q := range dropped {
			if addErr := addKernelPartition(device, q.number, q.start, q.length); addErr != nil {
				err = fmt.Errorf("%w; restoring partition %d also failed: %w", err, q.number, addErr)
			}
		}
		return err
	}
	for i, p := range parts {
		if err = dropKernelPartition(ctx, device, p.number); err != nil {
			return restore(err, parts[:i])
		}
	}
	if err = writeTable(device, gptLayout{sectorSize: ss, sectors: size / ss, disk: randomGUID()}); err != nil {
		return restore(err, parts)
	}
	// Once the empty table is written, the kernel's empty view matches it
	// even when the re-read fails.
	return rereadPartitions(device)
}

// writeTable wipes device's edges and writes layout's GPT; the caller then
// has the kernel read it.
func writeTable(device string, layout gptLayout) error {
	head, tail := layout.build()
	if err := wipeDeviceEdges(device); err != nil {
		return err
	}
	f, err := os.OpenFile(device, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	_, err = f.WriteAt(head, 0)
	if err == nil {
		_, err = f.WriteAt(tail, int64(layout.sectors*layout.sectorSize)-int64(len(tail)))
	}
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write partition table to %s: %w", device, err)
	}
	return nil
}

// FreeRegions lists, in disk order, the runs of sectors in [firstUsable,
// lastUsable] that no used range [first, last] (all inclusive LBAs)
// touches, trimmed to partitionAlign boundaries and at least minBytes long.
func FreeRegions(firstUsable, lastUsable uint64, used [][2]uint64, sectorSize, minBytes uint64) [][2]uint64 {
	if sectorSize == 0 || partitionAlign%sectorSize != 0 {
		return nil
	}
	align := partitionAlign / sectorSize
	var regions [][2]uint64
	consider := func(from, to uint64) { // the gap [from, to], inclusive
		from = (from + align - 1) / align * align
		end := (to + 1) / align * align // first LBA past the region
		if end > from && (end-from)*sectorSize >= minBytes {
			regions = append(regions, [2]uint64{from, end - 1})
		}
	}
	cursor := firstUsable
	for _, u := range slices.SortedFunc(slices.Values(used), func(a, b [2]uint64) int { return cmp.Compare(a[0], b[0]) }) {
		if u[0] > cursor {
			consider(cursor, min(u[0]-1, lastUsable))
		}
		cursor = max(cursor, min(u[1], lastUsable)+1)
	}
	if cursor <= lastUsable {
		consider(cursor, lastUsable)
	}
	return regions
}

// LargestFreeRegion is the longest of the FreeRegions at least minPartition
// long, the first in disk order on a tie.
func LargestFreeRegion(firstUsable, lastUsable uint64, used [][2]uint64, sectorSize uint64) (first, last uint64, ok bool) {
	for _, r := range FreeRegions(firstUsable, lastUsable, used, sectorSize, minPartition) {
		if !ok || r[1]-r[0] > last-first {
			first, last, ok = r[0], r[1], true
		}
	}
	return first, last, ok
}

// gptCopy is one validated GPT header (a full sector) and its entry array.
type gptCopy struct {
	header, entries         []byte
	firstUsable, lastUsable uint64
	entriesLBA              uint64
}

// readGPTCopy reads the header at lba, which must name alternate as its
// other copy, and refuses anything that does not check out.
func readGPTCopy(r io.ReaderAt, ss, lba, alternate uint64) (gptCopy, error) {
	c := gptCopy{header: make([]byte, ss)}
	if _, err := r.ReadAt(c.header, int64(lba*ss)); err != nil {
		return c, err
	}
	h := c.header
	size := binary.LittleEndian.Uint32(h[12:])
	if string(h[:8]) != "EFI PART" || binary.LittleEndian.Uint32(h[8:]) != 0x00010000 || size < gptHeaderLen || uint64(size) > ss {
		return c, fmt.Errorf("no valid GPT header at LBA %d", lba)
	}
	sum := slices.Clone(h[:size])
	clear(sum[16:20])
	if crc32.ChecksumIEEE(sum) != binary.LittleEndian.Uint32(h[16:]) {
		return c, fmt.Errorf("GPT header at LBA %d has a bad checksum", lba)
	}
	count, entrySize := binary.LittleEndian.Uint32(h[80:]), binary.LittleEndian.Uint32(h[84:])
	if binary.LittleEndian.Uint64(h[24:]) != lba || binary.LittleEndian.Uint64(h[32:]) != alternate ||
		entrySize != gptEntrySize || count == 0 || count > 1024 {
		return c, fmt.Errorf("GPT header at LBA %d is not the one expected there", lba)
	}
	c.firstUsable, c.lastUsable = binary.LittleEndian.Uint64(h[40:]), binary.LittleEndian.Uint64(h[48:])
	c.entriesLBA = binary.LittleEndian.Uint64(h[72:])
	sectors := (uint64(count)*gptEntrySize + ss - 1) / ss
	// The entry array must sit outside the usable area and the headers.
	if c.entriesLBA < 2 || c.firstUsable > c.lastUsable || (c.entriesLBA <= c.lastUsable && c.entriesLBA+sectors > c.firstUsable) ||
		(c.entriesLBA <= lba && lba < c.entriesLBA+sectors) {
		return c, fmt.Errorf("GPT header at LBA %d has an implausible layout", lba)
	}
	c.entries = make([]byte, count*gptEntrySize)
	if _, err := r.ReadAt(c.entries, int64(c.entriesLBA*ss)); err != nil {
		return c, err
	}
	if crc32.ChecksumIEEE(c.entries) != binary.LittleEndian.Uint32(h[88:]) {
		return c, fmt.Errorf("GPT entries of the header at LBA %d have a bad checksum", lba)
	}
	return c, nil
}

// withEntries returns the header after its entry array became entries.
func (c gptCopy) withEntries(entries []byte) []byte {
	h := slices.Clone(c.header)
	binary.LittleEndian.PutUint32(h[88:], crc32.ChecksumIEEE(entries))
	size := binary.LittleEndian.Uint32(h[12:])
	binary.LittleEndian.PutUint32(h[16:], 0)
	binary.LittleEndian.PutUint32(h[16:], crc32.ChecksumIEEE(h[:size]))
	return h
}

// readTables reads both GPT copies of device through r and refuses them
// unless both validate and agree; last is the backup header's LBA.
func readTables(r io.ReaderAt, device string) (ss, last uint64, primary, backup gptCopy, err error) {
	size, ss, err := diskGeometry(device)
	if err != nil {
		return 0, 0, primary, backup, err
	}
	last = size/ss - 1
	if primary, err = readGPTCopy(r, ss, 1, last); err == nil {
		backup, err = readGPTCopy(r, ss, last, 1)
	}
	if err != nil {
		return 0, 0, primary, backup, fmt.Errorf("%s: %w", device, err)
	}
	if !bytes.Equal(primary.entries, backup.entries) || !bytes.Equal(primary.header[56:72], backup.header[56:72]) ||
		primary.firstUsable != backup.firstUsable || primary.lastUsable != backup.lastUsable {
		return 0, 0, primary, backup, fmt.Errorf("%s: partition table copies differ; repair it first", device)
	}
	// FreeRegions offers the whole usable area, so it must not reach a header.
	if primary.firstUsable < 2 || primary.lastUsable >= last {
		return 0, 0, primary, backup, fmt.Errorf("%s: GPT usable area %d-%d does not fit between its headers", device, primary.firstUsable, primary.lastUsable)
	}
	return ss, last, primary, backup, nil
}

// writeEntry puts entry in slot of both GPT copies on f. The backup goes
// first: a crash in between leaves the primary, which still describes the
// old table, differing from the backup.
func writeEntry(f *os.File, ss, last uint64, primary, backup gptCopy, slot int, entry []byte) error {
	entries := slices.Clone(primary.entries)
	copy(entries[slot*gptEntrySize:], entry)
	writes := []struct {
		data []byte
		off  uint64
	}{
		{entry, backup.entriesLBA*ss + uint64(slot*gptEntrySize)},
		{backup.withEntries(entries), last * ss},
		{entry, primary.entriesLBA*ss + uint64(slot*gptEntrySize)},
		{primary.withEntries(entries), ss},
	}
	for _, w := range writes {
		if _, err := f.WriteAt(w.data, int64(w.off)); err != nil {
			return fmt.Errorf("write partition table to %s: %w", f.Name(), err)
		}
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("write partition table to %s: %w", f.Name(), err)
	}
	return nil
}

// AddPartition adds one Linux partition spanning the largest free region of
// device's GPT and returns it once udev created its node and its first and
// last 2 MiB are zeroed, so no old signature shows through. Existing
// partitions may be in use: only one entry slot changes and the kernel is
// told about the new partition alone. Anything unexpected is refused.
func AddPartition(ctx context.Context, device string) (string, error) {
	return newPartition(ctx, device, func(firstUsable, lastUsable uint64, used [][2]uint64, ss uint64) (uint64, uint64, error) {
		first, last, ok := LargestFreeRegion(firstUsable, lastUsable, used, ss)
		if !ok {
			return 0, 0, fmt.Errorf("no free space of at least %d MiB on %s", minPartition>>20, device)
		}
		return first, last, nil
	})
}

// CreatePartition is AddPartition for startBytes to startBytes+sizeBytes of
// device, rounded inward to whole MiB. The result must be at least
// MinUserPartition and lie inside one free region.
func CreatePartition(ctx context.Context, device string, startBytes, sizeBytes uint64) (string, error) {
	return newPartition(ctx, device, func(firstUsable, lastUsable uint64, used [][2]uint64, ss uint64) (uint64, uint64, error) {
		align := partitionAlign / ss
		first := startBytes / partitionAlign * align
		if startBytes%partitionAlign != 0 {
			first += align
		}
		// An end that wraps past 2^64 lands before the start: too small.
		end := (startBytes + sizeBytes) / partitionAlign * align // first LBA past the partition
		if end < first+MinUserPartition/ss {
			return 0, 0, fmt.Errorf("partition must be at least %d MiB once aligned to whole MiB", MinUserPartition>>20)
		}
		inside := func(r [2]uint64) bool { return r[0] <= first && end-1 <= r[1] }
		if !slices.ContainsFunc(FreeRegions(firstUsable, lastUsable, used, ss, partitionAlign), inside) {
			return 0, 0, fmt.Errorf("bytes %d to %d of %s are not free space", first*ss, end*ss, device)
		}
		return first, end - 1, nil
	})
}

// newPartition does the work of AddPartition and CreatePartition, whose
// place picks the new partition's first and last LBA.
func newPartition(ctx context.Context, device string, place func(firstUsable, lastUsable uint64, used [][2]uint64, ss uint64) (first, last uint64, err error)) (string, error) {
	tableMu.Lock()
	defer tableMu.Unlock()
	f, err := os.OpenFile(device, os.O_RDWR, 0)
	if err != nil {
		return "", err
	}
	defer f.Close()
	ss, last, primary, backup, err := readTables(f, device)
	if err != nil {
		return "", err
	}
	slot := -1
	var used [][2]uint64
	for i := range len(primary.entries) / gptEntrySize {
		e := primary.entries[i*gptEntrySize:][:gptEntrySize]
		if [16]byte(e[:16]) == [16]byte{} {
			if slot < 0 {
				slot = i
			}
			continue
		}
		used = append(used, [2]uint64{binary.LittleEndian.Uint64(e[32:]), binary.LittleEndian.Uint64(e[40:])})
	}
	first, end, err := place(primary.firstUsable, primary.lastUsable, used, ss)
	if err != nil {
		return "", err
	}
	if slot < 0 {
		return "", fmt.Errorf("%s: partition table has no free entry", device)
	}
	entry := gptLayout{first: first, last: end, part: randomGUID()}.entries()[:gptEntrySize]
	if err = writeEntry(f, ss, last, primary, backup, slot, entry); err != nil {
		return "", err
	}
	if err = addKernelPartition(device, slot+1, first*ss, (end-first+1)*ss); err != nil {
		// A partition the kernel does not know is never listed, so its entry
		// would keep the space taken where no one can delete it.
		if undoErr := writeEntry(f, ss, last, primary, backup, slot, make([]byte, gptEntrySize)); undoErr != nil {
			return "", fmt.Errorf("%w; removing its table entry also failed: %w", err, undoErr)
		}
		return "", err
	}
	part, err := waitForNode(ctx, partitionPath(device, slot+1))
	if err != nil {
		return "", err
	}
	if err = wipeDeviceEdges(part); err != nil {
		return "", err
	}
	return part, nil
}

// DeletePartition removes partition number from device's GPT, leaving its
// data in place. The kernel drops the partition first, which it refuses
// while the partition is in use, so a busy partition is never deleted.
func DeletePartition(ctx context.Context, device string, number int) error {
	tableMu.Lock()
	defer tableMu.Unlock()
	f, err := os.OpenFile(device, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	ss, last, primary, backup, err := readTables(f, device)
	if err != nil {
		return err
	}
	if number < 1 || number > len(primary.entries)/gptEntrySize || [16]byte(primary.entries[(number-1)*gptEntrySize:]) == [16]byte{} {
		return fmt.Errorf("%s has no partition %d", device, number)
	}
	e := primary.entries[(number-1)*gptEntrySize:]
	first, end := binary.LittleEndian.Uint64(e[32:]), binary.LittleEndian.Uint64(e[40:])
	// The kernel drops by number, so its partition number must be this entry.
	parts, err := kernelPartitions(device)
	if err != nil {
		return err
	}
	if !slices.Contains(parts, kernelPartition{number, first * ss, (end - first + 1) * ss}) {
		return fmt.Errorf("%s: %w", device, ErrStaleTable)
	}
	if err = dropKernelPartition(ctx, device, number); err != nil {
		return err
	}
	if err = writeEntry(f, ss, last, primary, backup, number-1, make([]byte, gptEntrySize)); err != nil {
		// The table may still list the partition, so the kernel gets it back.
		if addErr := addKernelPartition(device, number, first*ss, (end-first+1)*ss); addErr != nil {
			return fmt.Errorf("%w; restoring partition %d also failed: %w", err, number, addErr)
		}
		return err
	}
	return nil
}
