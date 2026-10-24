package btrfs

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json/v2"
	"hash/crc32"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestMixedGUID(t *testing.T) {
	want := [16]byte{0xAF, 0x3D, 0xC6, 0x0F, 0x83, 0x84, 0x72, 0x47, 0x8E, 0x79, 0x3D, 0x69, 0xD8, 0x47, 0x7D, 0xE4}
	if linuxFilesystemType != want {
		t.Fatalf("type GUID = % X", linuxFilesystemType)
	}
}

func TestPartitionPath(t *testing.T) {
	for device, want := range map[string]string{"/dev/sdb": "/dev/sdb3", "/dev/nvme0n1": "/dev/nvme0n1p3", "/dev/loop3": "/dev/loop3p3", "/dev/vdc": "/dev/vdc3"} {
		if got := partitionPath(device, 3); got != want {
			t.Errorf("%s: got %s, want %s", device, got, want)
		}
	}
}

func TestLargestFreeRegion(t *testing.T) {
	// 512-byte sectors: 1 MiB is 2048 LBAs, minPartition is 524288.
	tests := []struct {
		name        string
		first, last uint64
		ss          uint64
		used        [][2]uint64
		wantFirst   uint64
		wantLast    uint64
		wantOK      bool
	}{
		{"empty disk", 34, 2000000, 512, nil, 2048, 1998847, true},
		{"gap before", 34, 1999999, 512, [][2]uint64{{1000000, 1999999}}, 2048, 999423, true},
		{"gap after, unaligned neighbour", 34, 10000000, 512, [][2]uint64{{2048, 2100000}}, 2101248, 9998335, true},
		{"largest between", 34, 10000000, 512, [][2]uint64{{2048, 1000000}, {2000000, 2000100}, {8000000, 8000100}}, 2000896, 7999487, true},
		{"unsorted used", 34, 10000000, 512, [][2]uint64{{8000000, 8000100}, {2048, 1000000}, {2000000, 2000100}}, 2000896, 7999487, true},
		{"too small", 34, 1500000, 512, [][2]uint64{{2048, 1000000}}, 0, 0, false},
		{"full", 34, 1000000, 512, [][2]uint64{{34, 1000000}}, 0, 0, false},
		{"4k sectors", 6, 1000000, 4096, [][2]uint64{{256, 400000}}, 400128, 999935, true},
		{"used past usable area", 34, 1000000, 512, [][2]uint64{{2048, 1 << 63}}, 0, 0, false},
		{"unsupported sector size", 34, 1000000, 0, nil, 0, 0, false},
		// The disk from the field: EFI, md member, then 888 GiB unpartitioned.
		{"real host", 34, 2000409230, 512, [][2]uint64{{2048, 2099199}, {2099200, 136421375}}, 136421376, 2000408575, true},
	}
	for _, tt := range tests {
		first, last, ok := LargestFreeRegion(tt.first, tt.last, tt.used, tt.ss)
		if first != tt.wantFirst || last != tt.wantLast || ok != tt.wantOK {
			t.Errorf("%s: got %d-%d %v, want %d-%d %v", tt.name, first, last, ok, tt.wantFirst, tt.wantLast, tt.wantOK)
		}
	}
}

// writeTestDisk writes a sparse disk image whose primary GPT lists parts and
// whose backup lists backupParts (both [first, last] LBAs).
func writeTestDisk(t *testing.T, size uint64, parts, backupParts [][2]uint64) (string, gptLayout) {
	t.Helper()
	l := gptLayout{sectorSize: 512, sectors: size / 512, disk: randomGUID()}
	table := func(parts [][2]uint64) []byte {
		b := make([]byte, gptEntries*gptEntrySize)
		for i, p := range parts {
			e := b[i*gptEntrySize:]
			copy(e, linuxFilesystemType[:])
			guid := [16]byte{byte(i + 1)} // the same in both copies
			copy(e[16:], guid[:])
			binary.LittleEndian.PutUint64(e[32:], p[0])
			binary.LittleEndian.PutUint64(e[40:], p[1])
		}
		return b
	}
	device := filepath.Join(t.TempDir(), "sdb")
	f, err := os.Create(device)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err = f.Truncate(int64(size)); err != nil {
		t.Fatal(err)
	}
	last, es := l.sectors-1, l.entrySectors()
	primary, backup := table(parts), table(backupParts)
	mbr, _ := l.build()
	for off, b := range map[uint64][]byte{
		0:                 mbr[:512], // protective MBR
		512:               l.header(1, last, 2, crc32.ChecksumIEEE(primary)),
		2 * 512:           primary,
		(last - es) * 512: backup,
		last * 512:        l.header(last, 1, last-es, crc32.ChecksumIEEE(backup)),
	} {
		if _, err = f.WriteAt(b, int64(off)); err != nil {
			t.Fatal(err)
		}
	}
	return device, l
}

