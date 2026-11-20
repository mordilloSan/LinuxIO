package btrfs

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/common/systemd"
)

// TestBtrfsUnreadyIntegration stages, on loop devices under the running
// systemd and udev, what a boot leaves when udev scans both disks of a pool
// at once and the trigger in 64-btrfs.rules misses the first: that member
// was evaluated while the other was not registered yet, so udev marks it not
// ready (SYSTEMD_READY=0), /dev/disk/by-uuid/<uuid> points at it, and the
// kernel holds the whole pool. A mount unit as systemd-fstab-generator makes
// it then waits for a device systemd never finds. The boot check has udev
// evaluate that member again and systemd mounts the pool: once while the
// mount job still waits for the device, and once after it gave up. Last, a
// pool whose first disk is an md 1.0 array, stopped, so its leg shows the
// btrfs superblock: the check must neither register the leg nor send it a
// change. Run it with `make test-btrfs-integration`.
func TestBtrfsUnreadyIntegration(t *testing.T) {
	if os.Getenv("LINUXIO_RUN_BTRFS_INTEGRATION") != "1" {
		t.Skip("set LINUXIO_RUN_BTRFS_INTEGRATION=1 and run as root")
	}
	if os.Geteuid() != 0 {
		t.Fatal("the btrfs integration test needs root")
	}
	if _, err := os.Stat("/run/systemd/system"); err != nil {
		t.Skip("needs a running systemd and udev")
	}
	t.Setenv("TMPDIR", "/var/tmp")
	dir := t.TempDir()
	stubStatePaths(t, dir)
	r := &unreadyRig{t: t, ctx: context.Background(), dir: dir, target: filepath.Join(dir, "mnt")}
	r.a = newLoopDisk(t, filepath.Join(dir, "a.img"), 1<<30)
	r.b = newLoopDisk(t, filepath.Join(dir, "b.img"), 1<<30)
	t.Cleanup(func() { _ = unix.Unmount(r.target, 0) })
	uuid, err := CreatePool(r.ctx, "linuxio-unready", "raid1", "raid1", []string{r.a.dev, r.b.dev}, true, nil, nil)
	r.must("create pool", err)
	r.uuid = uuid
	r.must("mountpoint", os.Mkdir(r.target, 0o755))
	writeFstab(t, fmt.Sprintf("UUID=%s %s btrfs defaults,noatime,nofail 0 0\n", uuid, r.target))
	r.unit, err = systemd.EscapePath(r.target)
	r.must("unit name", err)
	r.unit += ".mount"
	r.device, err = systemd.EscapePath("/dev/disk/by-uuid/" + uuid)
	r.must("device unit name", err)
	r.device += ".device"

	for _, gaveUp := range []bool{false, true} {
		stopUnit := r.stage(gaveUp)
		r.bootCheckHasSystemdMountIt()
		r.must("unmount", unix.Unmount(r.target, 0))
		stopUnit()
	}
	r.mdLegIsLeftAlone()
}

type unreadyRig struct {
	t            *testing.T
	ctx          context.Context
	dir          string
	a, b         *loopDisk
	uuid, target string
	unit, device string // the mount unit of target, the device unit of the by-uuid link
}

func (r *unreadyRig) must(what string, err error) {
	r.t.Helper()
	if err != nil {
		r.t.Fatalf("%s: %v", what, err)
	}
}

