package btrfs

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json/v2"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"golang.org/x/sys/unix"
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

func TestFreeRegions(t *testing.T) {
	// 512-byte sectors: 1 MiB is 2048 LBAs.
	tests := []struct {
		name         string
		first, last  uint64
		ss, minBytes uint64
		used         [][2]uint64
		want         [][2]uint64
	}{
		{"empty disk", 34, 2000000, 512, 1 << 20, nil, [][2]uint64{{2048, 1998847}}},
		{"two gaps in disk order", 34, 10000000, 512, 1 << 20, [][2]uint64{{2000000, 2000100}, {2048, 1000000}}, [][2]uint64{{1001472, 1998847}, {2000896, 9998335}}},
		{"gap under minBytes dropped", 34, 10000000, 512, 256 << 20, [][2]uint64{{2048, 1000000}, {1010000, 1020000}}, [][2]uint64{{1021952, 9998335}}},
		{"gap of exactly minBytes kept", 34, 2000000, 512, 1 << 20, [][2]uint64{{2048, 4095}, {6144, 2000000}}, [][2]uint64{{4096, 6143}}},
		{"unaligned neighbours trimmed", 34, 2999999, 512, 1 << 20, [][2]uint64{{2048, 2100000}}, [][2]uint64{{2101248, 2998271}}},
		{"4k sectors", 6, 1000000, 4096, 1 << 20, [][2]uint64{{256, 400000}}, [][2]uint64{{400128, 999935}}},
		{"unsupported sector size", 34, 1000000, 0, 1 << 20, nil, nil},
	}
	for _, tt := range tests {
		if got := FreeRegions(tt.first, tt.last, tt.used, tt.ss, tt.minBytes); !slices.Equal(got, tt.want) {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
		}
	}
}

