package btrfs

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"log/slog"
	"os"
	"slices"
	"strings"
)

// MemberSignature is an md RAID or LVM2 signature that makes Device a member
// of an array or a volume group, read from the device itself the way
// libblkid reads it (util-linux libblkid/src/superblocks/linux_raid.c and
// lvm.c), so one udev has not probed yet is seen too. Kind is "md" or "lvm",
// or empty for neither; UUID is the array's or the physical volume's as
// libblkid formats it, which is what udev's ID_FS_UUID holds.
type MemberSignature struct {
	Device string
	Kind   string
	UUID   string
}

const (
	mdMagic = 0xa92b4efc
	// mdReservedBytes is the end of a device md 0.90 keeps for its superblock.
	mdReservedBytes = 64 << 10
	// mdMaxDevs is the most member slots an md 1.x superblock has: the
	// kernel's super_1_load refuses more than fit in its 4 KiB.
	mdMaxDevs = (4096 - 256) / 2
	// tinyDevice is libblkid's floppy: it probes no RAID or LVM signature
	// on a device this small (BLKID_FL_TINY_DEV).
	tinyDevice = 1440 << 10
)

// ReadMemberSignature reads device's md or LVM2 signature through an
// exclusive open, which the kernel refuses with EBUSY while the device is
// mounted, an active swap or held, and during which nothing can claim it.
// The open is read-only, so closing it does not make udev probe the device.
// For a whole disk, a 0.90 superblock in its last partition is the
// partition's, as for libblkid.
func ReadMemberSignature(device string) (MemberSignature, error) {
	parts, err := kernelPartitions(device)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return MemberSignature{}, err
	}
	f, err := openExclusive(device, os.O_RDONLY)
	if err != nil {
		return MemberSignature{}, err
	}
	defer f.Close()
	return fileSignature(f, device, parts)
}

// diskSignatures reads the md and LVM2 signatures of device, open as f, and
// of parts, its partitions, and returns those it found. Each partition is
// read through its own node, since the disk's page cache can hold a copy of
// a partition's blocks from before a write through the partition; the opens
// are read-only and not exclusive, which a claim on the disk would refuse.
func diskSignatures(f *os.File, device string, parts []kernelPartition) ([]MemberSignature, error) {
	var found []MemberSignature
	sig, err := fileSignature(f, device, parts)
	if err != nil {
		return nil, err
	}
	if sig.Kind != "" {
		found = append(found, sig)
	}
	for _, p := range parts {
		node := partitionPath(device, p.number)
		pf, err := os.Open(node)
		if err != nil {
			return nil, err
		}
		sig, err = fileSignature(pf, node, nil)
		pf.Close()
		if err != nil {
			return nil, err
		}
		if sig.Kind != "" {
			found = append(found, sig)
		}
	}
	return found, nil
}

// fileSignature reads the md or LVM2 signature of f, the open device, whose
// partitions, for a whole disk, are parts.
func fileSignature(f *os.File, device string, parts []kernelPartition) (MemberSignature, error) {
	size, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return MemberSignature{}, fmt.Errorf("size of %s: %w", device, err)
	}
	sig, unreadable := readMemberSignature(f, uint64(size), parts)
	if unreadable != nil {
		// What a failing disk cannot give back is no member anything can
		// use, and udev, which reads the same places, shows none there.
		slog.Warn("member signature place unreadable", "device", device, "error", unreadable)
	}
	sig.Device = device
	return sig, nil
}

// readMemberSignature reads the md or LVM2 signature of r, a device of size
// bytes whose partitions, for a whole disk, are parts. It checks what
// libblkid checks, in its order: md first, and of md the 0.90 superblock in
// the last 64 KiB, 1.0 8 KiB from the end, 1.1 at the start and 1.2 at
// 4 KiB; then an LVM2 label in one of the first four sectors. A place it
// cannot read holds no signature there; unreadable is the first such error.
func readMemberSignature(r io.ReaderAt, size uint64, parts []kernelPartition) (sig MemberSignature, unreadable error) {
	if size <= tinyDevice {
		return MemberSignature{}, nil
	}
	p := &probe{r: r, size: size}
	if uuid, ok := mdSignature(p, parts); ok {
		return MemberSignature{Kind: "md", UUID: uuid}, p.unreadable
	}
	if uuid, ok := lvm2Signature(p); ok {
		return MemberSignature{Kind: "lvm", UUID: uuid}, p.unreadable
	}
	return MemberSignature{}, p.unreadable
}

// probe is the device readMemberSignature reads, size bytes, keeping the
// first error of a place it could not read.
type probe struct {
	r          io.ReaderAt
	size       uint64
	unreadable error
}

// read fills b from off and reports whether it could.
func (p *probe) read(b []byte, off uint64) bool {
	if _, err := p.r.ReadAt(b, int64(off)); err != nil {
		if p.unreadable == nil {
			p.unreadable = fmt.Errorf("read %d bytes at %d: %w", len(b), off, err)
		}
		return false
	}
	return true
}

// mdSignature is linux_raid.c's probe_raid.
func mdSignature(p *probe, parts []kernelPartition) (string, bool) {
	if p.size > mdReservedBytes {
		if uuid, ok := md090Signature(p, parts); ok {
			return uuid, true
		}
		if uuid, ok := md1Signature(p, p.size&^0xfff-0x2000); ok {
			return uuid, true
		}
	}
	if uuid, ok := md1Signature(p, 0); ok {
		return uuid, true
	}
	return md1Signature(p, 0x1000)
}

