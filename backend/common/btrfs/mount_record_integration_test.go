package btrfs

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// TestBtrfsMountRecordIntegration checks against the machine's mount(8) and
// libmount what keeping a pool mounted rests on (unbindMount): that a mount
// made with mount(2) has no record of its fstab options, and that
// recordMountSource writes one libmount joins to the mount, for the top
// level and for a subvolume, whose record must carry the subvolume as its
// root. Whether systemd then drops its dependency is seen in the test VM
// (docs/btrfs.md, "Pools"). Run it with `make test-btrfs-integration`.
func TestBtrfsMountRecordIntegration(t *testing.T) {
	if os.Getenv("LINUXIO_RUN_BTRFS_INTEGRATION") != "1" {
		t.Skip("set LINUXIO_RUN_BTRFS_INTEGRATION=1 and run as root")
	}
	if os.Geteuid() != 0 {
		t.Fatal("the btrfs integration test needs root")
	}
	ctx := context.Background()
	dir := t.TempDir()
	stubStatePaths(t, dir) // MountPool records the pool for the boot check
	must := func(what string, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	}
	a := loopDevice(t, filepath.Join(dir, "a.img"), 1<<30)
	b := loopDevice(t, filepath.Join(dir, "b.img"), 1<<30)
	uuid, err := CreatePool(ctx, "linuxio-record-it", "raid1", "raid1", []string{a, b}, true, nil, nil)
	must("create pool", err)

	// umount(8) takes the record with the mount; a bare umount(2) would
	// leave the test's lines in the machine's utab until it restarts.
	mount := func(what, mountpoint string, options []string) {
		t.Helper()
		must("mount "+what, MountPool(ctx, a, mountpoint, options, false))
		t.Cleanup(func() {
			if _, umountErr := runCommand(ctx, "umount", mountpoint); umountErr != nil {
				_ = unix.Unmount(mountpoint, 0)
			}
		})
	}
	top, media := filepath.Join(dir, "top"), filepath.Join(dir, "media")
	mount("the top level", top, nil)
	_, err = CreateSubvolume(ctx, uuid, "@media")
	must("create the subvolume", err)
	mount("the subvolume", media, []string{"subvol=@media"})

	mounts, err := ListMounts(ctx)
	must("list mounts", err)
	for _, c := range []struct{ mountpoint, root string }{{top, "/"}, {media, "/@media"}} {
		at := slices.IndexFunc(mounts, func(m Mount) bool { return m.Mountpoint == c.mountpoint })
		if at < 0 {
			t.Fatalf("%s is not among the btrfs mounts", c.mountpoint)
		}
		recordsAndJoins(ctx, t, mounts[at], c.root)
	}
}

// joinedOptions returns the options of the mount at mountpoint as libmount
// joins them: the mount table with its records.
func joinedOptions(ctx context.Context, t *testing.T, mountpoint string) []string {
	t.Helper()
	out, err := runCommand(ctx, "findmnt", "--mtab", "-n", "-o", "OPTIONS", "--mountpoint", mountpoint)
	if err != nil {
		t.Fatalf("findmnt %s: %v", mountpoint, err)
	}
	return strings.Split(out, ",")
}

// recordsAndJoins fails unless mount m, made with mount(2), has no record of
// its options, and recordMountSource then writes one, under m's source and
// root, that libmount joins to it.
func recordsAndJoins(ctx context.Context, t *testing.T, m Mount, root string) {
	t.Helper()
	const option = "x-systemd.device-bound=false"
	if slices.Contains(joinedOptions(ctx, t, m.Mountpoint), option) || mountRecorded(m, option) {
		t.Fatalf("%s: a mount made with mount(2) already has a record of %s", m.Mountpoint, option)
	}
	if err := recordMountSource(ctx, m, []string{"_netdev", option}); err != nil {
		t.Fatalf("record %s: %v", m.Mountpoint, err)
	}
	if got := joinedOptions(ctx, t, m.Mountpoint); !slices.Contains(got, option) || !slices.Contains(got, "_netdev") {
		t.Errorf("%s: libmount gives the mount the options %q after the record, want %s and _netdev among them", m.Mountpoint, got, option)
	}
	if !mountRecorded(m, option) {
		t.Errorf("%s: the record is not found under source %s and root %s", m.Mountpoint, m.Device, root)
	}
	data, err := os.ReadFile(utabPath)
	if err != nil {
		t.Fatalf("read the record: %v", err)
	}
	var records []string
	for line := range strings.Lines(string(data)) {
		if strings.Contains(line, " TARGET="+m.Mountpoint+" ") {
			records = append(records, strings.TrimSpace(line))
		}
	}
	want := "SRC=" + m.Device + " TARGET=" + m.Mountpoint + " ROOT=" + root + " OPTS=_netdev," + option
	if !slices.Equal(records, []string{want}) {
		t.Errorf("%s: records %q, want %q alone, the subvolume as its root and not among its options", m.Mountpoint, records, want)
	}
}
