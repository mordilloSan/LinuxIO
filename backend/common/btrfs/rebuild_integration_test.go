package btrfs

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/alerts"
	"github.com/mordilloSan/LinuxIO/backend/common/scheduledrun"
)

// TestBtrfsAutoRebuildIntegration runs the automatic rebuild's own code path
// (CheckHealth deciding, Rebuild acting, Resync making the pool whole) on the
// test VM's kernel, with the proof's rig: virtio data disks pulled off the
// PCI bus and plugged back. The disks carry serials (packaging/vm), which is
// the identity the rebuild checks. It rebuilds a raid1 pool onto its
// returned disk in the state an older kernel keeps a dropped member in and
// in the state a degraded mount leaves; refuses a clone of the member on
// another disk, by its identity, and two disks that both claim the member;
// replaces the member onto the pool's hot spare; and, when the returned disk
// dies again during the rebuild, keeps the record, fails loudly, and lets
// LinuxIO's own mount erase the disk the kernel would otherwise "resume"
// past every extent, after which a person's replace makes the pool whole.
func TestBtrfsAutoRebuildIntegration(t *testing.T) {
	if os.Getenv("LINUXIO_RUN_BTRFS_INTEGRATION") != "1" {
		t.Skip("set LINUXIO_RUN_BTRFS_INTEGRATION=1 and run as root")
	}
	if os.Geteuid() != 0 {
		t.Fatal("the btrfs integration test needs root")
	}
	r := newRebuildRig(t, true)
	a := &autoRig{rebuildRig: r}
	oldSpares := SparePath
	SparePath = filepath.Join(r.dir, "spares.json")
	t.Cleanup(func() { SparePath = oldSpares })
	a.requireSerials()
	a.notMissing()
	a.missingState()
	a.refusesWhatIsNotTheMember()
	a.ontoTheSpare()
	a.targetDiesMidRebuild()
}

// autoRig is the proof's rig driving the automatic rebuild.
type autoRig struct {
	*rebuildRig
}

// requireSerials skips the test on disks without the serial the rebuild
// recognises them by: a VM defined before the rig gave them one, until its
// next make vm-reset.
func (a *autoRig) requireSerials() {
	disks, err := wholeDisks()
	a.must("whole disks", err)
	for _, name := range []string{"vdc", "vdd", "vde"} {
		if disk, ok := diskOf("/dev/"+name, disks); !ok || !disk.named() {
			a.t.Skipf("/dev/%s has no serial (%+v): run make vm-reset, which gives the rig's data disks one", name, disk)
		}
	}
}

// fresh zeroes the three disks and forgets what LinuxIO recorded, for a new
// pool.
func (a *autoRig) fresh() {
	a.t.Helper()
	if a.mounted() {
		a.unmount()
	}
	for name, d := range a.disks {
		if d.dev == "" {
			a.must("plug "+name+" back", d.rescan())
		}
		a.must("wipe "+d.dev, zeroSignatures(d.dev))
	}
	for _, path := range []string{PoolStatePath, SparePath, alerts.Path} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			a.t.Fatal(err)
		}
	}
	settle()
}

// health runs the health check and logs what it decided.
func (a *autoRig) health(label string) HealthActions {
	a.t.Helper()
	actions, err := CheckHealth(a.ctx)
	a.must("health run "+label, err)
	st := a.poolState()
	a.t.Logf("health run %s: %+v; record %+v, sighting %+v, refusal %q", label, actions, st.Rebuild, st.Returned, st.RebuildRefusal)
	return actions
}

func (a *autoRig) poolState() poolState {
	a.t.Helper()
	states, err := loadPoolStates()
	a.must("pool state", err)
	return states[a.uuid]
}

// rebuildStarts runs two health runs, the first of which must not start the
// rebuild and the second must.
func (a *autoRig) rebuildStarts(label string) {
	a.t.Helper()
	if slices.Contains(a.health(label+", first sighting").Rebuild, a.uuid) {
		a.t.Fatalf("%s: the rebuild started at the first sighting", label)
	}
	if !slices.Contains(a.health(label+", second sighting").Rebuild, a.uuid) {
		a.t.Fatalf("%s: the rebuild did not start at the second sighting", label)
	}
}

// rebuild runs the rebuild unit's work and checks it ended verified.
func (a *autoRig) rebuild(label string) {
	a.t.Helper()
	start := time.Now()
	err := Rebuild(a.ctx, a.uuid, func(float64) {})
	a.t.Logf("%s: Rebuild took %v: %v", label, time.Since(start).Round(time.Millisecond), err)
	a.kernelLog(label + ": rebuild")
	a.must(label+": rebuild", err)
	if st := a.poolState(); st.Rebuild != nil || len(st.Rebuilt) == 0 {
		a.t.Fatalf("%s: state after the rebuild = %+v, want no record and the disk noted rebuilt onto", label, st)
	}
	if obs, ok := a.alert("rebuilt-" + a.uuid); !ok || obs.Severity != alerts.SeverityInfo {
		a.t.Fatalf("%s: rebuilt alert = %+v, %v", label, obs, ok)
	}
}

