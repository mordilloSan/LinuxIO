package btrfs

import (
	"context"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/common/alerts"
	"github.com/mordilloSan/LinuxIO/backend/common/scheduledrun"
)

// stubStatePaths points every file the degraded mount and resync read or
// write at dir: pool state, fstab, scrub records and alerts.
func stubStatePaths(t *testing.T, dir string) {
	t.Helper()
	oldState, oldFstab, oldScrub, oldAlerts := PoolStatePath, FstabPath, ScrubRecordPath, alerts.Path
	PoolStatePath = filepath.Join(dir, "pools.json")
	FstabPath = filepath.Join(dir, "fstab")
	ScrubRecordPath = filepath.Join(dir, "scrub.json")
	alerts.Path = filepath.Join(dir, "alerts.json")
	t.Cleanup(func() {
		PoolStatePath, FstabPath, ScrubRecordPath, alerts.Path = oldState, oldFstab, oldScrub, oldAlerts
	})
}

func uuidBytes(uuid string) [16]byte {
	var b [16]byte
	raw, _ := hex.DecodeString(strings.ReplaceAll(uuid, "-", ""))
	copy(b[:], raw)
	return b
}

// stubPoolGeneration answers FS_INFO as filesystem uuid at generation gen
// with devids 1, 2, ... at paths ("" for a missing one), and points the
// state files at a temp dir.
func stubPoolGeneration(t *testing.T, uuid string, gen uint64, paths ...string) {
	t.Helper()
	stubStatePaths(t, t.TempDir())
	old, oldSize := fsIoctl, blockDeviceSize
	fsIoctl = func(_ string, req uintptr, arg unsafe.Pointer) error {
		switch req {
		case iocFSInfo:
			a := (*fsInfoArgs)(arg)
			a.FSID, a.Generation, a.Flags, a.MaxID = uuidBytes(uuid), gen, fsInfoFlagGeneration, uint64(len(paths))
		case iocDevInfo:
			a := (*devInfoArgs)(arg)
			copy(a.Path[:], paths[a.DevID-1])
		}
		return nil
	}
	blockDeviceSize = func(string) (uint64, error) { return 1 << 30, nil }
	t.Cleanup(func() { fsIoctl, blockDeviceSize = old, oldSize })
}