// writeTestDisk writes a sparse disk image with ss-byte sectors whose primary
// GPT lists parts and whose backup lists backupParts (both [first, last] LBAs).
func writeTestDisk(t *testing.T, size, ss uint64, parts, backupParts [][2]uint64) (string, gptLayout) {
	t.Helper()
	l := gptLayout{sectorSize: ss, sectors: size / ss, disk: randomGUID()}
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
		0:                mbr[:ss], // protective MBR
		ss:               l.header(1, last, 2, crc32.ChecksumIEEE(primary)),
		2 * ss:           primary,
		(last - es) * ss: backup,
		last * ss:        l.header(last, 1, last-es, crc32.ChecksumIEEE(backup)),
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

// diskCalls records what the partition functions asked of a stubbed disk.
type diskCalls struct {
	mu    sync.Mutex // the stubs may run on several goroutines
	adds  []kernelAdd
	wiped []string
}

// stubDisk stubs the device seams for a disk of size bytes with ss-byte
// sectors and records addKernelPartition and wipeDeviceEdges calls.
func stubDisk(t *testing.T, size, ss uint64) *diskCalls {
	t.Helper()
	oldSize, oldSS, oldAdd, oldWipe := blockDeviceSize, sectorSize, addKernelPartition, wipeDeviceEdges
	c := &diskCalls{}
	blockDeviceSize = func(string) (uint64, error) { return size, nil }
	sectorSize = func(string) (uint64, error) { return ss, nil }
	addKernelPartition = func(_ string, number int, start, length uint64) error {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.adds = append(c.adds, kernelAdd{number, start, length})
		return nil
	}
	wipeDeviceEdges = func(device string) error {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.wiped = append(c.wiped, device)
		return nil
	}
	t.Cleanup(func() {
		blockDeviceSize, sectorSize, addKernelPartition, wipeDeviceEdges = oldSize, oldSS, oldAdd, oldWipe
	})
	return c
}

// stubDelKernelPartition replaces delKernelPartition with del.
func stubDelKernelPartition(t *testing.T, del func(device string, number int) error) {
	t.Helper()
	old := delKernelPartition
	delKernelPartition = del
	t.Cleanup(func() { delKernelPartition = old })
}

// readCopies reads both GPT copies of the image at device and returns the
// primary, failing unless both validate and list the same entries.
func readCopies(t *testing.T, device string, ss, sectors uint64) gptCopy {
	t.Helper()
	img, err := os.Open(device)
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()
	primary, err := readGPTCopy(img, ss, 1, sectors-1)
	if err != nil {
		t.Fatal(err)
	}
	backup, err := readGPTCopy(img, ss, sectors-1, 1)
	if err != nil || !bytes.Equal(primary.entries, backup.entries) {
		t.Fatalf("copies differ: %v", err)
	}
	return primary
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

// checkOnlyEntryChanged fails when anything but entry slot of the array at
// byte offset entries, or the two CRC fields of the header at offset header,
// differs between before and after, two slices from base.
func checkOnlyEntryChanged(t *testing.T, before, after []byte, base, header, entries uint64, slot int) {
	t.Helper()
	entry := entries + uint64(slot)*gptEntrySize
	for i := range before {
		at := base + uint64(i)
		inEntry := at >= entry && at < entry+gptEntrySize
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
	device, l := writeTestDisk(t, size, 512, [][2]uint64{p1, p2}, [][2]uint64{p1, p2})
	calls := stubDisk(t, size, 512)
	if err := os.WriteFile(device+"3", nil, 0o600); err != nil { // udev's node
		t.Fatal(err)
	}
	beforeHead, beforeTail := diskEdges(t, device, size)

	part, err := AddPartition(context.Background(), device)
	if err != nil || part != device+"3" {
		t.Fatalf("part=%q err=%v", part, err)
	}
	const wantFirst, wantLast = 530432, 1431551
	if want := []kernelAdd{{3, wantFirst * 512, (wantLast - wantFirst + 1) * 512}}; !slices.Equal(calls.adds, want) {
		t.Fatalf("kernel got %v, want %v", calls.adds, want)
	}
	if !slices.Equal(calls.wiped, []string{part}) {
		t.Fatalf("wiped %v, want %s", calls.wiped, part)
	}

	afterHead, afterTail := diskEdges(t, device, size)
	es := l.entrySectors()
	checkOnlyEntryChanged(t, beforeHead, afterHead, 0, 512, 2*512, 2)
	checkOnlyEntryChanged(t, beforeTail, afterTail, size-edgeBytes, size-512, size-(1+es)*512, 2)
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
			device, _ := writeTestDisk(t, size, 512, tt.parts, tt.backup)
			calls := stubDisk(t, size, 512)
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
			if !bytes.Equal(beforeHead, afterHead) || !bytes.Equal(beforeTail, afterTail) || len(calls.adds) != 0 || len(calls.wiped) != 0 {
				t.Fatalf("refused call changed the disk or told the kernel: %v %v", calls.adds, calls.wiped)
			}
		})
	}
}

