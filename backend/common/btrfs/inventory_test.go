package btrfs

import (
	"context"
	"reflect"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestListFilesystemsReadsKernel(t *testing.T) {
	k := raid1Kernel()
	k.devices[2].stats = [5]uint64{5, 0, 0, 0, 1}
	k.devices[2].physical = 200 << 30 // replaced by a bigger disk, not grown yet
	stubKernel(t, k)

	got, err := ListFilesystems(context.Background())
	if err != nil || len(got) != 1 {
		t.Fatalf("got %+v err=%v", got, err)
	}
	fs := got[0]
	wantDevices := []BtrfsDevice{
		{ID: 1, Path: "/dev/sda1", SizeBytes: 100 << 30, UsedBytes: 51 << 30, PhysicalBytes: 100 << 30},
		{ID: 2, Path: "/dev/sdb1", SizeBytes: 100 << 30, UsedBytes: 51 << 30, PhysicalBytes: 200 << 30, Growable: true, Errors: BtrfsDeviceErrors{Write: 5, Generation: 1}},
	}
	if fs.Label != "data" || !reflect.DeepEqual(fs.Devices, wantDevices) {
		t.Fatalf("label=%q devices=%+v", fs.Label, fs.Devices)
	}
	wantAlloc := []BtrfsAllocation{
		{Kind: "data", Profile: "raid1", TotalBytes: 50 << 30, UsedBytes: 40 << 30},
		{Kind: "metadata", Profile: "raid1", TotalBytes: 1 << 30, UsedBytes: 512 << 20},
		{Kind: "system", Profile: "raid1", TotalBytes: 32 << 20, UsedBytes: 16 << 10},
	}
	if !reflect.DeepEqual(fs.Allocation, wantAlloc) {
		t.Fatalf("allocation = %+v", fs.Allocation)
	}
	// Raw: every raid1 byte is stored twice.
	if fs.TotalBytes != 200<<30 || fs.UnallocatedBytes != 98<<30 || fs.UsedBytes != 2*(40<<30+512<<20+16<<10) {
		t.Fatalf("total/unallocated/used = %d/%d/%d", fs.TotalBytes, fs.UnallocatedBytes, fs.UsedBytes)
	}
	if fs.Degraded || !fs.HealthKnown || fs.LowMetadataSpace || fs.Scrub.Status != "none" {
		t.Fatalf("degraded=%v known=%v low=%v scrub=%+v", fs.Degraded, fs.HealthKnown, fs.LowMetadataSpace, fs.Scrub)
	}
}

func TestListFilesystemsMissingDevice(t *testing.T) {
	k := raid1Kernel()
	k.devices[2].path = ""
	k.devices[2].stats = [5]uint64{5, 0, 0, 0, 0}
	stubKernel(t, k)

	got, err := ListFilesystems(context.Background())
	if err != nil || len(got) != 1 {
		t.Fatalf("got %+v err=%v", got, err)
	}
	fs := got[0]
	if !fs.Degraded || len(fs.Devices) != 2 || !fs.Devices[1].Missing || fs.Devices[1].Errors.Write != 5 || fs.Devices[1].PhysicalBytes != 0 {
		t.Fatalf("degraded=%v devices=%+v", fs.Degraded, fs.Devices)
	}
	// The missing device's size still counts; its slot is skipped by scrub progress.
	if fs.TotalBytes != 200<<30 || fs.Scrub.Status != "none" {
		t.Fatalf("total=%d scrub=%+v", fs.TotalBytes, fs.Scrub)
	}
}

func TestListFilesystemsHealthUnknownWhenStatsFail(t *testing.T) {
	k := raid1Kernel()
	k.fail = map[uintptr]error{iocGetDevStats: unix.EIO}
	stubKernel(t, k)
	got, err := ListFilesystems(context.Background())
	if err != nil || len(got) != 1 || got[0].HealthKnown || len(got[0].Devices) != 2 {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}

func TestListFilesystemsToleratesFailure(t *testing.T) {
	k := raid1Kernel()
	k.fail = map[uintptr]error{iocFSInfo: unix.EPERM}
	stubKernel(t, k)
	stubMounts(t, []Mount{
		{Mountpoint: "/data", UUID: dataUUID, SubvolID: 5},
		{Mountpoint: "/data/sub", UUID: dataUUID, SubvolID: 256},
		{Mountpoint: "/", UUID: rootUUID, SubvolID: 256},
	})
	got, err := ListFilesystems(context.Background())
	if err != nil || len(got) != 2 || got[0].UUID != dataUUID || got[1].UUID != rootUUID {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	if fs := got[0]; len(fs.Mountpoints) != 2 || fs.HealthKnown || len(fs.Devices) != 0 || fs.Scrub.Status != "none" {
		t.Fatalf("filesystem after failed fs info = %+v", fs)
	}
}

func TestScrubStatusRunning(t *testing.T) {
	k := raid1Kernel()
	k.devices[1].running = &scrubProgress{DataBytesScrubbed: 10 << 30, CsumErrors: 2, UncorrectableErrors: 1, CorrectedErrors: 1}
	stubKernel(t, k)
	got, err := ListFilesystems(context.Background())
	if err != nil || len(got) != 1 {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	s := got[0].Scrub
	if s.Status != "running" || s.BytesScrubbed != 10<<30 || s.ErrorCount != 2 || s.UncorrectableErrors != 1 || s.CorrectedErrors != 1 || s.Percent <= 12 || s.Percent >= 13 {
		t.Fatalf("scrub = %+v", s)
	}
}

// A crash mid-scrub leaves its record at "running"; with no device
// scrubbing it reads as aborted, so Scrub is offered again and a resync is
// not held back forever.
func TestScrubStatusInterruptedRecord(t *testing.T) {
	stubKernel(t, raid1Kernel())
	started := time.Now().Add(-time.Hour)
	saveScrubRecord(dataUUID, BtrfsScrubStatus{Status: "running", StartedAt: &started})
	got, err := ListFilesystems(context.Background())
	if err != nil || len(got) != 1 || got[0].Scrub.Status != "aborted" {
		t.Fatalf("got=%+v err=%v; want an aborted scrub", got, err)
	}
}

func TestLowMetadataSpace(t *testing.T) {
	meta := func(used, total uint64) []BtrfsAllocation {
		return []BtrfsAllocation{{Kind: "data", TotalBytes: 100, UsedBytes: 100}, {Kind: "metadata", TotalBytes: total, UsedBytes: used}}
	}
	cases := []struct {
		name        string
		alloc       []BtrfsAllocation
		unallocated uint64
		want        bool
	}{
		{"metadata full, nothing left", meta(95, 100), 0, true},
		{"metadata full but space to grow", meta(95, 100), 2 << 30, false},
		{"metadata fine", meta(50, 100), 0, false},
		{"mixed block groups", []BtrfsAllocation{{Kind: "data+metadata", TotalBytes: 100, UsedBytes: 91}}, 0, true},
		{"only data full", []BtrfsAllocation{{Kind: "data", TotalBytes: 100, UsedBytes: 100}}, 0, false},
	}
	for _, c := range cases {
		if got := lowMetadataSpace(c.alloc, c.unallocated); got != c.want {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
}

// TestInventoryOnHost reads every mounted btrfs filesystem through the real
// kernel. Mount listing and the info, stats and space ioctls need no
// privilege, so this checks the struct layouts on any host with btrfs.
func TestInventoryOnHost(t *testing.T) {
	mounts, err := ListMounts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(mounts) == 0 {
		t.Skip("no btrfs filesystem mounted")
	}
	for _, m := range mounts {
		if m.UUID == "" || m.Device == "" {
			t.Fatalf("mount without uuid or device: %+v", m)
		}
	}
	filesystems, err := ListFilesystems(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, fs := range filesystems {
		checkHostFilesystem(t, fs)
		t.Logf("%s %q: %d devices, %d/%d bytes, %+v", fs.UUID, fs.Label, len(fs.Devices), fs.UsedBytes, fs.TotalBytes, fs.Allocation)
	}
}

// checkHostFilesystem asserts what any healthy mounted filesystem shows.
func checkHostFilesystem(t *testing.T, fs BtrfsFilesystem) {
	t.Helper()
	if len(fs.Devices) == 0 || fs.TotalBytes == 0 || fs.UsedBytes == 0 {
		t.Fatalf("%s: devices=%+v total=%d used=%d", fs.UUID, fs.Devices, fs.TotalBytes, fs.UsedBytes)
	}
	kinds := map[string]bool{}
	for _, a := range fs.Allocation {
		if MinDevices(a.Profile) == 0 && a.Profile != "raid5" && a.Profile != "raid6" {
			t.Fatalf("%s: unknown profile in %+v", fs.UUID, a)
		}
		kinds[a.Kind] = true
	}
	if !(kinds["data"] && kinds["metadata"]) && !kinds["data+metadata"] {
		t.Fatalf("%s: allocation %+v", fs.UUID, fs.Allocation)
	}
	for _, d := range fs.Devices {
		if !d.Missing && (d.Path == "" || d.SizeBytes == 0) {
			t.Fatalf("%s: device %+v", fs.UUID, d)
		}
	}
}
