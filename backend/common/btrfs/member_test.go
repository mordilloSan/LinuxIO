package btrfs

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// md1Superblock is an md 1.x superblock of the array uuid, stored at off
// bytes into the device, as mdadm writes it and the kernel's super_1_load
// takes it: maxDev slots, each holding a role, a 1 MiB data area (1 MiB in
// for 1.1 and 1.2, before the superblock for 1.0 near the end), and the
// checksum of calc_sb_1_csum. libblkid 2.39 (Ubuntu 24.04) zeroes sb_csum
// in the buffer it sums before comparing the sum with sb_csum, and misses
// the superblock when that is the buffer it read sb_csum from: with no
// slots, which mdadm and the kernel never write, and for 1.1 with 128 or
// fewer, which fit the 512 bytes it already holds at the start of the
// device. A fixture checked against blkid has neither.
func md1Superblock(off uint64, uuid [16]byte, maxDev uint32) []byte {
	le := binary.LittleEndian
	sb := make([]byte, 256+2*maxDev)
	le.PutUint32(sb[0:], mdMagic)
	le.PutUint32(sb[4:], 1)
	copy(sb[16:], uuid[:])
	copy(sb[32:], "host:data")
	if off <= 0x1000 {
		le.PutUint64(sb[128:], 2048) // data_offset, in sectors
	}
	le.PutUint64(sb[136:], 2048) // data_size: the kernel wants 10 sectors or more
	le.PutUint64(sb[144:], off>>9)
	le.PutUint32(sb[220:], maxDev)
	for i := range maxDev {
		le.PutUint16(sb[256+2*i:], uint16(i+1))
	}
	setMD1Checksum(sb)
	return sb
}

// md1Sum is the sum calc_sb_1_csum (mdadm super1.c and the kernel's md.c)
// folds: the little-endian words of sb but its sb_csum, and a last odd slot
// as 16 bits.
func md1Sum(sb []byte) uint64 {
	le := binary.LittleEndian
	sum := -uint64(le.Uint32(sb[216:]))
	i := 0
	for ; i+4 <= len(sb); i += 4 {
		sum += uint64(le.Uint32(sb[i:]))
	}
	if i < len(sb) {
		sum += uint64(le.Uint16(sb[i:]))
	}
	return sum
}

// setMD1Checksum writes the sb_csum of calc_sb_1_csum, which folds the sum
// once and keeps 32 bits of it.
func setMD1Checksum(sb []byte) {
	sum := md1Sum(sb)
	binary.LittleEndian.PutUint32(sb[216:], uint32(sum)+uint32(sum>>32))
}

// md090Superblock is an md 0.90 superblock in byte order order, whose
// words set_uuid0 to set_uuid3 are uuid and whose members hold arrayKiB.
func md090Superblock(order binary.ByteOrder, uuid [4]uint32, arrayKiB uint32) []byte {
	sb := make([]byte, 64)
	order.PutUint32(sb[0:], mdMagic)
	order.PutUint32(sb[8:], 90) // minor version
	order.PutUint32(sb[20:], uuid[0])
	order.PutUint32(sb[32:], arrayKiB)
	order.PutUint32(sb[52:], uuid[1])
	order.PutUint32(sb[56:], uuid[2])
	order.PutUint32(sb[60:], uuid[3])
	return sb
}

// lvm2Label is the LVM2 label pvcreate writes in sector of a physical
// volume with pvUUID (32 characters), its CRC computed as lvm.c does.
func lvm2Label(sector uint64, pvUUID string) []byte {
	le := binary.LittleEndian
	l := make([]byte, 512)
	copy(l, "LABELONE")
	le.PutUint64(l[8:], sector)
	le.PutUint32(l[20:], 32) // offset_xl: the PV header follows the label header
	copy(l[24:], "LVM2 001")
	copy(l[32:], pvUUID)
	tab := [16]uint32{
		0x00000000, 0x1db71064, 0x3b6e20c8, 0x26d930ac, 0x76dc4190, 0x6b6b51f4, 0x4db26158, 0x5005713c,
		0xedb88320, 0xf00f9344, 0xd6d6a3e8, 0xcb61b38c, 0x9b64c2b0, 0x86d3d2d4, 0xa00ae278, 0xbdbdf21c,
	}
	crc := uint32(0xf597a6cf)
	for _, b := range l[20:] {
		crc ^= uint32(b)
		crc = crc>>4 ^ tab[crc&0xf]
		crc = crc>>4 ^ tab[crc&0xf]
	}
	le.PutUint32(l[16:], crc)
	return l
}

