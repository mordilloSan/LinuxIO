package btrfs

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// loopDevice backs a new loop device with a sparse file of size bytes and
// detaches it when the test ends.
func loopDevice(t *testing.T, file string, size int64) string {
	t.Helper()
	f, err := os.Create(file)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err = f.Truncate(size); err != nil {
		t.Fatal(err)
	}
	dev, fd := attachLoop(t, f)
	t.Cleanup(func() { detachLoop(fd) })
	return dev
}

// attachLoop backs a free loop device with f and returns it with the
// descriptor that detaches it.
func attachLoop(t *testing.T, f *os.File) (string, int) {
	t.Helper()
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
		// Partition scanning lets PartitionDisk's table show up as loopNp1.
		if err := unix.IoctlLoopSetStatus64(fd, &unix.LoopInfo64{Flags: unix.LO_FLAGS_PARTSCAN}); err != nil {
			detachLoop(fd)
			t.Fatalf("enable partition scanning on %s: %v", dev, err)
		}
		return dev, fd
	}
	t.Fatal("no free loop device")
	return "", -1
}

func detachLoop(fd int) {
	_ = unix.IoctlSetInt(fd, unix.LOOP_CLR_FD, 0)
	unix.Close(fd)
}

func devicePaths(fs BtrfsFilesystem) []string {
	var paths []string
	for _, d := range fs.Devices {
		paths = append(paths, d.Path)
	}
	slices.Sort(paths)
	return paths
}

// itPool is the pool the integration test builds on loop devices.
type itPool struct {
	t     *testing.T
	ctx   context.Context
	uuid  string
	mnt   string
	disks []string
}

// must fails the test with what when err is set.
func (p *itPool) must(what string, err error) {
	p.t.Helper()
	if err != nil {
		p.t.Fatalf("%s: %v", what, err)
	}
}

func (p *itPool) filesystem() BtrfsFilesystem {
	p.t.Helper()
	fs, err := FindFilesystem(p.ctx, p.uuid)
	p.must("inventory", err)
	return fs
}

// TestBtrfsIntegration drives every native operation against a real kernel
// on loop devices: pool create, mount, inventory, subvolumes, snapshots,
// quotas, scrub, balance, replace, add, remove, grow, relabel, send,
// unmounted-pool discovery, wipe and partitioning. Run it with
// `make test-btrfs-integration`.
func TestBtrfsIntegration(t *testing.T) {
	if os.Getenv("LINUXIO_RUN_BTRFS_INTEGRATION") != "1" {
		t.Skip("set LINUXIO_RUN_BTRFS_INTEGRATION=1 and run as root")
	}
	if os.Geteuid() != 0 {
		t.Fatal("the btrfs integration test needs root")
	}
	dir := t.TempDir()
	stubStatePaths(t, dir)
	p := &itPool{t: t, ctx: context.Background(), mnt: filepath.Join(dir, "mnt")}
	for i := range 3 {
		// Sparse, and big enough to partition with PartitionSlack to spare.
		p.disks = append(p.disks, loopDevice(t, filepath.Join(dir, fmt.Sprintf("disk%d.img", i)), 2<<30))
	}

	uuid, err := CreatePool(p.ctx, "linuxio-it", "raid1", "raid1", p.disks[:2], true, nil)
	p.must("create pool", err)
	p.uuid = uuid
	p.must("mount pool", MountPool(p.ctx, p.disks[0], p.mnt, []string{"noatime"}, false))
	t.Cleanup(func() { _ = unix.Unmount(p.mnt, 0) })
	fs := p.filesystem()
	if fs.Label != "linuxio-it" || len(fs.Devices) != 2 || fs.Degraded || !fs.HealthKnown || fs.TotalBytes == 0 {
		t.Fatalf("filesystem = %+v", fs)
	}
	for _, a := range fs.Allocation {
		if a.Profile != "raid1" {
			t.Fatalf("allocation %s is %s, want raid1", a.Kind, a.Profile)
		}
	}
	p.must("write", os.WriteFile(filepath.Join(p.mnt, "data.bin"), make([]byte, 4<<20), 0o644))

	snap := p.subvolumes()
	p.devices()
	p.send(filepath.Join(p.mnt, snap.Path))
	p.must("delete snapshot", DeleteSnapshot(p.ctx, p.uuid, snap.Path))
	p.must("delete subvolume", DeleteSubvolume(p.ctx, p.uuid, "share"))
	if _, found, findErr := findSubvolume(p.ctx, p.uuid, "share"); findErr != nil || found {
		t.Fatalf("share still listed: %v", findErr)
	}
	p.offline()
	p.partitioned()
}

