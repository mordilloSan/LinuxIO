package storage

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
)

func TestCheckPoolCreate(t *testing.T) {
	devices := []apischema.StorageBlockDevice{
		{Path: "/dev/sdb", Type: "disk", Mountpoints: []string{}},
		{Path: "/dev/sdc", Type: "disk", Mountpoints: []string{}},
		{Path: "/dev/sdd", Type: "disk", FSType: "ext4", Mountpoints: []string{}},
		{Path: "/dev/sde", Type: "disk", FSType: "btrfs", Mountpoints: []string{"/data"}},
		{Path: "/dev/nvme0n1", Type: "disk", FreeBytes: 888 << 30, Mountpoints: []string{}},
		{Path: "/dev/nvme0n1p1", Type: "part", FreeBytes: 1 << 30, Parents: []string{"/dev/nvme0n1"}, Mountpoints: []string{"/boot/efi"}},
		{Path: "/dev/nvme1n1", Type: "disk", Mountpoints: []string{}},
		{Path: "/dev/sdh", Type: "disk", FSType: "btrfs", UUID: "o1", Label: "old", Mountpoints: []string{}},
		{Path: "/dev/sdl", Type: "disk", FSType: "linux_raid_member", UUID: "a1", Mountpoints: []string{}},
		{Path: "/dev/sdn", Type: "disk", FreeBytes: 8 << 30, InFstab: true, Mountpoints: []string{}},
	}
	inv := partitionInventory{
		devices: devices,
		fss:     []btrfs.BtrfsFilesystem{{UUID: testBtrfsUUID, Devices: []btrfs.BtrfsDevice{{ID: 1, Path: "/dev/sde"}}}},
		offline: []btrfs.BtrfsOfflinePool{{UUID: "o1", Label: "old", Devices: []string{"/dev/sdh"}}},
	}
	fstab := map[string]fstabEntry{"/srv": {source: "UUID=x", fstype: "ext4"}}
	ok := apischema.BtrfsPoolCreateRequest{Label: "tank", Mountpoint: "/mnt/tank", DataProfile: "raid1", MetadataProfile: "raid1", Devices: []string{"/dev/sdb", "/dev/sdc"}}
	with := func(edit func(*apischema.BtrfsPoolCreateRequest)) apischema.BtrfsPoolCreateRequest {
		req := ok
		req.Devices = append([]string(nil), ok.Devices...)
		edit(&req)
		return req
	}
	cases := []struct {
		name    string
		req     apischema.BtrfsPoolCreateRequest
		wantErr string
	}{
		{"ok", ok, ""},
		{"single disk", with(func(r *apischema.BtrfsPoolCreateRequest) {
			r.Devices, r.DataProfile, r.MetadataProfile = []string{"/dev/sdb"}, "single", "dup"
		}), ""},
		{"force wipes signature", with(func(r *apischema.BtrfsPoolCreateRequest) { r.Devices[1], r.Force = "/dev/sdd", true }), ""},
		{"free space only", with(func(r *apischema.BtrfsPoolCreateRequest) {
			r.Devices, r.FreeSpace, r.DataProfile, r.MetadataProfile = nil, []string{"/dev/nvme0n1"}, "single", "dup"
		}), ""},
		{"devices and free space", with(func(r *apischema.BtrfsPoolCreateRequest) {
			r.Devices, r.FreeSpace = []string{"/dev/sdb"}, []string{"/dev/nvme0n1"}
		}), ""},
		{"free space counts for raid1", with(func(r *apischema.BtrfsPoolCreateRequest) { r.Devices, r.FreeSpace = nil, []string{"/dev/nvme0n1"} }), "needs at least 2 devices"},
		{"free space in devices", with(func(r *apischema.BtrfsPoolCreateRequest) { r.FreeSpace = []string{"/dev/sdb"} }), "listed twice"},
		{"duplicate free space", with(func(r *apischema.BtrfsPoolCreateRequest) { r.FreeSpace = []string{"/dev/nvme0n1", "/dev/nvme0n1"} }), "listed twice"},
		{"free space unknown disk", with(func(r *apischema.BtrfsPoolCreateRequest) { r.FreeSpace = []string{"/dev/nvme9n1"} }), "not found"},
		{"free space disk without room", with(func(r *apischema.BtrfsPoolCreateRequest) { r.FreeSpace = []string{"/dev/nvme1n1"} }), "no free space"},
		{"free space on a partition", with(func(r *apischema.BtrfsPoolCreateRequest) { r.FreeSpace = []string{"/dev/nvme0n1p1"} }), "no free space"},
		{"bad label", with(func(r *apischema.BtrfsPoolCreateRequest) { r.Label = "a/b" }), "label"},
		{"no devices", with(func(r *apischema.BtrfsPoolCreateRequest) { r.Devices = nil }), "at least one device"},
		{"duplicate device", with(func(r *apischema.BtrfsPoolCreateRequest) { r.Devices[1] = "/dev/sdb" }), "listed twice"},
		{"too few for raid1", with(func(r *apischema.BtrfsPoolCreateRequest) { r.Devices = []string{"/dev/sdb"} }), "needs at least 2 devices"},
		{"raid5", with(func(r *apischema.BtrfsPoolCreateRequest) { r.DataProfile = "raid5" }), "not supported"},
		{"signature without force", with(func(r *apischema.BtrfsPoolCreateRequest) { r.Devices[1] = "/dev/sdd" }), "has an existing ext4 filesystem; turn on force"},
		{"mounted member", with(func(r *apischema.BtrfsPoolCreateRequest) { r.Devices[1], r.Force = "/dev/sde", true }), "Mounted at /data"},
		{"force and an unmounted pool's member", with(func(r *apischema.BtrfsPoolCreateRequest) { r.Devices[1], r.Force = "/dev/sdh", true }), "Member of btrfs pool old; destroy the pool"},
		{"force and a stopped array's member", with(func(r *apischema.BtrfsPoolCreateRequest) { r.Devices[1], r.Force = "/dev/sdl", true }), "Member of md array a1"},
		{"free space on a disk in fstab", with(func(r *apischema.BtrfsPoolCreateRequest) { r.FreeSpace = []string{"/dev/sdn"} }), "In /etc/fstab"},
		{"relative mountpoint", with(func(r *apischema.BtrfsPoolCreateRequest) { r.Mountpoint = "mnt/tank" }), "invalid mountpoint"},
		{"unclean mountpoint", with(func(r *apischema.BtrfsPoolCreateRequest) { r.Mountpoint = "/mnt/../etc" }), "invalid mountpoint"},
		{"system mountpoint", with(func(r *apischema.BtrfsPoolCreateRequest) { r.Mountpoint = "/var" }), "system directory"},
		{"root mountpoint", with(func(r *apischema.BtrfsPoolCreateRequest) { r.Mountpoint = "/" }), "system directory"},
		{"fstab mountpoint", with(func(r *apischema.BtrfsPoolCreateRequest) { r.Mountpoint = "/srv" }), "already in /etc/fstab"},
		{"mountpoint in use", with(func(r *apischema.BtrfsPoolCreateRequest) { r.Mountpoint = "/data" }), "in use by /dev/sde"},
	}
	for _, c := range cases {
		err := checkPoolCreate(c.req, inv, fstab)
		if c.wantErr == "" && err != nil {
			t.Errorf("%s: err = %v", c.name, err)
		}
		if c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)) {
			t.Errorf("%s: err = %v, want %q", c.name, err, c.wantErr)
		}
	}
}