// fstabMountUnit writes and starts the mount unit systemd-fstab-generator
// makes from the fstab line of pool uuid at where (SourcePath is the fstab),
// with a device timeout of timeout. As at boot, its job waits for the
// by-uuid device. The returned func removes the unit.
func (r *unreadyRig) fstabMountUnit(uuid, where string, timeout time.Duration) func() {
	t := r.t
	unit, err := systemd.EscapePath(where)
	r.must("unit name", err)
	unit += ".mount"
	device, err := systemd.EscapePath("/dev/disk/by-uuid/" + uuid)
	r.must("device unit name", err)
	device += ".device"
	files := map[string]string{
		"/run/systemd/system/" + unit:                                  fmt.Sprintf("[Unit]\nSourcePath=%s\n\n[Mount]\nWhat=/dev/disk/by-uuid/%s\nWhere=%s\nType=btrfs\nOptions=noatime\n", FstabPath, uuid, where),
		"/run/systemd/system/" + device + ".d/linuxio-unready-it.conf": fmt.Sprintf("[Unit]\nJobRunningTimeoutSec=%ds\n", int(timeout.Seconds())),
	}
	remove := func() {
		// Stopped first, which unmounts what it mounted, and its jobs
		// cancelled: the start job and the device job it pulled in would
		// otherwise wait out the device timeout on the host.
		_ = systemd.StopUnit(r.ctx, unit)
		cancelJobs(t, unit, device)
		for path := range files {
			_ = os.Remove(path)
		}
		_ = os.Remove("/run/systemd/system/" + device + ".d")
		_ = systemd.DaemonReload(r.ctx)
		_ = systemd.ResetFailedUnit(r.ctx, unit)
	}
	t.Cleanup(remove)
	for path, content := range files {
		r.must("mkdir", os.MkdirAll(filepath.Dir(path), 0o755))
		r.must("write "+path, os.WriteFile(path, []byte(content), 0o644))
	}
	r.must("daemon-reload", systemd.DaemonReload(r.ctx))
	r.must("start "+unit, systemd.StartUnit(r.ctx, unit))
	return remove
}

// cancelJobs cancels every job systemd has queued for units, as `systemctl
// cancel` does, until none is left.
func cancelJobs(t *testing.T, units ...string) {
	t.Helper()
	for range 50 {
		out, err := exec.Command("systemctl", "list-jobs", "--no-legend", "--plain").Output()
		if err != nil {
			t.Errorf("list jobs: %v", err)
			return
		}
		var ids []string
		for line := range strings.Lines(string(out)) {
			if f := strings.Fields(line); len(f) >= 2 && slices.Contains(units, f[1]) {
				ids = append(ids, f[0])
			}
		}
		if len(ids) == 0 {
			return
		}
		_ = exec.Command("systemctl", append([]string{"cancel"}, ids...)...).Run()
		time.Sleep(100 * time.Millisecond)
	}
	t.Errorf("jobs of %v still queued", units)
}

// jobWaiting reports whether systemd has a job queued for unit.
func (r *unreadyRig) jobWaiting(unit string) bool {
	jobs, err := exec.Command("systemctl", "list-jobs", "--no-legend", "--plain").Output()
	r.must("list jobs", err)
	return strings.Contains(string(jobs), unit+" ")
}

// stage leaves a unready and the by-uuid link on it, the kernel holding both
// disks, and the mount unit's job waiting for the device (a 60 s timeout),
// or, with gaveUp, after a 3 s timeout ended it. It returns the unit's
// removal.
func (r *unreadyRig) stage(gaveUp bool) func() {
	t := r.t
	// Whatever udev still has queued for the disks (mkfs, the last unmount)
	// runs first, or it would evaluate them again.
	settleUdev(t)
	for _, d := range []*loopDisk{r.a, r.b} {
		r.must("forget "+d.dev, controlIoctl(iocForgetDev, d.dev))
	}
	// a is evaluated alone, then b is registered without udev.
	runTool(t, "udevadm", "trigger", "--action=change", "--settle", r.a.dev)
	timeout := 60 * time.Second
	if gaveUp {
		timeout = 3 * time.Second
	}
	stopUnit := r.fstabMountUnit(r.uuid, r.target, timeout)
	r.must("register b", RegisterDevice(r.b.dev))
	if gaveUp {
		time.Sleep(5 * time.Second)
	}
	if ready, err := kernelReady([]string{r.b.dev}); err != nil || !ready {
		t.Fatalf("the kernel does not hold the whole pool: ready %v, %v", ready, err)
	}
	state := r.udevState()
	t.Logf("staged (mount job gave up: %v): %s", gaveUp, state)
	if !strings.Contains(state, "a: ID_BTRFS_READY=0 SYSTEMD_READY=0") || !strings.Contains(state, "link: a") ||
		!strings.Contains(state, "device unit: inactive") || r.mountedUUID(r.target) != "" {
		t.Fatalf("not the state the race leaves: %s, mounted %q", state, r.mountedUUID(r.target))
	}
	// Right before the check: the case it covers.
	if waiting := r.jobWaiting(r.unit); waiting == gaveUp {
		t.Fatalf("mount job waiting: %v, want %v", waiting, !gaveUp)
	}
	return stopUnit
}

