package storage

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
)

// memberLoopDevice backs a free loop device with a sparse file of size bytes
// and detaches it when the test ends.
func memberLoopDevice(t *testing.T, file string, size int64) string {
	t.Helper()
	f, err := os.Create(file)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err = f.Truncate(size); err != nil {
		t.Fatal(err)
	}
	ctl, err := unix.Open("/dev/loop-control", unix.O_RDWR|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatalf("open loop-control: %v", err)
	}
	defer unix.Close(ctl)
	for range 10 {
		n, err := unix.IoctlRetInt(ctl, unix.LOOP_CTL_GET_FREE)
		if err != nil {
			t.Fatalf("get free loop device: %v", err)
		}
		dev := fmt.Sprintf("/dev/loop%d", n)
		fd, err := unix.Open(dev, unix.O_RDWR|unix.O_CLOEXEC, 0)
		if err != nil {
			t.Fatalf("open %s: %v", dev, err)
		}
		if err = unix.IoctlSetInt(fd, unix.LOOP_SET_FD, int(f.Fd())); errors.Is(err, unix.EBUSY) {
			unix.Close(fd) // another process took it first
			continue
		} else if err != nil {
			unix.Close(fd)
			t.Fatalf("attach %s: %v", dev, err)
		}
		t.Cleanup(func() {
			_, _ = unix.IoctlRetInt(fd, unix.LOOP_CLR_FD)
			unix.Close(fd)
		})
		return dev
	}
	t.Fatal("no free loop device")
	return ""
}

// mountMember registers devices with the kernel and mounts the filesystem
// of the first at mnt, unmounting it when the test ends.
func mountMember(t *testing.T, mnt string, devices ...string) {
	t.Helper()
	for _, d := range devices {
		if err := btrfs.RegisterDevice(d); err != nil {
			t.Fatalf("register %s: %v", d, err)
		}
	}
	if err := os.MkdirAll(mnt, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mount(devices[0], mnt, "btrfs", 0, ""); err != nil {
		t.Fatalf("mount %s at %s: %v", devices[0], mnt, err)
	}
	t.Cleanup(func() { _ = unix.Unmount(mnt, 0) })
}

// TestBtrfsMemberIntegration checks against a real kernel and real
// superblocks that force never takes a member of an unmounted pool: neither
// a new pool nor a device add may use it, and the old pool still mounts
// with its data afterwards. Run it with `make test-btrfs-integration`.
func TestBtrfsMemberIntegration(t *testing.T) {
	if os.Getenv("LINUXIO_RUN_BTRFS_INTEGRATION") != "1" {
		t.Skip("set LINUXIO_RUN_BTRFS_INTEGRATION=1 and run as root")
	}
	if os.Geteuid() != 0 {
		t.Fatal("the btrfs integration test needs root")
	}
	// The disks are loop devices, which the inventory leaves out otherwise.
	inventoryLoops = true
	t.Cleanup(func() { inventoryLoops = false })
	// Nothing below may reach /etc/fstab: a create that wrongly got this
	// far fails here and is rolled back.
	origFstab := addPoolFstab
	t.Cleanup(func() { addPoolFstab = origFstab })
	addPoolFstab = func(source, mountpoint, _ string, _ []string) error {
		t.Errorf("pool create reached /etc/fstab with %s at %s", source, mountpoint)
		return errors.New("refused by the test")
	}

	ctx := context.Background()
	dir := t.TempDir()
	var disks []string
	for i := range 3 {
		disks = append(disks, memberLoopDevice(t, filepath.Join(dir, fmt.Sprintf("disk%d.img", i)), 1<<30))
	}

	// The first pool, raid1 over disks 0 and 1, with data, then unmounted.
	const label = "linuxio-member-it"
	uuid, err := btrfs.CreatePool(ctx, label, "raid1", "raid1", disks[:2], true, nil)
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}
	first := filepath.Join(dir, "first")
	mountMember(t, first, disks[:2]...)
	data := make([]byte, 4<<20)
	if _, err = rand.Read(data); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(first, "data.bin"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	unix.Sync()
	if err = unix.Unmount(first, 0); err != nil {
		t.Fatalf("unmount first pool: %v", err)
	}

	wantMember := func(what string, err error) {
		t.Helper()
		if !errors.Is(err, bridgeipc.ErrConflict) || !strings.Contains(err.Error(), "Member of btrfs pool "+label) {
			t.Fatalf("%s: err = %v, want a conflict naming pool %s", what, err, label)
		}
	}

	// The check itself, on the live inventory.
	inv, err := loadPartitionInventory(ctx)
	if err != nil {
		t.Fatalf("inventory: %v", err)
	}
	wantMember("check with force", checkBtrfsDeviceFree(inv, disks[1], true))

	// A second pool with force over a member of the first and a blank disk.
	_, err = CreateBtrfsPool(ctx, apischema.BtrfsPoolCreateRequest{
		Label: "linuxio-member-it2", Mountpoint: filepath.Join(dir, "second"),
		DataProfile: "single", MetadataProfile: "dup",
		Devices: []string{disks[1], disks[2]}, Force: true,
	}, func(string, float64) {})
	wantMember("create with force", err)

	// A device add with force of that member into a mounted pool on disk 2.
	otherUUID, err := btrfs.CreatePool(ctx, "linuxio-member-it3", "single", "dup", disks[2:], true, nil)
	if err != nil {
		t.Fatalf("create other pool: %v", err)
	}
	mountMember(t, filepath.Join(dir, "other"), disks[2])
	_, err = AddBtrfsDevice(ctx, apischema.BtrfsDeviceAddRequest{UUID: otherUUID, Device: disks[1], Force: true})
	wantMember("add with force", err)

	// The first pool is whole: it mounts without degraded and keeps its data.
	mountMember(t, first, disks[:2]...)
	fs, err := btrfs.FindFilesystem(ctx, uuid)
	if err != nil {
		t.Fatalf("find first pool: %v", err)
	}
	if len(fs.Devices) != 2 || fs.Degraded {
		t.Fatalf("first pool = %+v, want both devices present", fs)
	}
	got, err := os.ReadFile(filepath.Join(first, "data.bin"))
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("first pool's data changed: err = %v, %d bytes", err, len(got))
	}
}