func TestReadTablesRefusesAUsableAreaOverTheHeaders(t *testing.T) {
	const ss, sectors = 512, 4096 // a 2 MiB disk
	stubDisk(t, sectors*ss, ss)
	l := gptLayout{sectorSize: ss, sectors: sectors}
	es, last := l.entrySectors(), uint64(sectors-1)
	crc := crc32.ChecksumIEEE(make([]byte, gptEntries*gptEntrySize))
	// Crafted, but each copy passes its own checks: CRCs match and the entry
	// arrays lie outside the usable area.
	for name, tt := range map[string]struct{ first, last, primaryEntries, backupEntries uint64 }{
		"starts on the primary header": {1, last - 2*es - 1, last - 2*es, last - es},
		"ends on the backup header":    {2 + 2*es, last, 2, 2 + es},
	} {
		img := make([]byte, sectors*ss)
		for _, c := range [][3]uint64{{1, last, tt.primaryEntries}, {last, 1, tt.backupEntries}} {
			h := l.header(c[0], c[1], c[2], crc)
			binary.LittleEndian.PutUint64(h[40:], tt.first)
			binary.LittleEndian.PutUint64(h[48:], tt.last)
			binary.LittleEndian.PutUint32(h[16:], 0)
			binary.LittleEndian.PutUint32(h[16:], crc32.ChecksumIEEE(h[:gptHeaderLen]))
			copy(img[c[0]*ss:], h)
		}
		if _, _, _, _, err := readTables(bytes.NewReader(img), "sdb"); err == nil || !strings.Contains(err.Error(), "does not fit between its headers") {
			t.Errorf("%s: err = %v", name, err)
		}
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

func TestCreatePartitionAtRange(t *testing.T) {
	for _, tt := range []struct {
		name string
		ss   uint64
	}{{"512 sectors", 512}, {"4k sectors", 4096}} {
		t.Run(tt.name, func(t *testing.T) {
			const size = 4 << 30
			ss := tt.ss
			p1 := [2]uint64{(1 << 20) / ss, (1<<30)/ss - 1}
			device, l := writeTestDisk(t, size, ss, [][2]uint64{p1}, [][2]uint64{p1})
			calls := stubDisk(t, size, ss)
			if err := os.WriteFile(device+"2", nil, 0o600); err != nil { // udev's node
				t.Fatal(err)
			}
			beforeHead, beforeTail := diskEdges(t, device, size)

			part, err := CreatePartition(context.Background(), device, 2<<30+1, 512<<20+7)
			if err != nil || part != partitionPath(device, 2) {
				t.Fatalf("part=%q err=%v", part, err)
			}
			// Rounded inward: from the next MiB boundary to the last one inside.
			wantFirst, wantLast := (2<<30+1<<20)/ss, (2<<30+512<<20)/ss-1
			if want := []kernelAdd{{2, wantFirst * ss, (wantLast - wantFirst + 1) * ss}}; !slices.Equal(calls.adds, want) {
				t.Fatalf("kernel got %v, want %v", calls.adds, want)
			}
			if !slices.Equal(calls.wiped, []string{part}) {
				t.Fatalf("wiped %v, want %s", calls.wiped, part)
			}
			afterHead, afterTail := diskEdges(t, device, size)
			checkOnlyEntryChanged(t, beforeHead, afterHead, 0, ss, 2*ss, 1)
			checkOnlyEntryChanged(t, beforeTail, afterTail, size-edgeBytes, size-ss, size-(1+l.entrySectors())*ss, 1)
			e := readCopies(t, device, ss, l.sectors).entries[gptEntrySize:][:gptEntrySize]
			if [16]byte(e[:16]) != linuxFilesystemType || [16]byte(e[16:32]) == [16]byte{} ||
				binary.LittleEndian.Uint64(e[32:]) != wantFirst || binary.LittleEndian.Uint64(e[40:]) != wantLast {
				t.Fatalf("new entry: % X", e)
			}
		})
	}
}

func TestCreatePartitionRefuses(t *testing.T) {
	const size = 4 << 30
	p1 := [2]uint64{2048, 2097151} // 1 MiB to 1 GiB
	full := make([][2]uint64, gptEntries)
	for i := range full {
		full[i] = [2]uint64{uint64(i+1) * 2048, uint64(i+2)*2048 - 1}
	}
	tests := []struct {
		name          string
		parts, backup [][2]uint64
		start, length uint64
		wantErr       string
	}{
		{"overlaps the existing partition", [][2]uint64{p1}, [][2]uint64{p1}, 512 << 20, 1 << 30, "not free space"},
		{"ends past lastUsable", [][2]uint64{p1}, [][2]uint64{p1}, 3 << 30, 1 << 30, "not free space"},
		{"under 1 MiB after rounding", [][2]uint64{p1}, [][2]uint64{p1}, 2<<30 + 1, 1 << 20, "at least 1 MiB"},
		{"copies differ", [][2]uint64{p1}, nil, 2 << 30, 1 << 30, "copies differ; repair it first"},
		{"no free entry", full, full, 2 << 30, 1 << 30, "no free entry"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			device, _ := writeTestDisk(t, size, 512, tt.parts, tt.backup)
			calls := stubDisk(t, size, 512)
			beforeHead, beforeTail := diskEdges(t, device, size)
			if _, err := CreatePartition(context.Background(), device, tt.start, tt.length); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want %q", err, tt.wantErr)
			}
			afterHead, afterTail := diskEdges(t, device, size)
			if !bytes.Equal(beforeHead, afterHead) || !bytes.Equal(beforeTail, afterTail) || len(calls.adds) != 0 || len(calls.wiped) != 0 {
				t.Fatalf("refused call changed the disk or told the kernel: %v %v", calls.adds, calls.wiped)
			}
		})
	}
}