func TestPartitionLockedHoldsTheDiskLock(t *testing.T) {
	orig := lockDisk
	t.Cleanup(func() { lockDisk = orig })
	var calls []string
	lockDisk = func(_ context.Context, device string) (func(), error) {
		calls = append(calls, "lock "+device)
		return func() { calls = append(calls, "unlock "+device) }, nil
	}
	partition := func(_ context.Context, disk string) (string, error) {
		calls = append(calls, "partition "+disk)
		if disk == "/dev/sdc" {
			return "", errors.New("no free space")
		}
		return disk + "1", nil
	}

	part, unlock, err := partitionLocked(context.Background(), "/dev/sdb", partition)
	if err != nil || part != "/dev/sdb1" {
		t.Fatalf("part = %q, err = %v", part, err)
	}
	if want := []string{"lock /dev/sdb", "partition /dev/sdb"}; !slices.Equal(calls, want) {
		t.Fatalf("calls = %q, want the lock still held: %q", calls, want)
	}
	unlock()

	calls = nil
	if _, _, err := partitionLocked(context.Background(), "/dev/sdc", partition); err == nil {
		t.Fatal("want the partition error")
	}
	if want := []string{"lock /dev/sdc", "partition /dev/sdc", "unlock /dev/sdc"}; !slices.Equal(calls, want) {
		t.Fatalf("calls = %q, want %q", calls, want)
	}
}

var errStep = errors.New("step failed")

