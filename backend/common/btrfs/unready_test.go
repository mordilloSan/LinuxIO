package btrfs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/alerts"
	"github.com/mordilloSan/LinuxIO/backend/common/systemd"
)

// dataDeviceUnit is the device unit of /dev/disk/by-uuid/<dataUUID>, which
// the mount unit of an fstab line "UUID=<dataUUID> ..." waits for.
const dataDeviceUnit = `dev-disk-by\x2duuid-9a2b4c6d\x2d1e3f\x2d4a5b\x2d8c7d\x2d9e0f1a2b3c4d.device`

// bootSystemd stands in for systemd at boot: its units, what it was asked
// to start, and what a start does. A unit not in units reads as one no file
// defines.
type bootSystemd struct {
	units   map[string]systemd.UnitState
	lists   int
	started []string
	onStart func(name string)
	onList  func(lists int)
}

func stubBootSystemd(t *testing.T, units map[string]systemd.UnitState) *bootSystemd {
	t.Helper()
	s := &bootSystemd{units: units}
	oldList, oldStart, oldWait, oldSettle := unitStates, startMountUnit, bringUpWait, udevSettle
	unitStates = func(_ context.Context, names []string) (map[string]systemd.UnitState, error) {
		s.lists++
		if s.onList != nil {
			s.onList(s.lists)
		}
		read := map[string]systemd.UnitState{}
		for _, name := range names {
			u, ok := s.units[name]
			if !ok {
				u = systemd.UnitState{LoadState: "not-found", ActiveState: "inactive"}
			}
			read[name] = u
		}
		return read, nil
	}
	startMountUnit = func(_ context.Context, name string) error {
		s.started = append(s.started, name)
		if s.onStart != nil {
			s.onStart(name)
		}
		return nil
	}
	bringUpWait, udevSettle = 50*time.Millisecond, 200*time.Millisecond
	t.Cleanup(func() { unitStates, startMountUnit, bringUpWait, udevSettle = oldList, oldStart, oldWait, oldSettle })
	return s
}

// waitingUnits is systemd at a boot where udev left the pool unready: the
// mount unit systemd generated from the fstab line of /data is loaded and
// waits for a device unit that never started.
func waitingUnits() map[string]systemd.UnitState {
	return map[string]systemd.UnitState{
		"data.mount":   {LoadState: "loaded", ActiveState: "inactive", SourcePath: FstabPath},
		dataDeviceUnit: {LoadState: "loaded", ActiveState: "inactive"},
	}
}

// stubDevicesReady answers BTRFS_IOC_DEVICES_READY with ready for every
// device and records the devices asked about.
func stubDevicesReady(t *testing.T, ready bool) *[]string {
	t.Helper()
	var asked []string
	old := devicesReady
	devicesReady = func(device string) (bool, error) {
		asked = append(asked, device)
		return ready, nil
	}
	t.Cleanup(func() { devicesReady = old })
	return &asked
}

// stubMemberSignatures answers the md and LVM signature read with kinds by
// device ("" for none) and records the devices read.
func stubMemberSignatures(t *testing.T, kinds map[string]string) *[]string {
	t.Helper()
	var read []string
	old := memberSignature
	memberSignature = func(device string) (MemberSignature, error) {
		read = append(read, device)
		return MemberSignature{Device: device, Kind: kinds[device]}, nil
	}
	t.Cleanup(func() { memberSignature = old })
	return &read
}

// udevDisk is a block device of the stubbed sysfs as udev's database shows
// it: props are its E: properties, nil when udev never processed it.
type udevDisk struct {
	name, devnum string
	props        map[string]string
	holders      []string
}

// scanned is what udev's database holds for a disk of the data pool that the
// btrfs rule scanned and marked ready ("1") or not ("0").
func scanned(ready string) map[string]string {
	return map[string]string{"ID_FS_TYPE": "btrfs", "ID_FS_UUID": dataUUID, "ID_BTRFS_READY": ready}
}

