package btrfs

import (
	"context"
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

	uuid, err := CreatePool(p.ctx, "linuxio-it", "raid1", "raid1", p.disks[:2], true)
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
	_, err = CreatePool(p.ctx, "parted", "single", "dup", []string{part}, false)
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
	_, err = CreatePool(p.ctx, "freespace", "single", "dup", []string{free}, false)
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