// poolCreateEnv stubs every seam CreateBtrfsPool locks, writes or undoes
// through; calls records each in order, and a step listed in fail fails.
type poolCreateEnv struct {
	calls []string
	fail  []string
}

func stubPoolCreate(t *testing.T) *poolCreateEnv {
	t.Helper()
	origDevices, origFss, origOffline, origLock, origDelete := storageBlockDevices, btrfsFilesystems, offlinePools, lockDisk, deletePartition
	origPartDisk, origAdd, origCreate, origMount := partitionDisk, addPartition, createBtrfsPool, mountBtrfsPool
	origWipe, origNumber, origUnmount, origFstab, origFind := wipePoolSuperblocks, partitionNumber, unmountPool, addPoolFstab, findBtrfsFilesystem
	t.Cleanup(func() {
		storageBlockDevices, btrfsFilesystems, offlinePools, lockDisk, deletePartition = origDevices, origFss, origOffline, origLock, origDelete
		partitionDisk, addPartition, createBtrfsPool, mountBtrfsPool = origPartDisk, origAdd, origCreate, origMount
		wipePoolSuperblocks, partitionNumber, unmountPool, addPoolFstab, findBtrfsFilesystem = origWipe, origNumber, origUnmount, origFstab, origFind
	})
	useTempFstab(t, "")

	env := &poolCreateEnv{}
	step := func(format string, args ...any) error {
		s := fmt.Sprintf(format, args...)
		env.calls = append(env.calls, s)
		if slices.Contains(env.fail, s) {
			return errStep
		}
		return nil
	}
	storageBlockDevices = func(context.Context) ([]apischema.StorageBlockDevice, error) {
		return []apischema.StorageBlockDevice{
			{Path: "/dev/sdb", Type: "disk", FreeBytes: 1 << 30, Mountpoints: []string{}},
			{Path: "/dev/sdc", Type: "disk", FreeBytes: 1 << 30, Mountpoints: []string{}},
		}, nil
	}
	btrfsFilesystems = func(context.Context) ([]btrfs.BtrfsFilesystem, error) { return nil, nil }
	offlinePools = func(context.Context) ([]btrfs.BtrfsOfflinePool, error) { return nil, nil }
	lockDisk = func(_ context.Context, disk string) (func(), error) {
		_ = step("lock %s", disk)
		return func() { _ = step("unlock %s", disk) }, nil
	}
	newPartition := func(_ context.Context, disk string) (string, error) {
		if err := step("partition %s", disk); err != nil {
			return "", err
		}
		return disk + "1", nil
	}
	partitionDisk, addPartition = newPartition, newPartition
	createBtrfsPool = func(_ context.Context, _, _, _ string, devices []string, _ bool, _ func(float64)) (string, error) {
		return "pool-uuid", step("mkfs %s", strings.Join(devices, " ")) // the UUID is known once mkfs ran
	}
	mountBtrfsPool = func(_ context.Context, device, _ string, _ []string, _ bool) error { return step("mount %s", device) }
	addPoolFstab = func(source, _, _ string, _ []string) error { return step("fstab %s", source) }
	findBtrfsFilesystem = func(_ context.Context, uuid string) (btrfs.BtrfsFilesystem, error) {
		return btrfs.BtrfsFilesystem{UUID: uuid}, step("find %s", uuid)
	}
	unmountPool = func(string, int) error { return step("unmount") }
	wipePoolSuperblocks = func(device, uuid string) (bool, error) { return true, step("wipe %s %s", device, uuid) }
	partitionNumber = func(string) (int, error) { return 1, nil }
	deletePartition = func(ctx context.Context, disk string, number int) error {
		if ctx.Err() != nil {
			return step("delete on a canceled context")
		}
		return step("delete %s %d", disk, number)
	}
	return env
}

// canceledCreate runs CreateBtrfsPool on a canceled context, as when the job
// is canceled mid-create: the undo must still run to the end.
func canceledCreate(t *testing.T, devices, freeSpace []string) error {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := apischema.BtrfsPoolCreateRequest{
		Label: "tank", Mountpoint: filepath.Join(t.TempDir(), "tank"), DataProfile: "raid1", MetadataProfile: "raid1",
		Devices: devices, FreeSpace: freeSpace,
	}
	_, err := CreateBtrfsPool(ctx, req, func(string, float64) {})
	return err
}