// resyncWhole has the next health run start the resync, runs it, and checks
// the pool whole, then again after an unmount and a mount.
func (a *autoRig) resyncWhole(label string, ids []int, want map[string]string) {
	a.t.Helper()
	if !slices.Contains(a.health(label+", after the rebuild").Resync, a.uuid) {
		a.t.Fatalf("%s: the health run did not start the resync", label)
	}
	a.must(label+": resync", Resync(a.ctx, a.uuid, nil))
	if st := a.poolState(); st.Degraded {
		a.t.Fatalf("%s: the resync left the episode open: %+v", label, st)
	}
	a.checkWhole(label+": after the rebuild and the resync", ids, want)
	a.unmount()
	for _, d := range a.disks {
		if d.dev != "" {
			if id, err := readIdentity(d.dev); err == nil && id.FSID == a.uuid {
				a.must("register "+d.dev, RegisterDevice(d.dev))
			}
		}
	}
	a.must("mount whole", MountPool(a.ctx, a.disks["vdc"].dev, a.mnt, []string{"noatime"}, false))
	a.checkWhole(label+": after an unmount and mount", ids, want)
}

func (a *autoRig) alert(key string) (alerts.Alert, bool) {
	doc, err := alerts.Load()
	a.must("alerts", err)
	for _, al := range doc.Alerts {
		if al.Key == key {
			return al, true
		}
	}
	return alerts.Alert{}, false
}

// identityOf is the identity udev reads now for dev.
func (a *autoRig) identityOf(dev string) BtrfsSpareDisk {
	a.t.Helper()
	disks, err := wholeDisks()
	a.must("whole disks", err)
	disk, _ := diskOf(dev, disks)
	return disk
}

// notMissing: the disk drops off the bus under the mounted pool, which keeps
// its dead block device (7.0 without btrfs_remove_bdev), and comes back
// under a new name. Two health runs see it, the rebuild erases it, replaces
// devid 2 onto it reading the other mirror and verifies, and the resync
// that follows declares the pool whole.
func (a *autoRig) notMissing() {
	vdc, vdd := a.disks["vdc"], a.disks["vdd"]
	a.fresh()
	a.createPool("raid1", vdc, vdd)
	a.write("before", 4, 32<<20)
	a.writeNocow("nocow.bin", 64<<20)
	serial := a.identityOf(vdd.dev).Serial
	a.health("whole")
	if ids := a.poolState().Identities; len(ids) != 2 || ids[1].Disk.Serial != serial {
		a.t.Fatalf("identities = %+v, want devid 2 recorded as %s", ids, serial)
	}
	_, returned := a.dropout(vdd, 2, "auto: not missing")
	a.rebuildStarts("not missing")
	a.rebuild("not missing")
	a.t.Logf("not missing: the rebuilt disk is %s", returned)
	a.resyncWhole("not missing", []int{1, 2}, raid1)
}

// missingState: a degraded mount without the disk, which every kernel calls
// missing; the disk returns and udev's scan names the member by its path.
// The rebuild's forget of the erased disk is refused (EBUSY) and accepted,
// and the resync converts the chunks the degraded mount wrote single.
func (a *autoRig) missingState() {
	vdc, vdd := a.disks["vdc"], a.disks["vdd"]
	a.fresh()
	a.createPool("raid1", vdc, vdd)
	a.write("before", 4, 32<<20)
	a.writeNocow("nocow.bin", 64<<20)
	a.health("whole")
	a.unmount()
	was := vdd.dev
	a.must("forget "+was, controlIoctl(iocForgetDev, was))
	vdd.pull()
	a.must("degraded mount", MountPool(a.ctx, vdc.dev, a.mnt, []string{"noatime"}, true))
	a.write("degraded", 4, 32<<20)
	a.rewriteNocow("nocow.bin", 32<<20, 16<<20)
	a.health("degraded")
	returned := vdd.plug()
	a.t.Logf("missing: %s came back as %s", was, returned)
	a.observe("missing: after the return and udev's scan")
	a.rebuildStarts("missing")
	a.rebuild("missing")
	a.resyncWhole("missing", []int{1, 2}, raid1)
}