// writeUdevDisks gives each disk, in the sysfs stubDisks made, its dev and
// uevent attributes and its holders, and its entry in a udev database of its
// own.
func writeUdevDisks(t *testing.T, disks ...udevDisk) {
	t.Helper()
	old := udevDataDir
	udevDataDir = t.TempDir()
	t.Cleanup(func() { udevDataDir = old })
	for _, d := range disks {
		dir := filepath.Join(sysBlockDir, d.name)
		files := map[string]string{filepath.Join(dir, "dev"): d.devnum + "\n", filepath.Join(dir, "uevent"): ""}
		if d.props != nil {
			var db strings.Builder
			db.WriteString("S:disk/by-uuid/" + dataUUID + "\n")
			for k, v := range d.props {
				db.WriteString("E:" + k + "=" + v + "\n")
			}
			files[filepath.Join(udevDataDir, "b"+d.devnum)] = db.String()
		}
		for _, h := range d.holders {
			files[filepath.Join(dir, "holders", h)] = ""
		}
		for path, content := range files {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// completePool is a complete two-disk pool on sdb and sdc that fstab mounts
// at /data with options, unmounted until mount is set. udev scanned both, and
// left sdb, scanned first, not ready, as the race does. The kernel behind
// /data is the fake one, so the alerts of a mounted pool can be read, and no
// disk carries an md or LVM signature.
func completePool(t *testing.T, options string) (mounted *bool) {
	t.Helper()
	stubKernel(t, raid1Kernel())
	member := func(devid uint64) Superblock {
		return Superblock{UUID: dataUUID, Label: "data", DevID: devid, NumDevices: 2, Generation: 90}
	}
	stubDisks(t, map[string]Superblock{"sdb": member(1), "sdc": member(2)})
	writeUdevDisks(t, udevDisk{name: "sdb", devnum: "8:16", props: scanned("0")}, udevDisk{name: "sdc", devnum: "8:32", props: scanned("1")})
	stubMemberSignatures(t, nil)
	writeFstab(t, "UUID="+dataUUID+" /data btrfs "+options+" 0 0\n")
	mounted = new(bool)
	old := listMounts
	listMounts = func(context.Context) ([]Mount, error) {
		if *mounted {
			return []Mount{{Mountpoint: "/data", UUID: dataUUID, SubvolID: 5}}, nil
		}
		return nil, nil
	}
	t.Cleanup(func() { listMounts = old })
	return mounted
}

// uevents returns what was written to each member's uevent attribute.
func uevents(t *testing.T) []string {
	t.Helper()
	var got []string
	for _, name := range []string{"sdb", "sdc"} {
		b, err := os.ReadFile(filepath.Join(sysBlockDir, name, "uevent"))
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, name+":"+string(b))
	}
	return got
}

// A complete pool whose device systemd never found, as when udev scanned
// both disks at once and left the one /dev/disk/by-uuid points to unready:
// the boot check reads each disk's md and LVM signature, asks the kernel,
// has udev evaluate the disk it left unready again, as the rule's trigger
// would, and asks systemd to start the fstab mount, which merges into the
// boot's own mount job. systemd mounts it; LinuxIO mounts nothing itself and
// resolves the offline alert a health run raised. A second run finds it
// mounted.
func TestBootCheckEvaluatesTheDisksOfAPoolSystemdDidNotFind(t *testing.T) {
	mounted := completePool(t, "defaults,noatime,nofail")
	calls := stubMountFS(t, nil)
	asked := stubDevicesReady(t, true)
	read := stubMemberSignatures(t, nil)
	RaiseOfflineAlert(context.Background(), BtrfsOfflinePool{UUID: dataUUID, Label: "data", NumDevices: 2}, "")
	sd := stubBootSystemd(t, waitingUnits())
	sd.onStart = func(string) {
		if got := uevents(t); !reflect.DeepEqual(got, []string{"sdb:change", "sdc:"}) {
			t.Errorf("uevents at the start = %v, want sdb's alone", got)
		}
		*mounted = true
	}

	for range 2 {
		if err := MountDegradedPools(context.Background(), 0); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(sd.started, []string{"data.mount"}) {
		t.Fatalf("started = %v, want data.mount once", sd.started)
	}
	if want := []string{"/dev/sdb", "/dev/sdc"}; !reflect.DeepEqual(*asked, want) || !reflect.DeepEqual(*read, want) {
		t.Fatalf("DEVICES_READY asked of %v, signatures read of %v; want %v", *asked, *read, want)
	}
	if len(*calls) != 0 {
		t.Fatalf("LinuxIO mounted the pool itself: %v", *calls)
	}
	if keys := alertKeys(t); keys["offline-"+dataUUID] != "" {
		t.Fatalf("alerts = %v, want the offline alert resolved once mounted", keys)
	}
}

// The bring-up counts and touches only the disks udev's btrfs rule scanned:
// a device udev probed as something else (an md leg with its metadata at the
// end shows the btrfs superblock too), one something holds, one udev never
// processed, or one with an md or LVM signature is neither counted, asked
// about, read nor sent a change, and two disks with one devid do not make a
// pool complete. Such a pool is left as it is.
func TestBootCheckTouchesOnlyDisksUdevScannedAsBtrfs(t *testing.T) {
	member := func(devid uint64) Superblock {
		return Superblock{UUID: dataUUID, Label: "data", DevID: devid, NumDevices: 2, Generation: 90}
	}
	leg := map[string]string{"ID_FS_TYPE": "linux_raid_member", "ID_FS_UUID": "5a6b7c8d-1111-2222-3333-444455556666"}
	for _, tc := range []struct {
		name   string
		supers map[string]Superblock
		disks  []udevDisk
		kinds  map[string]string
		read   []string // the signature reads expected
	}{
		{
			name:   "an md leg in place of the first disk",
			supers: map[string]Superblock{"sdb": member(1), "sdc": member(2)},
			disks:  []udevDisk{{name: "sdb", devnum: "8:16", props: leg}, {name: "sdc", devnum: "8:32", props: scanned("0")}},
		},
		{
			name:   "a held disk in place of the first",
			supers: map[string]Superblock{"sdb": member(1), "sdc": member(2)},
			disks:  []udevDisk{{name: "sdb", devnum: "8:16", props: scanned("1"), holders: []string{"md0"}}, {name: "sdc", devnum: "8:32", props: scanned("0")}},
		},
		{
			name:   "a disk udev never processed",
			supers: map[string]Superblock{"sdb": member(1), "sdc": member(2)},
			disks:  []udevDisk{{name: "sdb", devnum: "8:16"}, {name: "sdc", devnum: "8:32", props: scanned("0")}},
		},
		{
			name:   "two disks with one devid and none with the other",
			supers: map[string]Superblock{"sdb": member(1), "sdd": member(1)},
			disks:  []udevDisk{{name: "sdb", devnum: "8:16", props: scanned("0")}, {name: "sdd", devnum: "8:48", props: scanned("1")}},
		},
		{
			name:   "two disks with one devid beside the other",
			supers: map[string]Superblock{"sdb": member(1), "sdc": member(2), "sdd": member(1)},
			disks:  []udevDisk{{name: "sdb", devnum: "8:16", props: scanned("0")}, {name: "sdc", devnum: "8:32", props: scanned("1")}, {name: "sdd", devnum: "8:48", props: scanned("1")}},
		},
		{
			name:   "an md signature on a disk udev calls btrfs",
			supers: map[string]Superblock{"sdb": member(1), "sdc": member(2)},
			disks:  []udevDisk{{name: "sdb", devnum: "8:16", props: scanned("0")}, {name: "sdc", devnum: "8:32", props: scanned("1")}},
			kinds:  map[string]string{"/dev/sdc": "md"},
			read:   []string{"/dev/sdb", "/dev/sdc"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mounted := completePool(t, "defaults,noatime,nofail")
			stubDisks(t, tc.supers)
			writeUdevDisks(t, tc.disks...)
			read := stubMemberSignatures(t, tc.kinds)
			asked := stubDevicesReady(t, true)
			sd := stubBootSystemd(t, waitingUnits())
			sd.onStart = func(string) { *mounted = true }

			if err := MountDegradedPools(context.Background(), 0); err != nil {
				t.Fatal(err)
			}
			var changed []string
			for _, d := range tc.disks {
				if b, _ := os.ReadFile(filepath.Join(sysBlockDir, d.name, "uevent")); len(b) > 0 {
					changed = append(changed, d.name)
				}
			}
			if len(*asked) != 0 || len(changed) != 0 || len(sd.started) != 0 || !reflect.DeepEqual(*read, tc.read) {
				t.Fatalf("asked %v, changed %v, started %v, read %v; want nothing but reads %v", *asked, changed, sd.started, *read, tc.read)
			}
		})
	}
}

// A device systemd found is systemd's, whatever its mount does: done, under
// way or failed for a reason of its own. On a boot where udev is still busy
// with the disks when the check starts, the device turns up on a later look,
// and the check does nothing and does not wait out its grace.
func TestBootCheckLeavesAPoolWhoseDeviceSystemdFound(t *testing.T) {
	for name, foundAt := range map[string]int{"found at once": 1, "found on the second look": 2} {
		t.Run(name, func(t *testing.T) {
			completePool(t, "defaults,noatime,nofail")
			asked := stubDevicesReady(t, true)
			units := waitingUnits()
			sd := stubBootSystemd(t, units)
			sd.onList = func(lists int) {
				if lists == foundAt {
					units[dataDeviceUnit] = systemd.UnitState{LoadState: "loaded", ActiveState: "active"}
				}
			}

			start := time.Now()
			if err := MountDegradedPools(context.Background(), time.Minute); err != nil {
				t.Fatal(err)
			}
			if took := time.Since(start); took > udevSettle {
				t.Fatalf("the check took %v; it waited out its grace", took)
			}
			if len(sd.started) != 0 || len(*asked) != 0 || slices.Contains(uevents(t), "sdb:change") {
				t.Fatalf("started %v, asked %v, uevents %v; want nothing", sd.started, *asked, uevents(t))
			}
			if sd.lists != foundAt {
				t.Fatalf("looked %d times, want %d", sd.lists, foundAt)
			}
		})
	}
}

// Only a complete pool systemd mounts at boot is re-evaluated: not one fstab
// leaves to a person, not one without a loaded mount unit, and not one the
// degraded path or a replace owns.
func TestUnreadyFstabPoolsAreCompletePoolsSystemdWaitsFor(t *testing.T) {
	member := func(devid uint64) Superblock {
		return Superblock{UUID: dataUUID, Label: "data", DevID: devid, NumDevices: 2, Generation: 90}
	}
	complete := map[string]Superblock{"sdb": member(1), "sdc": member(2)}
	for _, tc := range []struct {
		name   string
		fstab  string
		supers map[string]Superblock
		units  func(map[string]systemd.UnitState)
		want   bool
	}{
		{name: "systemd waits for its device", want: true},
		{name: "no device unit", units: func(u map[string]systemd.UnitState) { delete(u, dataDeviceUnit) }, want: true},
		{name: "systemd found its device", units: func(u map[string]systemd.UnitState) {
			u[dataDeviceUnit] = systemd.UnitState{LoadState: "loaded", ActiveState: "active"}
		}},
		{name: "no unit file for the mount", units: func(u map[string]systemd.UnitState) { delete(u, "data.mount") }},
		{name: "a masked mount unit", units: func(u map[string]systemd.UnitState) {
			u["data.mount"] = systemd.UnitState{LoadState: "masked", ActiveState: "inactive", SourcePath: FstabPath}
		}},
		// Left loaded after an unmount, but no fstab line made it.
		{name: "a mount systemd only saw in mountinfo", units: func(u map[string]systemd.UnitState) {
			u["data.mount"] = systemd.UnitState{LoadState: "loaded", ActiveState: "inactive"}
		}},
		{name: "a mount unit an administrator wrote", units: func(u map[string]systemd.UnitState) {
			u["data.mount"] = systemd.UnitState{LoadState: "loaded", ActiveState: "inactive", SourcePath: ""}
		}},
		{name: "noauto", fstab: "UUID=" + dataUUID + " /data btrfs defaults,noauto 0 0\n"},
		{name: "_netdev", fstab: "UUID=" + dataUUID + " /data btrfs defaults,_netdev 0 0\n"},
		{name: "not in fstab", fstab: "UUID=other /data btrfs defaults 0 0\n"},
		{name: "missing a disk", supers: map[string]Superblock{"sdb": member(1), "sdc": {}}},
		{name: "a replace target", supers: map[string]Superblock{"sdb": member(1), "sdc": member(2), "sdd": member(0)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stubStatePaths(t, t.TempDir())
			stubMounts(t, nil)
			supers := tc.supers
			if supers == nil {
				supers = complete
			}
			stubDisks(t, supers)
			fstab := tc.fstab
			if fstab == "" {
				fstab = "UUID=" + dataUUID + " /data btrfs defaults,noatime,nofail 0 0\n"
			}
			writeFstab(t, fstab)
			units := waitingUnits()
			if tc.units != nil {
				tc.units(units)
			}
			stubBootSystemd(t, units)

			pools, err := unreadyFstabPools(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if got := len(pools) == 1 && pools[0].pool.UUID == dataUUID && reflect.DeepEqual(pools[0].mountpoints, []string{"/data"}); got != tc.want || !tc.want && len(pools) != 0 {
				t.Fatalf("pools = %+v, want the pool: %v", pools, tc.want)
			}
		})
	}
}

// The kernel's own count decides that every member is there, as udev's
// "btrfs ready" does: a pool the kernel does not call ready is left alone.
func TestBootCheckLeavesAPoolTheKernelDoesNotCallReady(t *testing.T) {
	completePool(t, "defaults,noatime,nofail")
	asked := stubDevicesReady(t, false)
	sd := stubBootSystemd(t, waitingUnits())

	if err := MountDegradedPools(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	if len(*asked) == 0 || len(sd.started) != 0 || slices.Contains(uevents(t), "sdb:change") {
		t.Fatalf("asked %v, started %v, uevents %v; want only the question", *asked, sd.started, uevents(t))
	}
}

// When systemd still does not mount the pool, the check says so in the
// pool's offline alert, as a health run would, and fails nothing.
func TestBootCheckRaisesTheOfflineAlertWhenSystemdStillDoesNotMount(t *testing.T) {
	completePool(t, "defaults,noatime,nofail")
	stubDevicesReady(t, true)
	sd := stubBootSystemd(t, waitingUnits())

	if err := MountDegradedPools(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sd.started, []string{"data.mount"}) {
		t.Fatalf("started = %v", sd.started)
	}
	doc, err := alerts.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Alerts) != 1 || doc.Alerts[0].Key != "offline-"+dataUUID || doc.Alerts[0].Severity != alerts.SeverityWarning ||
		!strings.Contains(doc.Alerts[0].Message, "complete but not mounted") {
		t.Fatalf("alerts = %+v, want the offline warning", doc.Alerts)
	}
}

// A member whose uevent cannot be written fails the check, and nothing is
// started for that pool.
func TestBootCheckReportsAFailedReevaluation(t *testing.T) {
	completePool(t, "defaults,noatime,nofail")
	stubDevicesReady(t, true)
	sd := stubBootSystemd(t, waitingUnits())
	if err := os.Remove(filepath.Join(sysBlockDir, "sdb", "uevent")); err != nil {
		t.Fatal(err)
	}

	err := MountDegradedPools(context.Background(), 0)
	if err == nil || !strings.Contains(err.Error(), "/dev/sdb") {
		t.Fatalf("err = %v, want the failed write to sdb", err)
	}
	if len(sd.started) != 0 {
		t.Fatalf("started = %v", sd.started)
	}
}

// A stop while the check waits for systemd ends it there: the pool missing a
// disk beside the unready one is not mounted degraded after the stop.
func TestBootCheckStoppedWhileItWaitsMountsNothing(t *testing.T) {
	completePool(t, "defaults,noatime,nofail")
	calls := stubMountFS(t, nil)
	stubDevicesReady(t, true)
	member := func(devid uint64) Superblock {
		return Superblock{UUID: dataUUID, Label: "data", DevID: devid, NumDevices: 2, Generation: 90}
	}
	const other = "1b2c3d4e-5f60-4a71-8b92-a3b4c5d6e7f8"
	stubDisks(t, map[string]Superblock{"sdb": member(1), "sdc": member(2), "sdd": {UUID: other, DevID: 1, NumDevices: 2, Generation: 7}})
	writeUdevDisks(t, udevDisk{name: "sdb", devnum: "8:16", props: scanned("0")}, udevDisk{name: "sdc", devnum: "8:32", props: scanned("1")})
	writeFstab(t, "UUID="+dataUUID+" /data btrfs defaults,noatime,nofail 0 0\nUUID="+other+" /other btrfs defaults,nofail 0 0\n")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sd := stubBootSystemd(t, waitingUnits())
	sd.onStart = func(string) { cancel() }

	if err := MountDegradedPools(ctx, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want the stop", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("mounted after the stop: %v", *calls)
	}
}

// Disks that come while the check waits for a pool missing one can leave
// their own pool unready too: the check looks again once the wait is over.
func TestBootCheckLooksAgainAfterWaitingForDisks(t *testing.T) {
	mounted := completePool(t, "defaults,noatime,nofail")
	stubDevicesReady(t, true)
	supers := map[string]Superblock{"sdb": {UUID: dataUUID, Label: "data", DevID: 1, NumDevices: 2, Generation: 90}, "sdc": {}}
	stubDisks(t, supers)
	writeUdevDisks(t, udevDisk{name: "sdb", devnum: "8:16", props: scanned("0")}, udevDisk{name: "sdc", devnum: "8:32", props: scanned("1")})
	old := readSuperblock
	looks := 0
	readSuperblock = func(device string) (Superblock, error) {
		if device == "/dev/sdb" {
			if looks++; looks == 3 { // the bring-up's look, then the wait's first
				supers["sdc"] = Superblock{UUID: dataUUID, Label: "data", DevID: 2, NumDevices: 2, Generation: 90}
			}
		}
		return old(device)
	}
	sd := stubBootSystemd(t, waitingUnits())
	sd.onStart = func(string) { *mounted = true }

	if err := MountDegradedPools(context.Background(), time.Minute); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sd.started, []string{"data.mount"}) || !reflect.DeepEqual(uevents(t), []string{"sdb:change", "sdc:"}) {
		t.Fatalf("started %v, uevents %v; want the late pool brought up", sd.started, uevents(t))
	}
}

// A pool whose mount is still under way when the wait ends, or whose state
// the check cannot read, raises no offline alert: delivery would send it at
// once while systemd is still mounting.
func TestBootCheckAlertsOnlyForAPoolKnownNotMounted(t *testing.T) {
	for _, tc := range []struct {
		name    string
		onStart func(sd *bootSystemd, k *fakeKernel, mounted *bool)
		mounts  error // listing the mounts fails from the start on, which the held-pool look then reports
	}{
		{name: "systemd found the device and is still mounting", onStart: func(sd *bootSystemd, _ *fakeKernel, _ *bool) {
			sd.units[dataDeviceUnit] = systemd.UnitState{LoadState: "loaded", ActiveState: "active"}
		}},
		{name: "the mount unit is activating", onStart: func(sd *bootSystemd, _ *fakeKernel, _ *bool) {
			u := sd.units["data.mount"]
			u.ActiveState = "activating"
			sd.units["data.mount"] = u
		}},
		{name: "the mounts cannot be listed", mounts: errors.New("mountinfo unreadable")},
		{name: "the mounted pool cannot be read", onStart: func(_ *bootSystemd, k *fakeKernel, mounted *bool) {
			k.fail = map[uintptr]error{iocFSInfo: errors.New("no answer")}
			*mounted = true
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mounted := completePool(t, "defaults,noatime,nofail")
			k := raid1Kernel()
			stubKernelIoctls(t, k)
			stubDevicesReady(t, true)
			sd := stubBootSystemd(t, waitingUnits())
			sd.onStart = func(string) {
				if tc.onStart != nil {
					tc.onStart(sd, k, mounted)
				}
				if tc.mounts != nil {
					old := listMounts
					listMounts = func(context.Context) ([]Mount, error) { return nil, tc.mounts }
					t.Cleanup(func() { listMounts = old })
				}
			}

			if err := MountDegradedPools(context.Background(), 0); (err != nil) != (tc.mounts != nil) {
				t.Fatalf("err = %v", err)
			}
			if len(sd.started) != 1 {
				t.Fatalf("started = %v", sd.started)
			}
			if keys := alertKeys(t); keys["offline-"+dataUUID] != "" {
				t.Fatalf("alerts = %v, want no offline alert", keys)
			}
		})
	}
}

// stubKernelIoctls answers the filesystem ioctls from k, keeping the mounts
// and paths the caller stubbed.
func stubKernelIoctls(t *testing.T, k *fakeKernel) {
	t.Helper()
	old := fsIoctl
	fsIoctl = k.ioctl
	t.Cleanup(func() { fsIoctl = old })
}