func writeAt(t *testing.T, image string, off uint64, b []byte) {
	t.Helper()
	f, err := os.OpenFile(image, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err = f.WriteAt(b, int64(off)); err != nil {
		t.Fatal(err)
	}
}

// blkidMember is what libblkid, probing image directly as udev does,
// reports it a member of: "md" or "lvm" and the UUID, or empty.
func blkidMember(t *testing.T, blkid, image string) (kind, uuid string) {
	t.Helper()
	out, err := exec.Command(blkid, "-p", "-o", "export", image).Output()
	if err != nil && len(out) == 0 {
		return "", "" // exit status 2: nothing found
	}
	props := map[string]string{}
	for s := bufio.NewScanner(bytes.NewReader(out)); s.Scan(); {
		k, v, _ := strings.Cut(s.Text(), "=")
		props[k] = v
	}
	switch props["TYPE"] {
	case "linux_raid_member":
		return "md", props["UUID"]
	case "LVM2_member":
		return "lvm", props["UUID"]
	}
	return "", ""
}

// memberImage is a sparse image file of size bytes.
func memberImage(t *testing.T, size uint64) string {
	t.Helper()
	image := filepath.Join(t.TempDir(), "disk.img")
	if err := os.WriteFile(image, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(image, int64(size)); err != nil {
		t.Fatal(err)
	}
	return image
}

func TestReadMemberSignature(t *testing.T) {
	const size = 64 << 20
	uuid := [16]byte{0x0f, 0x1e, 0x2d, 0x3c, 0x4b, 0x5a, 0x69, 0x78, 0x87, 0x96, 0xa5, 0xb4, 0xc3, 0xd2, 0xe1, 0xf0}
	const uuidText = "0f1e2d3c-4b5a-6978-8796-a5b4c3d2e1f0"
	words := [4]uint32{0x0f1e2d3c, 0x4b5a6978, 0x8796a5b4, 0xc3d2e1f0}
	const pvUUID = "Ab3dEf6hIj9kLm2nOp5qRs8tUv1wXy4z"
	const pvText = "Ab3dEf-6hIj-9kLm-2nOp-5qRs-8tUv-1wXy4z"
	end090 := uint64(size&^(64<<10-1) - 64<<10)
	end10 := uint64(size&^0xfff - 0x2000)
	badCsum := md1Superblock(0x1000, uuid, 1)
	badCsum[216]++
	badCRC := lvm2Label(1, pvUUID)
	badCRC[100]++
	part := kernelPartition{number: 1, start: 1 << 20, length: size - 1<<20}

	cases := []struct {
		name       string
		size       uint64
		write      func(t *testing.T, image string)
		parts      []kernelPartition // the kernel's partitions, for a whole disk
		kind, uuid string
	}{
		{"blank", size, func(*testing.T, string) {}, nil, "", ""},
		{"md 1.2 at 4 KiB", size, func(t *testing.T, img string) { writeAt(t, img, 0x1000, md1Superblock(0x1000, uuid, 1)) }, nil, "md", uuidText},
		// 384 slots, as mdadm writes for 129 to 384 devices; libblkid 2.39
		// misses a 1.1 superblock of 128 or fewer (md1Superblock).
		{"md 1.1 at the start", size, func(t *testing.T, img string) { writeAt(t, img, 0, md1Superblock(0, uuid, 384)) }, nil, "md", uuidText},
		// The checksum adds the last of an odd number of slots as 16 bits.
		{"md 1.2 with an odd number of slots", size, func(t *testing.T, img string) {
			writeAt(t, img, 0x1000, md1Superblock(0x1000, uuid, 3))
		}, nil, "md", uuidText},
		{"md 1.0 at the end", size, func(t *testing.T, img string) { writeAt(t, img, end10, md1Superblock(end10, uuid, 1)) }, nil, "md", uuidText},
		// mdadm prints these words as 0f1e2d3c:4b5a6978:...; libblkid
		// prints every word of a little-endian 0.90 superblock high byte
		// first, so both read the same digits.
		{"md 0.90 little-endian", size, func(t *testing.T, img string) {
			writeAt(t, img, end090, md090Superblock(binary.LittleEndian, words, (size-128<<10)>>10))
		}, nil, "md", uuidText},
		{"md 0.90 big-endian", size, func(t *testing.T, img string) {
			writeAt(t, img, end090, md090Superblock(binary.BigEndian, words, (size-128<<10)>>10))
		}, nil, "md", uuidText},
		{"md 0.90 whose array does not fit before it", size, func(t *testing.T, img string) {
			writeAt(t, img, end090, md090Superblock(binary.LittleEndian, words, size>>10))
		}, nil, "", ""},
		{"md 1.2 with a bad checksum", size, func(t *testing.T, img string) { writeAt(t, img, 0x1000, badCsum) }, nil, "", ""},
		{"md 1.2 naming another offset", size, func(t *testing.T, img string) { writeAt(t, img, 0x1000, md1Superblock(0, uuid, 1)) }, nil, "", ""},
		{"md on a device libblkid takes for a floppy", 1440 << 10, func(t *testing.T, img string) {
			writeAt(t, img, 0x1000, md1Superblock(0x1000, uuid, 1))
		}, nil, "", ""},
		{"LVM2 label in sector 1", size, func(t *testing.T, img string) { writeAt(t, img, 512, lvm2Label(1, pvUUID)) }, nil, "lvm", pvText},
		{"LVM2 label in sector 3", size, func(t *testing.T, img string) { writeAt(t, img, 1536, lvm2Label(3, pvUUID)) }, nil, "lvm", pvText},
		{"LVM2 label naming another sector", size, func(t *testing.T, img string) { writeAt(t, img, 512, lvm2Label(2, pvUUID)) }, nil, "", ""},
		{"LVM2 label with a bad CRC", size, func(t *testing.T, img string) { writeAt(t, img, 512, badCRC) }, nil, "", ""},
		{"md before LVM2", size, func(t *testing.T, img string) {
			writeAt(t, img, 512, lvm2Label(1, pvUUID))
			writeAt(t, img, end10, md1Superblock(end10, uuid, 1))
		}, nil, "md", uuidText},
		// The whole disk's last 64 KiB are also its last partition's, whose
		// 0.90 superblock libblkid's blkid_probe_is_covered_by_pt gives to
		// the partition alone.
		{"md 0.90 of the disk's last partition", size, func(t *testing.T, img string) {
			writeAt(t, img, end090, md090Superblock(binary.LittleEndian, words, uint32((part.length-128<<10)>>10)))
		}, []kernelPartition{part}, "", ""},
	}
	blkid, blkidErr := exec.LookPath("blkid")
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			image := memberImage(t, c.size)
			c.write(t, image)
			f, err := os.Open(image)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			sig, err := readMemberSignature(f, c.size, c.parts)
			if err != nil || sig.Kind != c.kind || sig.UUID != c.uuid {
				t.Fatalf("read %+v, %v; want %q %q", sig, err, c.kind, c.uuid)
			}
			// util-linux, when present, must read the same membership. It
			// takes partitions from the table, not the kernel, so a whole
			// disk's case has no cross-check.
			if blkidErr != nil || c.parts != nil {
				t.Log("blkid not installed or partitions from the kernel; skipping the cross-check")
				return
			}
			if kind, uuid := blkidMember(t, blkid, image); kind != c.kind || uuid != c.uuid {
				t.Fatalf("blkid read %q %q; want %q %q", kind, uuid, c.kind, c.uuid)
			}
		})
	}
}