func TestCreateBtrfsPoolUndoesOnFailure(t *testing.T) {
	made := []string{"lock /dev/sdb", "partition /dev/sdb", "lock /dev/sdc", "partition /dev/sdc", "mkfs /dev/sdb1 /dev/sdc1"}
	mounted := []string{"mount /dev/sdb1", "fstab UUID=pool-uuid"}
	wipes := []string{"wipe /dev/sdb1 pool-uuid", "wipe /dev/sdc1 pool-uuid"}
	unlocks := []string{"unlock /dev/sdc", "unlock /dev/sdb"}
	cases := []struct {
		name     string
		fail     []string
		want     []string
		undoFail bool
	}{
		{"second partition", []string{"partition /dev/sdc"}, slices.Concat(made[:4], []string{"unlock /dev/sdc", "delete /dev/sdb 1", "unlock /dev/sdb"}), false},
		{"mkfs", []string{made[4]}, slices.Concat(made, wipes, []string{"delete /dev/sdc 1", "delete /dev/sdb 1"}, unlocks), false},
		{"mount", []string{mounted[0]}, slices.Concat(made, mounted[:1], wipes, []string{"delete /dev/sdc 1", "delete /dev/sdb 1"}, unlocks), false},
		{"fstab", []string{mounted[1]}, slices.Concat(made, mounted, []string{"unmount"}, wipes, []string{"delete /dev/sdc 1", "delete /dev/sdb 1"}, unlocks), false},
		// A partition whose superblocks are still there is kept: its offsets would be lost.
		{"mkfs then a wipe", []string{made[4], wipes[1]}, slices.Concat(made, wipes, []string{"delete /dev/sdb 1"}, unlocks), true},
		// A pool still mounted is left alone.
		{"fstab then the unmount", []string{mounted[1], "unmount"}, slices.Concat(made, mounted, []string{"unmount"}, unlocks), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := stubPoolCreate(t)
			env.fail = c.fail
			err := canceledCreate(t, nil, []string{"/dev/sdb", "/dev/sdc"})
			if !errors.Is(err, errStep) {
				t.Fatalf("err = %v, want the step's error", err)
			}
			if got := strings.Contains(err.Error(), "undo btrfs pool create"); got != c.undoFail {
				t.Fatalf("err = %v, want an undo error: %t", err, c.undoFail)
			}
			if !slices.Equal(env.calls, c.want) {
				t.Fatalf("calls:\n%q\nwant:\n%q", env.calls, c.want)
			}
		})
	}
}

func TestCreateBtrfsPoolKeepsAPoolInFstab(t *testing.T) {
	env := stubPoolCreate(t)
	env.fail = []string{"find pool-uuid"}
	if err := canceledCreate(t, nil, []string{"/dev/sdb", "/dev/sdc"}); !errors.Is(err, errStep) || strings.Contains(err.Error(), "undo") {
		t.Fatalf("err = %v, want the lookup's error alone", err)
	}
	want := []string{"lock /dev/sdb", "partition /dev/sdb", "lock /dev/sdc", "partition /dev/sdc", "mkfs /dev/sdb1 /dev/sdc1", "mount /dev/sdb1", "fstab UUID=pool-uuid", "find pool-uuid", "unlock /dev/sdc", "unlock /dev/sdb"}
	if !slices.Equal(env.calls, want) {
		t.Fatalf("calls:\n%q\nwant:\n%q", env.calls, want)
	}
}

func TestCreateBtrfsPoolKeepsWholeDevicesOnFailure(t *testing.T) {
	env := stubPoolCreate(t)
	env.fail = []string{"mkfs /dev/sdb /dev/sdc"}
	if err := canceledCreate(t, []string{"/dev/sdb", "/dev/sdc"}, nil); !errors.Is(err, errStep) {
		t.Fatalf("err = %v, want the step's error", err)
	}
	want := []string{"mkfs /dev/sdb /dev/sdc", "wipe /dev/sdb pool-uuid", "wipe /dev/sdc pool-uuid"}
	if !slices.Equal(env.calls, want) {
		t.Fatalf("calls:\n%q\nwant:\n%q", env.calls, want)
	}
}