// md090Signature is linux_raid.c's probe_raid0: an md 0.90 superblock, in
// either byte order, in the last 64 KiB, with the array's size room before
// it.
func md090Signature(p *probe, parts []kernelPartition) (string, bool) {
	off := p.size&^(mdReservedBytes-1) - mdReservedBytes
	sb := make([]byte, 64)
	if !p.read(sb, off) {
		return "", false
	}
	var order binary.ByteOrder
	switch {
	case binary.LittleEndian.Uint32(sb) == mdMagic:
		order = binary.LittleEndian
	case binary.BigEndian.Uint32(sb) == mdMagic:
		order = binary.BigEndian
	default:
		return "", false
	}
	var id [16]byte
	copy(id[:4], sb[20:24]) // set_uuid0
	if order.Uint32(sb[8:]) >= 90 {
		copy(id[4:], sb[52:64]) // set_uuid1 to set_uuid3
	}
	if order == binary.LittleEndian {
		// libblkid swaps each word of a little-endian superblock, so the
		// UUID reads as mdadm prints the words.
		for w := range 4 {
			slices.Reverse(id[w*4 : w*4+4])
		}
	}
	array := uint64(order.Uint32(sb[32:])) << 10
	if p.size < array+mdReservedBytes || off < array {
		return "", false
	}
	// blkid_probe_is_covered_by_pt: on a whole disk, a superblock whose
	// array lies inside a partition is that partition's.
	for _, part := range parts {
		if part.start <= off-array && off+mdReservedBytes <= part.start+part.length {
			return "", false
		}
	}
	return blkidUUID(id[:]), true
}

// md1Signature is linux_raid.c's probe_raid1: an md 1.x superblock at off
// that names off as its own place and whose checksum holds.
func md1Signature(p *probe, off uint64) (string, bool) {
	le := binary.LittleEndian
	const sbSize = 256
	if off+sbSize > p.size {
		return "", false
	}
	sb := make([]byte, sbSize)
	if !p.read(sb, off) {
		return "", false
	}
	maxDev := le.Uint32(sb[220:])
	if le.Uint32(sb) != mdMagic || le.Uint32(sb[4:]) != 1 || le.Uint64(sb[144:]) != off>>9 || maxDev > mdMaxDevs {
		return "", false
	}
	csummed := make([]byte, sbSize+2*uint64(maxDev))
	if off+uint64(len(csummed)) > p.size || !p.read(csummed, off) {
		return "", false
	}
	// raid1_verify_csum: the sum of the little-endian words with sb_csum's
	// own taken out, folded once, compared without truncating to 32 bits.
	want := uint64(le.Uint32(sb[216:]))
	csum := -want
	words := len(csummed) &^ 3
	for i := 0; i < words; i += 4 {
		csum += uint64(le.Uint32(csummed[i:]))
	}
	if len(csummed)-words == 2 {
		csum += uint64(le.Uint16(csummed[words:]))
	}
	if csum>>32+csum&0xffffffff != want {
		return "", false
	}
	return blkidUUID(sb[16:32]), true
}

// lvm2Signature is lvm.c's probe_lvm2: the first of sectors 1, 0, 2 and 3
// typed "LVM2 001" leads to the "LABELONE" label of its KiB, which must name
// its own sector and carry a valid CRC.
func lvm2Signature(p *probe) (string, bool) {
	le := binary.LittleEndian
	buf := make([]byte, 2048)
	if !p.read(buf, 0) {
		return "", false
	}
	for _, m := range [...]struct{ kib, at int }{{0, 0x218}, {0, 0x18}, {1, 0x18}, {1, 0x218}} {
		if string(buf[m.kib<<10+m.at:][:8]) != "LVM2 001" {
			continue
		}
		sector := uint64(m.kib * 2)
		label := buf[m.kib<<10:][:512]
		if string(label[:8]) != "LABELONE" {
			sector++
			if label = buf[m.kib<<10+512:][:512]; string(label[:8]) != "LABELONE" {
				return "", false
			}
		}
		// LVM's CRC is CRC-32 over the label from offset_xl on, started at
		// 0xf597a6cf with no final inversion.
		if le.Uint64(label[8:]) != sector || ^crc32.Update(^uint32(0xf597a6cf), crc32.IEEETable, label[20:]) != le.Uint32(label[16:]) {
			return "", false
		}
		return lvmUUID(label[32:64]), true
	}
	return "", false
}

// lvmUUID is lvm.c's format_lvm_uuid: a PV UUID's 32 characters in groups
// of 6-4-4-4-4-4-6, or empty when they are all zero.
func lvmUUID(id []byte) string {
	if bytes.Equal(id, make([]byte, len(id))) {
		return ""
	}
	var b strings.Builder
	for i, c := range id {
		if i >= 6 && i <= 26 && (i-6)%4 == 0 {
			b.WriteByte('-')
		}
		b.WriteByte(c)
	}
	s, _, _ := strings.Cut(b.String(), "\x00") // printed with %s
	return s
}

// blkidUUID formats a binary UUID as libblkid does, or empty when it is all
// zero, which libblkid does not report.
func blkidUUID(id []byte) string {
	if bytes.Equal(id, make([]byte, len(id))) {
		return ""
	}
	return formatUUID(id)
}
