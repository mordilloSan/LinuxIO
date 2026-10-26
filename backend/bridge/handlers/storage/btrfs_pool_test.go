package storage

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
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
	}
	filesystems := []btrfs.BtrfsFilesystem{{UUID: testBtrfsUUID, Devices: []btrfs.BtrfsDevice{{ID: 1, Path: "/dev/sde"}}}}
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
		{"signature without force", with(func(r *apischema.BtrfsPoolCreateRequest) { r.Devices[1] = "/dev/sdd" }), "ext4 signature"},
		{"mounted member", with(func(r *apischema.BtrfsPoolCreateRequest) { r.Devices[1], r.Force = "/dev/sde", true }), "is mounted"},
		{"relative mountpoint", with(func(r *apischema.BtrfsPoolCreateRequest) { r.Mountpoint = "mnt/tank" }), "invalid mountpoint"},
		{"unclean mountpoint", with(func(r *apischema.BtrfsPoolCreateRequest) { r.Mountpoint = "/mnt/../etc" }), "invalid mountpoint"},
		{"system mountpoint", with(func(r *apischema.BtrfsPoolCreateRequest) { r.Mountpoint = "/var" }), "system directory"},
		{"root mountpoint", with(func(r *apischema.BtrfsPoolCreateRequest) { r.Mountpoint = "/" }), "system directory"},
		{"fstab mountpoint", with(func(r *apischema.BtrfsPoolCreateRequest) { r.Mountpoint = "/srv" }), "already in /etc/fstab"},
		{"mountpoint in use", with(func(r *apischema.BtrfsPoolCreateRequest) { r.Mountpoint = "/data" }), "in use by /dev/sde"},
	}
	for _, c := range cases {
		err := checkPoolCreate(c.req, devices, filesystems, fstab)
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