type kernelAdd struct {
	number        int
	start, length uint64
}

// stubAddPartition stubs the device seams for a 512-byte-sector disk and
// records addKernelPartition calls.
func stubAddPartition(t *testing.T, size uint64) *[]kernelAdd {
	t.Helper()
	oldSize, oldSS, oldAdd := blockDeviceSize, sectorSize, addKernelPartition
	var calls []kernelAdd
	blockDeviceSize = func(string) (uint64, error) { return size, nil }
	sectorSize = func(string) (uint64, error) { return 512, nil }
	addKernelPartition = func(_ string, number int, start, length uint64) error {
		calls = append(calls, kernelAdd{number, start, length})
		return nil
	}
	t.Cleanup(func() { blockDeviceSize, sectorSize, addKernelPartition = oldSize, oldSS, oldAdd })
	return &calls
}

// edgeBytes is how much of each end of a test disk the GPT occupies, rounded up.
const edgeBytes = 32 << 10

// diskEdges reads the first and last edgeBytes of device, where the GPT lives.
func diskEdges(t *testing.T, device string, size uint64) (head, tail []byte) {
	t.Helper()
	f, err := os.Open(device)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	head, tail = make([]byte, edgeBytes), make([]byte, edgeBytes)
	if _, err = f.ReadAt(head, 0); err == nil {
		_, err = f.ReadAt(tail, int64(size-edgeBytes))
	}
	if err != nil {
		t.Fatal(err)
	}
	return head, tail
}

// checkOnlyEntryChanged fails when anything but the third entry slot of the
// array at byte offset entries, or the two CRC fields of the header at
// offset header, differs between before and after, two slices from base.
func checkOnlyEntryChanged(t *testing.T, before, after []byte, base, header, entries uint64) {
	t.Helper()
	for i := range before {
		at := base + uint64(i)
		inEntry := at >= entries+2*gptEntrySize && at < entries+3*gptEntrySize
		inCRC := at >= header+16 && at < header+20 || at >= header+88 && at < header+92
		if before[i] != after[i] && !inEntry && !inCRC {
			t.Fatalf("byte at %d changed outside the new entry and the CRCs", at)
		}
	}
}

// flipBit toggles the low bit of the byte at off in f.
func flipBit(t *testing.T, f *os.File, off int64) {
	t.Helper()
	b := make([]byte, 1)
	if _, err := f.ReadAt(b, off); err != nil {
		t.Fatal(err)
	}
	b[0] ^= 1
	if _, err := f.WriteAt(b, off); err != nil {
		t.Fatal(err)
	}
}

func TestAddPartitionUsesLargestGap(t *testing.T) {
	const size = 700 << 20
	// Gaps: 256 MiB between the partitions, 440 MiB after the second.
	p1, p2 := [2]uint64{2048, 4095}, [2]uint64{528384, 530431}
	device, l := writeTestDisk(t, size, [][2]uint64{p1, p2}, [][2]uint64{p1, p2})
	calls := stubAddPartition(t, size)
	if err := os.WriteFile(device+"3", nil, 0o600); err != nil { // udev's node
		t.Fatal(err)
	}
	beforeHead, beforeTail := diskEdges(t, device, size)

	part, err := AddPartition(context.Background(), device)
	if err != nil || part != device+"3" {
		t.Fatalf("part=%q err=%v", part, err)
	}
	const wantFirst, wantLast = 530432, 1431551
	if want := []kernelAdd{{3, wantFirst * 512, (wantLast - wantFirst + 1) * 512}}; !slices.Equal(*calls, want) {
		t.Fatalf("kernel got %v, want %v", *calls, want)
	}

	afterHead, afterTail := diskEdges(t, device, size)
	es := l.entrySectors()
	checkOnlyEntryChanged(t, beforeHead, afterHead, 0, 512, 2*512)
	checkOnlyEntryChanged(t, beforeTail, afterTail, size-edgeBytes, size-512, size-(1+es)*512)
	img, err := os.Open(device)
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()
	primary, err := readGPTCopy(img, 512, 1, l.sectors-1)
	if err != nil {
		t.Fatal(err)
	}
	backup, err := readGPTCopy(img, 512, l.sectors-1, 1)
	if err != nil || !bytes.Equal(primary.entries, backup.entries) {
		t.Fatalf("copies differ: %v", err)
	}
	e := primary.entries[2*gptEntrySize:][:gptEntrySize]
	if [16]byte(e[:16]) != linuxFilesystemType || [16]byte(e[16:32]) == [16]byte{} ||
		binary.LittleEndian.Uint64(e[32:]) != wantFirst || binary.LittleEndian.Uint64(e[40:]) != wantLast ||
		binary.LittleEndian.Uint64(e[48:]) != 0 || string(e[56:56+2*len("linuxio")]) != "l\x00i\x00n\x00u\x00x\x00i\x00o\x00" {
		t.Fatalf("new entry: % X", e)
	}

	sfdisk, err := exec.LookPath("sfdisk")
	if err != nil {
		t.Log("sfdisk not installed; skipping the cross-check")
		return
	}
	out, err := exec.Command(sfdisk, "--json", device).Output()
	if err != nil {
		t.Fatalf("sfdisk: %v", err)
	}
	var table struct {
		PartitionTable struct {
			Partitions []struct {
				Start uint64 `json:"start"`
				Size  uint64 `json:"size"`
			} `json:"partitions"`
		} `json:"partitiontable"`
	}
	if err := json.Unmarshal(out, &table); err != nil {
		t.Fatal(err)
	}
	if got := table.PartitionTable.Partitions; len(got) != 3 || got[0].Start != p1[0] || got[1].Start != p2[0] ||
		got[2].Start != wantFirst || got[2].Size != wantLast-wantFirst+1 {
		t.Fatalf("sfdisk read %s", out)
	}
}