func TestCreatePartitionClearsAnEntryTheKernelRefuses(t *testing.T) {
	const size = 700 << 20
	p1 := [2]uint64{2048, 4095}
	device, _ := writeTestDisk(t, size, 512, [][2]uint64{p1}, [][2]uint64{p1})
	calls := stubDisk(t, size, 512)
	// A stale kernel partition overlapping the new one makes the kernel refuse it.
	addKernelPartition = func(_ string, number int, _, _ uint64) error {
		return fmt.Errorf("add partition %d: %w", number, unix.EBUSY)
	}
	beforeHead, beforeTail := diskEdges(t, device, size)

	if _, err := CreatePartition(context.Background(), device, 2<<20, 256<<20); !errors.Is(err, unix.EBUSY) {
		t.Fatalf("err = %v, want EBUSY", err)
	}
	afterHead, afterTail := diskEdges(t, device, size)
	if !bytes.Equal(beforeHead, afterHead) || !bytes.Equal(beforeTail, afterTail) || len(calls.wiped) != 0 {
		t.Fatalf("the table kept the refused partition, or it was wiped: %v", calls.wiped)
	}
}

func TestConcurrentCreatePartitionsKeepBoth(t *testing.T) {
	const size = 4 << 30
	// Free regions: 1 GiB to 2 GiB, and 3 GiB to the end.
	p1, p2 := [2]uint64{2048, 2097151}, [2]uint64{4194304, 6291455}
	device, l := writeTestDisk(t, size, 512, [][2]uint64{p1, p2}, [][2]uint64{p1, p2})
	stubDisk(t, size, 512)
	for _, n := range []int{3, 4} {
		if err := os.WriteFile(partitionPath(device, n), nil, 0o600); err != nil { // udev's nodes
			t.Fatal(err)
		}
	}
	want := [][2]uint64{{2097152, 4194303}, {6291456, 7340031}}
	errs := make([]error, len(want))
	var wg sync.WaitGroup
	for i, r := range want {
		wg.Go(func() { _, errs[i] = CreatePartition(context.Background(), device, r[0]*512, (r[1]-r[0]+1)*512) })
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		t.Fatal(err)
	}
	c := readCopies(t, device, 512, l.sectors)
	var got [][2]uint64
	for slot := 2; slot < 4; slot++ {
		e := c.entries[slot*gptEntrySize:]
		got = append(got, [2]uint64{binary.LittleEndian.Uint64(e[32:]), binary.LittleEndian.Uint64(e[40:])})
	}
	if !slices.Contains(got, want[0]) || !slices.Contains(got, want[1]) {
		t.Fatalf("slots 3 and 4 hold %v, want %v", got, want)
	}
}

func TestDeletePartition(t *testing.T) {
	const size = 700 << 20
	p1, p2 := [2]uint64{2048, 4095}, [2]uint64{528384, 530431}
	device, l := writeTestDisk(t, size, 512, [][2]uint64{p1, p2}, [][2]uint64{p1, p2})
	fakeSysfs(t, "sdb", size, [][2]uint64{p1, p2})
	calls := stubDisk(t, size, 512)
	beforeHead, beforeTail := diskEdges(t, device, size)
	var dropped []int
	stubDelKernelPartition(t, func(_ string, number int) error {
		// The kernel lets go of the partition before the table changes.
		if head, tail := diskEdges(t, device, size); !bytes.Equal(head, beforeHead) || !bytes.Equal(tail, beforeTail) {
			t.Error("table changed before the kernel dropped the partition")
		}
		dropped = append(dropped, number)
		return nil
	})

	if err := DeletePartition(context.Background(), device, 1); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(dropped, []int{1}) || len(calls.adds) != 0 || len(calls.wiped) != 0 {
		t.Fatalf("dropped %v, re-added %v, wiped %v", dropped, calls.adds, calls.wiped)
	}
	afterHead, afterTail := diskEdges(t, device, size)
	checkOnlyEntryChanged(t, beforeHead, afterHead, 0, 512, 2*512, 0)
	checkOnlyEntryChanged(t, beforeTail, afterTail, size-edgeBytes, size-512, size-(1+l.entrySectors())*512, 0)
	if e := readCopies(t, device, 512, l.sectors).entries[:gptEntrySize]; !bytes.Equal(e, make([]byte, gptEntrySize)) {
		t.Fatalf("entry 1 survived: % X", e)
	}
}