// refusesWhatIsNotTheMember: with the member's disk off the bus, a copy of
// its superblocks on another disk (vde, another serial) is refused by its
// identity and left as it is; with the disk back too, two disks claim the
// member and neither is taken; once the copy is gone, the disk is.
func (a *autoRig) refusesWhatIsNotTheMember() {
	vdc, vdd, vde := a.disks["vdc"], a.disks["vdd"], a.disks["vde"]
	a.fresh()
	a.createPool("raid1", vdc, vdd)
	a.write("before", 4, 32<<20)
	a.health("whole")
	copies := a.superblocksOf(vdd.dev)
	vdd.pull()
	a.write("out", 2, 32<<20)
	a.writeSuperblocks(vde.dev, copies)
	settle()
	for i := range 3 {
		if slices.Contains(a.health("clone "+strconv.Itoa(i+1)).Rebuild, a.uuid) {
			a.t.Fatal("the rebuild started onto a clone")
		}
	}
	if obs, ok := a.alert("rebuild-" + a.uuid); !ok || obs.Severity != alerts.SeverityError || !strings.Contains(obs.Message, vde.dev+" is not the disk that was device 2") {
		a.t.Fatalf("rebuild alert = %+v, %v; want the clone refused by its identity", obs, ok)
	}
	if err := Rebuild(a.ctx, a.uuid, func(float64) {}); !scheduledrun.AllReported(err) {
		a.t.Fatalf("Rebuild onto the clone = %v, want a reported refusal", err)
	}
	if id, err := readIdentity(vde.dev); err != nil || id.FSID != a.uuid || id.DevID != 2 {
		a.t.Fatalf("the clone on %s was touched: %+v, %v", vde.dev, id, err)
	}
	returned := vdd.plug()
	if slices.Contains(a.health("clone and disk").Rebuild, a.uuid) {
		a.t.Fatal("the rebuild started with two disks claiming the member")
	}
	if obs, _ := a.alert("rebuild-" + a.uuid); !strings.Contains(obs.Message, "all carry the superblock of device 2") {
		a.t.Fatalf("rebuild alert = %+v, want two disks claiming the member", obs)
	}
	a.must("wipe the clone", zeroSignatures(vde.dev))
	settle()
	a.rebuildStarts("the disk alone")
	a.rebuild("the disk alone")
	a.t.Logf("refusals: rebuilt onto %s", returned)
	a.resyncWhole("refusals", []int{1, 2}, raid1)
}

// superblocksOf reads dev's superblock copies at 64 KiB and 64 MiB.
func (a *autoRig) superblocksOf(dev string) map[int64][]byte {
	a.t.Helper()
	f, err := os.Open(dev)
	a.must("open "+dev, err)
	defer f.Close()
	copies := map[int64][]byte{}
	for _, off := range []int64{superOffset, 64 << 20} {
		b := make([]byte, superSize)
		_, err := f.ReadAt(b, off)
		a.must("read "+dev, err)
		copies[off] = b
	}
	return copies
}

// writeSuperblocks writes copies onto dev at their offsets.
func (a *autoRig) writeSuperblocks(dev string, copies map[int64][]byte) {
	a.t.Helper()
	f, err := os.OpenFile(dev, os.O_RDWR, 0)
	a.must("open "+dev, err)
	defer f.Close()
	for off, b := range copies {
		_, err := f.WriteAt(b, off)
		a.must("write "+dev, err)
	}
	a.must("sync "+dev, f.Sync())
}

// ontoTheSpare: the member's disk stays off the bus, and the pool's hot
// spare replaces it at the next health run (delay 0), after which the
// spare is a member and no longer the spare.
func (a *autoRig) ontoTheSpare() {
	vdc, vdd, vde := a.disks["vdc"], a.disks["vdd"], a.disks["vde"]
	a.fresh()
	a.createPool("raid1", vdc, vdd)
	a.write("before", 4, 32<<20)
	a.writeNocow("nocow.bin", 64<<20)
	a.health("whole")
	fs := a.filesystem()
	a.must("set the hot spare", SetSpare(a.ctx, fs, vde.dev, 0))
	vdd.pull()
	a.write("out", 4, 32<<20)
	a.rewriteNocow("nocow.bin", 16<<20, 16<<20)
	if !slices.Contains(a.health("spare").Rebuild, a.uuid) {
		a.t.Fatal("the spare was not used")
	}
	a.rebuild("spare")
	if spares, err := loadSpares(); err != nil || len(spares) != 0 {
		a.t.Fatalf("spares = %+v, %v; want the designation dropped", spares, err)
	}
	if d := a.filesystem().Devices; d[1].Path != vde.dev {
		a.t.Fatalf("devices = %+v, want devid 2 on %s", d, vde.dev)
	}
	a.resyncWhole("spare", []int{1, 2}, raid1)
}