// TestReadMemberSignatureKernelTakes covers md 1.x superblocks the kernel's
// super_1_load takes and some libblkid misses, which the read finds all the
// same, with no cross-check against blkid: one with no slots, a spare to the
// kernel, and a 1.1 one with the 128 slots mdadm writes for up to 128
// devices, both of which libblkid 2.39 misses, and one whose checksum fold
// carries, which the kernel and mdadm keep to 32 bits and libblkid compares
// whole.
func TestReadMemberSignatureKernelTakes(t *testing.T) {
	const size = 64 << 20
	uuid := [16]byte{0x0f, 0x1e, 0x2d, 0x3c}
	carry := md1Superblock(0x1000, uuid, 1)
	// ctime's low word brings the low half of the sum to 0xffffffff, so
	// adding the high half carries out of 32 bits.
	binary.LittleEndian.PutUint32(carry[64:], ^uint32(md1Sum(carry)))
	if sum := md1Sum(carry); uint64(uint32(sum))+sum>>32 <= 0xffffffff {
		t.Fatalf("the fixture's fold does not carry out of 32 bits: sum %#x", sum)
	}
	setMD1Checksum(carry)
	for _, c := range []struct {
		name string
		off  uint64
		sb   []byte
	}{
		{"no slots", 0x1000, md1Superblock(0x1000, uuid, 0)},
		{"md 1.1 with 128 slots", 0, md1Superblock(0, uuid, 128)},
		{"checksum fold carries", 0x1000, carry},
	} {
		t.Run(c.name, func(t *testing.T) {
			image := memberImage(t, size)
			writeAt(t, image, c.off, c.sb)
			f, err := os.Open(image)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			if sig, err := readMemberSignature(f, size, nil); err != nil || sig.Kind != "md" || sig.UUID != "0f1e2d3c-0000-0000-0000-000000000000" {
				t.Fatalf("read %+v, %v; want md", sig, err)
			}
		})
	}
}

