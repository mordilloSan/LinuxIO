package btrfs

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/common/alerts"
)

// scsiDisk is the disk of the scsi_debug module, which a test can delete
// from its SCSI bus and scan back, as a disk that drops off the bus and
// returns: the delete deletes its block device whoever holds it, and the
// scan adds a new one over the module's RAM store, with what the disk held.
// A loop device cannot be deleted while a pool holds it, and a
// device-mapper error table keeps the block device.
type scsiDisk struct {
	t    *testing.T
	scan string // the scan file of the module's SCSI host
	dev  string // the disk's block device; "" while it is deleted
}

// newSCSIDisk loads scsi_debug with one disk of sizeMB MiB and unloads it
// when the test ends. It skips where the module is missing, or loaded
// already, whose disks are another's.
func newSCSIDisk(t *testing.T, sizeMB int) *scsiDisk {
	t.Helper()
	if _, err := os.Stat("/sys/module/scsi_debug"); err == nil {
		t.Skip("scsi_debug is loaded already; its disks are another's")
	}
	if out, err := exec.Command("modprobe", "scsi_debug", "dev_size_mb="+strconv.Itoa(sizeMB), "num_tgts=1", "max_luns=1").CombinedOutput(); err != nil {
		t.Skipf("no scsi_debug module to unplug a disk with: %v: %s", err, out)
	}
	t.Cleanup(func() {
		_ = exec.Command("udevadm", "settle").Run()
		if out, err := exec.Command("modprobe", "-r", "scsi_debug").CombinedOutput(); err != nil {
			t.Errorf("unload scsi_debug: %v: %s", err, out)
		}
	})
	hosts, err := filepath.Glob("/sys/bus/pseudo/drivers/scsi_debug/adapter*/host*")
	if err != nil || len(hosts) != 1 {
		t.Fatalf("scsi_debug hosts = %v, %v; want one", hosts, err)
	}
	d := &scsiDisk{t: t, scan: filepath.Join("/sys/class/scsi_host", filepath.Base(hosts[0]), "scan")}
	d.find()
	return d
}

// find waits for the disk's block device and its node.
func (d *scsiDisk) find() {
	d.t.Helper()
	if err := d.lookup(); err != nil {
		d.t.Fatal(err)
	}
}

