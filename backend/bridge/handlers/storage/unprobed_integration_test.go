package storage

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
)

// lvm2Label is the label pvcreate writes in sector 1 of a physical volume
// with pvUUID (32 characters), its CRC as LVM computes it: CRC-32 over the
// label from offset 20 on, started at 0xf597a6cf, not inverted at the end.
func lvm2Label(pvUUID string) []byte {
	le := binary.LittleEndian
	l := make([]byte, 512)
	copy(l, "LABELONE")
	le.PutUint64(l[8:], 1)
	le.PutUint32(l[20:], 32)
	copy(l[24:], "LVM2 001")
	copy(l[32:], pvUUID)
	le.PutUint32(l[16:], ^crc32.Update(^uint32(0xf597a6cf), crc32.IEEETable, l[20:]))
	return l
}

// waitFor polls done for up to 30 s, failing the test with what when it
// never holds.
func waitFor(t *testing.T, what string, done func() bool) {
	t.Helper()
	for start := time.Now(); !done(); time.Sleep(100 * time.Millisecond) {
		if time.Since(start) > 30*time.Second {
			t.Fatalf("waited 30 s for %s", what)
		}
	}
}

// TestPartitionMemberIntegration checks against the real kernel and udev
// that Format refuses a partition whose LVM2 label udev has not probed,
// writing nothing: the write that made it is still open, so udev's watch has
// not fired and the inventory still shows the ext4 that was there. The
// refused Format then asks udev to probe the partition, which is how the
// label reaches the dialog, and Delete and Initialize refuse it as udev
// reports it; Initialize, whose writable open of the disk makes udev probe
// it, runs last. An LVM2 label, unlike an md superblock, makes udev assemble
// nothing that would claim the partition. Run it with
// `make test-btrfs-integration`.
func TestPartitionMemberIntegration(t *testing.T) {
	if os.Getenv("LINUXIO_RUN_BTRFS_INTEGRATION") != "1" {
		t.Skip("set LINUXIO_RUN_BTRFS_INTEGRATION=1 and run as root")
	}
	if os.Geteuid() != 0 {
		t.Fatal("the partition integration test needs root")
	}
	inventoryLoops = true
	t.Cleanup(func() { inventoryLoops = false })
	useTempFstab(t, "")
	ctx := context.Background()
	must := func(what string, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	}
	disk := memberLoopDevice(t, filepath.Join(t.TempDir(), "disk.img"), 512<<20)
	ctl, err := os.Open(disk)
	must("open "+disk, err)
	must("enable partition scanning", unix.IoctlLoopSetStatus64(int(ctl.Fd()), &unix.LoopInfo64{Flags: unix.LO_FLAGS_PARTSCAN}))
	ctl.Close()

	// A GPT disk with one ext4 partition, which udev then reports.
	unlock, err := btrfs.LockDisk(ctx, disk)
	must("lock "+disk, err)
	must("initialize", btrfs.InitializeDisk(ctx, disk, nil))
	part, err := btrfs.CreatePartition(ctx, disk, 1<<20, 256<<20)
	must("create partition", err)
	_, err = btrfs.Format(ctx, part, "ext4", "", false, nil)
	must("format", err)
	unlock()
	reported := func() (d, p apischema.StorageBlockDevice) {
		t.Helper()
		inv, invErr := loadPartitionInventory(ctx)
		must("inventory", invErr)
		d, _ = inv.device(disk)
		p, _ = inv.device(part)
		return d, p
	}
	waitFor(t, "udev to report the GPT and the ext4 partition", func() bool {
		d, p := reported()
		return d.PTType == "gpt" && p.FSType == "ext4"
	})
	// And is done with them, so no re-read of the table races the open below.
	if out, settleErr := exec.Command("udevadm", "settle", "--timeout=30").CombinedOutput(); settleErr != nil {
		t.Fatalf("udevadm settle: %v\n%s", settleErr, out)
	}

	// An LVM2 label written through a descriptor that stays open, erased
	// again through it before it closes, so no probe at the close finds it.
	f, err := os.OpenFile(part, os.O_RDWR, 0)
	must("open "+part, err)
	t.Cleanup(func() { f.Close() })
	label := lvm2Label("LinuxIOmemberIntegrationTestPV01")
	t.Cleanup(func() { _, _ = f.WriteAt(make([]byte, len(label)), 512) })
	_, err = f.WriteAt(label, 512)
	must("write the LVM2 label", err)
	must("sync", f.Sync())
	if _, p := reported(); p.FSType != "ext4" {
		t.Fatalf("udev reports %s as %q already; the test needs it unprobed", part, p.FSType)
	}

	want := part + ": Member of an LVM volume group (physical volume LinuxI-Omem-berI-nteg-rati-onTe-stPV01); "
	refused := func(what string, err error, confirm string) {
		t.Helper()
		if !errors.Is(err, bridgeipc.ErrConflict) || !strings.Contains(err.Error(), want+confirm) {
			t.Fatalf("%s: err = %v, want a conflict naming %q", what, err, want+confirm)
		}
	}
	err = FormatPartition(ctx, apischema.PartitionFormatRequest{Device: part, FSType: "ext4", Overwrite: true}, func(string, float64) {})
	refused("format", err, "confirm erasing it to format "+part)
	// Only the Format's request can make udev probe it: nothing closed a
	// writable open of the partition or the disk since.
	waitFor(t, "udev to report the LVM2 label after the refused format", func() bool {
		_, p := reported()
		return p.FSType == "LVM2_member"
	})
	_, err = DeletePartition(ctx, apischema.PartitionDeleteRequest{Device: part})
	refused("delete", err, "confirm deleting it")
	_, err = InitializeDisk(ctx, apischema.DiskInitializeRequest{Device: disk})
	refused("initialize", err, "confirm erasing it to initialize "+disk)

	// Nothing was written: the partition, its ext4 and the label are all
	// still there.
	if _, err = os.Stat(filepath.Join("/sys/class/block", filepath.Base(part))); err != nil {
		t.Fatalf("the kernel no longer lists %s: %v", part, err)
	}
	head := make([]byte, 2048)
	_, err = f.ReadAt(head, 0)
	must("read "+part, err)
	if binary.LittleEndian.Uint16(head[1024+56:]) != 0xef53 || !bytes.Equal(head[512:1024], label) {
		t.Fatal("a refused request wrote to the partition")
	}
}