// subvolumes exercises subvolume, snapshot, property and quota operations
// and returns the snapshot it took.
func (p *itPool) subvolumes() BtrfsSnapshot {
	t := p.t
	share, err := CreateSubvolume(p.ctx, p.uuid, "share")
	p.must("create subvolume", err)
	if share.ReadOnly || share.UUID == "" {
		t.Fatalf("subvolume = %+v", share)
	}
	p.must("write", os.WriteFile(filepath.Join(p.mnt, "share", "file.txt"), []byte("hello"), 0o644))
	snap, err := CreateSnapshot(p.ctx, p.uuid, "share", KindManual, time.Now())
	p.must("snapshot", err)
	subvols, err := ListSubvolumes(p.ctx, p.uuid)
	p.must("list subvolumes", err)
	i := slices.IndexFunc(subvols, func(s BtrfsSubvolume) bool { return s.Path == snap.Path })
	if i < 0 || !subvols[i].ReadOnly || !subvols[i].IsSnapshot || subvols[i].ParentUUID != share.UUID || subvols[i].CreatedAt == nil {
		t.Fatalf("snapshot in listing: %+v", subvols)
	}
	sv, err := SetSubvolumeReadOnly(p.ctx, p.uuid, "share", true)
	p.must("set read-only", err)
	if !sv.ReadOnly {
		t.Fatal("read-only flag not set")
	}
	sv, err = SetSubvolumeReadOnly(p.ctx, p.uuid, "share", false)
	p.must("clear read-only", err)
	if sv.ReadOnly {
		t.Fatal("read-only flag not cleared")
	}
	_, err = SetSubvolumeCompression(p.ctx, p.uuid, "share", "zstd")
	p.must("compression property", err)
	buf := make([]byte, 16)
	n, err := unix.Getxattr(filepath.Join(p.mnt, "share"), "btrfs.compression", buf)
	p.must("read compression xattr", err)
	if string(buf[:n]) != "zstd" {
		t.Fatalf("compression xattr = %q", buf[:n])
	}
	p.quotas()
	return snap
}

func (p *itPool) quotas() {
	t := p.t
	p.must("enable quotas", SetQuotas(p.ctx, p.uuid, true))
	limit := uint64(1 << 30)
	sv, err := SetQgroupLimit(p.ctx, p.uuid, "share", &limit)
	p.must("qgroup limit", err)
	if sv.MaxReferencedBytes == nil || *sv.MaxReferencedBytes != limit {
		t.Fatalf("limit = %v", sv.MaxReferencedBytes)
	}
	sv, err = SetQgroupLimit(p.ctx, p.uuid, "share", nil)
	p.must("clear qgroup limit", err)
	if sv.MaxReferencedBytes != nil {
		t.Fatalf("limit not cleared: %v", *sv.MaxReferencedBytes)
	}
	settings, err := GetSettings(p.ctx, p.uuid)
	p.must("settings", err)
	if !settings.QuotasEnabled {
		t.Fatal("quotas not reported enabled")
	}
	p.must("disable quotas", SetQuotas(p.ctx, p.uuid, false))
}

// devices exercises scrub, balance, replace, add, remove, grow and relabel.
func (p *itPool) devices() {
	t := p.t
	unix.Sync() // commit the written files so the scrub reads their data too
	s, err := Scrub(p.ctx, p.uuid, func(BtrfsScrubStatus) {})
	p.must("scrub", err)
	if s.Status != "finished" || s.ErrorCount != 0 || s.BytesScrubbed == 0 {
		t.Fatalf("scrub = %+v", s)
	}
	usage := 50
	p.must("balance", Balance(p.ctx, p.uuid, BalanceOptions{DataUsageMax: &usage}, func(float64) {}))
	p.must("replace", Replace(p.ctx, p.uuid, 2, p.disks[2], func(float64) {}))
	_, err = ReplaceResult(p.ctx, p.uuid, 2, p.disks[2])
	p.must("replace result", err)
	p.must("add device", AddDevice(p.ctx, p.uuid, p.disks[1], true))
	fs := p.filesystem()
	i := slices.IndexFunc(fs.Devices, func(d BtrfsDevice) bool { return d.Path == p.disks[1] })
	if len(fs.Devices) != 3 || i < 0 {
		t.Fatalf("after add: %v", devicePaths(fs))
	}
	p.must("remove device", RemoveDevice(p.ctx, p.uuid, fs.Devices[i].ID))
	if got := devicePaths(p.filesystem()); !slices.Equal(got, []string{p.disks[0], p.disks[2]}) {
		t.Fatalf("after remove: %v", got)
	}
	p.must("grow", GrowDevice(p.ctx, p.uuid, 1))
	p.must("relabel", SetLabel(p.ctx, p.uuid, "linuxio-it2"))
	if label := p.filesystem().Label; label != "linuxio-it2" {
		t.Fatalf("label = %q", label)
	}
}

