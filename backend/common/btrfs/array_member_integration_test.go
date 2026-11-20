package btrfs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/sys/unix"
)

// TestBtrfsArrayMemberIntegration builds a raid1 pool whose disk 1 is an md
// 1.0 mirror of two loop devices, whose metadata sits at the end so each leg
// shows the pool's superblock too, and whose disk 2 is the loop device c.
// With c unplugged, the listing must leave the legs out and count one disk
// missing, and the boot check must mount the pool degraded through the
// array: LinuxIO registers the array alone, the mounted pool runs on it, and
// the kernel logs no new registration of a leg. Then, with c back and the
// array stopped, a leg registered by hand (as `btrfs device scan` naming it
// would) must not be opened by Mount degraded through c: the kernel keeps
// one device for each devid, so without the forget the mount would take the
// leg as disk 1. Run it with `make test-btrfs-integration`.
func TestBtrfsArrayMemberIntegration(t *testing.T) {
	if os.Getenv("LINUXIO_RUN_BTRFS_INTEGRATION") != "1" {
		t.Skip("set LINUXIO_RUN_BTRFS_INTEGRATION=1 and run as root")
	}
	if os.Geteuid() != 0 {
		t.Fatal("the btrfs integration test needs root")
	}
	if _, err := os.Stat("/run/systemd/system"); err != nil {
		t.Skip("needs a running systemd and udev")
	}
	mdadm, err := exec.LookPath("mdadm")
	if err != nil {
		t.Skip("needs mdadm")
	}
	r := newArrayRig(t, mdadm)
	r.mountsDegradedThroughTheArray()
	r.forgetsALegRegisteredByHand()
}

const memberITArray = "/dev/md/linuxio-member-it"

// arrayRig is the pool on an md 1.0 mirror (legs leg1 and leg2, node md) and
// on c, with fstab mounting it at target, and the devices LinuxIO registers.
type arrayRig struct {
	t                  *testing.T
	ctx                context.Context
	mdadm              string
	leg1, leg2, c      *loopDisk
	md, uuid, target   string
	scanned            []string
	sum                [32]byte // of target/file, written while the pool was whole
	legRegistrationsAt int
}

func (r *arrayRig) must(what string, err error) {
	r.t.Helper()
	if err != nil {
		r.t.Fatalf("%s: %v", what, err)
	}
}

// forget makes the kernel forget device by its own path, never an empty
// one, which would forget every unmounted btrfs device of the host.
func (r *arrayRig) forget(device string) {
	if device == "" {
		return
	}
	if err := controlIoctl(iocForgetDev, device); err != nil && !errors.Is(err, unix.ENOENT) {
		r.t.Errorf("forget %s: %v", device, err)
	}
}

// newArrayRig builds the array and the pool, writes a file to the pool while
// it is whole, and records each device LinuxIO registers from then on.
func newArrayRig(t *testing.T, mdadm string) *arrayRig {
	t.Setenv("TMPDIR", "/var/tmp")
	dir := t.TempDir()
	stubStatePaths(t, dir)
	r := &arrayRig{t: t, ctx: context.Background(), mdadm: mdadm, target: filepath.Join(dir, "mnt")}
	r.leg1 = newLoopDisk(t, filepath.Join(dir, "leg1.img"), 1<<30)
	r.leg2 = newLoopDisk(t, filepath.Join(dir, "leg2.img"), 1<<30)
	r.c = newLoopDisk(t, filepath.Join(dir, "c.img"), 1<<30)
	// Cleanups run last first: the unmount; the array and c forgotten; the
	// array stopped and its superblocks zeroed, after which udev probes the
	// legs as btrfs and its rule registers them, so they are forgotten once
	// udev is done; last the loop devices are detached.
	t.Cleanup(func() {
		_ = exec.Command(mdadm, "--stop", memberITArray).Run()
		_ = exec.Command(mdadm, "--zero-superblock", r.leg1.dev).Run()
		_ = exec.Command(mdadm, "--zero-superblock", r.leg2.dev).Run()
		_ = exec.Command("udevadm", "settle").Run()
		r.forget(r.leg1.dev)
		r.forget(r.leg2.dev)
	})
	runTool(t, mdadm, "--create", memberITArray, "--run", "--level=1", "--raid-devices=2", "--metadata=1.0", "--assume-clean", r.leg1.dev, r.leg2.dev)
	md, err := filepath.EvalSymlinks(memberITArray)
	r.must("array node", err)
	r.md = md
	t.Cleanup(func() {
		r.forget(r.md)
		r.forget(r.c.dev) // "" once unplugged, which forgot it
	})
	t.Cleanup(func() { _ = unix.Unmount(r.target, 0) })

	r.uuid, err = CreatePool(r.ctx, "linuxio-member-it", "raid1", "raid1", []string{r.md, r.c.dev}, true, nil, nil)
	r.must("create the pool on the array and c", err)
	writeFstab(t, fmt.Sprintf("UUID=%s %s btrfs defaults,noatime,nofail 0 0\n", r.uuid, r.target))
	settleUdev(t)
	for _, leg := range []*loopDisk{r.leg1, r.leg2} {
		if holders, _ := os.ReadDir(filepath.Join("/sys/class/block", filepath.Base(leg.dev), "holders")); len(holders) != 1 {
			t.Fatalf("%s is not held by the array: %v", leg.dev, holders)
		}
	}
	realIoctl := controlIoctl
	controlIoctl = func(req uintptr, device string) error {
		if req == iocScanDev {
			r.scanned = append(r.scanned, device)
		}
		return realIoctl(req, device)
	}
	t.Cleanup(func() { controlIoctl = realIoctl })
	r.legRegistrationsAt = registrations(t, r.leg1.dev) + registrations(t, r.leg2.dev)

	// Whole: the array and c are the pool's disks, the legs are not.
	pool := r.wantPool("the whole pool", []string{r.leg1.dev, r.leg2.dev, r.md, r.c.dev}, []string{r.md, r.c.dev}, 0)
	r.must("mount the whole pool", MountOfflinePool(r.ctx, pool, r.target, []string{"noatime"}, false))
	r.sum = writeRandom(t, filepath.Join(r.target, "file"), 16<<20)
	r.must("unmount the whole pool", unix.Unmount(r.target, 0))
	return r
}