// holeReader reads r but fails, as a bad sector does, wherever a read
// touches one of holes, byte ranges [start, end).
type holeReader struct {
	r     io.ReaderAt
	holes [][2]int64
}

func (h holeReader) ReadAt(b []byte, off int64) (int, error) {
	for _, hole := range h.holes {
		if off < hole[1] && off+int64(len(b)) > hole[0] {
			return 0, syscall.EIO
		}
	}
	return h.r.ReadAt(b, off)
}

// TestReadMemberSignatureSkipsUnreadablePlaces covers a failing disk: a
// place that cannot be read holds no signature anyone can use, so the read
// goes on to the next place and reports the first error beside what it
// found, instead of failing.
func TestReadMemberSignatureSkipsUnreadablePlaces(t *testing.T) {
	const size = 64 << 20
	uuid := [16]byte{0x0f, 0x1e, 0x2d, 0x3c}
	end := [2]int64{size - 64<<10, size} // md 0.90 and 1.0
	start := [2]int64{0, 8 << 10}        // md 1.1 and 1.2, LVM2
	cases := []struct {
		name  string
		write func(t *testing.T, image string)
		holes [][2]int64
		kind  string
	}{
		{"md 1.2 behind an unreadable end", func(t *testing.T, img string) {
			writeAt(t, img, 0x1000, md1Superblock(0x1000, uuid, 1))
		}, [][2]int64{end}, "md"},
		{"LVM2 behind an unreadable end", func(t *testing.T, img string) {
			writeAt(t, img, 512, lvm2Label(1, "Ab3dEf6hIj9kLm2nOp5qRs8tUv1wXy4z"))
		}, [][2]int64{end}, "lvm"},
		{"nothing readable", func(*testing.T, string) {}, [][2]int64{start, end}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			image := memberImage(t, size)
			c.write(t, image)
			f, err := os.Open(image)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			sig, unreadable := readMemberSignature(holeReader{f, c.holes}, size, nil)
			if sig.Kind != c.kind || !errors.Is(unreadable, syscall.EIO) {
				t.Fatalf("read %+v, unreadable %v; want %q and EIO", sig, unreadable, c.kind)
			}
		})
	}
}
