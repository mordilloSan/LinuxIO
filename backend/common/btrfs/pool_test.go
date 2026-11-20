package btrfs

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// superblock builds a primary superblock buffer with the given identity.
func superblock(uuid [16]byte, label string, devid, numDevices, totalBytes uint64) []byte {
	b := make([]byte, superSize)
	copy(b[superFSID:], uuid[:])
	binary.LittleEndian.PutUint64(b[superMagicAt:], superMagic)
	binary.LittleEndian.PutUint64(b[superTotalBytes:], totalBytes)
	binary.LittleEndian.PutUint64(b[superNumDevices:], numDevices)
	binary.LittleEndian.PutUint64(b[superDevID:], devid)
	copy(b[superLabel:], label)
	return b
}

// stubExec records invocations and replaces the command with a no-op.
func stubExec(t *testing.T) *[][]string {
	t.Helper()
	var calls [][]string
	old := execCommand
	execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		calls = append(calls, append([]string{name}, args...))
		return exec.CommandContext(ctx, "true")
	}
	t.Cleanup(func() { execCommand = old })
	return &calls
}

// stubControlIoctl records control-device ioctls instead of issuing them.
func stubControlIoctl(t *testing.T, err error) *[]string {
	t.Helper()
	var calls []string
	old := controlIoctl
	controlIoctl = func(req uintptr, device string) error {
		calls = append(calls, map[uintptr]string{iocScanDev: "scan", iocForgetDev: "forget"}[req]+" "+device)
		return err
	}
	t.Cleanup(func() { controlIoctl = old })
	return &calls
}

func TestParseSuperblock(t *testing.T) {
	uuid := [16]byte{0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0xde, 0xf0, 1, 2, 3, 4, 5, 6, 7, 8}
	sb, err := parseSuperblock(superblock(uuid, "tank", 2, 3, 300))
	if err != nil {
		t.Fatal(err)
	}
	want := Superblock{UUID: "12345678-9abc-def0-0102-030405060708", Label: "tank", DevID: 2, DevUUID: "00000000-0000-0000-0000-000000000000", NumDevices: 3, TotalBytes: 300}
	if sb != want {
		t.Fatalf("got %+v, want %+v", sb, want)
	}
	if _, err := parseSuperblock(make([]byte, superSize)); !errors.Is(err, errNotBtrfs) {
		t.Fatalf("blank: err = %v", err)
	}
	if _, err := parseSuperblock(nil); err == nil {
		t.Fatal("short buffer accepted")
	}
}

func TestNewUUIDIsVersion4(t *testing.T) {
	u := newUUID()
	if len(u) != 36 || u[14] != '4' || !strings.ContainsRune("89ab", rune(u[19])) || u == newUUID() {
		t.Fatalf("uuid = %q", u)
	}
}