// wantPool checks that the listing of candidates is one pool on disks, with
// missing disks missing and every leg among candidates left out.
func (r *arrayRig) wantPool(what string, candidates, disks []string, missing int) BtrfsOfflinePool {
	r.t.Helper()
	pools, err := FindOfflinePools(r.ctx, candidates)
	r.must("offline pools", err)
	legs := 0
	for _, d := range candidates {
		if d == r.leg1.dev || d == r.leg2.dev {
			legs++
		}
	}
	if len(pools) != 1 || !slices.Equal(pools[0].Devices, disks) || pools[0].Missing != missing || len(pools[0].legs) != legs {
		r.t.Fatalf("%s: %+v, want disks %v, %d missing, %d legs left out", what, pools, disks, missing, legs)
	}
	return pools[0]
}

// wantMounted checks that the pool is mounted degraded at target on disk and
// on no leg, that the mount registered only disk, and that the file reads
// back.
func (r *arrayRig) wantMounted(what, disk string) {
	r.t.Helper()
	fs, err := FindFilesystem(r.ctx, r.uuid)
	r.must(what, err)
	if !slices.ContainsFunc(fs.Mountpoints, func(m Mount) bool { return m.Mountpoint == r.target }) || !fs.Degraded {
		r.t.Fatalf("%s: mounted at %+v, degraded %v; want degraded at %s", what, fs.Mountpoints, fs.Degraded, r.target)
	}
	if paths := devicePaths(fs); !slices.Contains(paths, disk) || slices.Contains(paths, r.leg1.dev) || slices.Contains(paths, r.leg2.dev) {
		r.t.Fatalf("%s: the pool runs on %v, want %s", what, paths, disk)
	}
	if !slices.Equal(r.scanned, []string{disk}) {
		r.t.Fatalf("%s: registered %v, want %s alone", what, r.scanned, disk)
	}
	if digest(r.t, filepath.Join(r.target, "file")) != r.sum {
		r.t.Fatalf("%s: the file read back differs", what)
	}
}

// mountsDegradedThroughTheArray unplugs c and has the boot check mount the
// pool degraded through the array.
func (r *arrayRig) mountsDegradedThroughTheArray() {
	r.c.unplug()
	settleUdev(r.t)
	r.wantPool("without c", []string{r.leg1.dev, r.leg2.dev, r.md}, []string{r.md}, 1)
	r.scanned = nil
	r.must("boot check", MountDegradedPools(r.ctx, 0))
	r.wantMounted("after the boot check", r.md)
	if after := registrations(r.t, r.leg1.dev) + registrations(r.t, r.leg2.dev); after != r.legRegistrationsAt {
		r.t.Fatalf("the kernel registered a leg: %d registrations of the legs, %d before", after, r.legRegistrationsAt)
	}
	r.must("unmount the degraded pool", unix.Unmount(r.target, 0))
}

// forgetsALegRegisteredByHand plugs c back, stops the array, registers a
// leg by hand and mounts the pool degraded through c, as Mount degraded on
// the overview does.
func (r *arrayRig) forgetsALegRegisteredByHand() {
	r.c.plug()
	settleUdev(r.t)
	r.forget(r.md)
	runTool(r.t, r.mdadm, "--stop", memberITArray)
	settleUdev(r.t)
	if holders, _ := os.ReadDir(filepath.Join("/sys/class/block", filepath.Base(r.leg1.dev), "holders")); len(holders) > 0 {
		r.t.Fatalf("udev assembled the array again after its stop: %s is held", r.leg1.dev)
	}
	r.must("register a leg by hand", RegisterDevice(r.leg1.dev))
	pool := r.wantPool("with the array stopped", []string{r.leg1.dev, r.leg2.dev, r.c.dev}, []string{r.c.dev}, 1)
	r.scanned = nil
	r.must("mount degraded through c", MountOfflinePool(r.ctx, pool, r.target, []string{"noatime"}, true))
	r.wantMounted("mounted through c", r.c.dev)
	r.must("unmount the pool mounted through c", unix.Unmount(r.target, 0))
}