// targetDiesMidRebuild: the returned disk drops out again while the rebuild
// copies onto it. The kernel copies on, installs the dead target and forces
// the pool read-only, and reports success: the rebuild's check refuses it,
// keeps the record as failed and raises an error. LinuxIO's mount, degraded,
// then erases the disk, back with the replace target's superblock, before
// the kernel could resume the replace past every extent, and cancels the
// replace the kernel suspends. No rebuild is tried onto that disk again; a
// person's replace makes the pool whole, and clears the record.
func (a *autoRig) targetDiesMidRebuild() {
	vdc, vdd := a.disks["vdc"], a.disks["vdd"]
	a.fresh()
	a.createPool("raid1", vdc, vdd)
	a.write("before", 4, 32<<20)
	a.writeNocow("nocow.bin", 64<<20)
	a.health("whole")
	_, returned := a.dropout(vdd, 2, "dies again")
	a.rebuildStarts("dies again")
	speed := filepath.Join(sysFSBtrfs, a.uuid, "devinfo", "2", "scrub_speed_max")
	a.must("throttle the replace", sysWrite(speed, strconv.Itoa(16<<20)))
	done := make(chan error, 1)
	go func() { done <- Rebuild(a.ctx, a.uuid, func(float64) {}) }()
	a.waitCopied(2, 96<<20, done)
	a.t.Logf("dies again: pulling %s mid-rebuild", returned)
	vdd.pull()
	_ = sysWrite(speed, "0")
	var err error
	select {
	case err = <-done:
	case <-time.After(10 * time.Minute):
		a.t.Fatal("the rebuild did not end after its target was pulled")
	}
	a.kernelLog("dies again: target pulled")
	a.t.Logf("dies again: Rebuild = %v", err)
	if !scheduledrun.AllReported(err) {
		a.t.Fatalf("Rebuild with its target pulled = %v, want a reported failure", err)
	}
	st := a.poolState()
	if st.Rebuild == nil || st.Rebuild.Failed == "" {
		a.t.Fatalf("record = %+v, want it kept as failed", st.Rebuild)
	}
	if obs, ok := a.alert("rebuild-" + a.uuid); !ok || obs.Severity != alerts.SeverityError {
		a.t.Fatalf("rebuild alert = %+v, %v; want an error", obs, ok)
	}
	if slices.Contains(a.health("after the failure").Resync, a.uuid) {
		a.t.Fatal("the health run started a resync after the failed rebuild")
	}

	a.unmount()
	again := vdd.plug()
	id, err := readIdentity(again)
	a.t.Logf("dies again: %s is back as %s: %+v, %v", returned, again, id, err)
	pools, err := FindOfflinePools(a.ctx, BlockDevices())
	a.must("offline pools", err)
	i := slices.IndexFunc(pools, func(p BtrfsOfflinePool) bool { return p.UUID == a.uuid })
	if i < 0 {
		a.t.Fatalf("offline pools = %+v, want %s", pools, a.uuid)
	}
	a.t.Logf("dies again: offline pool %+v", pools[i])
	a.must("LinuxIO's mount", MountOfflinePool(a.ctx, pools[i], a.mnt, []string{"noatime"}, true))
	a.kernelLog("dies again: LinuxIO's degraded mount")
	if erased, idErr := readIdentity(again); idErr != nil || len(erased.Copies) != 0 {
		a.t.Fatalf("%s still carries superblocks after LinuxIO's mount: %+v, %v", again, erased, idErr)
	}
	state, pct, _, err := replaceStatusOf(a.mnt)
	a.t.Logf("dies again: replace status after the mount: state %d at %.1f%%, %v", state, pct, err)
	if err != nil || state != replaceStateCanceled {
		a.t.Fatalf("replace state %d after LinuxIO's mount (%v), want it canceled", state, err)
	}
	if st := a.poolState(); st.Rebuild == nil || st.Rebuild.Failed == "" {
		a.t.Fatalf("record after the mount = %+v, want it still failed", st.Rebuild)
	}
	if slices.Contains(a.health("after LinuxIO's mount").Rebuild, a.uuid) {
		a.t.Fatal("the rebuild was tried again onto the disk that failed")
	}

	// A person replaces the device onto the erased disk.
	err = Replace(a.ctx, a.uuid, 2, again, func(float64) {})
	a.kernelLog("dies again: a person's replace")
	a.must("a person's replace", err)
	_, err = ReplaceResult(a.ctx, a.uuid, 2, again)
	a.must("a person's replace, checked", err)
	if st := a.poolState(); st.Rebuild != nil {
		a.t.Fatalf("record after a person's replace = %+v, want it cleared", st.Rebuild)
	}
	a.resyncWhole("dies again", []int{1, 2}, raid1)
}