func writeFstab(t *testing.T, content string) {
	t.Helper()
	if err := os.WriteFile(FstabPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDegradedMountBlocker(t *testing.T) {
	// pool has disks with devids 1, 2, ... at the given generations.
	pool := func(gens ...uint64) BtrfsOfflinePool {
		p := BtrfsOfflinePool{NumDevices: 4, generations: gens}
		for i := range gens {
			p.Devices = append(p.Devices, "/dev/sd"+string(rune('b'+i)))
			p.devids = append(p.devids, uint64(i+1))
		}
		return p
	}
	newest := "the present disks missed writes, so the newest data is on a missing disk"
	for _, tc := range []struct {
		name  string
		pool  BtrfsOfflinePool
		state poolState
		want  string
	}{
		{"present disks hold the newest state", pool(90, 90, 90), poolState{Generation: 80}, ""},
		{"never recorded", pool(90), poolState{}, ""},
		{"recorded at exactly this generation", pool(90, 90), poolState{Generation: 90}, ""},
		{"the disks of the last degraded run are present", pool(90, 90, 90), poolState{Generation: 90, Degraded: true, Present: []uint64{1, 2, 3}}, ""},
		// Disk A was out while B, C, D took writes; now D is out and A is back.
		{"a returned disk missed writes", pool(70, 90, 90), poolState{Generation: 90}, "/dev/sdb missed writes the other present disks have"},
		// Two-disk mirror: devid 2 ran alone, then devid 1 comes back alone.
		{"the newest data is on a missing disk", pool(70), poolState{Generation: 90}, newest},
		// The same before a health run recorded the newer generation: devid
		// 1 still carries the generation of the degraded mount.
		{"a disk of the last degraded run is missing", pool(90), poolState{Generation: 90, Degraded: true, Present: []uint64{2}}, newest},
		{"a resynced pool forgets the degraded run", pool(90), poolState{Generation: 90, Present: []uint64{2}}, ""},
	} {
		if got := degradedMountBlocker(tc.pool, tc.state); got != tc.want {
			t.Errorf("%s: blocker = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestBootMounts(t *testing.T) {
	stubStatePaths(t, t.TempDir())
	if got := bootMounts(dataUUID); got != nil {
		t.Fatalf("without an fstab: %v", got)
	}
	writeFstab(t, "# UUID="+dataUUID+" /commented btrfs defaults 0 0\n"+
		"UUID=other /other btrfs defaults 0 0\n"+
		"UUID="+dataUUID+" /srv/data/docker btrfs subvol=@docker,noatime,nofail 0 0\n"+
		"uuid="+strings.ToUpper(dataUUID)+" /srv/data btrfs defaults,noatime 0 0\n"+
		"UUID="+dataUUID+" /mnt/manual btrfs noauto 0 0\n"+
		"UUID="+dataUUID+" /short\n")
	want := []fstabMount{
		{"/srv/data", []string{"defaults", "noatime"}},
		{"/srv/data/docker", []string{"subvol=@docker", "noatime", "nofail"}},
	}
	if got := bootMounts(dataUUID); !reflect.DeepEqual(got, want) {
		t.Fatalf("bootMounts = %v, want %v", got, want)
	}
}

func TestKernelMountOptions(t *testing.T) {
	got := kernelMountOptions([]string{"defaults", "noauto", "nofail", "_netdev", "x-systemd.device-timeout=5", "X-mount.mkdir", "comment=x", "users", "exec", "atime", "noatime", "subvol=@", "compress=zstd:3"})
	if want := []string{"defaults", "noatime", "subvol=@", "compress=zstd:3"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("kernelMountOptions = %v, want %v", got, want)
	}
}

func TestConvertTargets(t *testing.T) {
	alloc := func(kindProfiles ...string) []BtrfsAllocation {
		var a []BtrfsAllocation
		for _, kp := range kindProfiles {
			kind, profile, _ := strings.Cut(kp, ":")
			a = append(a, BtrfsAllocation{Kind: kind, Profile: profile})
		}
		return a
	}
	str := func(p *string) string {
		if p == nil {
			return "-"
		}
		return *p
	}
	for _, tc := range []struct {
		alloc      []BtrfsAllocation
		data, meta string
	}{
		{alloc("data:raid1", "metadata:raid1", "system:raid1"), "-", "-"},
		// Written while a two-disk mirror ran on one disk.
		{alloc("data:raid1", "data:single", "metadata:raid1", "metadata:single", "system:raid1"), "raid1", "raid1"},
		{alloc("data:single", "data:raid10", "metadata:raid1c3"), "raid10", "-"},
		// A system chunk alone is converted with metadata.
		{alloc("data:raid1", "metadata:raid1", "system:raid1", "system:single"), "-", "raid1"},
		{alloc("data:raid1c3", "data:raid1"), "raid1c3", "-"},
	} {
		data, meta := convertTargets(tc.alloc)
		if str(data) != tc.data || str(meta) != tc.meta {
			t.Errorf("convertTargets(%v) = %s, %s; want %s, %s", tc.alloc, str(data), str(meta), tc.data, tc.meta)
		}
	}
}

func TestMarkUnopened(t *testing.T) {
	dir := t.TempDir()
	old := sysFSBtrfs
	sysFSBtrfs = dir
	t.Cleanup(func() { sysFSBtrfs = old })
	writeable := func(id, value string) {
		p := filepath.Join(dir, dataUUID, "devinfo", id)
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p, "writeable"), []byte(value+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	devices := func() []BtrfsDevice {
		return []BtrfsDevice{{ID: 1, Path: "/dev/sda"}, {ID: 2, Path: "/dev/sdb"}, {ID: 3, Missing: true}}
	}

	// No devinfo: an older kernel, nothing changes.
	got := devices()
	markUnopened(dataUUID, got)
	if !reflect.DeepEqual(got, devices()) {
		t.Fatalf("without devinfo: %+v", got)
	}

	// sdb came back after the degraded mount: listed but never opened.
	writeable("1", "1")
	writeable("2", "0")
	got = devices()
	markUnopened(dataUUID, got)
	if !got[1].Missing || got[0].Missing || got[1].Path != "/dev/sdb" {
		t.Fatalf("returned disk: %+v", got)
	}

	// A read-only pool has no writeable device to compare with.
	writeable("1", "0")
	got = devices()
	markUnopened(dataUUID, got)
	if got[0].Missing || got[1].Missing {
		t.Fatalf("read-only pool: %+v", got)
	}

	// Since 7.1 the kernel marks a member missing when its block device
	// goes away (btrfs_remove_bdev); DEV_INFO then names it "<missing disk>"
	// and sysfs says missing, read-only pool or not.
	if err := os.WriteFile(filepath.Join(dir, dataUUID, "devinfo", "2", "missing"), []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got = []BtrfsDevice{{ID: 1, Path: "/dev/sda"}, {ID: 2, Path: "<missing disk>"}}
	markUnopened(dataUUID, got)
	if got[0].Missing || !got[1].Missing {
		t.Fatalf("device marked missing at runtime: %+v", got)
	}
}

// stubDisks makes BlockDevices list names and readSuperblock answer from
// supers, which a test may change between polls.
func stubDisks(t *testing.T, supers map[string]Superblock) {
	t.Helper()
	dir := t.TempDir()
	for name := range supers {
		if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name, "size"), []byte("1000\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	oldDir, oldRead, oldPoll := sysBlockDir, readSuperblock, degradedPoll
	sysBlockDir, degradedPoll = dir, time.Millisecond
	readSuperblock = func(device string) (Superblock, error) {
		sb, ok := supers[strings.TrimPrefix(device, "/dev/")]
		if !ok || sb.UUID == "" {
			return Superblock{}, errNotBtrfs
		}
		return sb, nil
	}
	t.Cleanup(func() { sysBlockDir, readSuperblock, degradedPoll = oldDir, oldRead, oldPoll })
}

// stubMountCalls records mounts and device registrations, with no
// filesystem mounted beforehand.
func stubMountCalls(t *testing.T) *[]string {
	t.Helper()
	var calls []string
	stubMounts(t, nil)
	oldMount, oldControl := mountFS, controlIoctl
	mountFS = func(source, target, _ string, _ uintptr, data string) error {
		calls = append(calls, "mount "+source+" "+target+" "+data)
		return nil
	}
	controlIoctl = func(_ uintptr, device string) error {
		calls = append(calls, "register "+device)
		return nil
	}
	t.Cleanup(func() { mountFS, controlIoctl = oldMount, oldControl })
	return &calls
}

func TestMountDegradedPoolsMountsWhatFstabMounts(t *testing.T) {
	stubPoolGeneration(t, dataUUID, 91, "/dev/sdb", "/dev/sdc", "")
	calls := stubMountCalls(t)
	member := func(devid uint64) Superblock {
		return Superblock{UUID: dataUUID, Label: "data", DevID: devid, NumDevices: 3, Generation: 90}
	}
	stubDisks(t, map[string]Superblock{
		"sdb": member(1), "sdc": member(2),
		// Missing a disk but not in fstab: not ours to mount.
		"sdd": {UUID: "other-uuid", NumDevices: 2, Generation: 5},
	})
	mnt := filepath.Join(t.TempDir(), "data")
	writeFstab(t, "UUID="+dataUUID+" "+mnt+"/docker btrfs subvol=@docker 0 0\n"+
		"UUID="+dataUUID+" "+mnt+" btrfs defaults,noatime,nofail,subvol=@data 0 0\n")
	if err := updatePoolStates(context.Background(), func(s map[string]poolState) { s[dataUUID] = poolState{Generation: 90} }); err != nil {
		t.Fatal(err)
	}

	if err := MountDegradedPools(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"register /dev/sdb", "register /dev/sdc", "mount /dev/sdb " + mnt + " subvol=@data,degraded",
		"register /dev/sdb", "register /dev/sdc", "mount /dev/sdb " + mnt + "/docker subvol=@docker,degraded",
	}
	if !reflect.DeepEqual(*calls, want) {
		t.Fatalf("calls = %v, want %v", *calls, want)
	}
	states, err := loadPoolStates()
	if err != nil || !reflect.DeepEqual(states[dataUUID], poolState{Generation: 91, Degraded: true, Present: []uint64{1, 2}}) {
		t.Fatalf("state = %+v, %v", states, err)
	}
}

func TestMountDegradedPoolsSkipsInsideAFailedMount(t *testing.T) {
	stubPoolGeneration(t, dataUUID, 0)
	calls := stubMountCalls(t)
	mnt := filepath.Join(t.TempDir(), "data")
	old := mountFS
	mountFS = func(source, target, fstype string, flags uintptr, data string) error {
		if target == mnt {
			return unix.EINVAL
		}
		return old(source, target, fstype, flags, data)
	}
	stubDisks(t, map[string]Superblock{"sdb": {UUID: dataUUID, DevID: 1, NumDevices: 2, Generation: 9}})
	writeFstab(t, "UUID="+dataUUID+" "+mnt+" btrfs defaults 0 0\nUUID="+dataUUID+" "+mnt+"/docker btrfs subvol=@docker 0 0\n")

	if err := MountDegradedPools(context.Background(), 0); err == nil {
		t.Fatal("a failed mount was not reported")
	}
	if slices.ContainsFunc(*calls, func(c string) bool { return strings.HasPrefix(c, "mount ") }) {
		t.Fatalf("mounted inside the failed mountpoint: %v", *calls)
	}
}

func TestMountDegradedPoolsLeavesStalePoolsToAPerson(t *testing.T) {
	stubPoolGeneration(t, dataUUID, 0)
	calls := stubMountCalls(t)
	// A two-disk mirror whose last mount, at generation 90, ran without sdb.
	stubDisks(t, map[string]Superblock{"sdb": {UUID: dataUUID, Label: "data", DevID: 2, NumDevices: 2, Generation: 70}})
	writeFstab(t, "UUID="+dataUUID+" /srv/data btrfs defaults 0 0\n")
	if err := updatePoolStates(context.Background(), func(s map[string]poolState) { s[dataUUID] = poolState{Generation: 90, Degraded: true} }); err != nil {
		t.Fatal(err)
	}

	if err := MountDegradedPools(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 0 {
		t.Fatalf("mounted a stale pool: %v", *calls)
	}
	doc, err := alerts.Load()
	if err != nil || len(doc.Alerts) != 1 || !strings.Contains(doc.Alerts[0].Message, "newest data is on a missing disk") {
		t.Fatalf("alerts = %+v, %v", doc.Alerts, err)
	}
}

func TestMountDegradedPoolsWaitsForLateDisks(t *testing.T) {
	stubPoolGeneration(t, dataUUID, 0)
	calls := stubMountCalls(t)
	member := func(devid uint64) Superblock {
		return Superblock{UUID: dataUUID, DevID: devid, NumDevices: 2, Generation: 90}
	}
	supers := map[string]Superblock{"sdb": member(1), "sdc": {}}
	stubDisks(t, supers)
	writeFstab(t, "UUID="+dataUUID+" /srv/data btrfs defaults 0 0\n")
	old := readSuperblock
	polls := 0
	readSuperblock = func(device string) (Superblock, error) {
		if device == "/dev/sdb" {
			if polls++; polls == 2 {
				supers["sdc"] = member(2) // the slow disk shows up on the second look
			}
		}
		return old(device)
	}

	if err := MountDegradedPools(context.Background(), time.Minute); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 0 || polls != 2 {
		t.Fatalf("calls = %v after %d polls; the complete pool is systemd's to mount", *calls, polls)
	}
}

// A power cut mid-replace leaves the target, devid 0, scanned next to every
// member, and udev never calls the pool ready: DEVICES_READY counts the
// target against the superblock's num_devices. The boot check mounts it as
// fstab says, so the kernel continues the replace, unless a member missed
// writes. A pool also missing a member mounts degraded, as without the
// replace.
func TestMountDegradedPoolsMountsAPoolMidReplace(t *testing.T) {
	disk := func(devid, gen uint64) Superblock {
		return Superblock{UUID: dataUUID, Label: "data", DevID: devid, NumDevices: 2, Generation: gen}
	}
	for _, tc := range []struct {
		name   string
		supers map[string]Superblock
		want   []string // calls, with MNT for the mountpoint
		alert  string
	}{
		{
			"every member and the target", map[string]Superblock{"sdb": disk(1, 90), "sdc": disk(2, 90), "sdd": disk(0, 90)},
			[]string{"register /dev/sdb", "register /dev/sdc", "register /dev/sdd", "mount /dev/sdb MNT subvol=@data"}, "",
		},
		{
			"a member missed writes", map[string]Superblock{"sdb": disk(1, 80), "sdc": disk(2, 90), "sdd": disk(0, 90)},
			nil, "did not mount the pool because /dev/sdb missed writes the other present disks have",
		},
		{
			"the target missed writes", map[string]Superblock{"sdb": disk(1, 90), "sdc": disk(2, 90), "sdd": disk(0, 80)},
			nil, "did not mount the pool because the replace target /dev/sdd missed writes the other present disks have",
		},
		{
			"a member is missing too", map[string]Superblock{"sdb": disk(1, 90), "sdd": disk(0, 90)},
			[]string{"register /dev/sdb", "register /dev/sdd", "mount /dev/sdb MNT subvol=@data,degraded"}, "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stubPoolGeneration(t, dataUUID, 91, "/dev/sdb", "/dev/sdc")
			calls := stubMountCalls(t)
			stubDisks(t, tc.supers)
			mnt := filepath.Join(t.TempDir(), "data")
			writeFstab(t, "UUID="+dataUUID+" "+mnt+" btrfs defaults,nofail,subvol=@data 0 0\n")

			if err := MountDegradedPools(context.Background(), 0); err != nil {
				t.Fatal(err)
			}
			var want []string
			for _, c := range tc.want {
				want = append(want, strings.ReplaceAll(c, "MNT", mnt))
			}
			if !reflect.DeepEqual(*calls, want) {
				t.Fatalf("calls = %v, want %v", *calls, want)
			}
			wantRefusal(t, tc.alert)
		})
	}
}

// wantRefusal checks that the boot check raised only an error alert saying
// want, or none for "", and that a health run raises the same on its own:
// an alert keeps the message of its first raise while its occurrence holds,
// so whichever runs first must say why.
func wantRefusal(t *testing.T, want string) {
	t.Helper()
	check := func(after string) {
		t.Helper()
		doc, err := alerts.Load()
		if err != nil {
			t.Fatal(err)
		}
		// A refusal is an error, as it is for a pool missing a disk.
		if want == "" && len(doc.Alerts) != 0 || want != "" && (len(doc.Alerts) != 1 || !strings.Contains(doc.Alerts[0].Message, want) || doc.Alerts[0].Severity != alerts.SeverityError) {
			t.Fatalf("alerts after %s = %+v, want %q", after, doc.Alerts, want)
		}
	}
	check("the boot check")
	if want == "" {
		return
	}
	stubAlerts(t)
	for _, after := range []string{"a health run alone", "a second health run"} {
		if _, err := CheckHealth(context.Background()); err != nil {
			t.Fatal(err)
		}
		check(after)
	}
}

func TestResyncScrubsThenConvertsSoftly(t *testing.T) {
	k := raid1Kernel()
	// A mirror that ran on one disk: data written then has no second copy.
	k.spaces = append(k.spaces, spaceInfo{Flags: bgData, TotalBytes: 1 << 30, UsedBytes: 1 << 30})
	var balanced balanceArgs
	k.balance = func(a *balanceArgs) error {
		balanced = *a
		k.spaces = k.spaces[:len(k.spaces)-1]
		return nil
	}
	k.scrub = func(_, _ uint64) (scrubProgress, error) { return scrubProgress{CorrectedErrors: 3}, nil }
	stubKernel(t, k)
	ctx := context.Background()
	if err := updatePoolStates(ctx, func(s map[string]poolState) { s[dataUUID] = poolState{Generation: 9, Degraded: true} }); err != nil {
		t.Fatal(err)
	}

	if err := Resync(ctx, dataUUID, nil); err != nil {
		t.Fatal(err)
	}
	if balanced.Flags != balanceData || balanced.Data.Flags != balanceArgsConvert|balanceArgsSoft || balanced.Data.Target != bgRaid1 {
		t.Fatalf("balance = %+v, want a soft data convert to raid1", balanced)
	}
	if states, _ := loadPoolStates(); !reflect.DeepEqual(states[dataUUID], poolState{Generation: 9}) {
		t.Fatalf("state = %+v, want the degraded mark cleared", states[dataUUID])
	}
	if !slices.Contains(k.Calls(), "SCRUB 1") {
		t.Fatalf("no scrub ran: %v", k.Calls())
	}
	// The bell says the pool is whole again at once, not at the next health run.
	if got := alertKeys(t); got["resynced-"+dataUUID] != alerts.SeverityInfo || got["resync-"+dataUUID] != "" {
		t.Fatalf("alerts = %v, want only the resynced notice", got)
	}
}

// storeResyncReport stores what a pool resync reports, as the settings page
// would.
func storeResyncReport(t *testing.T, report string) {
	t.Helper()
	if _, err := alerts.UpdateNotifications(t.Context(), func(n *alerts.NotificationSettings) error {
		n.Report[alerts.KindResync] = report
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// A pool that is whole again is reported under changes, the default, and not
// under failures; either way the resync alert is resolved.
func TestWholeAgainFollowsTheSetting(t *testing.T) {
	for _, tc := range []struct {
		report string
		whole  bool
	}{
		{alerts.ReportChanges, true},
		{alerts.ReportFailures, false},
	} {
		t.Run(tc.report, func(t *testing.T) {
			stubKernel(t, raid1Kernel())
			ctx := t.Context()
			storeResyncReport(t, tc.report)
			if err := updatePoolStates(ctx, func(s map[string]poolState) { s[dataUUID] = poolState{Generation: 9, Degraded: true} }); err != nil {
				t.Fatal(err)
			}

			if err := Resync(ctx, dataUUID, nil); err != nil {
				t.Fatal(err)
			}
			got := alertKeys(t)
			if severity, raised := got["resynced-"+dataUUID]; raised != tc.whole || raised && severity != alerts.SeverityInfo {
				t.Fatalf("alerts = %v, want the resynced notice %v", got, tc.whole)
			}
			if _, raised := got["resync-"+dataUUID]; raised {
				t.Fatalf("alerts = %v, want the resync alert resolved", got)
			}
		})
	}
}

// A resync that fails its last attempt raises the same alert under failures as
// under changes, and no resynced notice.
func TestFailedResyncIgnoresTheSetting(t *testing.T) {
	changes := failedResyncAlerts(t, alerts.ReportChanges)
	failures := failedResyncAlerts(t, alerts.ReportFailures)
	if !reflect.DeepEqual(failures, changes) {
		t.Fatalf("failures raised %+v, changes raised %+v; want the same", failures, changes)
	}
}

// failedResyncAlerts fails the last resync attempt of a pool under report and
// returns the alerts it leaves, without their timestamps.
func failedResyncAlerts(t *testing.T, report string) []alerts.Alert {
	t.Helper()
	k := raid1Kernel()
	k.scrub = func(_, _ uint64) (scrubProgress, error) { return scrubProgress{}, unix.ECANCELED }
	stubKernel(t, k)
	ctx := t.Context()
	storeResyncReport(t, report)
	if err := updatePoolStates(ctx, func(s map[string]poolState) {
		s[dataUUID] = poolState{Generation: 9, Degraded: true, ResyncFailures: maxResyncAttempts - 1}
	}); err != nil {
		t.Fatal(err)
	}

	err := Resync(ctx, dataUUID, nil)
	if err == nil {
		t.Fatalf("%s: Resync succeeded, want the canceled scrub to fail it", report)
	}
	// The pool's alert reports it, so the unit needs no run alert.
	if !scheduledrun.AllReported(err) {
		t.Fatalf("%s: the failure the resync alert reports is not marked reported: %v", report, err)
	}
	if got := alertKeys(t); got["resync-"+dataUUID] != alerts.SeverityError || got["resynced-"+dataUUID] != "" {
		t.Fatalf("%s: alerts = %v, want the failed resync alert and no resynced notice", report, got)
	}
	doc, err := alerts.Load()
	if err != nil {
		t.Fatal(err)
	}
	var raised []alerts.Alert
	for _, a := range doc.Alerts {
		raised = append(raised, alerts.Alert{ID: a.ID, Severity: a.Severity, Title: a.Title, Message: a.Message, Link: a.Link})
	}
	return raised
}

// A resync of a pool that is not there fails with no alert of the pool to
// report it: the unit's run alert has to.
func TestResyncOfAnUnknownPoolIsNotReported(t *testing.T) {
	stubKernel(t, raid1Kernel())
	useTempAlerts(t)
	err := Resync(t.Context(), "0a1b2c3d-1111-2222-3333-444455556666", nil)
	if err == nil || scheduledrun.AllReported(err) {
		t.Fatalf("Resync = %v, reported %v; want a failure that is not marked reported", err, scheduledrun.AllReported(err))
	}
	if got := alertKeys(t); len(got) != 0 {
		t.Fatalf("alerts = %v, want none", got)
	}
}

func TestResyncRefusesWhileADiskIsMissing(t *testing.T) {
	k := raid1Kernel()
	k.devices[2].path = ""
	stubKernel(t, k)
	if err := Resync(context.Background(), dataUUID, nil); !errors.Is(err, errDiskMissing) {
		t.Fatalf("Resync = %v, want a missing disk", err)
	}
	if states, _ := loadPoolStates(); states[dataUUID].ResyncFailures != 0 {
		t.Fatalf("a missing disk counted as a failed resync: %+v", states[dataUUID])
	}
}

func TestResyncDoesNotTrustACanceledScrub(t *testing.T) {
	k := raid1Kernel()
	k.scrub = func(_, _ uint64) (scrubProgress, error) { return scrubProgress{}, unix.ECANCELED }
	stubKernel(t, k)
	ctx := context.Background()
	if err := updatePoolStates(ctx, func(s map[string]poolState) { s[dataUUID] = poolState{Generation: 9, Degraded: true} }); err != nil {
		t.Fatal(err)
	}

	if err := Resync(ctx, dataUUID, nil); err == nil || !strings.Contains(err.Error(), "canceled before it checked every copy") {
		t.Fatalf("Resync = %v", err)
	}
	states, _ := loadPoolStates()
	if s := states[dataUUID]; !s.Degraded || s.Scrubbed || s.ResyncFailures != 1 {
		t.Fatalf("state = %+v, want still degraded, unscrubbed, one failure", s)
	}
}

// resyncRunID is the resync scrub run the continuation tests store.
const resyncRunID = "resync-continuation-test"

// resyncEpisode stores the state of a pool that ran degraded, whose resync
// scrub is run ("" for none yet), and rec, when set, as its scrub record,
// as an earlier resync leaves them.
func resyncEpisode(t *testing.T, run string, rec *scrubRecord) {
	t.Helper()
	if err := updatePoolStates(t.Context(), func(s map[string]poolState) {
		s[dataUUID] = poolState{Generation: 9, Degraded: true, ResyncRun: run}
	}); err != nil {
		t.Fatal(err)
	}
	if rec != nil {
		saveScrubRecord(t.Context(), dataUUID, *rec)
	}
}

// wantScrubCalls checks the SCRUB calls the fake kernel saw, in any order.
func wantScrubCalls(t *testing.T, k *fakeKernel, want ...string) {
	t.Helper()
	var calls []string
	for _, c := range k.Calls() {
		if strings.HasPrefix(c, "SCRUB ") {
			calls = append(calls, c)
		}
	}
	slices.Sort(calls)
	slices.Sort(want)
	if !slices.Equal(calls, want) {
		t.Fatalf("scrub calls = %v, want %v", calls, want)
	}
}

// wantWhole checks that the resync ended the episode.
func wantWhole(t *testing.T) {
	t.Helper()
	if states, _ := loadPoolStates(); !reflect.DeepEqual(states[dataUUID], poolState{Generation: 9}) {
		t.Fatalf("state = %+v, want the degraded mark and the resync run cleared", states[dataUUID])
	}
}

// A resync cut by a power loss, a crash or a kill leaves its scrub record
// running with a writer that is gone. The next resync continues it: a device
// not done from one byte below its saved offset (0 stays 0), a done one
// skipped, the first start kept and the counters added up.
func TestResyncContinuesItsScrubAfterACut(t *testing.T) {
	resyncContinues(t, "running")
}

// Stop check, a shutdown or systemctl stop leaves the record canceled; the
// next resync continues it as after a cut.
func TestResyncContinuesAfterAStop(t *testing.T) {
	resyncContinues(t, "canceled")
}

func resyncContinues(t *testing.T, status string) {
	t.Helper()
	first := time.Unix(1700000000, 0)
	k := threeDevices()
	for _, d := range k.devices {
		d.scrubResult = scrubProgress{DataBytesScrubbed: 10 << 20, CorrectedErrors: 1}
	}
	stubKernel(t, k)
	resyncEpisode(t, resyncRunID, &scrubRecord{
		Status: status, StartedAt: &first, CorrectedErrors: 2, BytesScrubbed: 1 << 30,
		Run: resyncRunID,
		Devices: map[int]scrubDeviceRecord{
			1: {UUID: devUUID(1), Offset: 4096, scrubCounters: scrubCounters{CorrectedErrors: 1, BytesScrubbed: 100 << 20}},
			2: {UUID: devUUID(2)},
			3: {UUID: devUUID(3), Offset: 9000, Done: true},
		},
	})

	if err := Resync(t.Context(), dataUUID, nil); err != nil {
		t.Fatal(err)
	}
	wantScrubCalls(t, k, "SCRUB 1 from 4095", "SCRUB 2")
	rec, _ := loadScrubRecord(dataUUID)
	// What was saved, device 1's running call, then 10 MiB and one
	// correction for each device scrubbed now.
	if rec.Run != resyncRunID || rec.Status != "finished" || !rec.StartedAt.Equal(first) ||
		rec.BytesScrubbed != 1<<30+100<<20+20<<20 || rec.CorrectedErrors != 5 {
		t.Fatalf("record = %+v, want the cut scrub continued to the end", rec)
	}
	wantWhole(t)
}

// A device whose scrub aborted is scanned again from its offset: the resync
// must check every copy, where a job's resume keeps the abort.
func TestResyncScansAnAbortedDeviceAgain(t *testing.T) {
	first := time.Unix(1700000000, 0)
	k := threeDevices()
	stubKernel(t, k)
	resyncEpisode(t, resyncRunID, &scrubRecord{
		Status: "aborted", StartedAt: &first, Run: resyncRunID,
		Devices: map[int]scrubDeviceRecord{
			1: {UUID: devUUID(1), Offset: 4096},
			2: {UUID: devUUID(2), Offset: 500, Aborted: true},
			3: {UUID: devUUID(3), Offset: 9000, Done: true},
		},
	})

	if err := Resync(t.Context(), dataUUID, nil); err != nil {
		t.Fatal(err)
	}
	wantScrubCalls(t, k, "SCRUB 1 from 4095", "SCRUB 2 from 499")
	if rec, _ := loadScrubRecord(dataUUID); rec.Status != "finished" || !rec.StartedAt.Equal(first) {
		t.Fatalf("record = %+v, want the abort scanned again and the scrub finished", rec)
	}
	wantWhole(t)
}

// A scrub of this run that finished counts, though the cut came before the
// resync noted it: no device is scrubbed again, and the convert still runs.
func TestResyncCountsItsFinishedScrub(t *testing.T) {
	first := time.Unix(1700000000, 0)
	k := threeDevices()
	k.spaces = append(k.spaces, spaceInfo{Flags: bgData, TotalBytes: 1 << 30, UsedBytes: 1 << 30})
	k.balance = func(*balanceArgs) error {
		k.spaces = k.spaces[:len(k.spaces)-1]
		return nil
	}
	stubKernel(t, k)
	resyncEpisode(t, resyncRunID, &scrubRecord{
		Status: "finished", StartedAt: &first, FinishedAt: &first, Percent: 100, Run: resyncRunID,
		Devices: map[int]scrubDeviceRecord{1: {UUID: devUUID(1), Done: true}, 2: {UUID: devUUID(2), Done: true}, 3: {UUID: devUUID(3), Done: true}},
	})

	if err := Resync(t.Context(), dataUUID, nil); err != nil {
		t.Fatal(err)
	}
	wantScrubCalls(t, k)
	if !slices.ContainsFunc(k.Calls(), func(c string) bool { return strings.HasPrefix(c, "BALANCE") }) {
		t.Fatalf("calls = %v, want the convert", k.Calls())
	}
	wantWhole(t)
}

// A scrub record of an earlier episode is no progress of this one: the
// health run that saw the pool degraded again cleared the run, and writes
// made since may sit below the saved offsets.
func TestResyncStartsOverInANewEpisode(t *testing.T) {
	resyncStartsOver(t, "", "resync-of-an-earlier-episode")
}

// A scrub job's record, or one an older build wrote for a resync, holds no
// progress of the resync's run.
func TestResyncIgnoresAJobsScrubRecord(t *testing.T) {
	for _, run := range []string{"job-1", ""} {
		t.Run("run "+run, func(t *testing.T) { resyncStartsOver(t, resyncRunID, run) })
	}
}

// resyncStartsOver runs a resync of episode run over a running record of
// recRun, from a writer that is gone, and checks that it scrubbed every
// device from 0 as a new scrub of its own run.
func resyncStartsOver(t *testing.T, run, recRun string) {
	t.Helper()
	first := time.Unix(1700000000, 0)
	k := threeDevices()
	stubKernel(t, k)
	resyncEpisode(t, run, &scrubRecord{
		Status: "running", StartedAt: &first, CorrectedErrors: 7, BytesScrubbed: 1 << 30, Run: recRun,
		Devices: map[int]scrubDeviceRecord{1: {UUID: devUUID(1), Offset: 4096}, 2: {UUID: devUUID(2), Offset: 8192}},
	})
	// The episode's run while it scrubs; the state is cleared at the end.
	var during atomic.Value
	k.scrub = func(uint64, uint64) (scrubProgress, error) {
		if states, err := loadPoolStates(); err == nil {
			during.Store(states[dataUUID].ResyncRun)
		}
		return scrubProgress{}, nil
	}

	if err := Resync(t.Context(), dataUUID, nil); err != nil {
		t.Fatal(err)
	}
	wantScrubCalls(t, k, "SCRUB 1", "SCRUB 2", "SCRUB 3")
	rec, _ := loadScrubRecord(dataUUID)
	seen, _ := during.Load().(string)
	if rec.Run == recRun || !strings.HasPrefix(rec.Run, "resync-") || run != "" && rec.Run != run || rec.Run != seen ||
		rec.StartedAt.Equal(first) || rec.CorrectedErrors != 0 || rec.Status != "finished" {
		t.Fatalf("record = %+v (episode run %q while scrubbing), want a new scrub of the episode's run", rec, seen)
	}
	wantWhole(t)
}

// A device the record names with another UUID is another disk under that
// device id: it is scrubbed from 0.
func TestResyncStartsADeviceWithAnotherUUIDAtZero(t *testing.T) {
	first := time.Unix(1700000000, 0)
	k := threeDevices()
	stubKernel(t, k)
	resyncEpisode(t, resyncRunID, &scrubRecord{
		Status: "running", StartedAt: &first, Run: resyncRunID,
		Devices: map[int]scrubDeviceRecord{
			1: {UUID: devUUID(1), Offset: 4096},
			2: {UUID: devUUID(7), Offset: 8192},
			3: {UUID: devUUID(3), Done: true},
		},
	})

	if err := Resync(t.Context(), dataUUID, nil); err != nil {
		t.Fatal(err)
	}
	wantScrubCalls(t, k, "SCRUB 1 from 4095", "SCRUB 2")
	wantWhole(t)
}

// A health run that sees the pool degraded while the resync scrubs starts a
// new episode: the scrub that then finishes does not count for it, and a disk
// that dropped out is no failed resync.
func TestResyncScrubDoesNotCountForANewEpisode(t *testing.T) {
	k := threeDevices()
	stubKernel(t, k)
	resyncEpisode(t, "", nil)
	k.scrub = func(id, _ uint64) (scrubProgress, error) {
		if id == 1 {
			if err := updatePoolStates(context.Background(), func(s map[string]poolState) {
				state := s[dataUUID]
				state.markDegraded([]BtrfsDevice{{ID: 1}, {ID: 2}, {ID: 3, Missing: true}})
				s[dataUUID] = state
			}); err != nil {
				t.Error(err)
			}
		}
		return scrubProgress{}, nil
	}

	if err := Resync(t.Context(), dataUUID, nil); !errors.Is(err, errDiskMissing) {
		t.Fatalf("Resync = %v, want a missing disk", err)
	}
	states, _ := loadPoolStates()
	if s := states[dataUUID]; !s.Degraded || s.Scrubbed || s.ResyncRun != "" || s.ResyncFailures != 0 {
		t.Fatalf("state = %+v, want the new episode unscrubbed and no failure counted", s)
	}
}

// A disk whose writes fail while it stays attached is not marked missing
// (the kernel does so at mount, and since 7.1 when the block device goes
// away); only its write and flush
// error counters rise, and writes made meanwhile can land below its saved
// offset. The continuation scans a device whose counters changed since its
// scan began again from 0. One whose counters stayed as they were, errors
// from before its scan included, continues from its offset.
func TestResyncRestartsADeviceThatFailedWritesSinceItsScanBegan(t *testing.T) {
	first := time.Unix(1700000000, 0)
	k := threeDevices()
	k.setErrors(1, 3, 0) // since the save
	k.setErrors(2, 5, 1) // as when its scan began
	stubKernel(t, k)
	resyncEpisode(t, resyncRunID, &scrubRecord{
		Status: "running", StartedAt: &first, Run: resyncRunID,
		Devices: map[int]scrubDeviceRecord{
			1: {UUID: devUUID(1), Offset: 4096},
			2: {UUID: devUUID(2), Offset: 8192, WriteErrors: 5, FlushErrors: 1},
			3: {UUID: devUUID(3), Offset: 9000, Done: true},
		},
	})

	if err := Resync(t.Context(), dataUUID, nil); err != nil {
		t.Fatal(err)
	}
	wantScrubCalls(t, k, "SCRUB 1", "SCRUB 2 from 8191")
	if rec, _ := loadScrubRecord(dataUUID); rec.Devices[1].WriteErrors != 3 || rec.Devices[2].WriteErrors != 5 || rec.Devices[2].FlushErrors != 1 {
		t.Fatalf("record devices = %+v, want each scan's counters", rec.Devices)
	}
	wantWhole(t)
}

// A finished scrub of the run does not count when a device's counters
// changed since its scan began: writes may have missed it after the scrub
// passed, so the pool is scrubbed again.
func TestResyncScansAgainWhenWritesFailedSinceItsFinishedScrub(t *testing.T) {
	first := time.Unix(1700000000, 0)
	k := threeDevices()
	k.setErrors(2, 0, 2)
	stubKernel(t, k)
	resyncEpisode(t, resyncRunID, &scrubRecord{
		Status: "finished", StartedAt: &first, FinishedAt: &first, Percent: 100, Run: resyncRunID,
		Devices: map[int]scrubDeviceRecord{1: {UUID: devUUID(1), Done: true}, 2: {UUID: devUUID(2), Done: true}, 3: {UUID: devUUID(3), Done: true}},
	})

	if err := Resync(t.Context(), dataUUID, nil); err != nil {
		t.Fatal(err)
	}
	wantScrubCalls(t, k, "SCRUB 1", "SCRUB 2", "SCRUB 3")
	wantWhole(t)
}

// A device whose writes or flushes fail during the resync's scrub may keep
// stale copies, a failed repair included, which the kernel counts as
// corrected since 6.4. The resync does not declare the pool whole: the
// attempt fails and says which disk, and the next one scans again.
func TestResyncFailsWhenADeviceFailsWritesDuringItsScrub(t *testing.T) {
	k := threeDevices()
	var failed atomic.Bool
	k.scrub = func(id, _ uint64) (scrubProgress, error) {
		if id == 2 && !failed.Swap(true) {
			k.setErrors(2, 4, 0)
		}
		return scrubProgress{CorrectedErrors: 1}, nil
	}
	stubKernel(t, k)
	resyncEpisode(t, "", nil)

	err := Resync(t.Context(), dataUUID, nil)
	if err == nil || !strings.Contains(err.Error(), "device 2 (/dev/sdb1) failed writes") {
		t.Fatalf("Resync = %v, want it to name the device whose writes failed", err)
	}
	states, _ := loadPoolStates()
	if s := states[dataUUID]; !s.Degraded || s.Scrubbed || s.ResyncFailures != 1 || !strings.Contains(s.ResyncError, "failed writes") {
		t.Fatalf("state = %+v, want a failed attempt with its reason", s)
	}
	if got := alertKeys(t); got["resynced-"+dataUUID] != "" {
		t.Fatalf("alerts = %v, want no whole-again notice", got)
	}

	if err := Resync(t.Context(), dataUUID, nil); err != nil {
		t.Fatalf("the next resync: %v", err)
	}
	wantScrubCalls(t, k, "SCRUB 1", "SCRUB 2", "SCRUB 3", "SCRUB 1", "SCRUB 2", "SCRUB 3")
	wantWhole(t)
}

// A finished scrub of another run is no scrub of this episode's.
func TestResyncDoesNotCountAnotherRunsFinishedScrub(t *testing.T) {
	first := time.Unix(1700000000, 0)
	k := threeDevices()
	stubKernel(t, k)
	resyncEpisode(t, resyncRunID, &scrubRecord{
		Status: "finished", StartedAt: &first, FinishedAt: &first, Percent: 100, Run: "resync-of-an-earlier-episode",
		Devices: map[int]scrubDeviceRecord{1: {UUID: devUUID(1), Done: true}, 2: {UUID: devUUID(2), Done: true}, 3: {UUID: devUUID(3), Done: true}},
	})

	if err := Resync(t.Context(), dataUUID, nil); err != nil {
		t.Fatal(err)
	}
	wantScrubCalls(t, k, "SCRUB 1", "SCRUB 2", "SCRUB 3")
	wantWhole(t)
}

// A failed final flush leaves the record aborted with no device aborted;
// the next resync continues it from the offsets it holds.
func TestResyncContinuesTheRecordAFailedFlushLeft(t *testing.T) {
	first := time.Unix(1700000000, 0)
	k := threeDevices()
	stubKernel(t, k)
	resyncEpisode(t, resyncRunID, &scrubRecord{
		Status: "aborted", StartedAt: &first, FinishedAt: &first, Run: resyncRunID,
		Devices: map[int]scrubDeviceRecord{1: {UUID: devUUID(1), Offset: 4096}, 2: {UUID: devUUID(2), Offset: 50 << 20, Done: true}, 3: {UUID: devUUID(3), Offset: 8192}},
	})

	if err := Resync(t.Context(), dataUUID, nil); err != nil {
		t.Fatal(err)
	}
	wantScrubCalls(t, k, "SCRUB 1 from 4095", "SCRUB 3 from 8191")
	wantWhole(t)
}

// Records are kept by pool UUID: another pool's record, even of a run with
// the same name, is no progress of this pool's resync.
func TestResyncIgnoresAnotherPoolsRecord(t *testing.T) {
	first := time.Unix(1700000000, 0)
	k := threeDevices()
	stubKernel(t, k)
	resyncEpisode(t, resyncRunID, nil)
	saveScrubRecord(t.Context(), "0a1b2c3d-1111-2222-3333-444455556666", scrubRecord{
		Status: "running", StartedAt: &first, Run: resyncRunID,
		Devices: map[int]scrubDeviceRecord{1: {UUID: devUUID(1), Offset: 4096}, 2: {UUID: devUUID(2), Offset: 8192}},
	})

	if err := Resync(t.Context(), dataUUID, nil); err != nil {
		t.Fatal(err)
	}
	wantScrubCalls(t, k, "SCRUB 1", "SCRUB 2", "SCRUB 3")
	wantWhole(t)
}

// The continuation refuses a pool missing a disk itself, before it counts
// a finished scrub: a scrub would skip the missing disk.
func TestContinueScrubRefusesAPoolMissingADisk(t *testing.T) {
	first := time.Unix(1700000000, 0)
	k := threeDevices()
	k.devices[3].path = ""
	stubKernel(t, k)
	saveScrubRecord(t.Context(), dataUUID, scrubRecord{
		Status: "finished", StartedAt: &first, FinishedAt: &first, Run: resyncRunID,
		Devices: map[int]scrubDeviceRecord{1: {UUID: devUUID(1), Done: true}, 2: {UUID: devUUID(2), Done: true}},
	})

	if _, err := continueScrub(t.Context(), dataUUID, resyncRunID, func(BtrfsScrubStatus) {}); !errors.Is(err, errDiskMissing) {
		t.Fatalf("continueScrub = %v, want a missing disk", err)
	}
	wantScrubCalls(t, k)
}

// Each way LinuxIO learns of a degraded run ends the resync's episode: a
// degraded mount of its own (recordMount), a health run that sees the pool
// degraded (recordFilesystems), and the boot check over a pool already
// mounted degraded, such as a root the initramfs mounted with
// rootflags=degraded, which otherwise waits for the first health run.
func TestEveryDegradedRunEndsTheResyncEpisode(t *testing.T) {
	t.Run("a degraded mount of its own", func(t *testing.T) {
		stubPoolGeneration(t, dataUUID, 91, "/dev/sdb", "")
		stubMountCalls(t)
		stubDisks(t, map[string]Superblock{"sdb": {UUID: dataUUID, Label: "data", DevID: 1, NumDevices: 2, Generation: 90}})
		writeFstab(t, "UUID="+dataUUID+" "+filepath.Join(t.TempDir(), "data")+" btrfs defaults 0 0\n")
		resyncEpisode(t, resyncRunID, nil)
		if err := MountDegradedPools(context.Background(), 0); err != nil {
			t.Fatal(err)
		}
		wantEpisodeEnded(t, []uint64{1})
	})
	for name, run := range map[string]func(context.Context) error{
		"a health run": func(ctx context.Context) error { _, err := CheckHealth(ctx); return err },
		"the boot check over a pool mounted degraded": func(ctx context.Context) error { return MountDegradedPools(ctx, 0) },
	} {
		t.Run(name, func(t *testing.T) {
			k := threeDevices()
			k.devices[3].path = ""
			stubKernel(t, k)
			useTempAlerts(t)
			resyncEpisode(t, resyncRunID, nil)
			if err := run(t.Context()); err != nil {
				t.Fatal(err)
			}
			wantEpisodeEnded(t, []uint64{1, 2})
		})
	}
}

// wantEpisodeEnded checks that the pool reads degraded with present taking
// its writes and no resync run left.
func wantEpisodeEnded(t *testing.T, present []uint64) {
	t.Helper()
	states, _ := loadPoolStates()
	if s := states[dataUUID]; !s.Degraded || s.ResyncRun != "" || !slices.Equal(s.Present, present) {
		t.Fatalf("state = %+v, want degraded on %v with the resync run cleared", s, present)
	}
}

// The check before a resync counts its scrub cannot pass on what it could
// not read: when the error counters cannot be read they would read as 0,
// and a disk that went missing would be skipped. A disk that went missing
// starts a new episode, which counts as no failed attempt.
func TestResyncCheckRefusesWhatItCannotRead(t *testing.T) {
	for _, tc := range []struct {
		name    string
		hook    func(k *fakeKernel)
		missing bool
		reason  string
	}{
		{"the error counters", func(k *fakeKernel) { k.statsFail.Store(true) }, false, "could not read the devices"},
		{"a disk gone missing", func(k *fakeKernel) { k.devices[3].path = "" }, true, "a disk is missing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k := threeDevices()
			var once atomic.Bool
			k.scrub = func(id, _ uint64) (scrubProgress, error) {
				if id == 1 && !once.Swap(true) {
					tc.hook(k)
				}
				return scrubProgress{}, nil
			}
			stubKernel(t, k)
			resyncEpisode(t, "", nil)

			err := Resync(t.Context(), dataUUID, nil)
			if err == nil || !strings.Contains(err.Error(), tc.reason) || errors.Is(err, errDiskMissing) != tc.missing {
				t.Fatalf("Resync = %v, want it to fail with %q", err, tc.reason)
			}
			states, _ := loadPoolStates()
			if s := states[dataUUID]; !s.Degraded || s.Scrubbed || (s.ResyncFailures == 0) == !tc.missing {
				t.Fatalf("state = %+v, want unscrubbed, a failure counted unless a disk went missing", s)
			}
			if got := alertKeys(t); got["resynced-"+dataUUID] != "" {
				t.Fatalf("alerts = %v, want no whole-again notice", got)
			}
		})
	}
}

// singleDataKernel is threeDevices with data written without redundancy,
// which the resync converts; hook runs inside the convert, which then
// leaves no single chunk.
func singleDataKernel(hook func(k *fakeKernel)) *fakeKernel {
	k := threeDevices()
	k.spaces = append(k.spaces, spaceInfo{Flags: bgData, TotalBytes: 1 << 30, UsedBytes: 1 << 30})
	k.balance = func(*balanceArgs) error {
		hook(k)
		k.spaces = slices.DeleteFunc(k.spaces, func(s spaceInfo) bool { return s.Flags == bgData })
		return nil
	}
	return k
}

// The convert writes the second copies onto the returned disk, so the
// counters are checked again right before the pool is declared whole. A
// disk whose writes failed meanwhile ends the attempt, the scrub no longer
// counts, and the next resync scans again from 0.
func TestResyncScansAgainWhenWritesFailDuringTheConvert(t *testing.T) {
	k := singleDataKernel(func(k *fakeKernel) { k.setErrors(2, 4, 0) })
	stubKernel(t, k)
	resyncEpisode(t, "", nil)

	err := Resync(t.Context(), dataUUID, nil)
	if err == nil || !strings.Contains(err.Error(), "device 2 (/dev/sdb1) failed writes") {
		t.Fatalf("Resync = %v, want the failed writes during the convert", err)
	}
	states, _ := loadPoolStates()
	if s := states[dataUUID]; !s.Degraded || s.Scrubbed || s.ResyncFailures != 1 {
		t.Fatalf("state = %+v, want unscrubbed and one failure", s)
	}
	if err := Resync(t.Context(), dataUUID, nil); err != nil {
		t.Fatalf("the next resync: %v", err)
	}
	wantScrubCalls(t, k, "SCRUB 1", "SCRUB 2", "SCRUB 3", "SCRUB 1", "SCRUB 2", "SCRUB 3")
	wantWhole(t)
}

// A device of the scrub that is no longer in the pool, as one removed during
// the convert, is not covered by the check: the attempt fails and the next
// resync scans the pool as it now is.
func TestResyncCheckFailsWhenADeviceLeftThePool(t *testing.T) {
	k := singleDataKernel(func(k *fakeKernel) { delete(k.devices, 3) })
	stubKernel(t, k)
	resyncEpisode(t, "", nil)

	if err := Resync(t.Context(), dataUUID, nil); err == nil || !strings.Contains(err.Error(), "device 3") {
		t.Fatalf("Resync = %v, want it to name the device that left", err)
	}
	if err := Resync(t.Context(), dataUUID, nil); err != nil {
		t.Fatalf("the next resync: %v", err)
	}
	wantScrubCalls(t, k, "SCRUB 1", "SCRUB 2", "SCRUB 3", "SCRUB 1", "SCRUB 2")
	wantWhole(t)
}

// An attempt that skips the scrub because it already counts checks it too:
// a disk whose writes failed since, or a record no longer of this run (a
// scrub started from the pool details), makes the next resync scan again.
func TestResyncChecksAScrubThatAlreadyCounts(t *testing.T) {
	first := time.Unix(1700000000, 0)
	for _, tc := range []struct {
		name, recRun, reason string
		errors               bool
	}{
		{"writes failed since", resyncRunID, "device 2 (/dev/sdb1) failed writes", true},
		{"another scrub's record", "job-1", "no longer this resync's", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k := threeDevices()
			if tc.errors {
				k.setErrors(2, 2, 0)
			}
			stubKernel(t, k)
			resyncEpisode(t, resyncRunID, &scrubRecord{
				Status: "finished", StartedAt: &first, FinishedAt: &first, Percent: 100, Run: tc.recRun,
				Devices: map[int]scrubDeviceRecord{1: {UUID: devUUID(1), Done: true}, 2: {UUID: devUUID(2), Done: true}, 3: {UUID: devUUID(3), Done: true}},
			})
			if err := updatePoolStates(t.Context(), func(s map[string]poolState) {
				state := s[dataUUID]
				state.Scrubbed = true
				s[dataUUID] = state
			}); err != nil {
				t.Fatal(err)
			}

			if err := Resync(t.Context(), dataUUID, nil); err == nil || !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("Resync = %v, want it to fail with %q", err, tc.reason)
			}
			wantScrubCalls(t, k)
			if states, _ := loadPoolStates(); states[dataUUID].Scrubbed {
				t.Fatalf("state = %+v, want the scrub no longer counted", states[dataUUID])
			}
			if err := Resync(t.Context(), dataUUID, nil); err != nil {
				t.Fatalf("the next resync: %v", err)
			}
			wantScrubCalls(t, k, "SCRUB 1", "SCRUB 2", "SCRUB 3")
			wantWhole(t)
		})
	}
}

// A state an older build left reads scrubbed with no resync run: no record
// can prove that scrub, so the first resync after the upgrade scrubs rather
// than failing an attempt on a record that is not its run's.
func TestResyncAfterAnUpgradeScrubs(t *testing.T) {
	k := threeDevices()
	stubKernel(t, k)
	if err := updatePoolStates(t.Context(), func(s map[string]poolState) {
		s[dataUUID] = poolState{Generation: 9, Degraded: true, Scrubbed: true}
	}); err != nil {
		t.Fatal(err)
	}

	if err := Resync(t.Context(), dataUUID, nil); err != nil {
		t.Fatal(err)
	}
	wantScrubCalls(t, k, "SCRUB 1", "SCRUB 2", "SCRUB 3")
	wantWhole(t)
}

// A disk added after the scrub is no part of it: the convert may write
// second copies onto it, so the final check fails, and the next resync
// scans the pool with that disk.
func TestResyncCheckFailsForADeviceAddedAfterTheScrub(t *testing.T) {
	k := singleDataKernel(func(k *fakeKernel) {
		k.maxID = 4
		k.devices[4] = &fakeDevice{path: "/dev/sdd1", uuid: devUUID(4), total: 100 << 30, used: 1 << 30, physical: 100 << 30}
		if err := os.MkdirAll(filepath.Join(sysFSBtrfs, dataUUID, "devices", "sdd1"), 0o755); err != nil {
			t.Error(err)
		}
	})
	stubKernel(t, k)
	resyncEpisode(t, "", nil)

	if err := Resync(t.Context(), dataUUID, nil); err == nil || !strings.Contains(err.Error(), "device 4 (/dev/sdd1) is no part of the scrub") {
		t.Fatalf("Resync = %v, want the added device to fail the check", err)
	}
	if err := Resync(t.Context(), dataUUID, nil); err != nil {
		t.Fatalf("the next resync: %v", err)
	}
	wantScrubCalls(t, k, "SCRUB 1", "SCRUB 2", "SCRUB 3", "SCRUB 1", "SCRUB 2", "SCRUB 3", "SCRUB 4")
	wantWhole(t)
}

// resyncConvertBalance is the soft data convert to raid1 a resync starts,
// as the kernel reports it once it continued the balance at mount.
func resyncConvertBalance(state uint64) balanceArgs {
	return balanceArgs{
		Flags: balanceData | balanceResume, State: state,
		Data: balanceFilter{Flags: balanceArgsConvert | balanceArgsSoft, Target: bgRaid1},
	}
}

// convertKernel is raid1Kernel with a single data chunk to convert and a
// balance the kernel already holds, reported by progress until it ends.
func convertKernel(progress func(polls int) (balanceArgs, error)) *fakeKernel {
	k := uuidKernel()
	k.spaces = append(k.spaces, spaceInfo{Flags: bgData, TotalBytes: 1 << 30, UsedBytes: 1 << 30})
	polls := 0
	k.balanceProgress = func() (balanceArgs, error) {
		polls++
		return progress(polls)
	}
	k.balance = func(*balanceArgs) error { return unix.EINPROGRESS }
	return k
}

// uuidKernel is raid1Kernel with a UUID for each device, as the scrubbed
// record names them.
func uuidKernel() *fakeKernel {
	k := raid1Kernel()
	k.devices[1].uuid, k.devices[2].uuid = devUUID(1), devUUID(2)
	return k
}

// scrubbedEpisode stores a pool whose resync scrub already counts.
func scrubbedEpisode(t *testing.T) {
	t.Helper()
	if err := updatePoolStates(t.Context(), func(s map[string]poolState) {
		s[dataUUID] = poolState{Generation: 9, Degraded: true, Scrubbed: true, ResyncRun: resyncRunID}
	}); err != nil {
		t.Fatal(err)
	}
	first := time.Unix(1700000000, 0)
	saveScrubRecord(t.Context(), dataUUID, scrubRecord{
		Status: "finished", StartedAt: &first, FinishedAt: &first, Percent: 100, Run: resyncRunID,
		Devices: map[int]scrubDeviceRecord{1: {UUID: devUUID(1), Done: true}, 2: {UUID: devUUID(2), Done: true}},
	})
}

// wantNoBalanceCall checks that the resync neither started, resumed nor
// canceled a balance.
func wantNoBalanceCall(t *testing.T, k *fakeKernel) {
	t.Helper()
	if calls := k.Calls(); slices.ContainsFunc(calls, func(c string) bool { return strings.HasPrefix(c, "BALANCE") }) {
		t.Fatalf("calls = %v, want no balance started, resumed or canceled", calls)
	}
}

// A power cut or a shutdown during the resync's convert leaves the balance
// item, and the kernel continues the balance when the pool is mounted
// read-write (volumes.c btrfs_resume_balance_async) while the scrub already
// counts. The next resync recognises its own convert by the arguments the
// kernel reports and follows it to the end, rather than failing on the new
// balance the kernel refuses with EINPROGRESS.
func TestResyncFollowsABalanceTheKernelContinued(t *testing.T) {
	var k *fakeKernel
	k = convertKernel(func(polls int) (balanceArgs, error) {
		if polls > 3 {
			if polls == 4 { // the kernel's balance converted the single chunk
				k.spaces = k.spaces[:len(k.spaces)-1]
			}
			return balanceArgs{}, unix.ENOTCONN
		}
		a := resyncConvertBalance(balanceStateRunning)
		a.Stat = balanceProgress{Expected: 4, Completed: uint64(polls)}
		return a, nil
	})
	stubKernel(t, k)
	scrubbedEpisode(t)

	if err := Resync(t.Context(), dataUUID, nil); err != nil {
		t.Fatal(err)
	}
	wantNoBalanceCall(t, k)
	wantWhole(t)
}

// A running balance that is not the resync's own convert, an admin's or a
// job's, is not followed: the attempt fails as before, saying so, and the
// balance is left alone. A paused balance, even the resync's own, is not
// resumed: an admin paused it, or mounted with skip_balance, the recovery
// option for a balance that fails at mount.
func TestResyncTakesOverNoBalanceItDidNotStart(t *testing.T) {
	usage := resyncConvertBalance(balanceStateRunning)
	usage.Data = balanceFilter{Flags: balanceArgsUsage, Usage: 50}
	for _, tc := range []struct {
		name    string
		balance balanceArgs
		reason  string
	}{
		{"an admin's running balance", usage, "not the resync's convert"},
		{"its own convert, paused", resyncConvertBalance(0), "a paused balance waits"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// It ends after a while, so a resync that follows it returns.
			k := convertKernel(func(polls int) (balanceArgs, error) {
				if polls > 20 {
					return balanceArgs{}, unix.ENOTCONN
				}
				return tc.balance, nil
			})
			stubKernel(t, k)
			scrubbedEpisode(t)

			err := Resync(t.Context(), dataUUID, nil)
			if err == nil || !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("Resync = %v, want it to fail with %q", err, tc.reason)
			}
			wantNoBalanceCall(t, k)
			if states, _ := loadPoolStates(); states[dataUUID].ResyncFailures != 1 || !states[dataUUID].Degraded {
				t.Fatalf("state = %+v, want one failed attempt", states[dataUUID])
			}
		})
	}
}

// A resync stopped while it follows its convert, at a shutdown or by a stop
// of its unit, stops watching and leaves the balance running: the kernel
// pauses it at unmount and continues it at the next mount.
func TestResyncLeavesTheBalanceItFollowsWhenStopped(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	k := convertKernel(func(polls int) (balanceArgs, error) {
		switch {
		case polls == 3:
			cancel()
		case polls > 20: // it ends after a while, so a watch that outlives the stop returns
			return balanceArgs{}, unix.ENOTCONN
		}
		return resyncConvertBalance(balanceStateRunning), nil
	})
	stubKernel(t, k)
	scrubbedEpisode(t)

	if err := Resync(ctx, dataUUID, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("Resync = %v, want it stopped", err)
	}
	wantNoBalanceCall(t, k)
	if states, _ := loadPoolStates(); states[dataUUID].ResyncFailures != 0 {
		t.Fatalf("state = %+v, want no failed attempt for a stop", states[dataUUID])
	}
}

// The pool is declared whole only when nothing is left to convert, by the
// rule that started the convert: system chunks count with metadata, so a
// single system chunk beside raid1 metadata fails the attempt.
func TestResyncFailsWhileChunksStillNeedConverting(t *testing.T) {
	k := uuidKernel()
	k.spaces = append(k.spaces, spaceInfo{Flags: bgSystem, TotalBytes: 32 << 20, UsedBytes: 16 << 10})
	k.balance = func(*balanceArgs) error { return nil } // converts nothing
	stubKernel(t, k)
	scrubbedEpisode(t)

	if err := Resync(t.Context(), dataUUID, nil); err == nil || !strings.Contains(err.Error(), "still has chunks to convert") {
		t.Fatalf("Resync = %v, want the single system chunk to fail it", err)
	}
	if states, _ := loadPoolStates(); !states[dataUUID].Degraded {
		t.Fatalf("state = %+v, want the pool still degraded", states[dataUUID])
	}
}

// convertedEpisode stores a scrubbed episode whose resync started a soft
// convert of data to dataTarget and metadata to metaTarget ("" for none).
func convertedEpisode(t *testing.T, dataTarget, metaTarget string) {
	t.Helper()
	scrubbedEpisode(t)
	if err := updatePoolStates(t.Context(), func(s map[string]poolState) {
		state := s[dataUUID]
		state.ConvertData, state.ConvertMetadata = dataTarget, metaTarget
		s[dataUUID] = state
	}); err != nil {
		t.Fatal(err)
	}
}

// The kernel relocates chunks from the highest address down, whatever their
// kind, so a convert of data and metadata cut midway can leave only one
// kind to convert while the kernel continues the whole convert. The resync
// recognises it by the convert it recorded starting, not by what is left
// now, and follows it.
func TestResyncFollowsItsConvertAfterOneKindFinished(t *testing.T) {
	both := balanceArgs{
		Flags: balanceData | balanceMetadata | balanceSystem | balanceResume, State: balanceStateRunning,
		Data: balanceFilter{Flags: balanceArgsConvert | balanceArgsSoft, Target: bgRaid1},
		Meta: balanceFilter{Flags: balanceArgsConvert | balanceArgsSoft, Target: bgRaid1},
		Sys:  balanceFilter{Flags: balanceArgsConvert | balanceArgsSoft, Target: bgRaid1},
	}
	k := uuidKernel()
	k.spaces = append(k.spaces, spaceInfo{Flags: bgMetadata, TotalBytes: 256 << 20, UsedBytes: 1 << 20}) // the data is done
	polls := 0
	k.balanceProgress = func() (balanceArgs, error) {
		polls++
		if polls > 3 {
			if polls == 4 {
				k.spaces = k.spaces[:len(k.spaces)-1]
			}
			return balanceArgs{}, unix.ENOTCONN
		}
		return both, nil
	}
	k.balance = func(*balanceArgs) error { return unix.EINPROGRESS }
	stubKernel(t, k)
	convertedEpisode(t, "raid1", "raid1")

	if err := Resync(t.Context(), dataUUID, nil); err != nil {
		t.Fatal(err)
	}
	wantNoBalanceCall(t, k)
	wantWhole(t)
}

// A running balance unlike the convert the resync recorded is no convert
// of its own, even when it matches what is left to convert now.
func TestResyncRefusesABalanceUnlikeItsRecordedConvert(t *testing.T) {
	k := convertKernel(func(polls int) (balanceArgs, error) {
		if polls > 20 {
			return balanceArgs{}, unix.ENOTCONN
		}
		return resyncConvertBalance(balanceStateRunning), nil // data to raid1
	})
	stubKernel(t, k)
	convertedEpisode(t, "raid10", "")

	if err := Resync(t.Context(), dataUUID, nil); err == nil || !strings.Contains(err.Error(), "not the resync's convert") {
		t.Fatalf("Resync = %v, want the balance refused", err)
	}
	wantNoBalanceCall(t, k)
}

// The resync records the convert it starts before it issues it, and the
// record goes with the episode: cleared when the pool is whole again, and
// by a degraded run that starts a new episode.
func TestResyncRecordsItsConvertForTheEpisode(t *testing.T) {
	k := singleDataKernel(func(*fakeKernel) {
		if states, _ := loadPoolStates(); states[dataUUID].ConvertData != "raid1" || states[dataUUID].ConvertMetadata != "" {
			t.Errorf("state while converting = %+v, want the data convert to raid1 recorded", states[dataUUID])
		}
	})
	stubKernel(t, k)
	resyncEpisode(t, "", nil)

	if err := Resync(t.Context(), dataUUID, nil); err != nil {
		t.Fatal(err)
	}
	wantWhole(t)
	state := poolState{Degraded: true, ResyncRun: resyncRunID, ConvertData: "raid1", ConvertMetadata: "raid1"}
	state.markDegraded(nil)
	if state.ConvertData != "" || state.ConvertMetadata != "" {
		t.Fatalf("state after a degraded run = %+v, want the convert record cleared", state)
	}
}

// A pool that went degraded during the convert is a new episode, not a
// failed attempt, though new single chunks are then left to convert.
func TestResyncRefusesAPoolThatWentDegradedDuringTheConvert(t *testing.T) {
	k := uuidKernel()
	k.maxID = 3
	k.devices[3] = &fakeDevice{path: "/dev/sdc1", uuid: devUUID(3), total: 100 << 30, used: 51 << 30, physical: 100 << 30}
	k.spaces = append(k.spaces, spaceInfo{Flags: bgData, TotalBytes: 1 << 30, UsedBytes: 1 << 30})
	k.balance = func(*balanceArgs) error {
		k.devices[3].path = "" // gone; its writes now land on single chunks
		return nil
	}
	stubKernel(t, k)
	resyncEpisode(t, "", nil)

	if err := Resync(t.Context(), dataUUID, nil); !errors.Is(err, errDiskMissing) {
		t.Fatalf("Resync = %v, want a missing disk", err)
	}
	if states, _ := loadPoolStates(); states[dataUUID].ResyncFailures != 0 {
		t.Fatalf("state = %+v, want no failed attempt", states[dataUUID])
	}
}
