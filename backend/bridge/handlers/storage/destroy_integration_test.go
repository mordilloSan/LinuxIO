package storage

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
)

// TestBtrfsDestroyIntegration checks against a real kernel that a pool used
// through a subvolume mount can be destroyed after a read mounted its top
// level under /run/linuxio/btrfs, a protected path: destroy unmounts that
// mount of LinuxIO's own with the rest. Run it with
// `make test-btrfs-integration`.
func TestBtrfsDestroyIntegration(t *testing.T) {
	if os.Getenv("LINUXIO_RUN_BTRFS_INTEGRATION") != "1" {
		t.Skip("set LINUXIO_RUN_BTRFS_INTEGRATION=1 and run as root")
	}
	if os.Geteuid() != 0 {
		t.Fatal("the btrfs integration test needs root")
	}
	useTempFstab(t, "") // destroy drops the pool's fstab lines
	ctx := context.Background()
	dir := t.TempDir()
	disk := memberLoopDevice(t, filepath.Join(dir, "disk.img"), 1<<30)
	must := func(what string, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	}

	uuid, err := btrfs.CreatePool(ctx, "linuxio-destroy-it", "single", "dup", []string{disk}, true, nil)
	must("create pool", err)
	top := filepath.Join(dir, "top")
	mountMember(t, top, disk)
	_, err = btrfs.CreateSubvolume(ctx, uuid, "data")
	must("create subvolume", err)
	must("unmount the top level", unix.Unmount(top, 0))

	// Used only through its subvolume, as a pool mounted with subvol= is.
	data := filepath.Join(dir, "data")
	must("create the subvolume mountpoint", os.MkdirAll(data, 0o755))
	must("mount the subvolume", unix.Mount(disk, data, "btrfs", 0, "subvol=data"))
	t.Cleanup(func() { _ = unix.Unmount(data, 0) })

	// A subvolume read mounts the top level where LinuxIO keeps it.
	helper := filepath.Join(btrfs.TopLevelDir, uuid)
	t.Cleanup(func() {
		_ = unix.Unmount(helper, 0)
		_ = os.Remove(helper)
	})
	_, err = btrfs.ListSubvolumes(ctx, uuid)
	must("list subvolumes", err)
	mounted := func() []string {
		t.Helper()
		mounts, listErr := btrfs.ListMounts(ctx)
		must("list mounts", listErr)
		var at []string
		for _, m := range mounts {
			if m.UUID == uuid {
				at = append(at, m.Mountpoint)
			}
		}
		return at
	}
	if at := mounted(); !slices.Contains(at, helper) || !slices.Contains(at, data) {
		t.Fatalf("pool mounted at %v, want %s and %s", at, data, helper)
	}

	must("destroy the pool", DestroyBtrfsPool(ctx, uuid))
	if at := mounted(); len(at) != 0 {
		t.Fatalf("pool still mounted at %v after destroy", at)
	}
	f, err := os.Open(disk)
	must("open the disk", err)
	defer f.Close()
	var magic [8]byte
	_, err = f.ReadAt(magic[:], 65536+64) // the primary superblock's magic
	must("read the superblock magic", err)
	if binary.LittleEndian.Uint64(magic[:]) != 0 {
		t.Fatalf("superblock magic %#x kept after destroy", magic)
	}
}