// deleteWhileBusy deletes partition 1 of a test disk whose kernel answers
// the first busy drop requests with EBUSY, and returns how many it got and
// DeletePartition's error. A refused delete must leave the disk alone and a
// done one must clear the entry.
func deleteWhileBusy(t *testing.T, ctx context.Context, busy int) (int, error) {
	t.Helper()
	const size = 700 << 20
	p1 := [2]uint64{2048, 4095}
	device, l := writeTestDisk(t, size, 512, [][2]uint64{p1}, [][2]uint64{p1})
	fakeSysfs(t, "sdb", size, [][2]uint64{p1})
	calls := stubDisk(t, size, 512)
	beforeHead, beforeTail := diskEdges(t, device, size)
	n := 0
	stubDelKernelPartition(t, func(string, int) error {
		n++
		if n <= busy {
			return fmt.Errorf("delete partition 1: %w", unix.EBUSY)
		}
		return nil
	})

	err := DeletePartition(ctx, device, 1)
	if err == nil {
		if e := readCopies(t, device, 512, l.sectors).entries[:gptEntrySize]; !bytes.Equal(e, make([]byte, gptEntrySize)) {
			t.Fatalf("entry 1 survived: % X", e)
		}
		return n, nil
	}
	afterHead, afterTail := diskEdges(t, device, size)
	if !bytes.Equal(beforeHead, afterHead) || !bytes.Equal(beforeTail, afterTail) || len(calls.adds) != 0 {
		t.Fatalf("refused delete changed the disk or re-added %v", calls.adds)
	}
	return n, err
}