// send streams the snapshot at path through the kernel's send ioctl.
func (p *itPool) send(path string) {
	r, w, err := os.Pipe()
	p.must("pipe", err)
	sent := make(chan int64, 1)
	go func() {
		n, _ := io.Copy(io.Discard, r)
		sent <- n
	}()
	sendErr := sendStream(path, "", w)
	_ = w.Close()
	n := <-sent
	_ = r.Close()
	p.must("send", sendErr)
	if n == 0 {
		p.t.Fatal("send produced no stream")
	}
}

// offline unmounts the pool, finds it from the superblocks and wipes it.
func (p *itPool) offline() {
	t := p.t
	p.must("unmount", unix.Unmount(p.mnt, 0))
	pools, err := FindOfflinePools(p.ctx, p.disks)
	p.must("offline pools", err)
	if len(pools) != 1 || pools[0].UUID != p.uuid || pools[0].NumDevices != 2 || len(pools[0].Devices) != 2 {
		t.Fatalf("offline pools: %+v", pools)
	}
	for _, d := range pools[0].Devices {
		p.must("wipe "+d, WipeDevice(d))
	}
	pools, err = FindOfflinePools(p.ctx, p.disks)
	p.must("offline pools after wipe", err)
	if len(pools) != 0 {
		t.Fatalf("pools after wipe: %+v", pools)
	}
}

// partitioned builds a single-disk pool on a GPT partition of a blank disk,
// then a second pool on the free space behind it while the first is mounted.
func (p *itPool) partitioned() {
	part, err := PartitionDisk(p.ctx, p.disks[1])
	p.must("partition", err)
	_, err = CreatePool(p.ctx, "parted", "single", "dup", []string{part}, false, nil)
	p.must("create on partition", err)
	pools, err := FindOfflinePools(p.ctx, []string{part})
	p.must("partitioned pool", err)
	if len(pools) != 1 || pools[0].Label != "parted" {
		p.t.Fatalf("partitioned pool: %+v", pools)
	}

	// With p1 mounted the kernel cannot reread the table, so the new
	// partition must be added on its own.
	p.must("mount partition", MountPool(p.ctx, part, p.mnt, []string{"noatime"}, false))
	p.t.Cleanup(func() { _ = unix.Unmount(p.mnt, 0) })
	free, err := AddPartition(p.ctx, p.disks[1])
	p.must("add partition", err)
	if want := partitionPath(p.disks[1], 2); free != want {
		p.t.Fatalf("added partition %s, want %s", free, want)
	}
	_, err = CreatePool(p.ctx, "freespace", "single", "dup", []string{free}, false, nil)
	p.must("create on free space", err)
	pools, err = FindOfflinePools(p.ctx, []string{free})
	p.must("free space pool", err)
	if len(pools) != 1 || pools[0].Label != "freespace" {
		p.t.Fatalf("free space pool: %+v", pools)
	}
	p.must("unmount partition", unix.Unmount(p.mnt, 0))
	p.must("wipe free space", WipeDevice(free))
	p.must("wipe partition", WipeDevice(part))
}

// itDisk is the loop disk TestPartitionIntegration partitions.
type itDisk struct {
	t        *testing.T
	ctx      context.Context
	dev, mnt string
}

func (d *itDisk) must(what string, err error) {
	d.t.Helper()
	if err != nil {
		d.t.Fatalf("%s: %v", what, err)
	}
}

// busy fails unless err is the kernel's EBUSY.
func (d *itDisk) busy(what string, err error) {
	d.t.Helper()
	if !errors.Is(err, unix.EBUSY) {
		d.t.Fatalf("%s = %v, want EBUSY", what, err)
	}
}

// locked runs fn holding the disk's udev lock, as the bridge does around a
// change. Meanwhile udev cannot re-read the table, which would remove and
// re-add every partition not in use while fn works on or checks them.
func (d *itDisk) locked(fn func()) {
	d.t.Helper()
	unlock, err := LockDisk(d.ctx, d.dev)
	d.must("lock "+d.dev, err)
	defer unlock()
	fn()
}

