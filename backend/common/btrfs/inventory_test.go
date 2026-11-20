package btrfs

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestListFilesystemsReadsKernel(t *testing.T) {
	k := raid1Kernel()
	k.devices[2].stats = [5]uint64{5, 0, 0, 0, 1}
	k.devices[2].physical = 200 << 30 // replaced by a bigger disk, not grown yet
	k.devices[1].uuid, k.devices[2].uuid = devUUID(1), devUUID(2)
	stubKernel(t, k)

	got, err := ListFilesystems(context.Background())
	if err != nil || len(got) != 1 {
		t.Fatalf("got %+v err=%v", got, err)
	}
	fs := got[0]
	wantDevices := []BtrfsDevice{
		{ID: 1, Path: "/dev/sda1", SizeBytes: 100 << 30, UsedBytes: 51 << 30, PhysicalBytes: 100 << 30, UUID: devUUID(1)},
		{ID: 2, Path: "/dev/sdb1", SizeBytes: 100 << 30, UsedBytes: 51 << 30, PhysicalBytes: 200 << 30, Growable: true, Errors: BtrfsDeviceErrors{Write: 5, Generation: 1}, UUID: devUUID(2)},
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

// goneLink turns the kernel's link to member name of the stubbed pool into
// one whose block device was deleted: the link stays, its target does not.
func goneLink(t *testing.T, name string) {
	t.Helper()
	link := filepath.Join(sysFSBtrfs, dataUUID, "devices", name)
	if err := os.RemoveAll(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../../../devices/virtual/block/"+name, link); err != nil {
		t.Fatal(err)
	}
}

// kernelMissing marks member id of the stubbed pool missing in sysfs, as
// the kernel does since 7.1 for a member whose block device went away
// (btrfs_remove_bdev), and has DEV_INFO name it "<missing disk>".
func kernelMissing(t *testing.T, k *fakeKernel, id uint64) {
	t.Helper()
	k.devices[id].path = missingDiskName
	devinfo := filepath.Join(sysFSBtrfs, dataUUID, "devinfo", strconv.FormatUint(id, 10))
	if err := os.MkdirAll(devinfo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(devinfo, "missing"), []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// resolveTo has path resolve to target, as a link udev made.
func resolveTo(t *testing.T, path, target string) {
	old := evalSymlinks
	evalSymlinks = func(p string) (string, error) {
		if p == path {
			return target, nil
		}
		return old(p)
	}
	t.Cleanup(func() { evalSymlinks = old })
}

// notBusy has the exclusive open of node answer err, and every other one
// EBUSY.
func notBusy(node string, err error) {
	deviceNotBusy = func(device string) error {
		if device == node {
			return err
		}
		return unix.EBUSY
	}
}

// A kernel without btrfs_remove_bdev keeps a member whose block device went
// away while the pool is mounted, with its path, and calls it neither
// missing nor unwriteable. It counts as missing, and as dropped out, when
// the kernel's link to the block device it opened names a device that is
// gone, or one nothing holds, which is not the one the pool opened: the
// same disk back under its old name, or another disk.
func TestListFilesystemsDiskThatDroppedOut(t *testing.T) {
	errs := BtrfsDeviceErrors{Write: 9, Flush: 2}
	present := BtrfsDevice{ID: 1, Path: "/dev/sda1", SizeBytes: 100 << 30, UsedBytes: 51 << 30, PhysicalBytes: 100 << 30, UUID: devUUID(1)}
	held := BtrfsDevice{ID: 2, Path: "/dev/sdb1", SizeBytes: 100 << 30, UsedBytes: 51 << 30, PhysicalBytes: 200 << 30, Growable: true, Errors: errs, UUID: devUUID(2)}
	dropped := BtrfsDevice{ID: 2, Path: "/dev/sdb1", SizeBytes: 100 << 30, UsedBytes: 51 << 30, Missing: true, DroppedOut: true, Errors: errs, UUID: devUUID(2)}
	named := func(d BtrfsDevice, path string) BtrfsDevice {
		d.Path = path
		return d
	}
	for _, tc := range []struct {
		name    string
		stage   func(t *testing.T, k *fakeKernel)
		want    []BtrfsDevice
		unknown bool
	}{
		{name: "the pool holds every member", stage: func(*testing.T, *fakeKernel) {}, want: []BtrfsDevice{present, held}},
		{name: "its block device is gone", stage: func(t *testing.T, _ *fakeKernel) { goneLink(t, "sdb1") }, want: []BtrfsDevice{present, dropped}},
		{
			// The kernel's link reaches a device again, but nothing holds it.
			name:  "a device the pool does not hold has its name",
			stage: func(*testing.T, *fakeKernel) { notBusy("/dev/sdb1", nil) },
			want:  []BtrfsDevice{present, dropped},
		},
		{
			name:    "its device cannot be asked",
			stage:   func(*testing.T, *fakeKernel) { notBusy("/dev/sdb1", unix.EIO) },
			want:    []BtrfsDevice{present, held},
			unknown: true,
		},
		{
			// A bridge of a session without sudo may not open the device,
			// which tells nothing and is no fault.
			name:  "the reader may not open its device",
			stage: func(*testing.T, *fakeKernel) { notBusy("/dev/sdb1", unix.EACCES) },
			want:  []BtrfsDevice{present, held},
		},
		{
			// 6.8 keeps the path a mount was given, and udev points the link
			// at the disk left once the other is gone.
			name: "it is named by a link to the disk left",
			stage: func(t *testing.T, k *fakeKernel) {
				k.devices[2].path = "/dev/disk/by-uuid/" + dataUUID
				goneLink(t, "sdb1")
				resolveTo(t, "/dev/disk/by-uuid/"+dataUUID, "/dev/sda1")
			},
			want: []BtrfsDevice{present, named(dropped, "/dev/disk/by-uuid/"+dataUUID)},
		},
		{
			// A root the kernel mounted without an initramfs is named
			// /dev/root, which /dev has no node of.
			name:  "it is named by a path no link of the kernel's matches",
			stage: func(_ *testing.T, k *fakeKernel) { k.devices[2].path = "/dev/root" },
			want:  []BtrfsDevice{present, named(held, "/dev/root")},
		},
		{
			// Since 7.1 the kernel marks the member missing itself
			// (btrfs_remove_bdev), and DEV_INFO names it "<missing disk>".
			name: "the kernel marked it missing",
			stage: func(t *testing.T, k *fakeKernel) {
				kernelMissing(t, k, 2)
				goneLink(t, "sdb1")
			},
			want: []BtrfsDevice{present, {ID: 2, SizeBytes: 100 << 30, UsedBytes: 51 << 30, Missing: true, Errors: errs, UUID: devUUID(2)}},
		},
		{
			// The gone link is the missing member's, not that of a member no
			// link matches.
			name: "the kernel marked one missing and no link matches the other",
			stage: func(t *testing.T, k *fakeKernel) {
				kernelMissing(t, k, 2)
				goneLink(t, "sdb1")
				k.devices[1].path = "/dev/root"
			},
			want: []BtrfsDevice{named(present, "/dev/root"), {ID: 2, SizeBytes: 100 << 30, UsedBytes: 51 << 30, Missing: true, Errors: errs, UUID: devUUID(2)}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k := raid1Kernel()
			k.devices[2].stats = [5]uint64{9, 0, 2, 0, 0}
			k.devices[2].physical = 200 << 30
			k.devices[1].uuid, k.devices[2].uuid = devUUID(1), devUUID(2)
			stubKernel(t, k)
			tc.stage(t, k)
			fs, err := FindFilesystem(context.Background(), dataUUID)
			missing := slices.ContainsFunc(tc.want, func(d BtrfsDevice) bool { return d.Missing })
			if err != nil || !reflect.DeepEqual(fs.Devices, tc.want) || fs.Degraded != missing || fs.HealthKnown == tc.unknown {
				t.Fatalf("devices = %+v, degraded = %v, health known = %v, %v\nwant %+v", fs.Devices, fs.Degraded, fs.HealthKnown, err, tc.want)
			}
		})
	}
}

// The pool removes a member's link before it lets the member's device go,
// as the member leaves the pool or the pool is unmounted: a device found
// gone counts only while its link is still there.
func TestLinkGoneOnlyWhileTheLinkIsThere(t *testing.T) {
	dir := t.TempDir()
	dangling, live := filepath.Join(dir, "sdb"), filepath.Join(dir, "sdc")
	if err := os.Symlink(filepath.Join(dir, "deleted"), dangling); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(live, 0o755); err != nil {
		t.Fatal(err)
	}
	old := deviceNotBusy
	t.Cleanup(func() { deviceNotBusy = old })
	// The pool lets the device go, removing the link first, while it is
	// asked.
	deviceNotBusy = func(string) error { return os.Remove(live) }
	for _, tc := range []struct {
		link string
		want bool
	}{{dangling, true}, {filepath.Join(dir, "removed"), false}, {live, false}} {
		if gone, err := linkGone(tc.link, "/dev/"+filepath.Base(tc.link)); err != nil || gone != tc.want {
			t.Errorf("linkGone(%s) = %v, %v; want %v", filepath.Base(tc.link), gone, err, tc.want)
		}
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
	saveScrubRecord(context.Background(), dataUUID, scrubRecord{Status: "running", StartedAt: &started})
	got, err := ListFilesystems(context.Background())
	if err != nil || len(got) != 1 || got[0].Scrub.Status != "aborted" {
		t.Fatalf("got=%+v err=%v; want an aborted scrub", got, err)
	}
}

// A running scrub on record is merged into the kernel's progress only while
// the process that wrote it lives: a record a crash left, or an older
// build's job host still scrubbing, gives only the start, which the banner
// keys the scrub on. A live one is running even while the kernel reports
// nothing, between calls or waiting for a thaw.
func TestScrubStatusMergesOnlyALiveRecord(t *testing.T) {
	k := raid1Kernel()
	stubKernel(t, k)
	started := time.Now().Add(-time.Hour)
	stale := scrubRecord{Status: "running", StartedAt: &started, BytesScrubbed: 20 << 30, UncorrectableErrors: 3}
	saveScrubRecord(context.Background(), dataUUID, stale)
	k.setRunning(1, &scrubProgress{DataBytesScrubbed: 1 << 30})
	fs, err := FindFilesystem(context.Background(), dataUUID)
	if err != nil || fs.Scrub.Status != "running" || fs.Scrub.BytesScrubbed != 1<<30 || fs.Scrub.UncorrectableErrors != 0 || !fs.Scrub.StartedAt.Equal(started) {
		t.Fatalf("scrub = %+v err=%v, want the kernel's progress and the record's start", fs.Scrub, err)
	}
	live := stale
	live.writtenByThisProcess()
	saveScrubRecord(context.Background(), dataUUID, live)
	if fs, err = FindFilesystem(context.Background(), dataUUID); err != nil || fs.Scrub.BytesScrubbed != 21<<30 || fs.Scrub.UncorrectableErrors != 3 || !fs.Scrub.StartedAt.Equal(started) {
		t.Fatalf("scrub = %+v err=%v, want the record's counters added", fs.Scrub, err)
	}
	k.setRunning(1, nil)
	if fs, err = FindFilesystem(context.Background(), dataUUID); err != nil || fs.Scrub.Status != "running" || fs.Scrub.BytesScrubbed != 20<<30 {
		t.Fatalf("scrub = %+v err=%v, want the record, still running", fs.Scrub, err)
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
		if _, known := profiles[a.Profile]; !known {
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