func TestDeletePartitionBusy(t *testing.T) {
	if n, err := deleteWhileBusy(t, context.Background(), 99); n != 10 || !errors.Is(err, unix.EBUSY) {
		t.Fatalf("always busy: %d calls, err %v", n, err)
	}
	// A probe holding the partition for a moment does not stop the delete.
	if n, err := deleteWhileBusy(t, context.Background(), 2); n != 3 || err != nil {
		t.Fatalf("busy twice: %d calls, err %v", n, err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if n, err := deleteWhileBusy(t, cancelled, 99); n != 1 || !errors.Is(err, unix.EBUSY) || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled: %d calls, err %v", n, err)
	}
}

func TestDeletePartitionReaddsWhenTheWriteFails(t *testing.T) {
	const size = 64 << 20
	p1 := [2]uint64{2048, 4095}
	image, _ := writeTestDisk(t, size, 512, [][2]uint64{p1}, [][2]uint64{p1})
	head, tail := diskEdges(t, image, size)
	// A memfd sealed against writes stands in for a disk that starts failing them.
	fd, err := unix.MemfdCreate("disk", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
	if err != nil {
		t.Fatal(err)
	}
	disk := os.NewFile(uintptr(fd), "disk")
	defer disk.Close()
	if err = disk.Truncate(size); err == nil {
		if _, err = disk.WriteAt(head, 0); err == nil {
			_, err = disk.WriteAt(tail, size-edgeBytes)
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	device := fmt.Sprintf("/proc/self/fd/%d", fd)
	fakeSysfs(t, filepath.Base(device), size, [][2]uint64{p1})
	calls := stubDisk(t, size, 512)
	stubDelKernelPartition(t, func(string, int) error {
		_, sealErr := unix.FcntlInt(disk.Fd(), unix.F_ADD_SEALS, unix.F_SEAL_WRITE)
		return sealErr
	})

	if err = DeletePartition(context.Background(), device, 1); !errors.Is(err, unix.EPERM) {
		t.Fatalf("err = %v, want the failed write", err)
	}
	if want := []kernelAdd{{1, p1[0] * 512, (p1[1] - p1[0] + 1) * 512}}; !slices.Equal(calls.adds, want) {
		t.Fatalf("re-added %v, want %v", calls.adds, want)
	}
	if afterHead, afterTail := diskEdges(t, device, size); !bytes.Equal(afterHead, head) || !bytes.Equal(afterTail, tail) {
		t.Fatal("the table changed")
	}
}

func TestDeletePartitionRefusesAStaleKernelTable(t *testing.T) {
	const size = 700 << 20
	p1 := [2]uint64{2048, 4095}
	// After an out-of-band edit the kernel has not re-read, its partition 1
	// is not the one the table lists.
	for name, kernel := range map[string][][2]uint64{
		"moved":   {{6144, 8191}},
		"resized": {{2048, 6143}},
		"unknown": nil,
	} {
		t.Run(name, func(t *testing.T) {
			device, _ := writeTestDisk(t, size, 512, [][2]uint64{p1}, [][2]uint64{p1})
			fakeSysfs(t, "sdb", size, kernel)
			calls := stubDisk(t, size, 512)
			stubDelKernelPartition(t, func(string, int) error { t.Error("dropped a partition"); return nil })
			beforeHead, beforeTail := diskEdges(t, device, size)

			if err := DeletePartition(context.Background(), device, 1); !errors.Is(err, ErrStaleTable) {
				t.Fatalf("err = %v, want ErrStaleTable", err)
			}
			afterHead, afterTail := diskEdges(t, device, size)
			if !bytes.Equal(beforeHead, afterHead) || !bytes.Equal(beforeTail, afterTail) || len(calls.adds) != 0 {
				t.Fatalf("refused delete changed the disk or re-added %v", calls.adds)
			}
		})
	}
}

// initDisk is a stubbed disk at a path named nvme0n1, with the kernel's
// sysfs view of its partitions.
type initDisk struct {
	device  string
	sectors uint64
	calls   *diskCalls
	reread  int
}

// fakeSysfs points sysBlockDir at a fake sysfs tree in which the kernel
// knows parts (512-byte LBAs), numbered from 1, as the partitions of the disk
// name of size bytes. As in /sys/class/block, each partition is also listed
// beside the disk.
func fakeSysfs(t *testing.T, name string, size uint64, parts [][2]uint64) {
	t.Helper()
	sys := t.TempDir()
	disk := filepath.Join(sys, name)
	// The disk's own attributes and attribute groups are not partitions.
	if err := os.MkdirAll(filepath.Join(disk, "queue"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(disk, "size"), fmt.Appendf(nil, "%d\n", size/512), 0o644); err != nil {
		t.Fatal(err)
	}
	for i, p := range parts {
		dir := filepath.Join(disk, partitionPath(name, i+1))
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(dir, filepath.Join(sys, partitionPath(name, i+1))); err != nil {
			t.Fatal(err)
		}
		// sysfs counts 512-byte units whatever the logical sector size.
		for attr, v := range map[string]uint64{"partition": uint64(i + 1), "start": p[0], "size": p[1] - p[0] + 1} {
			if err := os.WriteFile(filepath.Join(dir, attr), fmt.Appendf(nil, "%d\n", v), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	old := sysBlockDir
	sysBlockDir = sys
	t.Cleanup(func() { sysBlockDir = old })
}

func newInitDisk(t *testing.T, size uint64, parts [][2]uint64) *initDisk {
	t.Helper()
	fakeSysfs(t, "nvme0n1", size, parts)
	image, l := writeTestDisk(t, size, 512, parts, parts)
	d := &initDisk{device: filepath.Join(t.TempDir(), "nvme0n1"), sectors: l.sectors, calls: stubDisk(t, size, 512)}
	if err := os.Symlink(image, d.device); err != nil {
		t.Fatal(err)
	}
	oldReread := rereadPartitions
	rereadPartitions = func(string) error { d.reread++; return nil }
	t.Cleanup(func() { rereadPartitions = oldReread })
	return d
}

func TestInitializeDisk(t *testing.T) {
	d := newInitDisk(t, 700<<20, [][2]uint64{{2048, 4095}, {528384, 530431}})
	var dropped []int
	stubDelKernelPartition(t, func(_ string, number int) error {
		dropped = append(dropped, number)
		return nil
	})

	if err := InitializeDisk(context.Background(), d.device); err != nil {
		t.Fatal(err)
	}
	if d.reread != 1 || !slices.Equal(dropped, []int{1, 2}) || !slices.Equal(d.calls.wiped, []string{d.device}) {
		t.Fatalf("reread=%d dropped=%v wiped=%v", d.reread, dropped, d.calls.wiped)
	}
	if c := readCopies(t, d.device, 512, d.sectors); !bytes.Equal(c.entries, make([]byte, len(c.entries))) {
		t.Fatal("partitions survived")
	}
}

func TestInitializeDiskBusy(t *testing.T) {
	const size = 700 << 20
	p1 := [2]uint64{2048, 4095}
	d := newInitDisk(t, size, [][2]uint64{p1, {528384, 530431}})
	stubDelKernelPartition(t, func(_ string, number int) error {
		if number == 2 {
			return unix.EBUSY
		}
		return nil
	})
	beforeHead, beforeTail := diskEdges(t, d.device, size)

	err := InitializeDisk(context.Background(), d.device)
	// p1 was dropped before p2 refused, so the kernel gets it back.
	if want := []kernelAdd{{1, p1[0] * 512, (p1[1] - p1[0] + 1) * 512}}; !errors.Is(err, unix.EBUSY) || !slices.Equal(d.calls.adds, want) {
		t.Fatalf("err=%v re-added %v, want %v", err, d.calls.adds, want)
	}
	afterHead, afterTail := diskEdges(t, d.device, size)
	if !bytes.Equal(beforeHead, afterHead) || !bytes.Equal(beforeTail, afterTail) || d.reread != 0 || len(d.calls.wiped) != 0 {
		t.Fatalf("refused initialize touched the disk: reread=%d wiped=%v", d.reread, d.calls.wiped)
	}
}

func TestInitializeDiskReaddsWhenTheWriteFails(t *testing.T) {
	const size = 700 << 20
	parts := [][2]uint64{{2048, 4095}, {528384, 530431}}
	d := newInitDisk(t, size, parts)
	stubDelKernelPartition(t, func(string, int) error { return nil })
	wipeDeviceEdges = func(string) error { return unix.EIO } // the table write's first step
	beforeHead, beforeTail := diskEdges(t, d.device, size)

	err := InitializeDisk(context.Background(), d.device)
	want := []kernelAdd{{1, 2048 * 512, 2048 * 512}, {2, 528384 * 512, 2048 * 512}}
	if !errors.Is(err, unix.EIO) || !slices.Equal(d.calls.adds, want) || d.reread != 0 {
		t.Fatalf("err=%v re-added %v reread=%d, want %v", err, d.calls.adds, d.reread, want)
	}
	if afterHead, afterTail := diskEdges(t, d.device, size); !bytes.Equal(beforeHead, afterHead) || !bytes.Equal(beforeTail, afterTail) {
		t.Fatal("the table changed")
	}

	// A re-read that fails after the new table is written leaves the kernel
	// without partitions, as the new table is: nothing is re-added.
	d = newInitDisk(t, size, parts)
	rereadPartitions = func(string) error { return unix.EBUSY }
	if err = InitializeDisk(context.Background(), d.device); !errors.Is(err, unix.EBUSY) || len(d.calls.adds) != 0 {
		t.Fatalf("failed re-read: err=%v re-added %v", err, d.calls.adds)
	}
	if c := readCopies(t, d.device, 512, d.sectors); !bytes.Equal(c.entries, make([]byte, len(c.entries))) {
		t.Fatal("partitions survived")
	}
}

func TestTableWritersRefuseAPartition(t *testing.T) {
	const size = 4 << 30
	calls := stubDisk(t, size, 512)
	fakeSysfs(t, "sdb", size, [][2]uint64{{2048, 4095}})
	stubDelKernelPartition(t, func(string, int) error { t.Error("dropped a partition"); return nil })
	part := filepath.Join(t.TempDir(), "sdb1")

	if err := InitializeDisk(context.Background(), part); err == nil || !strings.Contains(err.Error(), "not a whole disk") {
		t.Errorf("initialize: err = %v", err)
	}
	if _, err := PartitionDisk(context.Background(), part); err == nil || !strings.Contains(err.Error(), "not a whole disk") {
		t.Errorf("partition: err = %v", err)
	}
	if len(calls.wiped) != 0 {
		t.Fatalf("wiped %v", calls.wiped)
	}
}

func TestInitializeDiskRefusesUnder2MiB(t *testing.T) {
	stubDisk(t, 2<<20-512, 512)
	stubDelKernelPartition(t, func(string, int) error { t.Error("dropped a partition"); return nil })
	if err := InitializeDisk(context.Background(), filepath.Join(t.TempDir(), "nvme0n1")); err == nil || !strings.Contains(err.Error(), "too small") {
		t.Fatalf("err = %v", err)
	}
}
