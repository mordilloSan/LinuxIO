package btrfs

import (
	"context"
	"encoding/binary"
	"encoding/json/v2"
	"hash/crc32"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestMixedGUID(t *testing.T) {
	want := [16]byte{0xAF, 0x3D, 0xC6, 0x0F, 0x83, 0x84, 0x72, 0x47, 0x8E, 0x79, 0x3D, 0x69, 0xD8, 0x47, 0x7D, 0xE4}
	if linuxFilesystemType != want {
		t.Fatalf("type GUID = % X", linuxFilesystemType)
	}
}

func TestPartitionPath(t *testing.T) {
	for device, want := range map[string]string{"/dev/sdb": "/dev/sdb1", "/dev/nvme0n1": "/dev/nvme0n1p1", "/dev/loop3": "/dev/loop3p1", "/dev/vdc": "/dev/vdc1"} {
		if got := partitionPath(device); got != want {
			t.Errorf("%s: got %s, want %s", device, got, want)
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