// create adds the partition want places and returns its path.
func (d *itDisk) create(want kernelPartition) string {
	d.t.Helper()
	part, err := CreatePartition(d.ctx, d.dev, want.start, want.length)
	d.must("create partition", err)
	return part
}

// table fails unless the kernel and both GPT copies list exactly want.
func (d *itDisk) table(want ...kernelPartition) {
	d.t.Helper()
	size, ss, err := diskGeometry(d.dev)
	d.must("disk geometry", err)
	kernel, err := kernelPartitions(d.dev)
	d.must("kernel partitions", err)
	var gpt []kernelPartition
	entries := readCopies(d.t, d.dev, ss, size/ss).entries
	for i := range len(entries) / gptEntrySize {
		if e := entries[i*gptEntrySize:]; [16]byte(e) != [16]byte{} {
			first, last := binary.LittleEndian.Uint64(e[32:]), binary.LittleEndian.Uint64(e[40:])
			gpt = append(gpt, kernelPartition{i + 1, first * ss, (last - first + 1) * ss})
		}
	}
	if !slices.Equal(kernel, want) || !slices.Equal(gpt, want) {
		d.t.Fatalf("the kernel lists %+v and the GPT %+v, want %+v", kernel, gpt, want)
	}
}

// trims checks that the trim Format runs before mkfs discards for real:
// the loop device punches a hole in its file, so a block written to part
// reads back as zeros, and the progress runs from 0 to 100.
func (d *itDisk) trims(part string) {
	d.t.Helper()
	mark := bytes.Repeat([]byte{0xff}, 4096)
	f, err := os.OpenFile(part, os.O_WRONLY, 0)
	d.must("open "+part, err)
	_, err = f.WriteAt(mark, 128<<20)
	d.must("write "+part, err)
	d.must("sync "+part, f.Sync())
	d.must("close "+part, f.Close())
	var pcts []float64
	d.must("trim "+part, trim(d.ctx, []string{part}, func(pct float64) { pcts = append(pcts, pct) }))
	f, err = os.Open(part)
	d.must("open "+part, err)
	defer f.Close()
	_, err = f.ReadAt(mark, 128<<20)
	d.must("read "+part, err)
	if !bytes.Equal(mark, make([]byte, len(mark))) || !slices.Equal(pcts, []float64{0, 100}) {
		d.t.Fatalf("trim of %s: block not discarded or progress %v", part, pcts)
	}
}

// gptBytes reads the first and last 32 KiB of the disk, where the GPT lives.
func (d *itDisk) gptBytes() []byte {
	d.t.Helper()
	size, err := blockDeviceSize(d.dev)
	d.must("disk size", err)
	head, tail := diskEdges(d.t, d.dev, size)
	return append(head, tail...)
}

