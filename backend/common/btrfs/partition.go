package btrfs

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

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

// partitionPath names the first partition of device: sdb1, nvme0n1p1.
func partitionPath(device string) string {
	if r := device[len(device)-1]; unicode.IsDigit(rune(r)) {
		return device + "p1"
	}
	return device + "1"
}

// PartitionDisk replaces everything on device with a GPT holding one Linux
// partition that leaves PartitionSlack free at the end, and returns the
// partition once udev created its device node. Callers check device is free.
func PartitionDisk(ctx context.Context, device string) (string, error) {
	size, err := blockDeviceSize(device)
	if err != nil {
		return "", err
	}
	ss, err := sectorSize(device)
	if err != nil {
		return "", err
	}
	if ss == 0 || partitionAlign%ss != 0 {
		return "", fmt.Errorf("unsupported sector size %d on %s", ss, device)
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
	part := partitionPath(device)
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