func TestPoolMountpointsDeepestFirst(t *testing.T) {
	fs := btrfs.BtrfsFilesystem{Mountpoints: []btrfs.Mount{
		{Mountpoint: "/mnt/tank"}, {Mountpoint: "/mnt/tank/media"}, {Mountpoint: "/mnt/tank"}, {Mountpoint: "/run/linuxio/btrfs/x"},
	}}
	if _, err := poolMountpoints(fs); err == nil || !strings.Contains(err.Error(), "/run/linuxio/btrfs/x") {
		t.Fatalf("protected mount: err = %v", err)
	}
	fs.Mountpoints = fs.Mountpoints[:3]
	got, err := poolMountpoints(fs)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"/mnt/tank/media", "/mnt/tank"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestFstabMountpointFor(t *testing.T) {
	entries := map[string]fstabEntry{
		"/mnt/tank/media": {source: "UUID=pool"},
		"/mnt/tank":       {source: "uuid=POOL"},
		"/srv":            {source: "UUID=other"},
	}
	if got := fstabMountpointFor(entries, "pool"); got != "/mnt/tank" {
		t.Fatalf("got %q", got)
	}
	if got := fstabMountpointFor(entries, "missing"); got != "" {
		t.Fatalf("got %q", got)
	}
}

// A pool whose present devices include a replace target, scanned as a
// device but no member, still misses a member and mounts only degraded.
func TestMountBtrfsPoolNeedsDegradedWhenMissing(t *testing.T) {
	useTempFstab(t, "UUID=cold-uuid /mnt/cold btrfs noatime 0 0\n")
	oldOffline, oldMount := offlinePools, mountOfflinePool
	t.Cleanup(func() { offlinePools, mountOfflinePool = oldOffline, oldMount })
	pool := btrfs.BtrfsOfflinePool{UUID: "cold-uuid", NumDevices: 2, Devices: []string{"/dev/sdc", "/dev/sdd"}, Mountpoint: "/mnt/cold", Missing: 1}
	offlinePools = func(context.Context) ([]btrfs.BtrfsOfflinePool, error) { return []btrfs.BtrfsOfflinePool{pool}, nil }
	var mounts []bool
	mountOfflinePool = func(_ context.Context, _ btrfs.BtrfsOfflinePool, _ string, _ []string, degraded bool) error {
		mounts = append(mounts, degraded)
		return errStep
	}
	ctx := context.Background()

	_, err := MountBtrfsPool(ctx, apischema.BtrfsPoolMountRequest{UUID: "cold-uuid"})
	if !errors.Is(err, bridgeipc.ErrConflict) || !strings.Contains(err.Error(), "missing 1 of 2 devices") || len(mounts) != 0 {
		t.Fatalf("plain mount: err = %v, mounts = %v; want a conflict before any mount", err, mounts)
	}
	if _, err = MountBtrfsPool(ctx, apischema.BtrfsPoolMountRequest{UUID: "gone-uuid", Degraded: true}); !errors.Is(err, bridgeipc.ErrNotFound) || len(mounts) != 0 {
		t.Fatalf("unknown pool: err = %v, mounts = %v; want not found before any mount", err, mounts)
	}
	_, err = MountBtrfsPool(ctx, apischema.BtrfsPoolMountRequest{UUID: "cold-uuid", Degraded: true})
	if !errors.Is(err, errStep) || !slices.Equal(mounts, []bool{true}) {
		t.Fatalf("degraded mount: err = %v, mounts = %v; want one degraded mount", err, mounts)
	}
}

func TestDefaultPoolMountpoint(t *testing.T) {
	if got := defaultPoolMountpoint(btrfs.BtrfsOfflinePool{UUID: "u", Label: "tank"}); got != "/mnt/tank" {
		t.Fatalf("got %q", got)
	}
	if got := defaultPoolMountpoint(btrfs.BtrfsOfflinePool{UUID: "u", Label: "my pool"}); got != "/mnt/u" {
		t.Fatalf("got %q", got)
	}
}

func TestRemovePoolFstabEntries(t *testing.T) {
	useTempFstab(t, strings.Join([]string{
		"UUID=" + testBtrfsUUID + " /mnt/a btrfs defaults 0 0",
		"/dev/disk/by-uuid/" + testBtrfsUUID + " /mnt/b btrfs defaults 0 0",
		"LABEL=tank /mnt/c btrfs defaults 0 0",
		"UUID=other /mnt/d btrfs defaults 0 0",
		"/dev/disk/by-uuid/" + testBtrfsUUID + " /mnt/e ext4 defaults 0 0",
		"",
	}, "\n"))
	mounts := []btrfs.Mount{{UUID: testBtrfsUUID, Mountpoint: "/mnt/c"}}
	if err := removePoolFstabEntries(testBtrfsUUID, mounts); err != nil {
		t.Fatal(err)
	}
	got := getFstabEntries()
	if len(got) != 2 || got["/mnt/d"].source != "UUID=other" || got["/mnt/e"].fstype != "ext4" {
		t.Fatalf("remaining entries = %v", got)
	}
}