// TestPartitionIntegration drives partition management against the real
// kernel on a loop device, holding the disk's udev lock around each change as
// the bridge does: initialize, create, format, mount, delete and initialize
// again. The kernel must refuse to drop a mounted partition and to initialize
// the disk while a partition is mounted or merely open, leaving the table as
// it was; in the open case Initialize must give back the partitions it had
// already dropped. A partition created where a deleted one was must not show
// the old filesystem. Run it with `make test-btrfs-integration`.
func TestPartitionIntegration(t *testing.T) {
	if os.Getenv("LINUXIO_RUN_BTRFS_INTEGRATION") != "1" {
		t.Skip("set LINUXIO_RUN_BTRFS_INTEGRATION=1 and run as root")
	}
	if os.Geteuid() != 0 {
		t.Fatal("the partition integration test needs root")
	}
	dir := t.TempDir()
	d := &itDisk{t: t, ctx: context.Background(), dev: loopDevice(t, filepath.Join(dir, "disk.img"), 2<<30), mnt: filepath.Join(dir, "mnt")}
	d.must("create mountpoint", os.Mkdir(d.mnt, 0o755))
	t.Cleanup(func() { _ = unix.Unmount(d.mnt, 0) })
	first := kernelPartition{1, 1 << 20, 256 << 20}
	// Big enough for xfs: MinFormatSize("xfs") is 300 MiB.
	second := kernelPartition{2, 512 << 20, 512 << 20}

	// 1. Initialize: an empty GPT and no partitions.
	d.locked(func() {
		d.must("initialize", InitializeDisk(d.ctx, d.dev))
		d.table()
	})

	// 2 and 3. Create two partitions, formatting and mounting p1 right after
	// the second create under its lock: without the lock, udev would re-read
	// the table when that create closes the disk and remove p1 under mkfs.
	// The kernel and both GPT copies then list both partitions.
	var p1, p2 string
	d.locked(func() { p1 = d.create(first) })

	d.locked(func() { d.trims(p1) })
	d.locked(func() {
		p2 = d.create(second)
		_, err := Format(d.ctx, p1, "ext4", "it", false, nil)
		d.must("format "+p1, err)
		d.must("mount "+p1, unix.Mount(p1, d.mnt, "ext4", 0, ""))
		d.table(first, second)
	})

	// 4. The kernel refuses to drop the mounted p1, before the table is
	// touched, and to open it exclusively.
	d.locked(func() {
		before := d.gptBytes()
		d.busy("delete of mounted "+p1, DeletePartition(d.ctx, d.dev, 1))
		if !bytes.Equal(d.gptBytes(), before) {
			t.Fatal("the refused delete changed the partition table")
		}
	})
	d.busy("exclusive open of mounted "+p1, CheckNotBusy(p1))

	// 5. Unmounted, p1 is deleted and its node goes.
	d.must("unmount "+p1, unix.Unmount(d.mnt, 0))
	d.locked(func() {
		d.must("delete "+p1, DeletePartition(d.ctx, d.dev, 1))
		d.table(second)
	})
	for i := 0; ; i++ {
		_, err := os.Stat(p1)
		if errors.Is(err, os.ErrNotExist) {
			break
		}
		if i == 100 { // waitForNode's 10 s
			t.Fatalf("%s is still there 10 s after its delete: %v", p1, err)
		}
		time.Sleep(100 * time.Millisecond)
	}

	// 6. A partition created in p1's place starts with zeros, so the old
	// ext4 superblock is gone.
	d.locked(func() {
		part := d.create(first)
		f, err := os.Open(part)
		d.must("open "+part, err)
		defer f.Close()
		head := make([]byte, edgeWipeBytes)
		_, err = f.ReadAt(head, 0)
		d.must("read "+part, err)
		if !bytes.Equal(head, make([]byte, edgeWipeBytes)) {
			t.Fatalf("the first %d bytes of the new %s are not zeros", edgeWipeBytes, part)
		}
	})

	// 7. Initialize is refused while p2 is mounted and leaves both
	// partitions. A claimed partition marks its whole disk as claimed too,
	// so Initialize's exclusive open of the disk is what refuses.
	fsType := "ext4"
	if _, err := findCommand("mkfs.xfs"); err == nil {
		fsType = "xfs"
	}
	d.locked(func() {
		_, err := Format(d.ctx, p2, fsType, "", false, nil)
		d.must("format "+p2, err)
		d.must("mount "+p2, unix.Mount(p2, d.mnt, fsType, 0, ""))
	})
	d.locked(func() {
		d.busy("exclusive open of the disk with "+p2+" mounted", CheckNotBusy(d.dev))
		d.busy("initialize with "+p2+" mounted", InitializeDisk(d.ctx, d.dev))
		d.table(first, second)
	})
	d.must("unmount "+p2, unix.Unmount(d.mnt, 0))

	// 8. With p2 merely open nothing is claimed, so the exclusive open of the
	// disk succeeds. kernelPartitions lists p1 before p2, so Initialize drops
	// p1, finds p2 busy and must give p1 back: p1's node is made anew, and
	// the kernel, both GPT copies and the table bytes are as before.
	d.locked(func() {
		f, err := os.Open(p2)
		d.must("open "+p2, err)
		defer f.Close()
		d.must("exclusive open of the disk with "+p2+" open", CheckNotBusy(d.dev))
		var node, renode unix.Stat_t
		d.must("stat "+p1, unix.Stat(p1, &node))
		before := d.gptBytes()
		d.busy("initialize with "+p2+" open", InitializeDisk(d.ctx, d.dev))
		for _, part := range []string{p1, p2} {
			_, err = waitForNode(d.ctx, part)
			d.must("node of "+part, err)
		}
		d.must("stat "+p1, unix.Stat(p1, &renode))
		if renode.Ino == node.Ino {
			t.Fatalf("%s kept its node, so Initialize never dropped and re-added it", p1)
		}
		d.table(first, second)
		if !bytes.Equal(d.gptBytes(), before) {
			t.Fatal("the refused initialize changed the partition table")
		}
	})

	// 9. With nothing open, Initialize empties the table.
	d.locked(func() {
		d.must("initialize", InitializeDisk(d.ctx, d.dev))
		d.table()
	})
}
