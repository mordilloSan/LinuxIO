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
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/common/alerts"
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
	member := Superblock{UUID: dataUUID, NumDevices: 2, Generation: 90}
	supers := map[string]Superblock{"sdb": member, "sdc": {}}
	stubDisks(t, supers)
	writeFstab(t, "UUID="+dataUUID+" /srv/data btrfs defaults 0 0\n")
	old := readSuperblock
	polls := 0
	readSuperblock = func(device string) (Superblock, error) {
		if device == "/dev/sdb" {
			if polls++; polls == 2 {
				supers["sdc"] = member // the slow disk shows up on the second look
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
	k.scrub = func(uint64) (scrubProgress, error) { return scrubProgress{CorrectedErrors: 3}, nil }
	stubKernel(t, k)
	ctx := context.Background()
	if err := updatePoolStates(ctx, func(s map[string]poolState) { s[dataUUID] = poolState{Generation: 9, Degraded: true} }); err != nil {
		t.Fatal(err)
	}

	if err := Resync(ctx, dataUUID); err != nil {
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
}

func TestResyncRefusesWhileADiskIsMissing(t *testing.T) {
	k := raid1Kernel()
	k.devices[2].path = ""
	stubKernel(t, k)
	if err := Resync(context.Background(), dataUUID); !errors.Is(err, errDiskMissing) {
		t.Fatalf("Resync = %v, want a missing disk", err)
	}
	if states, _ := loadPoolStates(); states[dataUUID].ResyncFailures != 0 {
		t.Fatalf("a missing disk counted as a failed resync: %+v", states[dataUUID])
	}
}

func TestResyncDoesNotTrustACanceledScrub(t *testing.T) {
	k := raid1Kernel()
	k.scrub = func(uint64) (scrubProgress, error) { return scrubProgress{}, unix.ECANCELED }
	stubKernel(t, k)
	ctx := context.Background()
	if err := updatePoolStates(ctx, func(s map[string]poolState) { s[dataUUID] = poolState{Generation: 9, Degraded: true} }); err != nil {
		t.Fatal(err)
	}

	if err := Resync(ctx, dataUUID); err == nil || !strings.Contains(err.Error(), "canceled before it checked every copy") {
		t.Fatalf("Resync = %v", err)
	}
	states, _ := loadPoolStates()
	if s := states[dataUUID]; !s.Degraded || s.Scrubbed || s.ResyncFailures != 1 {
		t.Fatalf("state = %+v, want still degraded, unscrubbed, one failure", s)
	}
}