func TestValidateLabel(t *testing.T) {
	for _, bad := range []string{"", "a/b", "a\nb", strings.Repeat("x", labelSize)} {
		if err := ValidateLabel(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if err := ValidateLabel("tank 1"); err != nil {
		t.Fatal(err)
	}
}

func TestCreatePoolFormatsAndRegisters(t *testing.T) {
	if _, err := findCommand("mkfs.btrfs"); err != nil {
		t.Skip("mkfs.btrfs not installed")
	}
	calls := stubExec(t)
	ioctls := stubControlIoctl(t, nil)
	oldSize, oldDiscard := blockDeviceSize, discardDevice
	t.Cleanup(func() { blockDeviceSize, discardDevice = oldSize, oldDiscard })
	blockDeviceSize = func(string) (uint64, error) { return 1 << 30, nil }
	discardDevice = func(_ context.Context, device string, size uint64, advance func(uint64)) error {
		*ioctls = append(*ioctls, "discard "+device)
		advance(size)
		return nil
	}

	uuid, err := CreatePool(context.Background(), "tank", "raid1", "raid1", []string{"/dev/sdb", "/dev/sdc"}, true, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 {
		t.Fatalf("calls = %v", *calls)
	}
	want := []string{"-L", "tank", "-U", uuid, "-d", "raid1", "-m", "raid1", "-K", "-f", "/dev/sdb", "/dev/sdc"}
	if got := (*calls)[0][1:]; !reflect.DeepEqual(got, want) {
		t.Fatalf("mkfs args = %v, want %v", got, want)
	}
	if !reflect.DeepEqual(*ioctls, []string{"discard /dev/sdb", "discard /dev/sdc", "scan /dev/sdb", "scan /dev/sdc"}) {
		t.Fatalf("ioctls = %v", *ioctls)
	}
}

// TestCreatePoolChecksEachDeviceRightBeforeWriting covers check, which the
// bridge uses to read each device's member signature: it runs right before
// the device's trim, and for every device again right before mkfs.btrfs,
// which starts only after the last trim. A refusal stops the create there.
func TestCreatePoolChecksEachDeviceRightBeforeWriting(t *testing.T) {
	if _, err := findCommand("mkfs.btrfs"); err != nil {
		t.Skip("mkfs.btrfs not installed")
	}
	devices := []string{"/dev/sdb", "/dev/sdc"}
	var steps []string
	oldSize, oldDiscard := blockDeviceSize, discardDevice
	t.Cleanup(func() { blockDeviceSize, discardDevice = oldSize, oldDiscard })
	blockDeviceSize = func(string) (uint64, error) { return 1 << 30, nil }
	discardDevice = func(_ context.Context, device string, size uint64, advance func(uint64)) error {
		steps = append(steps, "discard "+device)
		advance(size)
		return nil
	}
	refusal := errors.New("member")
	checkRefusing := func(refuse int) func(string) error {
		return func(device string) error {
			steps = append(steps, "check "+device)
			if refuse--; refuse == 0 {
				return refusal
			}
			return nil
		}
	}
	calls := stubExec(t)
	stubControlIoctl(t, nil)

	if _, err := CreatePool(context.Background(), "tank", "raid1", "raid1", devices, true, checkRefusing(-1), nil); err != nil {
		t.Fatal(err)
	}
	want := []string{"check /dev/sdb", "discard /dev/sdb", "check /dev/sdc", "discard /dev/sdc", "check /dev/sdb", "check /dev/sdc"}
	if !slices.Equal(steps, want) || len(*calls) != 1 {
		t.Fatalf("steps = %q, mkfs runs %d; want %q, then mkfs", steps, len(*calls), want)
	}

	// Refused before the second trim, then before mkfs.
	for refuse, done := range map[int]int{2: 3, 3: 5} {
		steps, *calls = nil, nil
		if _, err := CreatePool(context.Background(), "tank", "raid1", "raid1", devices, true, checkRefusing(refuse), nil); !errors.Is(err, refusal) {
			t.Fatalf("refused at check %d: err = %v", refuse, err)
		}
		if !slices.Equal(steps, want[:done]) || len(*calls) != 0 {
			t.Fatalf("refused at check %d: steps = %q, mkfs runs %d", refuse, steps, len(*calls))
		}
	}
}

func TestCreatePoolRefusesBeforeFormatting(t *testing.T) {
	calls := stubExec(t)
	cases := map[string]func() error{
		"too few devices": func() error {
			_, err := CreatePool(context.Background(), "tank", "raid1", "raid1", []string{"/dev/sdb"}, false, nil, nil)
			return err
		},
		"raid5": func() error {
			_, err := CreatePool(context.Background(), "tank", "raid5", "raid1", []string{"/dev/sdb", "/dev/sdc", "/dev/sdd"}, false, nil, nil)
			return err
		},
		"bad label": func() error {
			_, err := CreatePool(context.Background(), "a/b", "single", "dup", []string{"/dev/sdb"}, false, nil, nil)
			return err
		},
		"no devices": func() error {
			_, err := CreatePool(context.Background(), "tank", "single", "dup", nil, false, nil, nil)
			return err
		},
	}
	for name, run := range cases {
		if err := run(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if len(*calls) != 0 {
		t.Fatalf("mkfs ran: %v", *calls)
	}
}

func TestMountPool(t *testing.T) {
	var got []any
	old := mountFS
	mountFS = func(source, target, fstype string, flags uintptr, data string) error {
		got = []any{source, target, fstype, flags, data}
		return nil
	}
	t.Cleanup(func() { mountFS = old })
	stubPoolGeneration(t, dataUUID, 7, "/dev/sdb", "")
	target := filepath.Join(t.TempDir(), "mnt", "tank")

	// fstab-only options are dropped, VFS ones become flags and the rest,
	// such as subvol=, reaches btrfs.
	options := []string{"defaults", "noatime", "nofail", "x-systemd.device-timeout=10s", "subvol=@data", "compress=zstd"}
	if err := MountPool(context.Background(), "/dev/sdb", target, options, true); err != nil {
		t.Fatal(err)
	}
	want := []any{"/dev/sdb", target, "btrfs", uintptr(unix.MS_NOATIME), "subvol=@data,compress=zstd,degraded"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mount = %v, want %v", got, want)
	}
	if states, err := loadPoolStates(); err != nil || !reflect.DeepEqual(withoutMissingAt(states[dataUUID]), poolState{Generation: 7, Degraded: true, Present: []uint64{1}, Missing: []missingDisk{{ID: 2, UUID: "00000000-0000-0000-0000-000000000000"}}, Since: states[dataUUID].Since}) || states[dataUUID].Since == 0 {
		t.Fatalf("state = %+v, %v; want generation 7, degraded on devid 1", states, err)
	}
	if info, err := os.Stat(target); err != nil || !info.IsDir() {
		t.Fatalf("mountpoint not created: %v", err)
	}
}

func TestWipeDevice(t *testing.T) {
	ioctls := stubControlIoctl(t, unix.ENOENT)
	device := filepath.Join(t.TempDir(), "disk")
	image := make([]byte, superOffset+superSize)
	copy(image[superOffset:], superblock([16]byte{1}, "tank", 1, 1, 0))
	if err := os.WriteFile(device, image, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := WipeDevice(device); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(device)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseSuperblock(after[superOffset:]); !errors.Is(err, errNotBtrfs) {
		t.Fatalf("magic survived: %v", err)
	}
	if string(after[superOffset+superLabel:superOffset+superLabel+4]) != "tank" {
		t.Fatal("more than the magic was overwritten")
	}
	if !reflect.DeepEqual(*ioctls, []string{"forget " + device}) {
		t.Fatalf("ioctls = %v", *ioctls)
	}

	// A device that is not btrfs is refused untouched.
	if err := WipeDevice(device); !errors.Is(err, errNotBtrfs) {
		t.Fatalf("second wipe: err = %v", err)
	}
}

func TestSuperblockOffsetsSkipsBeyondDevice(t *testing.T) {
	if got, want := superblockOffsets(100<<20), []int64{65536, 67108864}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestWipePoolSuperblocksOnlyTouchesItsOwnFSID(t *testing.T) {
	ioctls := stubControlIoctl(t, unix.ENOENT)
	ours, theirs := [16]byte{0xa}, [16]byte{0xb}
	image := filepath.Join(t.TempDir(), "disk")
	f, err := os.Create(image)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err = f.Truncate(257 << 30); err != nil { // sparse: three superblocks' worth on disk
		t.Fatal(err)
	}
	// A failed mkfs leaves the temporary magic; a pool of another fsid owns the last copy.
	temporary := superblock(ours, "tank", 1, 1, 0)
	binary.LittleEndian.PutUint64(temporary[superMagicAt:], superMagicTemporary)
	for off, sb := range map[int64][]byte{superOffset: superblock(ours, "tank", 1, 1, 0), 64 << 20: temporary, 256 << 30: superblock(theirs, "other", 1, 1, 0)} {
		if _, err = f.WriteAt(sb, off); err != nil {
			t.Fatal(err)
		}
	}
	magic := func(off int64) uint64 {
		b := make([]byte, 8)
		if _, readErr := f.ReadAt(b, off+superMagicAt); readErr != nil {
			t.Fatal(readErr)
		}
		return binary.LittleEndian.Uint64(b)
	}

	wiped, err := WipePoolSuperblocks(image, formatUUID(ours[:]))
	if err != nil || !wiped {
		t.Fatalf("wiped = %t, err = %v", wiped, err)
	}
	if got := []uint64{magic(superOffset), magic(64 << 20), magic(256 << 30)}; !reflect.DeepEqual(got, []uint64{0, 0, superMagic}) {
		t.Fatalf("magics = %#x, want only the other fsid's left", got)
	}
	label := make([]byte, 4)
	if _, err = f.ReadAt(label, superOffset+superLabel); err != nil || string(label) != "tank" {
		t.Fatalf("more than the magic was overwritten: %q %v", label, err)
	}
	if !reflect.DeepEqual(*ioctls, []string{"forget " + image}) {
		t.Fatalf("ioctls = %v", *ioctls)
	}

	if wiped, err = WipePoolSuperblocks(image, formatUUID(ours[:])); err != nil || wiped {
		t.Fatalf("second call: wiped = %t, err = %v", wiped, err)
	}
	if len(*ioctls) != 1 {
		t.Fatalf("forgot a device it did not wipe: %v", *ioctls)
	}
}

func TestFindOfflinePools(t *testing.T) {
	stubMounts(t, []Mount{{Mountpoint: "/data", UUID: "mounted-uuid", SubvolID: 5}})
	stubDisks(t, map[string]Superblock{
		"sdb": {UUID: "pool-uuid", Label: "tank", DevID: 1, NumDevices: 3, TotalBytes: 300, Generation: 8},
		"sdc": {UUID: "pool-uuid", Label: "tank", DevID: 3, NumDevices: 3, TotalBytes: 300, Generation: 9},
		"sdd": {UUID: "mounted-uuid", Label: "data", NumDevices: 1},
		// A replace target is scanned but is no member: cold misses a disk.
		"sdf": {UUID: "cold-uuid", Label: "cold", DevID: 1, NumDevices: 2, Generation: 4},
		"sdg": {UUID: "cold-uuid", Label: "cold", DevID: replaceTargetDevID, NumDevices: 2, Generation: 4},
	})

	pools, err := FindOfflinePools(context.Background(), []string{"/dev/sdb", "/dev/sdd", "/dev/sde", "/dev/sdc", "/dev/sdf", "/dev/sdg"})
	if err != nil {
		t.Fatal(err)
	}
	want := []BtrfsOfflinePool{
		{UUID: "pool-uuid", Label: "tank", NumDevices: 3, Devices: []string{"/dev/sdb", "/dev/sdc"}, TotalBytes: 300, Missing: 1, devids: []uint64{1, 3}, generations: []uint64{8, 9}},
		{UUID: "cold-uuid", Label: "cold", NumDevices: 2, Devices: []string{"/dev/sdf", "/dev/sdg"}, Missing: 1, devids: []uint64{1, replaceTargetDevID}, generations: []uint64{4, 4}},
	}
	if !reflect.DeepEqual(pools, want) {
		t.Fatalf("got %+v, want %+v", pools, want)
	}
}

// raidLeg is what udev's database holds for a leg of an md array: libblkid
// reads the md superblock before any filesystem, so the btrfs superblock the
// leg also shows is not reported.
var raidLeg = map[string]string{"ID_FS_TYPE": "linux_raid_member", "ID_FS_UUID": "a1b2c3d4-0000-0000-0000-000000000001"}

// Disk 1 of a four-disk pool is the md array md127, whose metadata sits at
// the end, so each leg shows the pool's superblock as well; so do the paths
// of a multipath disk and the image of an LVM mirror. The listing keeps the
// devices that are disks of the pool and counts the disks it lacks by
// devid, leaving out what is part of another device: what something holds,
// a partition of a disk something holds, which the kernel lets nothing claim,
// what udev probed as a member of another set, and, of a device udev has no
// entry for, one whose own md or LVM signature shows. Only those devices
// are read. A device udev probed as something else, as an entry udev has not
// updated yet can say, stays a disk. The pool's label and size come from a
// disk, not from a leg, whose page cache can hold a superblock from before
// the array changed it. A pool that shows only on legs is listed without
// disks.
func TestFindOfflinePoolsLeavesOutPartsOfOtherDevices(t *testing.T) {
	stubMounts(t, nil)
	disk := func(devid uint64) Superblock {
		return Superblock{UUID: dataUUID, Label: "data", DevID: devid, NumDevices: 4, Generation: 9}
	}
	const cold = "0b1c2d3e-4f50-4a61-8b72-c3d4e5f60718"
	stubDisks(t, map[string]Superblock{
		"md127": disk(1), "sdb": disk(1), "sdc": disk(1), "sdd": disk(1), "sde": disk(2),
		"sdf": disk(1), "dm-3": disk(1), "sdh": disk(3), "sdi1": disk(1),
		"dm-1": {UUID: dataUUID, Label: "old", DevID: 1, NumDevices: 3, Generation: 8},
		"sdj":  {UUID: cold, Label: "cold", DevID: 1, NumDevices: 2, Generation: 4},
	})
	// sdi1 is a partition of sdi, which dm-7 holds, in sysfs's layout.
	sdi := filepath.Join(sysBlockDir, "sdi")
	for _, err := range []error{
		os.RemoveAll(filepath.Join(sysBlockDir, "sdi1")),
		os.MkdirAll(filepath.Join(sdi, "sdi1"), 0o755),
		os.MkdirAll(filepath.Join(sdi, "holders", "dm-7"), 0o755),
		os.WriteFile(filepath.Join(sdi, "sdi1", "partition"), []byte("1\n"), 0o644),
		os.Symlink(filepath.Join("sdi", "sdi1"), filepath.Join(sysBlockDir, "sdi1")),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	writeUdevDisks(t,
		udevDisk{name: "dm-1", devnum: "252:1", props: map[string]string{"DM_UDEV_DISABLE_OTHER_RULES_FLAG": "1"}, holders: []string{"dm-9"}},
		udevDisk{name: "sdi1", devnum: "8:129", props: map[string]string{"ID_FS_TYPE": "btrfs", "ID_FS_UUID": dataUUID, "DM_MULTIPATH_DEVICE_PATH": "1"}},
		udevDisk{name: "sdj", devnum: "8:144", props: raidLeg},
		udevDisk{name: "md127", devnum: "9:127", props: scanned("0")},
		// A leg md bound to the array.
		udevDisk{name: "sdb", devnum: "8:16", props: raidLeg, holders: []string{"md127"}},
		// A leg of an array nobody started.
		udevDisk{name: "sdc", devnum: "8:32", props: raidLeg},
		// udev has processed neither sdd nor sde; sdd is a leg.
		udevDisk{name: "sdd", devnum: "8:48"},
		udevDisk{name: "sde", devnum: "8:64"},
		// A path multipath's rule took, before the map exists.
		udevDisk{name: "sdf", devnum: "8:80", props: map[string]string{"ID_FS_TYPE": "mpath_member", "DM_MULTIPATH_DEVICE_PATH": "1"}},
		// An image of an LVM mirror, held by its logical volume.
		udevDisk{name: "dm-3", devnum: "252:3", props: map[string]string{"DM_UDEV_DISABLE_OTHER_RULES_FLAG": "1"}, holders: []string{"dm-5"}},
		udevDisk{name: "sdh", devnum: "8:112", props: map[string]string{"ID_FS_TYPE": "ext4"}},
	)
	read := stubPeekSignatures(t, map[string]string{"/dev/sdd": "md"})

	pools, err := FindOfflinePools(context.Background(), []string{"/dev/dm-1", "/dev/md127", "/dev/sdb", "/dev/sdc", "/dev/sdd", "/dev/sde", "/dev/sdf", "/dev/dm-3", "/dev/sdh", "/dev/sdi1", "/dev/sdj"})
	if err != nil {
		t.Fatal(err)
	}
	want := []BtrfsOfflinePool{{
		UUID: dataUUID, Label: "data", NumDevices: 4, Devices: []string{"/dev/md127", "/dev/sde", "/dev/sdh"}, Missing: 1,
		devids: []uint64{1, 2, 3}, generations: []uint64{9, 9, 9},
		legs: []poolLeg{
			{"/dev/dm-1", 1, "is held by /dev/dm-9"},
			{"/dev/sdb", 1, "is held by /dev/md127"},
			{"/dev/sdc", 1, "udev probed as linux_raid_member"},
			{"/dev/sdd", 1, "carries an md signature"},
			{"/dev/sdf", 1, "udev probed as mpath_member"},
			{"/dev/dm-3", 1, "is held by /dev/dm-5"},
			{"/dev/sdi1", 1, "is a partition of /dev/sdi, held by /dev/dm-7"},
		},
	}, {
		UUID: cold, Label: "cold", NumDevices: 2, Missing: 2,
		legs: []poolLeg{{"/dev/sdj", 1, "udev probed as linux_raid_member"}},
	}}
	if !reflect.DeepEqual(pools, want) {
		t.Fatalf("got %+v, want %+v", pools, want)
	}
	if !slices.Equal(*read, []string{"/dev/sdd", "/dev/sde"}) {
		t.Fatalf("read the signatures of %v, want only the devices udev has no entry for", *read)
	}
}

// Two disks that carry one devid, as two paths of one disk do before
// multipath maps them and as a disk and its copy do, count as one, and a
// replace target as none.
func TestMissingDisksCountsDevids(t *testing.T) {
	for _, tc := range []struct {
		devids []uint64
		want   int
	}{
		{[]uint64{1, 2}, 0},
		{[]uint64{1, 1}, 1},
		{[]uint64{1, 1, 2}, 0},
		{[]uint64{1, replaceTargetDevID}, 1},
		{[]uint64{1, 2, replaceTargetDevID}, 0},
	} {
		if got := missingDisks(BtrfsOfflinePool{NumDevices: 2, devids: tc.devids}); got != tc.want {
			t.Errorf("devids %v of 2: missing %d, want %d", tc.devids, got, tc.want)
		}
	}
}

// MountOfflinePool registers nothing and mounts nothing when it cannot tell
// that each device is a disk of the pool: none is present, two carry one
// devid, udev marks one a path of a multipath disk, or its own md or LVM
// signature shows, read through an exclusive open right before the
// registration. Otherwise it makes the kernel forget each leg, which
// something else may have registered and the mount would then open in place
// of its array, registers each disk and mounts the first.
func TestMountOfflinePoolRegistersOnlyDisksItCanTell(t *testing.T) {
	pool := func(devids ...uint64) BtrfsOfflinePool {
		p := BtrfsOfflinePool{UUID: dataUUID, NumDevices: 2, legs: []poolLeg{{"/dev/sdd", 1, "is held by /dev/md127"}}}
		for i, id := range devids {
			p.Devices = append(p.Devices, "/dev/sd"+string(rune('b'+i)))
			p.devids, p.generations = append(p.devids, id), append(p.generations, 9)
		}
		return p
	}
	for _, tc := range []struct {
		name  string
		pool  BtrfsOfflinePool
		props map[string]string // udev's entry for sdc
		kinds map[string]string
		want  string
	}{
		{"no disk is present", pool(), nil, nil, "no disk of the pool is present"},
		{"two devices carry one devid", pool(1, 1), nil, nil, "/dev/sdb and /dev/sdc both carry disk 1 of the pool"},
		{"a path of a multipath disk", pool(1, 2), map[string]string{"ID_FS_TYPE": "btrfs", "DM_MULTIPATH_DEVICE_PATH": "1"}, nil, "udev marks /dev/sdc a path of a multipath disk"},
		{"an md signature udev has not probed", pool(1, 2), scanned("0"), map[string]string{"/dev/sdc": "md"}, "/dev/sdc carries an md signature"},
		{"the pool's disks", pool(1, 2), scanned("0"), nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stubPoolGeneration(t, dataUUID, 9, "/dev/sdb", "/dev/sdc")
			stubDisks(t, map[string]Superblock{"sdb": {}, "sdc": {}})
			writeUdevDisks(t, udevDisk{name: "sdb", devnum: "8:16", props: scanned("0")}, udevDisk{name: "sdc", devnum: "8:32", props: tc.props})
			stubMemberSignatures(t, tc.kinds)
			calls := stubMountCalls(t)
			mnt := filepath.Join(t.TempDir(), "data")

			err := MountOfflinePool(context.Background(), tc.pool, mnt, nil, false)
			if tc.want == "" {
				if want := []string{"forget /dev/sdd", "register /dev/sdb", "register /dev/sdc", "mount /dev/sdb " + mnt + " "}; err != nil || !slices.Equal(*calls, want) {
					t.Fatalf("err = %v, calls = %q, want %q", err, *calls, want)
				}
				return
			}
			if !errors.Is(err, ErrUnclearDisks) || !strings.Contains(err.Error(), tc.want) || len(*calls) != 0 {
				t.Fatalf("err = %v, calls = %q; want ErrUnclearDisks naming %q and nothing registered", err, *calls, tc.want)
			}
		})
	}
}

func TestBlockDevicesSkipsEmptyAndVirtual(t *testing.T) {
	dir := t.TempDir()
	for name, size := range map[string]string{"sda": "1000", "sda1": "500", "ram0": "10", "zram0": "10", "sr0": "10", "sdb": "0", "loop0": "20", "dm-0": "30"} {
		if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name, "size"), []byte(size+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	old := sysBlockDir
	sysBlockDir = dir
	t.Cleanup(func() { sysBlockDir = old })
	got := BlockDevices()
	if want := []string{"/dev/dm-0", "/dev/loop0", "/dev/sda", "/dev/sda1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
