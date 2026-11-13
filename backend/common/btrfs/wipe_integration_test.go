package btrfs

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// TestBtrfsAddWipeIntegration checks against a real kernel that a forced
// add never zeroes a device that was mounted or claimed after the checks
// before it, and that a device the wipe opened exclusively still joins the
// pool once that open is closed. Run it with `make test-btrfs-integration`.
func TestBtrfsAddWipeIntegration(t *testing.T) {
	if os.Getenv("LINUXIO_RUN_BTRFS_INTEGRATION") != "1" {
		t.Skip("set LINUXIO_RUN_BTRFS_INTEGRATION=1 and run as root")
	}
	if os.Geteuid() != 0 {
		t.Fatal("the btrfs integration test needs root")
	}
	ctx := context.Background()
	dir := t.TempDir()
	stubStatePaths(t, dir) // MountPool records the pool for the boot check
	pool := loopDevice(t, filepath.Join(dir, "pool.img"), 1<<30)
	disk := loopDevice(t, filepath.Join(dir, "disk.img"), 1<<30)
	must := func(what string, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	}

	uuid, err := CreatePool(ctx, "linuxio-wipe-it", "single", "dup", []string{pool}, true, nil)
	must("create pool", err)
	mnt := filepath.Join(dir, "pool")
	must("mount pool", MountPool(ctx, pool, mnt, nil, false))
	t.Cleanup(func() { _ = unix.Unmount(mnt, 0) })

	// The disk carries a filesystem of its own, with a marker in its first
	// 4 KiB, which btrfs never writes: a wipe would zero it.
	_, err = CreatePool(ctx, "linuxio-wipe-it-other", "single", "dup", []string{disk}, true, nil)
	must("create the other filesystem", err)
	marker := make([]byte, 4096)
	_, err = rand.Read(marker)
	must("random marker", err)
	f, err := os.OpenFile(disk, os.O_RDWR, 0)
	must("open the disk", err)
	_, err = f.WriteAt(marker, 0)
	if err == nil {
		err = f.Sync()
	}
	must("write the marker", errors.Join(err, f.Close()))
	head := func() (first []byte, magic uint64) {
		t.Helper()
		r, openErr := os.Open(disk)
		must("open the disk", openErr)
		defer r.Close()
		first = make([]byte, len(marker))
		_, readErr := r.ReadAt(first, 0)
		must("read the first sector", readErr)
		var m [8]byte
		_, readErr = r.ReadAt(m[:], superOffset+superMagicAt)
		must("read the superblock magic", readErr)
		return first, binary.LittleEndian.Uint64(m[:])
	}
	refused := func(what string) {
		t.Helper()
		if addErr := AddDevice(ctx, uuid, disk, true); !errors.Is(addErr, unix.EBUSY) {
			t.Fatalf("forced add of a %s device: err = %v, want EBUSY", what, addErr)
		}
		if first, magic := head(); !bytes.Equal(first, marker) || magic != superMagic {
			t.Fatalf("forced add of a %s device wrote to it: marker kept %t, magic %#x", what, bytes.Equal(first, marker), magic)
		}
		if fs, findErr := FindFilesystem(ctx, uuid); findErr != nil || len(fs.Devices) != 1 {
			t.Fatalf("forced add of a %s device: pool = %+v, %v", what, fs.Devices, findErr)
		}
	}

	// Mounted after the checks would have passed it.
	other := filepath.Join(dir, "other")
	must("mount the other filesystem", MountPool(ctx, disk, other, nil, false))
	refused("mounted")
	must("unmount the other filesystem", unix.Unmount(other, 0))

	// Held: an exclusive open claims the device as LVM, md, dm-crypt, a swap
	// or a mount does.
	held, err := os.OpenFile(disk, os.O_RDONLY|os.O_EXCL, 0)
	must("hold the disk", err)
	refused("held")
	must("release the disk", held.Close())

	// Free: the wipe's exclusive claim ends with its close, so the add's own
	// exclusive open of the device succeeds. The other filesystem is
	// unregistered first, as Destroy does.
	must("wipe the other filesystem", WipeDevice(disk))
	must("forced add of a free device", AddDevice(ctx, uuid, disk, true))
	fs, err := FindFilesystem(ctx, uuid)
	must("inventory", err)
	if len(fs.Devices) != 2 {
		t.Fatalf("after the add: %v", devicePaths(fs))
	}
	if first, _ := head(); !bytes.Equal(first, make([]byte, len(first))) {
		t.Fatal("the forced add did not zero the start of the device")
	}
}
