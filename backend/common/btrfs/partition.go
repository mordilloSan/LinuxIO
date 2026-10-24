package btrfs

import (
	"bytes"
	"cmp"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
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
	// PartitionSlack is left unused at the end of a partitioned disk, so a
	// replacement disk of the same nominal size that is a little smaller fits.
	PartitionSlack = 1 << 30
	// minPartition refuses disks too small to be worth a pool member.
	minPartition = 256 << 20
)

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

// addKernelPartition tells the kernel about one new partition of device
// (start and length in bytes) without rereading the table, which fails while
// other partitions are busy; a var so tests can stub it.
var addKernelPartition = func(device string, number int, start, length uint64) error {
	f, err := os.Open(device)
	if err != nil {
		return err
	}
	defer f.Close()
	part := unix.BlkpgPartition{Start: int64(start), Length: int64(length), Pno: int32(number)}
	arg := unix.BlkpgIoctlArg{Op: unix.BLKPG_ADD_PARTITION, Datalen: int32(unsafe.Sizeof(part)), Data: (*byte)(unsafe.Pointer(&part))}
	// x/sys has no BLKPG request number: _IO(0x12, 105), ten past BLKRRPART's
	// _IO(0x12, 95), which keeps the per-architecture _IO encoding.
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, f.Fd(), unix.BLKRRPART+10, uintptr(unsafe.Pointer(&arg)))
	runtime.KeepAlive(&part)
	if errno != 0 {
		return fmt.Errorf("add partition %d to %s: %w", number, device, errno)
	}
	return nil
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
	size, ss, err := diskGeometry(device)
	if err != nil {
		return "", err
	}
	layout, err := newGPTLayout(size, ss)
	if err != nil {
		return "", fmt.Errorf("%s: %w", device, err)
	}
	head, tail := layout.build()
	if err = wipeDeviceEdges(device); err != nil {
		return "", err
	}
	f, err := os.OpenFile(device, os.O_RDWR, 0)
	if err != nil {
		return "", err
	}
	_, err = f.WriteAt(head, 0)
	if err == nil {
		_, err = f.WriteAt(tail, int64(layout.sectors*ss)-int64(len(tail)))
	}
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", fmt.Errorf("write partition table to %s: %w", device, err)
	}
	if err := rereadPartitions(device); err != nil {
		return "", err
	}
	return waitForNode(ctx, partitionPath(device, 1))
}

// LargestFreeRegion is the biggest run of sectors in [firstUsable, lastUsable]
// that no used range [first, last] (all inclusive LBAs) touches, trimmed to
// partitionAlign boundaries and at least minPartition long.
func LargestFreeRegion(firstUsable, lastUsable uint64, used [][2]uint64, sectorSize uint64) (first, last uint64, ok bool) {
	if sectorSize == 0 || partitionAlign%sectorSize != 0 {
		return 0, 0, false
	}
	align := partitionAlign / sectorSize
	consider := func(from, to uint64) { // the gap [from, to], inclusive
		from = (from + align - 1) / align * align
		end := (to + 1) / align * align // first LBA past the partition
		if end <= from || (end-from)*sectorSize < minPartition || (ok && end-from <= last-first+1) {
			return
		}
		first, last, ok = from, end-1, true
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

// AddPartition adds one Linux partition spanning the largest free region of
// device's GPT and returns it once udev created its node. Existing
// partitions may be in use: only one entry slot changes and the kernel is
// told about the new partition alone. Anything unexpected is refused.
func AddPartition(ctx context.Context, device string) (string, error) {
	size, ss, err := diskGeometry(device)
	if err != nil {
		return "", err
	}
	last := size/ss - 1
	f, err := os.OpenFile(device, os.O_RDWR, 0)
	if err != nil {
		return "", err
	}
	defer f.Close()
	primary, err := readGPTCopy(f, ss, 1, last)
	if err != nil {
		return "", fmt.Errorf("%s: %w", device, err)
	}
	backup, err := readGPTCopy(f, ss, last, 1)
	if err != nil {
		return "", fmt.Errorf("%s: %w", device, err)
	}
	if !bytes.Equal(primary.entries, backup.entries) || !bytes.Equal(primary.header[56:72], backup.header[56:72]) ||
		primary.firstUsable != backup.firstUsable || primary.lastUsable != backup.lastUsable {
		return "", fmt.Errorf("%s: partition table copies differ; repair it first", device)
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
	first, end, ok := LargestFreeRegion(primary.firstUsable, primary.lastUsable, used, ss)
	if !ok {
		return "", fmt.Errorf("no free space of at least %d MiB on %s", minPartition>>20, device)
	}
	if slot < 0 {
		return "", fmt.Errorf("%s: partition table has no free entry", device)
	}
	entries := slices.Clone(primary.entries)
	entry := gptLayout{first: first, last: end, part: randomGUID()}.entries()[:gptEntrySize]
	copy(entries[slot*gptEntrySize:], entry)
	// Backup first: a crash in between leaves the primary, which still
	// describes only the old partitions, differing from the backup.
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
		if _, err = f.WriteAt(w.data, int64(w.off)); err != nil {
			return "", fmt.Errorf("write partition table to %s: %w", device, err)
		}
	}
	if err = f.Sync(); err != nil {
		return "", fmt.Errorf("write partition table to %s: %w", device, err)
	}
	if err = addKernelPartition(device, slot+1, first*ss, (end-first+1)*ss); err != nil {
		return "", err
	}
	return waitForNode(ctx, partitionPath(device, slot+1))
}