// bootCheckHasSystemdMountIt runs the boot check twice and checks that the
// pool is mounted whole at target by systemd's mount unit.
func (r *unreadyRig) bootCheckHasSystemdMountIt() {
	t := r.t
	for range 2 {
		r.must("boot check", MountDegradedPools(r.ctx, 0))
	}
	if got := r.mountedUUID(r.target); got != r.uuid {
		t.Fatalf("after the boot check: %q mounted at %s, want %s", got, r.target, r.uuid)
	}
	if active, err := systemd.GetActiveState(r.ctx, r.unit); err != nil || active != "active" {
		t.Fatalf("%s: %s, %v; want systemd's own mount", r.unit, active, err)
	}
	t.Logf("after the boot check: %s", r.udevState())
	if fs, err := FindFilesystem(r.ctx, r.uuid); err != nil || fs.Degraded {
		t.Fatalf("pool after the boot check: %v, degraded %v", err, fs.Degraded)
	}
}

// mdLegIsLeftAlone builds a pool on an md 1.0 array, whose metadata sits at
// the end so its leg shows the btrfs superblock at the start, and on c, then
// stops the array: by superblocks the leg and c are the whole pool, but udev
// probes the leg as an md member. With c left unready and a mount unit
// waiting, the boot check must leave the pool alone: no registration of the
// leg (the kernel logs each device it registers), no change event, no mount.
func (r *unreadyRig) mdLegIsLeftAlone() {
	t := r.t
	mdadm, err := exec.LookPath("mdadm")
	if err != nil {
		t.Log("mdadm not installed; skipping the md leg")
		return
	}
	leg := newLoopDisk(t, filepath.Join(r.dir, "leg.img"), 1<<30)
	c := newLoopDisk(t, filepath.Join(r.dir, "c.img"), 1<<30)
	const array = "/dev/md/linuxio-unready-it"
	// Registered before the create, so an array a failed command left is
	// stopped too, and before the loop disks are detached.
	t.Cleanup(func() {
		_ = exec.Command(mdadm, "--stop", array).Run()
		_ = exec.Command(mdadm, "--zero-superblock", leg.dev).Run()
	})
	runTool(t, mdadm, "--create", array, "--run", "--force", "--level=1", "--raid-devices=1", "--metadata=1.0", leg.dev)
	md, err := filepath.EvalSymlinks(array)
	r.must("array node", err)
	target := filepath.Join(r.dir, "md-mnt")
	r.must("mountpoint", os.Mkdir(target, 0o755))
	// Should the check mount the pool after all, it is unmounted before the
	// array is stopped and the disks detached.
	t.Cleanup(func() { _ = unix.Unmount(target, 0) })
	uuid, err := CreatePool(r.ctx, "linuxio-unready-md", "raid1", "raid1", []string{md, c.dev}, true, nil, nil)
	r.must("create the pool on the array", err)
	writeFstab(t, fmt.Sprintf("UUID=%s %s btrfs defaults,noatime,nofail 0 0\n", uuid, target))
	settleUdev(t)
	// The test's own devices are forgotten, never every unmounted one: the
	// array before it stops, then c, which is then scanned alone.
	r.must("forget the array", controlIoctl(iocForgetDev, md))
	runTool(t, mdadm, "--stop", array)
	settleUdev(t)
	if holders, _ := os.ReadDir(filepath.Join("/sys/class/block", filepath.Base(leg.dev), "holders")); len(holders) > 0 {
		t.Fatalf("udev assembled the array again after its stop: %s is held", leg.dev)
	}
	props, err := udevProperties(filepath.Base(leg.dev))
	r.must("udev database of the leg", err)
	if props["ID_FS_TYPE"] != "linux_raid_member" {
		t.Fatalf("udev probed the leg as %q, want linux_raid_member", props["ID_FS_TYPE"])
	}
	pools, err := FindOfflinePools(r.ctx, []string{leg.dev, c.dev})
	r.must("offline pools", err)
	if len(pools) != 1 || len(pools[0].Devices) != 2 || missingDisks(pools[0]) != 0 {
		t.Fatalf("by superblocks the leg and c are not the whole pool: %+v", pools)
	}
	r.must("forget c", controlIoctl(iocForgetDev, c.dev))
	runTool(t, "udevadm", "trigger", "--action=change", "--settle", c.dev)
	props, err = udevProperties(filepath.Base(c.dev))
	r.must("udev database of c", err)
	if props["ID_BTRFS_READY"] != "0" {
		t.Fatalf("c alone reads ready to udev: %v", props)
	}
	stopUnit := r.fstabMountUnit(uuid, target, 60*time.Second)
	defer stopUnit()
	before := registrations(t, leg.dev)

	r.must("boot check", MountDegradedPools(r.ctx, 0))
	time.Sleep(2 * time.Second)
	if after := registrations(t, leg.dev); after != before {
		t.Fatalf("the boot check registered the md leg %s with the kernel", leg.dev)
	}
	props, err = udevProperties(filepath.Base(c.dev))
	r.must("udev database of c", err)
	if props["ID_BTRFS_READY"] != "0" {
		t.Fatalf("c was evaluated again: ID_BTRFS_READY=%q", props["ID_BTRFS_READY"])
	}
	if got := r.mountedUUID(target); got != "" {
		t.Fatalf("the pool on the md leg was mounted: %s", got)
	}
}