func TestAddPartitionRefuses(t *testing.T) {
	const size = 700 << 20
	p1 := [2]uint64{2048, 4095}
	whole := [2]uint64{2048, size/512 - 2 - 32 - 1}
	tests := []struct {
		name          string
		parts, backup [][2]uint64
		corrupt       int64 // byte to flip; negative counts from the disk end
		wantErr       string
	}{
		{"broken backup header", [][2]uint64{p1}, [][2]uint64{p1}, -512 + 56, "bad checksum"},
		{"broken primary header", [][2]uint64{p1}, [][2]uint64{p1}, 512 + 56, "bad checksum"},
		{"broken entries", [][2]uint64{p1}, [][2]uint64{p1}, 2*512 + 60, "bad checksum"},
		{"copies differ", [][2]uint64{p1}, nil, 0, "copies differ"},
		{"no free space", [][2]uint64{whole}, [][2]uint64{whole}, 0, "no free space of at least 256 MiB"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			device, _ := writeTestDisk(t, size, tt.parts, tt.backup)
			calls := stubAddPartition(t, size)
			if tt.corrupt != 0 {
				f, err := os.OpenFile(device, os.O_RDWR, 0)
				if err != nil {
					t.Fatal(err)
				}
				off := tt.corrupt
				if off < 0 {
					off += size
				}
				flipBit(t, f, off)
				f.Close()
			}
			beforeHead, beforeTail := diskEdges(t, device, size)
			if _, err := AddPartition(context.Background(), device); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want %q", err, tt.wantErr)
			}
			afterHead, afterTail := diskEdges(t, device, size)
			if !bytes.Equal(beforeHead, afterHead) || !bytes.Equal(beforeTail, afterTail) || len(*calls) != 0 {
				t.Fatalf("refused call changed the disk or told the kernel: %v", *calls)
			}
		})
	}
}

// checkGPT verifies the structure build wrote, reading n bytes at off.
func checkGPT(t *testing.T, read func(off, n uint64) []byte, l gptLayout) {
	t.Helper()
	ss := l.sectorSize
	if mbr := read(0, 512); mbr[510] != 0x55 || mbr[511] != 0xAA || mbr[446+4] != 0xEE {
		t.Fatal("protective MBR missing")
	}
	for _, lba := range []uint64{1, l.sectors - 1} {
		h := read(lba*ss, gptHeaderLen)
		if string(h[:8]) != "EFI PART" || binary.LittleEndian.Uint64(h[24:]) != lba {
			t.Fatalf("header at LBA %d: % X", lba, h[:32])
		}
		crc := binary.LittleEndian.Uint32(h[16:])
		check := append([]byte(nil), h...)
		binary.LittleEndian.PutUint32(check[16:], 0)
		if crc32.ChecksumIEEE(check) != crc {
			t.Fatalf("header CRC at LBA %d", lba)
		}
		start := binary.LittleEndian.Uint64(h[72:])
		entries := read(start*ss, gptEntries*gptEntrySize)
		if crc32.ChecksumIEEE(entries) != binary.LittleEndian.Uint32(h[88:]) {
			t.Fatalf("entries CRC at LBA %d", lba)
		}
		if binary.LittleEndian.Uint64(entries[32:]) != l.first || binary.LittleEndian.Uint64(entries[40:]) != l.last {
			t.Fatalf("partition bounds")
		}
	}
}