// lookup waits for the disk's block device and its node, and for udev to
// settle.
func (d *scsiDisk) lookup() error {
	deadline := time.Now().Add(20 * time.Second)
	for {
		blocks, _ := filepath.Glob("/sys/bus/pseudo/drivers/scsi_debug/adapter*/host*/target*/*/block/*")
		if len(blocks) == 1 {
			dev := "/dev/" + filepath.Base(blocks[0])
			if _, err := os.Stat(dev); err == nil {
				d.dev = dev
				if out, err := exec.Command("udevadm", "settle").CombinedOutput(); err != nil {
					return fmt.Errorf("udevadm settle: %w: %s", err, out)
				}
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the scsi_debug disk never appeared: %v", blocks)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// unplug deletes the disk from its SCSI bus, as a disk that drops off it:
// the kernel deletes its block device and the node goes.
func (d *scsiDisk) unplug() {
	d.t.Helper()
	name := filepath.Base(d.dev)
	if err := os.WriteFile(filepath.Join("/sys/class/block", name, "device", "delete"), []byte("1"), 0o200); err != nil {
		d.t.Fatalf("delete %s: %v", d.dev, err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		if _, err := os.Stat(d.dev); errors.Is(err, os.ErrNotExist) {
			d.dev = ""
			return
		}
		if time.Now().After(deadline) {
			d.t.Fatalf("%s is still there after its delete", d.dev)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// plug scans the SCSI host, which finds the disk again as a new block device.
func (d *scsiDisk) plug() {
	d.t.Helper()
	if err := os.WriteFile(d.scan, []byte("- - -"), 0o200); err != nil {
		d.t.Fatalf("scan %s: %v", d.scan, err)
	}
	d.find()
}

// dropoutPool is the two-disk raid1 pool of the dropout test: a loop disk
// and the scsi_debug disk that drops out, with the files written to it.
type dropoutPool struct {
	*itPool
	a     *loopDisk
	s     *scsiDisk
	files map[string][32]byte
}

// write writes n files of 16 MiB to the pool and syncs them.
func (p *dropoutPool) write(prefix string, n int) {
	p.t.Helper()
	for i := range n {
		name := fmt.Sprintf("%s-%02d.bin", prefix, i)
		p.files[name] = writeRandom(p.t, filepath.Join(p.mnt, name), 16<<20)
	}
	unix.Sync()
}

// health runs the health check and reports whether it started a resync of
// the pool.
func (p *dropoutPool) health(what string) bool {
	p.t.Helper()
	actions, err := CheckHealth(p.ctx)
	p.must("health run "+what, err)
	return slices.Contains(actions.Resync, p.uuid)
}

func (p *dropoutPool) state() poolState {
	p.t.Helper()
	states, err := loadPoolStates()
	p.must("pool state", err)
	return states[p.uuid]
}

// member is the pool's device other than devid 1, the loop disk.
func (p *dropoutPool) member() BtrfsDevice {
	p.t.Helper()
	devices := p.filesystem().Devices
	i := slices.IndexFunc(devices, func(d BtrfsDevice) bool { return d.ID != 1 })
	if i < 0 || devices[0].ID != 1 {
		p.t.Fatalf("devices = %+v; want devid 1 and the scsi_debug disk", devices)
	}
	return devices[i]
}

// alertSays reports whether the alert with key is up and its message
// contains text.
func (p *dropoutPool) alertSays(key, text string) bool {
	p.t.Helper()
	doc, err := alerts.Load()
	p.must("alerts", err)
	return slices.ContainsFunc(doc.Alerts, func(x alerts.Alert) bool { return x.Key == key && strings.Contains(x.Message, text) })
}

// TestBtrfsDropoutIntegration drops a disk out of a mounted two-disk raid1
// pool while the pool is written, a scsi_debug disk deleted from its SCSI
// bus, and checks against the real kernel what LinuxIO makes of it. A kernel
// without btrfs_remove_bdev keeps the member with its path, and LinuxIO
// counts it missing and dropped out: the health run opens the episode while
// the pool runs on the other disk, and the resync waits, also once the disk
// is back, which the mounted pool does not take, and counts no failed
// attempt. Once the pool is mounted again with the disk, the health run
// starts the resync, whose scrub rewrites the copies the disk missed, after
// which that disk alone holds every file and scrubs clean. Run it with
// `make test-btrfs-integration` (scsi_debug is in Ubuntu's linux-modules).
func TestBtrfsDropoutIntegration(t *testing.T) {
	if os.Getenv("LINUXIO_RUN_BTRFS_INTEGRATION") != "1" {
		t.Skip("set LINUXIO_RUN_BTRFS_INTEGRATION=1 and run as root")
	}
	if os.Geteuid() != 0 {
		t.Fatal("the btrfs integration test needs root")
	}
	t.Setenv("TMPDIR", "/var/tmp")
	dir := t.TempDir()
	stubStatePaths(t, dir)
	var uts unix.Utsname
	if err := unix.Uname(&uts); err != nil {
		t.Fatal(err)
	}
	t.Logf("kernel %s", unix.ByteSliceToString(uts.Release[:]))

	p := &dropoutPool{itPool: &itPool{t: t, ctx: t.Context(), mnt: filepath.Join(dir, "mnt")}, files: map[string][32]byte{}}
	p.a = newLoopDisk(t, filepath.Join(dir, "a.img"), 1<<30)
	p.s = newSCSIDisk(t, 1024)
	// Registered after the disks, so it runs before they go: the pool is
	// unmounted, a disk left deleted is scanned back so the kernel's entry
	// for it can be forgotten by its path, and no path is ever empty, which
	// would make the kernel forget every unmounted btrfs device of the host.
	t.Cleanup(func() {
		_ = unix.Unmount(p.mnt, 0)
		if p.s.dev == "" {
			if err := os.WriteFile(p.s.scan, []byte("- - -"), 0o200); err != nil {
				t.Errorf("scan the deleted disk back: %v", err)
			} else if err = p.s.lookup(); err != nil {
				t.Errorf("scan the deleted disk back: %v", err)
			}
		}
		_ = exec.Command("udevadm", "settle").Run()
		for _, dev := range []string{p.a.dev, p.s.dev} {
			if dev != "" {
				_ = controlIoctl(iocForgetDev, dev)
			}
		}
	})
	uuid, err := CreatePool(p.ctx, "linuxio-dropout", "raid1", "raid1", []string{p.a.dev, p.s.dev}, true, nil, nil)
	p.must("create pool", err)
	p.uuid = uuid
	p.must("mount", MountPool(p.ctx, p.a.dev, p.mnt, []string{"noatime"}, false))
	p.write("before", 4)
	if p.health("of the whole pool") {
		t.Fatal("the health run of the whole pool started a resync")
	}
	if whole := p.member(); whole.Missing || canonicalPath(whole.Path) != p.s.dev {
		t.Fatalf("the scsi_debug disk in the whole pool = %+v, want %s present", whole, p.s.dev)
	}

	old := p.dropOut()
	p.heldWhileMounted(old)
	p.resyncOnceMountedAgain()
	p.returnedDiskHoldsEverything()
}

// dropOut deletes the scsi_debug disk under the mounted pool while it is
// written, and checks that it counts missing and dropped out, that the
// health run opens the episode without it and starts no resync, and that
// the health alert says it dropped out. It returns the disk's old path.
func (p *dropoutPool) dropOut() string {
	t := p.t
	old := p.s.dev
	p.s.unplug()
	p.write("dropout", 4)
	gone := p.member()
	devinfo := func(attr string) string {
		b, readErr := os.ReadFile(filepath.Join("/sys/fs/btrfs", p.uuid, "devinfo", strconv.Itoa(gone.ID), attr))
		if readErr != nil {
			return readErr.Error()
		}
		return strings.TrimSpace(string(b))
	}
	_, linkErr := os.Stat(filepath.Join("/sys/fs/btrfs", p.uuid, "devices", filepath.Base(old)))
	t.Logf("%s deleted under the mounted pool: DEV_INFO path %q, devinfo missing=%s writeable=%s, its devices link: %v; LinuxIO reads %+v",
		old, gone.Path, devinfo("missing"), devinfo("writeable"), linkErr, gone)
	// A kernel with btrfs_remove_bdev (7.1, or 6.19 and 7.0 built with
	// CONFIG_BTRFS_EXPERIMENTAL) marks the member missing itself.
	kernelMissing := devinfo("missing") == "1"
	switch {
	case kernelMissing:
		if !gone.Missing || gone.DroppedOut || gone.Path != "" {
			t.Fatalf("the member the kernel marked missing reads %+v; want missing, with no path", gone)
		}
	case !gone.Missing || !gone.DroppedOut || gone.Path != old || gone.Errors.Write == 0:
		t.Fatalf("the dropped member reads %+v; want missing and dropped out at %s, with the writes that failed", gone, old)
	}
	if p.health("after the dropout") {
		t.Fatal("the health run started a resync while the disk is out")
	}
	state := p.state()
	if !state.Degraded || !slices.Equal(state.Present, []uint64{1}) || !slices.ContainsFunc(state.Missing, func(m missingDisk) bool { return m.ID == uint64(gone.ID) && m.UUID == gone.UUID }) {
		t.Fatalf("state after the dropout = %+v; want an episode without devid %d", state, gone.ID)
	}
	if !kernelMissing && !p.alertSays("health-"+p.uuid, old+" dropped out while the pool was mounted") {
		t.Fatalf("the health alert does not say %s dropped out", old)
	}
	return old
}

// heldWhileMounted brings the disk back while the pool is still mounted,
// which does not take it, and checks that the member still counts missing,
// that no resync starts, and that one started anyway counts no failure.
func (p *dropoutPool) heldWhileMounted(old string) {
	t := p.t
	p.s.plug()
	t.Logf("the disk is back as %s (it was %s); registering it while the pool is mounted: %v", p.s.dev, old, RegisterDevice(p.s.dev))
	back := p.member()
	t.Logf("with the disk back LinuxIO reads %+v", back)
	if !back.Missing {
		t.Fatalf("the mounted pool took the disk back: %+v", back)
	}
	if p.health("with the disk back") {
		t.Fatal("the health run started a resync while the pool holds the gone block device")
	}
	if err := Resync(p.ctx, p.uuid, nil); !errors.Is(err, errDiskMissing) {
		t.Fatalf("Resync while the pool holds the gone block device = %v, want %v", err, errDiskMissing)
	}
	if state := p.state(); state.ResyncFailures != 0 || !state.Degraded {
		t.Fatalf("state after the held resync = %+v; want the episode, no failure counted", state)
	}
}

// resyncOnceMountedAgain mounts the pool again with the disk back and checks
// that the health run starts the resync, whose scrub corrects the copies the
// disk missed and whose notice says why the disk keeps its counters.
func (p *dropoutPool) resyncOnceMountedAgain() {
	t := p.t
	p.must("unmount", unix.Unmount(p.mnt, 0))
	pools, err := FindOfflinePools(p.ctx, []string{p.a.dev, p.s.dev})
	if err != nil || len(pools) != 1 || len(pools[0].Devices) != 2 {
		t.Fatalf("offline pools with the disk back = %+v, %v", pools, err)
	}
	p.must("mount with the disk back", MountOfflinePool(p.ctx, pools[0], p.mnt, []string{"noatime"}, false))
	if returned := p.member(); returned.Missing || returned.DroppedOut || canonicalPath(returned.Path) != p.s.dev {
		t.Fatalf("the remounted pool reads the returned disk as %+v, want it present at %s", returned, p.s.dev)
	}
	if !p.health("of the remounted pool") {
		t.Fatal("the health run of the remounted pool started no resync")
	}
	p.must("resync", Resync(p.ctx, p.uuid, nil))
	rec, _ := loadScrubRecord(p.uuid)
	t.Logf("the resync scrub corrected %d errors, %d uncorrectable", rec.CorrectedErrors, rec.UncorrectableErrors)
	if rec.CorrectedErrors == 0 || rec.UncorrectableErrors != 0 {
		t.Fatalf("the resync scrub = %+v; want the stale copies corrected", rec.BtrfsScrubStatus)
	}
	if state := p.state(); state.Degraded {
		t.Fatalf("state after the resync = %+v", state)
	}
	if !p.alertSays("resynced-"+p.uuid, "it counts a failed read, write or flush") {
		t.Fatal("the whole again notice does not say the returned disk keeps the writes that failed")
	}
}

// returnedDiskHoldsEverything checks that the returned disk alone holds
// every file, and that every copy on it checks out.
func (p *dropoutPool) returnedDiskHoldsEverything() {
	t := p.t
	p.must("unmount", unix.Unmount(p.mnt, 0))
	p.a.unplug()
	pools, err := FindOfflinePools(p.ctx, []string{p.s.dev})
	if err != nil || len(pools) != 1 {
		t.Fatalf("offline pools of the returned disk = %+v, %v", pools, err)
	}
	p.must("mount the returned disk alone", MountOfflinePool(p.ctx, pools[0], p.mnt, []string{"noatime"}, true))
	for name, want := range p.files {
		if digest(t, filepath.Join(p.mnt, name)) != want {
			t.Fatalf("the returned disk does not hold %s as written", name)
		}
	}
	status, err := ScrubFrom(p.ctx, p.uuid, "", func(BtrfsScrubStatus) {})
	if err != nil || status.ErrorCount != 0 {
		t.Fatalf("scrub of the returned disk alone = %+v, %v; want no errors", status, err)
	}
	p.must("unmount", unix.Unmount(p.mnt, 0))
}