// registrations counts the kernel log lines of btrfs registering device
// (device_list_add logs "... <device> ... scanned by ...").
func registrations(t *testing.T, device string) int {
	t.Helper()
	out, err := exec.Command("dmesg").Output()
	if err != nil {
		t.Fatalf("dmesg: %v", err)
	}
	n := 0
	for line := range strings.Lines(string(out)) {
		if strings.Contains(line, "scanned by") && strings.Contains(line, device+" ") {
			n++
		}
	}
	return n
}

func settleUdev(t *testing.T) {
	t.Helper()
	runTool(t, "udevadm", "settle")
}

// udevState describes udev's view of a and b, where /dev/disk/by-uuid/<uuid>
// points ("a" or "b") and the state of its device unit.
func (r *unreadyRig) udevState() string {
	t := r.t
	settleUdev(t)
	var parts []string
	for i, d := range []*loopDisk{r.a, r.b} {
		out, err := exec.Command("udevadm", "info", "--query=property", "--property=ID_BTRFS_READY,SYSTEMD_READY", d.dev).Output()
		r.must("udevadm info "+d.dev, err)
		props := strings.Fields(string(out))
		if !strings.Contains(string(out), "SYSTEMD_READY=") {
			props = append(props, "SYSTEMD_READY=unset")
		}
		slices.Sort(props)
		parts = append(parts, []string{"a", "b"}[i]+": "+strings.Join(props, " "))
	}
	link, err := filepath.EvalSymlinks("/dev/disk/by-uuid/" + r.uuid)
	switch {
	case err != nil:
		link = err.Error()
	case link == r.a.dev:
		link = "a"
	case link == r.b.dev:
		link = "b"
	}
	active, err := systemd.GetActiveState(r.ctx, r.device)
	r.must("device unit state", err)
	return strings.Join(append(parts, "link: "+link, "device unit: "+active), "; ")
}

// mountedUUID returns the UUID of the btrfs filesystem mounted at mnt, or "".
func (r *unreadyRig) mountedUUID(mnt string) string {
	mounts, err := ListMounts(r.ctx)
	r.must("list mounts", err)
	for _, m := range mounts {
		if m.Mountpoint == mnt {
			return m.UUID
		}
	}
	return ""
}