func TestGPTLayout(t *testing.T) {
	for _, ss := range []uint64{512, 4096} {
		size := uint64(10<<30) + 12345*ss
		l, err := newGPTLayout(size, ss)
		if err != nil {
			t.Fatal(err)
		}
		if l.first*ss != partitionAlign || (l.last+1)*ss%partitionAlign != 0 || size-(l.last+1)*ss < PartitionSlack {
			t.Fatalf("ss %d: partition %d-%d", ss, l.first, l.last)
		}
		head, tail := l.build()
		end := l.sectors * ss
		checkGPT(t, func(off, n uint64) []byte {
			if off < uint64(len(head)) {
				return head[off : off+n]
			}
			return tail[off-(end-uint64(len(tail))) : off-(end-uint64(len(tail)))+n]
		}, l)
	}
	// The slack is subtracted from unsigned sizes, so a disk at or near it
	// must be refused rather than wrap into a partition past the disk's end.
	for _, size := range []uint64{0, 100 << 20, PartitionSlack, PartitionSlack + 1<<20, PartitionSlack + 100<<20} {
		if l, err := newGPTLayout(size, 512); err == nil {
			t.Errorf("%d-byte disk accepted: partition %d-%d", size, l.first, l.last)
		}
	}
	l, err := newGPTLayout(PartitionSlack+partitionAlign+minPartition, 512)
	if err != nil || (l.last-l.first+1)*512 != minPartition {
		t.Fatalf("smallest disk: %+v %v", l, err)
	}
}

func TestPartitionDiskWritesTableAndWaitsForNode(t *testing.T) {
	dir := t.TempDir()
	device := filepath.Join(dir, "sdb")
	const size = 4 << 30
	f, err := os.Create(device)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(size); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err = os.WriteFile(device+"1", nil, 0o600); err != nil { // udev's node
		t.Fatal(err)
	}
	oldSize, oldSS, oldReread := blockDeviceSize, sectorSize, rereadPartitions
	blockDeviceSize = func(string) (uint64, error) { return size, nil }
	sectorSize = func(string) (uint64, error) { return 512, nil }
	reread := 0
	rereadPartitions = func(string) error { reread++; return nil }
	t.Cleanup(func() { blockDeviceSize, sectorSize, rereadPartitions = oldSize, oldSS, oldReread })

	part, err := PartitionDisk(context.Background(), device)
	if err != nil || part != device+"1" || reread != 1 {
		t.Fatalf("part=%q reread=%d err=%v", part, reread, err)
	}
	img, err := os.Open(device)
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()
	read := func(off, n uint64) []byte {
		b := make([]byte, n)
		if _, readErr := img.ReadAt(b, int64(off)); readErr != nil {
			t.Fatal(readErr)
		}
		return b
	}
	l := gptLayout{sectorSize: 512, sectors: size / 512}
	entry := read(2*512, gptEntrySize)
	l.first, l.last = binary.LittleEndian.Uint64(entry[32:]), binary.LittleEndian.Uint64(entry[40:])
	checkGPT(t, read, l)

	// util-linux, when present, must read the same table.
	sfdisk, err := exec.LookPath("sfdisk")
	if err != nil {
		t.Log("sfdisk not installed; skipping the cross-check")
		return
	}
	out, err := exec.Command(sfdisk, "--json", device).Output()
	if err != nil {
		t.Fatalf("sfdisk: %v", err)
	}
	var table struct {
		PartitionTable struct {
			Label      string `json:"label"`
			Partitions []struct {
				Start uint64 `json:"start"`
				Size  uint64 `json:"size"`
				Type  string `json:"type"`
			} `json:"partitions"`
		} `json:"partitiontable"`
	}
	if err := json.Unmarshal(out, &table); err != nil {
		t.Fatal(err)
	}
	pt := table.PartitionTable
	if pt.Label != "gpt" || len(pt.Partitions) != 1 || pt.Partitions[0].Start != l.first ||
		pt.Partitions[0].Size != l.last-l.first+1 || pt.Partitions[0].Type != "0FC63DAF-8483-4772-8E79-3D69D8477DE4" {
		t.Fatalf("sfdisk read %s", out)
	}
}
