package storage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
)

// partitionDevices is a GPT disk /dev/sdb with 1-2 GiB free, an unused ext4
// partition sdb1 before the gap and a blank partition sdb2 after it.
func partitionDevices() []apischema.StorageBlockDevice {
	return []apischema.StorageBlockDevice{
		{
			Path: "/dev/sdb", Type: "disk", SizeBytes: 4 << 30, PTType: "gpt", Parents: []string{}, Mountpoints: []string{},
			FreeRegions: []apischema.StorageRegion{{StartBytes: 1 << 30, SizeBytes: 1 << 30}},
		},
		{
			Path: "/dev/sdb1", Type: "part", PartNumber: 1, StartBytes: 1 << 20, SizeBytes: 1<<30 - 1<<20, PTType: "gpt",
			FSType: "ext4", UUID: "u1", Parents: []string{"/dev/sdb"}, Mountpoints: []string{},
		},
		{
			Path: "/dev/sdb2", Type: "part", PartNumber: 2, StartBytes: 2 << 30, SizeBytes: 2<<30 - 1<<20, PTType: "gpt",
			Parents: []string{"/dev/sdb"}, Mountpoints: []string{},
		},
	}
}

// partitionEnv stubs every seam of partitions.go; calls records, in order,
// each lock and unlock, kernel busy check and write.
type partitionEnv struct {
	devices []apischema.StorageBlockDevice
	fss     []btrfs.BtrfsFilesystem
	offline []btrfs.BtrfsOfflinePool
	// signatures is what the devices' own md and LVM2 signatures show, by
	// path, which udev may not have reported in devices yet.
	signatures                                       map[string]btrfs.MemberSignature
	calls                                            []string
	busyErr, formatErr, deleteErr, mountErr, initErr error
}

// stubSignatures makes readMemberSignature find sigs, by device, and no
// signature on any other device.
func stubSignatures(t *testing.T, sigs map[string]btrfs.MemberSignature) {
	t.Helper()
	orig := readMemberSignature
	t.Cleanup(func() { readMemberSignature = orig })
	readMemberSignature = func(device string) (btrfs.MemberSignature, error) { return sigs[device], nil }
}

func stubPartitions(t *testing.T, fstab string) *partitionEnv {
	t.Helper()
	origDevices, origFss, origOffline := storageBlockDevices, btrfsFilesystems, offlinePools
	origCreate, origDelete, origInit, origFormat := createPartition, deletePartition, initializeDisk, formatDevice
	origBusy, origSig, origMount, origLock, origProbe := checkNotBusy, readMemberSignature, mountDevice, lockDisk, askUdevToProbe
	t.Cleanup(func() {
		storageBlockDevices, btrfsFilesystems, offlinePools = origDevices, origFss, origOffline
		createPartition, deletePartition, initializeDisk, formatDevice = origCreate, origDelete, origInit, origFormat
		checkNotBusy, readMemberSignature, mountDevice, lockDisk, askUdevToProbe = origBusy, origSig, origMount, origLock, origProbe
	})
	useTempFstab(t, fstab)

	env := &partitionEnv{devices: partitionDevices()}
	record := func(format string, args ...any) { env.calls = append(env.calls, fmt.Sprintf(format, args...)) }
	storageBlockDevices = func(context.Context) ([]apischema.StorageBlockDevice, error) { return env.devices, nil }
	btrfsFilesystems = func(context.Context) ([]btrfs.BtrfsFilesystem, error) { return env.fss, nil }
	offlinePools = func(context.Context) ([]btrfs.BtrfsOfflinePool, error) { return env.offline, nil }
	lockDisk = func(_ context.Context, device string) (func(), error) {
		record("lock %s", device)
		return func() { record("unlock %s", device) }, nil
	}
	checkNotBusy = func(device string) error {
		record("check %s", device)
		return env.busyErr
	}
	// The read opens the device exclusively, so it is the busy check too.
	readMemberSignature = func(device string) (btrfs.MemberSignature, error) {
		record("check %s", device)
		sig := env.signatures[device]
		sig.Device = device // as btrfs.ReadMemberSignature fills it, found or not
		return sig, env.busyErr
	}
	askUdevToProbe = func(device string) { record("probe %s", device) }
	createPartition = func(_ context.Context, device string, start, size uint64) (string, error) {
		record("create %s %d %d", device, start, size)
		return "/dev/sdb3", nil
	}
	formatDevice = func(_ context.Context, device, fsType, label string, overwrite bool, progress func(float64)) (string, error) {
		record("format %s %s %q overwrite=%t", device, fsType, label, overwrite)
		progress(50) // halfway through the trim
		progress(100)
		return "new-uuid", env.formatErr
	}
	mountDevice = func(source, target, fsType string, flags uintptr, data string) error {
		record("mount %s %s %s %d %q", source, target, fsType, flags, data)
		return env.mountErr
	}
	deletePartition = func(_ context.Context, device string, number int) error {
		record("delete %s %d", device, number)
		return env.deleteErr
	}
	// Like btrfs.InitializeDisk, it hands check the signatures of the disk
	// and its partitions before it writes anything.
	initializeDisk = func(_ context.Context, device string, check func([]btrfs.MemberSignature) error) error {
		if env.initErr != nil {
			return env.initErr
		}
		var found []btrfs.MemberSignature
		for _, d := range env.devices {
			if s := env.signatures[d.Path]; s.Kind != "" && (d.Path == device || slices.Contains(d.Parents, device)) {
				found = append(found, s)
			}
		}
		if err := check(found); err != nil {
			return err
		}
		record("initialize %s", device)
		return nil
	}
	return env
}

func TestDeviceInUse(t *testing.T) {
	free := partitionDevices()[1]
	fss := []btrfs.BtrfsFilesystem{{UUID: "f1", Label: "tank", Devices: []btrfs.BtrfsDevice{{Path: "/dev/sdc1"}}}}
	offline := []btrfs.BtrfsOfflinePool{{UUID: "o1", Devices: []string{"/dev/sdd1"}}}
	with := func(edit func(*apischema.StorageBlockDevice)) apischema.StorageBlockDevice {
		d := free
		edit(&d)
		return d
	}
	cases := []struct {
		name, reason string
		dev          apischema.StorageBlockDevice
	}{
		{"mounted", "Mounted at /x", with(func(d *apischema.StorageBlockDevice) { d.Busy = "Mounted at /x" })},
		{"swap", "Active swap", with(func(d *apischema.StorageBlockDevice) { d.Busy = "Active swap" })},
		{"holder", "Used by /dev/md0", with(func(d *apischema.StorageBlockDevice) { d.Busy = "Used by /dev/md0" })},
		{"fstab", "In /etc/fstab", with(func(d *apischema.StorageBlockDevice) { d.InFstab = true })},
		{"mounted btrfs member", "Member of btrfs pool tank", with(func(d *apischema.StorageBlockDevice) { d.Path = "/dev/sdc1" })},
		{"offline pool member", "Member of btrfs pool o1", with(func(d *apischema.StorageBlockDevice) { d.Path = "/dev/sdd1" })},
	}
	for _, c := range cases {
		err := deviceInUse(c.dev, fss, offline, false, false)
		if !errors.Is(err, bridgeipc.ErrConflict) || !strings.Contains(err.Error(), c.reason) {
			t.Errorf("%s: err = %v, want a conflict naming %q", c.name, err, c.reason)
		}
	}
	if err := deviceInUse(free, fss, offline, false, false); err != nil {
		t.Errorf("free partition: err = %v", err)
	}
	inFstab := with(func(d *apischema.StorageBlockDevice) { d.InFstab = true })
	if err := deviceInUse(inFstab, fss, offline, true, false); err != nil {
		t.Errorf("mount just for now: err = %v, want /etc/fstab ignored", err)
	}
	if err := deviceInUse(inFstab, fss, offline, true, true); !errors.Is(err, bridgeipc.ErrConflict) {
		t.Errorf("mount at boot: err = %v, want a conflict", err)
	}
}

// A disk that dropped out of a mounted pool is still the pool's: the pool
// takes it back when it is mounted again. The kernel keeps its old path for
// the dead block device, and the disk comes back under that name or another,
// before udev probed it or after: what is at the old path, and a disk whose
// own superblock names the pool, are refused while the member is dropped
// out.
func TestDeviceInUseRefusesADiskThatDroppedOutOfAMountedPool(t *testing.T) {
	fss := []btrfs.BtrfsFilesystem{{UUID: "f1", Label: "tank", Devices: []btrfs.BtrfsDevice{
		{ID: 1, Path: "/dev/sdb"},
		{ID: 2, Path: "/dev/sdc", Missing: true, DroppedOut: true},
	}}}
	disk := func(path, fsType, uuid string) apischema.StorageBlockDevice {
		return apischema.StorageBlockDevice{Path: path, Type: "disk", FSType: fsType, UUID: uuid}
	}
	for _, dev := range []apischema.StorageBlockDevice{disk("/dev/sde", "btrfs", "f1"), disk("/dev/sdc", "btrfs", "f1"), disk("/dev/sdc", "", "")} {
		err := deviceInUse(dev, fss, nil, false, false)
		if !errors.Is(err, bridgeipc.ErrConflict) || !strings.Contains(err.Error(), "dropped out of btrfs pool tank") || !strings.Contains(err.Error(), "mounted again") {
			t.Errorf("%s (%q signature): err = %v, want a conflict saying it may have dropped out of tank", dev.Path, dev.FSType, err)
		}
	}
	// A superblock of a pool that lacks no disk is left to each caller's
	// rule for members.
	fss[0].Devices[1] = btrfs.BtrfsDevice{ID: 2, Path: "/dev/sdd"}
	if err := deviceInUse(disk("/dev/sde", "btrfs", "f1"), fss, nil, false, false); err != nil {
		t.Errorf("an old member of a whole pool: err = %v, want it left to the caller", err)
	}
}

func TestCreatePartitionValidatesBeforeWriting(t *testing.T) {
	req := func(edit func(*apischema.PartitionCreateRequest)) apischema.PartitionCreateRequest {
		r := apischema.PartitionCreateRequest{
			Device: "/dev/sdb", StartBytes: 1 << 30, SizeBytes: 512 << 20,
			Format: &apischema.PartitionFormatOptions{FSType: "ext4", Label: "data"},
		}
		edit(&r)
		return r
	}
	same := func(*apischema.PartitionCreateRequest) {}
	cases := []struct {
		name string
		req  apischema.PartitionCreateRequest
		edit func([]apischema.StorageBlockDevice)
		want error
	}{
		{"unknown device", req(func(r *apischema.PartitionCreateRequest) { r.Device = "/dev/sdz" }), nil, bridgeipc.ErrNotFound},
		{"not a disk", req(func(r *apischema.PartitionCreateRequest) { r.Device = "/dev/sdb1" }), nil, bridgeipc.ErrInvalidArgs},
		{"empty drive", req(same), func(d []apischema.StorageBlockDevice) { d[0].SizeBytes = 0 }, bridgeipc.ErrInvalidArgs},
		{"read-only disk", req(same), func(d []apischema.StorageBlockDevice) { d[0].ReadOnly = true }, bridgeipc.ErrConflict},
		{"dos table", req(same), func(d []apischema.StorageBlockDevice) { d[0].PTType = "dos" }, bridgeipc.ErrInvalidArgs},
		{"disk in use", req(same), func(d []apischema.StorageBlockDevice) { d[0].Busy = "Used by /dev/md0" }, bridgeipc.ErrConflict},
		{"outside free space", req(func(r *apischema.PartitionCreateRequest) { r.StartBytes = 1 << 20 }), nil, bridgeipc.ErrConflict},
		{"past the region end", req(func(r *apischema.PartitionCreateRequest) { r.SizeBytes = 1<<30 + 1<<20 }), nil, bridgeipc.ErrConflict},
		{"under 1 MiB", req(func(r *apischema.PartitionCreateRequest) { r.SizeBytes = 1<<20 - 1 }), nil, bridgeipc.ErrInvalidArgs},
		{"xfs under 300 MiB", req(func(r *apischema.PartitionCreateRequest) {
			r.SizeBytes, r.Format = 299<<20, &apischema.PartitionFormatOptions{FSType: "xfs"}
		}), nil, bridgeipc.ErrInvalidArgs},
		// 300 MiB from an unaligned start is 299 MiB once rounded inward.
		{"xfs under 300 MiB once aligned", req(func(r *apischema.PartitionCreateRequest) {
			r.StartBytes, r.SizeBytes, r.Format = 1<<30+1, 300<<20, &apischema.PartitionFormatOptions{FSType: "xfs"}
		}), nil, bridgeipc.ErrInvalidArgs},
		{"label too long for ext4", req(func(r *apischema.PartitionCreateRequest) {
			r.Format = &apischema.PartitionFormatOptions{FSType: "ext4", Label: "seventeen-bytes!!"}
		}), nil, bridgeipc.ErrInvalidArgs},
		{"mount without format", req(func(r *apischema.PartitionCreateRequest) {
			r.Format, r.Mount = nil, &apischema.PartitionMountOptions{Mountpoint: "/srv/new"}
		}), nil, bridgeipc.ErrInvalidArgs},
		{"system mountpoint", req(func(r *apischema.PartitionCreateRequest) {
			r.Mount = &apischema.PartitionMountOptions{Mountpoint: "/etc"}
		}), nil, bridgeipc.ErrInvalidArgs},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := stubPartitions(t, "")
			if c.edit != nil {
				c.edit(env.devices)
			}
			_, err := CreatePartition(context.Background(), c.req, func(string, float64) {})
			if !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
			if len(env.calls) != 0 {
				t.Fatalf("calls = %q, want none before validation passes", env.calls)
			}
		})
	}
}

func TestCreatePartitionFormatsAndMounts(t *testing.T) {
	env := stubPartitions(t, "")
	mp := filepath.Join(t.TempDir(), "new")
	var steps []string
	part, err := CreatePartition(context.Background(), apischema.PartitionCreateRequest{
		Device: "/dev/sdb", StartBytes: 1 << 30, SizeBytes: 512 << 20,
		Format: &apischema.PartitionFormatOptions{FSType: "ext4", Label: "data"},
		Mount:  &apischema.PartitionMountOptions{Mountpoint: mp, Persist: true},
	}, func(step string, _ float64) { steps = append(steps, step) })
	if err != nil || part != "/dev/sdb3" {
		t.Fatalf("part = %q, err = %v", part, err)
	}
	wantSteps := []string{"Partitioning", "Trimming before formatting: 50% complete", "Formatting", "Mounting at " + mp}
	if !slices.Equal(steps, wantSteps) {
		t.Errorf("steps = %q, want %q", steps, wantSteps)
	}
	// The disk lock is taken once, before the first write, and held to the end.
	wantCalls := []string{
		"lock /dev/sdb",
		"create /dev/sdb 1073741824 536870912",
		`format /dev/sdb3 ext4 "data" overwrite=false`,
		fmt.Sprintf(`mount /dev/sdb3 %s ext4 0 ""`, mp),
		"unlock /dev/sdb",
	}
	if !slices.Equal(env.calls, wantCalls) {
		t.Errorf("calls = %q, want %q", env.calls, wantCalls)
	}
	if got, want := readFstab(t), "UUID=new-uuid "+mp+" ext4 defaults,nofail 0 2\n"; got != want {
		t.Errorf("fstab = %q, want %q", got, want)
	}
	if info, err := os.Stat(mp); err != nil || !info.IsDir() {
		t.Errorf("mountpoint was not created: %v", err)
	}
}

func TestCreatePartitionRawAndFailedSteps(t *testing.T) {
	env := stubPartitions(t, "")
	var steps []string
	req := apischema.PartitionCreateRequest{Device: "/dev/sdb", StartBytes: 1 << 30, SizeBytes: 1 << 30}
	if _, err := CreatePartition(context.Background(), req, func(step string, _ float64) { steps = append(steps, step) }); err != nil {
		t.Fatal(err)
	}
	if want := []string{"lock /dev/sdb", "create /dev/sdb 1073741824 1073741824", "unlock /dev/sdb"}; !slices.Equal(env.calls, want) {
		t.Errorf("calls = %q, want %q", env.calls, want)
	}
	if !slices.Equal(steps, []string{"Partitioning"}) {
		t.Errorf("steps = %q", steps)
	}

	env = stubPartitions(t, "")
	env.formatErr = errors.New("mkfs.xfs -m uuid=new-uuid: boom")
	req.Format = &apischema.PartitionFormatOptions{FSType: "xfs"}
	_, err := CreatePartition(context.Background(), req, func(string, float64) {})
	if err == nil || !strings.Contains(err.Error(), "partition /dev/sdb3 was created but not formatted") {
		t.Fatalf("err = %v", err)
	}
	if last := env.calls[len(env.calls)-1]; last != "unlock /dev/sdb" {
		t.Errorf("last call = %q, want the disk unlocked", last)
	}

	// The mount worked, so the error must not claim it did not.
	stubPartitions(t, "")
	fstabPath = filepath.Join(t.TempDir(), "missing")
	mp := t.TempDir()
	req.Mount = &apischema.PartitionMountOptions{Mountpoint: mp, Persist: true}
	_, err = CreatePartition(context.Background(), req, func(string, float64) {})
	if want := "/dev/sdb3 is mounted at " + mp + " but could not be added to /etc/fstab"; err == nil || !strings.HasPrefix(err.Error(), want) {
		t.Fatalf("err = %v, want it to start with %q", err, want)
	}
}

func TestFormatPartition(t *testing.T) {
	ctx := context.Background()
	format := func(device string, overwrite bool) apischema.PartitionFormatRequest {
		return apischema.PartitionFormatRequest{Device: device, FSType: "xfs", Label: "logs", Overwrite: overwrite}
	}
	refusals := []struct {
		name  string
		req   apischema.PartitionFormatRequest
		edit  func(*partitionEnv)
		want  error
		calls []string
	}{
		{"too small for xfs", format("/dev/sdb2", false), func(e *partitionEnv) { e.devices[2].SizeBytes = 299 << 20 }, bridgeipc.ErrInvalidArgs, nil},
		{"signature without overwrite", format("/dev/sdb1", false), func(*partitionEnv) {}, bridgeipc.ErrConflict, nil},
		{"in use", format("/dev/sdb1", true), func(e *partitionEnv) { e.devices[1].Busy = "Mounted at /x" }, bridgeipc.ErrConflict, nil},
		{"kernel finds it busy", format("/dev/sdb2", false), func(e *partitionEnv) {
			e.busyErr = fmt.Errorf("/dev/sdb2 is in use: %w", unix.EBUSY)
		}, bridgeipc.ErrConflict, []string{"lock /dev/sdb", "check /dev/sdb2", "unlock /dev/sdb"}},
		{"data udev did not report", format("/dev/sdb2", false), func(e *partitionEnv) {
			e.formatErr = fmt.Errorf("/dev/sdb2: %w; overwrite to replace it", btrfs.ErrDeviceHasData)
		}, bridgeipc.ErrConflict, []string{"lock /dev/sdb", "check /dev/sdb2", `format /dev/sdb2 xfs "logs" overwrite=false`, "unlock /dev/sdb"}},
	}
	for _, c := range refusals {
		t.Run(c.name, func(t *testing.T) {
			env := stubPartitions(t, "")
			c.edit(env)
			if err := FormatPartition(ctx, c.req, func(string, float64) {}); !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
			if !slices.Equal(env.calls, c.calls) {
				t.Fatalf("calls = %q, want %q", env.calls, c.calls)
			}
		})
	}

	t.Run("overwrite", func(t *testing.T) {
		env := stubPartitions(t, "")
		env.devices[1].SizeBytes = 300 << 20 // exactly the xfs minimum
		var steps []string
		if err := FormatPartition(ctx, format("/dev/sdb1", true), func(step string, _ float64) { steps = append(steps, step) }); err != nil {
			t.Fatal(err)
		}
		// The O_EXCL check runs under the lock, right before mkfs, never before
		// a wait for the lock that can outlast another format.
		want := []string{"lock /dev/sdb", "check /dev/sdb1", `format /dev/sdb1 xfs "logs" overwrite=true`, "unlock /dev/sdb"}
		if !slices.Equal(env.calls, want) {
			t.Errorf("calls = %q, want %q", env.calls, want)
		}
		if !slices.Equal(steps, []string{"Trimming before formatting: 50% complete", "Formatting"}) {
			t.Errorf("steps = %q", steps)
		}
	})
}

func TestDeletePartition(t *testing.T) {
	ctx := context.Background()
	sdb1 := apischema.PartitionDeleteRequest{Device: "/dev/sdb1"}

	env := stubPartitions(t, "")
	if _, err := DeletePartition(ctx, sdb1); err != nil {
		t.Fatal(err)
	}
	if want := []string{"lock /dev/sdb", "check /dev/sdb1", "delete /dev/sdb 1", "unlock /dev/sdb"}; !slices.Equal(env.calls, want) {
		t.Errorf("calls = %q, want %q", env.calls, want)
	}

	env = stubPartitions(t, "")
	env.deleteErr = fmt.Errorf("delete partition 1 from /dev/sdb: %w", unix.EBUSY)
	if _, err := DeletePartition(ctx, sdb1); !errors.Is(err, bridgeipc.ErrConflict) {
		t.Errorf("busy: err = %v, want a conflict", err)
	}

	env = stubPartitions(t, "")
	env.deleteErr = fmt.Errorf("/dev/sdb: %w", btrfs.ErrStaleTable)
	if _, err := DeletePartition(ctx, sdb1); !errors.Is(err, bridgeipc.ErrConflict) {
		t.Errorf("stale kernel table: err = %v, want a conflict", err)
	}

	env = stubPartitions(t, "")
	env.devices[0].PTType = "dos"
	if _, err := DeletePartition(ctx, sdb1); !errors.Is(err, bridgeipc.ErrInvalidArgs) || len(env.calls) != 0 {
		t.Errorf("dos disk: err = %v, calls = %q", err, env.calls)
	}

	env = stubPartitions(t, "")
	env.devices[1].InFstab = true
	if _, err := DeletePartition(ctx, sdb1); !errors.Is(err, bridgeipc.ErrConflict) || len(env.calls) != 0 {
		t.Errorf("in fstab: err = %v, calls = %q", err, env.calls)
	}
}

func TestCallsGiveUpOnALockedDisk(t *testing.T) {
	env := stubPartitions(t, "")
	orig := callLockWait
	t.Cleanup(func() { callLockWait = orig })
	callLockWait = 10 * time.Millisecond
	lockDisk = func(ctx context.Context, device string) (func(), error) {
		<-ctx.Done() // another operation holds the disk for longer than the wait
		return nil, fmt.Errorf("lock %s: %w", device, ctx.Err())
	}
	// The request itself would wait a second: the calls must give up first.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := DeletePartition(ctx, apischema.PartitionDeleteRequest{Device: "/dev/sdb1"})
	if !errors.Is(err, bridgeipc.ErrConflict) || !strings.Contains(err.Error(), "another operation is running on /dev/sdb") {
		t.Fatalf("delete: err = %v, want a conflict naming the disk", err)
	}
	_, err = InitializeDisk(ctx, apischema.DiskInitializeRequest{Device: "/dev/sdb"})
	if !errors.Is(err, bridgeipc.ErrConflict) {
		t.Fatalf("initialize: err = %v, want a conflict", err)
	}
	if len(env.calls) != 0 {
		t.Fatalf("calls = %q, want no write", env.calls)
	}

	// A request that ended on its own keeps its own error.
	ended, end := context.WithCancel(context.Background())
	end()
	if _, err = DeletePartition(ended, apischema.PartitionDeleteRequest{Device: "/dev/sdb1"}); !errors.Is(err, context.Canceled) || errors.Is(err, bridgeipc.ErrConflict) {
		t.Fatalf("canceled request: err = %v, want context.Canceled unchanged", err)
	}
}

func TestMountPartition(t *testing.T) {
	ctx := context.Background()
	t.Run("at boot", func(t *testing.T) {
		env := stubPartitions(t, "")
		mp := t.TempDir()
		res, err := MountPartition(ctx, apischema.PartitionMountRequest{Device: "/dev/sdb1", Mountpoint: mp, Persist: true})
		if err != nil || !res.Success {
			t.Fatalf("res = %+v, err = %v", res, err)
		}
		if want := []string{"check /dev/sdb1", fmt.Sprintf(`mount /dev/sdb1 %s ext4 0 ""`, mp)}; !slices.Equal(env.calls, want) {
			t.Errorf("calls = %q, want %q", env.calls, want)
		}
		if got, want := readFstab(t), "UUID=u1 "+mp+" ext4 defaults,nofail 0 2\n"; got != want {
			t.Errorf("fstab = %q, want %q", got, want)
		}
	})
	t.Run("just for now leaves fstab", func(t *testing.T) {
		const fstab = "UUID=u1 /srv/old ext4 defaults,nofail 0 2\n"
		env := stubPartitions(t, fstab)
		env.devices[1].InFstab = true
		if _, err := MountPartition(ctx, apischema.PartitionMountRequest{Device: "/dev/sdb1", Mountpoint: t.TempDir()}); err != nil {
			t.Fatal(err)
		}
		if got := readFstab(t); got != fstab {
			t.Errorf("fstab = %q, want it untouched", got)
		}
	})

	refusals := []struct {
		name    string
		persist bool
		edit    func(t *testing.T, e *partitionEnv, mp string)
		want    error
	}{
		{"not a partition", false, func(_ *testing.T, e *partitionEnv, _ string) { e.devices[1].Type = "disk" }, bridgeipc.ErrInvalidArgs},
		{"swap", false, func(_ *testing.T, e *partitionEnv, _ string) { e.devices[1].FSType = "swap" }, bridgeipc.ErrInvalidArgs},
		{"LUKS container", false, func(_ *testing.T, e *partitionEnv, _ string) { e.devices[1].FSType = "crypto_LUKS" }, bridgeipc.ErrInvalidArgs},
		{"at boot when in fstab", true, func(_ *testing.T, e *partitionEnv, _ string) { e.devices[1].InFstab = true }, bridgeipc.ErrConflict},
		{"offline pool member", false, func(_ *testing.T, e *partitionEnv, _ string) {
			e.offline = []btrfs.BtrfsOfflinePool{{UUID: "o1", Devices: []string{"/dev/sdb1"}}}
		}, bridgeipc.ErrConflict},
		{"no uuid for fstab", true, func(_ *testing.T, e *partitionEnv, _ string) { e.devices[1].UUID = "" }, bridgeipc.ErrInvalidArgs},
		{"mountpoint not empty", false, func(t *testing.T, _ *partitionEnv, mp string) {
			if err := os.WriteFile(filepath.Join(mp, "keep"), nil, 0o644); err != nil {
				t.Fatal(err)
			}
		}, bridgeipc.ErrConflict},
		{"kernel finds it busy", false, func(_ *testing.T, e *partitionEnv, _ string) {
			e.busyErr = fmt.Errorf("/dev/sdb1 is in use: %w", unix.EBUSY)
		}, bridgeipc.ErrConflict},
	}
	for _, c := range refusals {
		t.Run(c.name, func(t *testing.T) {
			env := stubPartitions(t, "")
			mp := t.TempDir()
			c.edit(t, env, mp)
			_, err := MountPartition(ctx, apischema.PartitionMountRequest{Device: "/dev/sdb1", Mountpoint: mp, Persist: c.persist})
			if !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
			if slices.ContainsFunc(env.calls, func(call string) bool { return !strings.HasPrefix(call, "check ") }) {
				t.Fatalf("calls = %q, want no lock and no mount", env.calls)
			}
		})
	}
}

func TestMountPartitionBusyIsAConflict(t *testing.T) {
	env := stubPartitions(t, "")
	env.mountErr = unix.EBUSY // mount(2) itself finds the device busy
	_, err := MountPartition(context.Background(), apischema.PartitionMountRequest{Device: "/dev/sdb1", Mountpoint: t.TempDir()})
	if !errors.Is(err, bridgeipc.ErrConflict) {
		t.Fatalf("err = %v, want a conflict", err)
	}
}

func TestInitializeDisk(t *testing.T) {
	ctx := context.Background()
	sdb := apischema.DiskInitializeRequest{Device: "/dev/sdb"}

	env := stubPartitions(t, "")
	env.devices[2].Busy = "Mounted at /srv"
	if _, err := InitializeDisk(ctx, sdb); !errors.Is(err, bridgeipc.ErrConflict) || len(env.calls) != 0 {
		t.Fatalf("mounted partition: err = %v, calls = %q", err, env.calls)
	}

	env = stubPartitions(t, "")
	if _, err := InitializeDisk(ctx, sdb); err != nil {
		t.Fatal(err)
	}
	if want := []string{"lock /dev/sdb", "initialize /dev/sdb", "unlock /dev/sdb"}; !slices.Equal(env.calls, want) {
		t.Errorf("calls = %q, want %q", env.calls, want)
	}
}

// TestInitializeDiskConfirmsMembers covers a disk whose partitions belong to
// a stopped md array and an inactive LVM volume group: nothing holds them,
// so only the confirmation stands between them and the wipe.
func TestInitializeDiskConfirmsMembers(t *testing.T) {
	ctx := context.Background()
	md := apischema.DiskMemberConfirmed{Device: "/dev/sdb1", Kind: "md", UUID: "a1"}
	lvm := apischema.DiskMemberConfirmed{Device: "/dev/sdb2", Kind: "lvm", UUID: "pv2"}
	members := func(env *partitionEnv) {
		env.devices[1].FSType, env.devices[1].UUID, env.devices[1].Label = "linux_raid_member", "a1", "host:data"
		env.devices[2].FSType, env.devices[2].UUID = "LVM2_member", "pv2"
	}
	for _, c := range []struct {
		name    string
		confirm []apischema.DiskMemberConfirmed
		want    string
	}{
		{"no confirmation", nil, "Member of md array host:data; confirm erasing it"},
		{"one of two", []apischema.DiskMemberConfirmed{md}, "Member of an LVM volume group (physical volume pv2)"},
		{"another array", []apischema.DiskMemberConfirmed{{Device: "/dev/sdb1", Kind: "md", UUID: "b9"}, lvm}, "Member of md array host:data"},
		{"a member that is gone", []apischema.DiskMemberConfirmed{md, lvm, {Device: "/dev/sdb3", Kind: "md", UUID: "c3"}}, "no longer the md member"},
	} {
		env := stubPartitions(t, "")
		members(env)
		_, err := InitializeDisk(ctx, apischema.DiskInitializeRequest{Device: "/dev/sdb", ConfirmMembers: c.confirm})
		if !errors.Is(err, bridgeipc.ErrConflict) || !strings.Contains(err.Error(), c.want) || len(env.calls) != 0 {
			t.Errorf("%s: err = %v, calls = %q, want a conflict naming %q and no write", c.name, err, env.calls, c.want)
		}
	}

	env := stubPartitions(t, "")
	members(env)
	if _, err := InitializeDisk(ctx, apischema.DiskInitializeRequest{Device: "/dev/sdb", ConfirmMembers: []apischema.DiskMemberConfirmed{lvm, md}}); err != nil {
		t.Fatalf("both confirmed: %v", err)
	}
	if want := []string{"lock /dev/sdb", "initialize /dev/sdb", "unlock /dev/sdb"}; !slices.Equal(env.calls, want) {
		t.Errorf("calls = %q, want %q", env.calls, want)
	}

	// A btrfs signature that belongs to no pool the bridge can read cannot be
	// destroyed as a pool, so it is erased only when named, like the rest.
	stray := apischema.DiskMemberConfirmed{Device: "/dev/sdb1", Kind: "btrfs", UUID: "s1"}
	for _, confirm := range [][]apischema.DiskMemberConfirmed{nil, {stray}} {
		env = stubPartitions(t, "")
		env.devices[1].FSType, env.devices[1].UUID = "btrfs", "s1"
		_, strayErr := InitializeDisk(ctx, apischema.DiskInitializeRequest{Device: "/dev/sdb", ConfirmMembers: confirm})
		if confirm == nil && (!errors.Is(strayErr, bridgeipc.ErrConflict) || !strings.Contains(strayErr.Error(), "Member of btrfs pool s1; confirm erasing it") || len(env.calls) != 0) {
			t.Errorf("stray btrfs signature, not confirmed: err = %v, calls = %q", strayErr, env.calls)
		}
		if confirm != nil && strayErr != nil {
			t.Errorf("stray btrfs signature, confirmed: %v", strayErr)
		}
	}

	// A btrfs pool member stays refused, confirmed or not.
	env = stubPartitions(t, "")
	env.devices[1].FSType, env.devices[1].UUID = "btrfs", "p1"
	env.offline = []btrfs.BtrfsOfflinePool{{UUID: "p1", Label: "old", Devices: []string{"/dev/sdb1"}}}
	confirm := []apischema.DiskMemberConfirmed{{Device: "/dev/sdb1", Kind: "btrfs", UUID: "p1"}}
	_, err := InitializeDisk(ctx, apischema.DiskInitializeRequest{Device: "/dev/sdb", ConfirmMembers: confirm})
	if !errors.Is(err, bridgeipc.ErrConflict) || !strings.Contains(err.Error(), "Member of btrfs pool old; destroy the pool") || len(env.calls) != 0 {
		t.Errorf("btrfs member: err = %v, calls = %q", err, env.calls)
	}
}

// memberAction is Format or Delete of /dev/sdb1 with a confirmation, the
// calls it makes when it goes through and what its refusal asks for.
type memberAction struct {
	name   string
	run    func(confirm []apischema.DiskMemberConfirmed) error
	writes []string
	what   string
}

// memberCase is a member signature put on /dev/sdb1 and how it is named.
type memberCase struct {
	name   string
	set    func(*apischema.StorageBlockDevice)
	member apischema.DiskMemberConfirmed
	names  string
}

// TestFormatAndDeleteConfirmMembers covers Format and Delete of a partition
// that belongs to a stopped md array or an inactive LVM volume group, which
// nothing holds: the request must confirm exactly that member, and a member
// of a btrfs pool the bridge knows stays refused, confirmed or not.
func TestFormatAndDeleteConfirmMembers(t *testing.T) {
	ctx := context.Background()
	actions := []memberAction{
		{
			name: "format",
			run: func(confirm []apischema.DiskMemberConfirmed) error {
				return FormatPartition(ctx, apischema.PartitionFormatRequest{Device: "/dev/sdb1", FSType: "ext4", Overwrite: true, ConfirmMembers: confirm}, func(string, float64) {})
			},
			writes: []string{"lock /dev/sdb", "check /dev/sdb1", `format /dev/sdb1 ext4 "" overwrite=true`, "unlock /dev/sdb"},
			what:   "confirm erasing it to format /dev/sdb1",
		},
		{
			name: "delete",
			run: func(confirm []apischema.DiskMemberConfirmed) error {
				_, err := DeletePartition(ctx, apischema.PartitionDeleteRequest{Device: "/dev/sdb1", ConfirmMembers: confirm})
				return err
			},
			writes: []string{"lock /dev/sdb", "check /dev/sdb1", "delete /dev/sdb 1", "unlock /dev/sdb"},
			what:   "confirm deleting it",
		},
	}
	members := []memberCase{
		{"md", func(d *apischema.StorageBlockDevice) {
			d.FSType, d.UUID, d.Label = "linux_raid_member", "a1", "host:data"
		}, apischema.DiskMemberConfirmed{Device: "/dev/sdb1", Kind: "md", UUID: "a1"}, "Member of md array host:data"},
		{"lvm", func(d *apischema.StorageBlockDevice) {
			d.FSType, d.UUID = "LVM2_member", "pv1"
		}, apischema.DiskMemberConfirmed{Device: "/dev/sdb1", Kind: "lvm", UUID: "pv1"}, "Member of an LVM volume group (physical volume pv1)"},
	}
	for _, a := range actions {
		for _, m := range members {
			checkMemberConfirmation(t, a, m)
		}
		t.Run(a.name+" a confirmation of a device that is no member", func(t *testing.T) {
			env := stubPartitions(t, "")
			err := a.run([]apischema.DiskMemberConfirmed{{Device: "/dev/sdb1", Kind: "md", UUID: "a1"}})
			if !errors.Is(err, bridgeipc.ErrConflict) || !strings.Contains(err.Error(), "no longer the md member") || len(env.calls) != 0 {
				t.Fatalf("err = %v, calls = %q", err, env.calls)
			}
		})
		t.Run(a.name+" btrfs pool member", func(t *testing.T) {
			env := stubPartitions(t, "")
			env.devices[1].FSType, env.devices[1].UUID = "btrfs", "p1"
			env.offline = []btrfs.BtrfsOfflinePool{{UUID: "p1", Label: "old", Devices: []string{"/dev/sdb1"}}}
			err := a.run([]apischema.DiskMemberConfirmed{{Device: "/dev/sdb1", Kind: "btrfs", UUID: "p1"}})
			if !errors.Is(err, bridgeipc.ErrConflict) || !strings.Contains(err.Error(), "Member of btrfs pool old; destroy the pool") || len(env.calls) != 0 {
				t.Fatalf("err = %v, calls = %q", err, env.calls)
			}
		})
	}
}

// checkMemberConfirmation runs a on /dev/sdb1 carrying m: refused with
// nothing written without the confirmation, with another member's, or with
// one more than it finds, and carried out with exactly m's.
func checkMemberConfirmation(t *testing.T, a memberAction, m memberCase) {
	wrong := m.member
	wrong.UUID = "other"
	for _, c := range []struct {
		name    string
		confirm []apischema.DiskMemberConfirmed
		want    string
	}{
		{"no confirmation", nil, m.names + "; " + a.what},
		{"another member", []apischema.DiskMemberConfirmed{wrong}, m.names + "; " + a.what},
		{"another device too", []apischema.DiskMemberConfirmed{m.member, {Device: "/dev/sdb2", Kind: "md", UUID: "a1"}}, "/dev/sdb2 is no longer the md member"},
	} {
		t.Run(a.name+" "+m.name+" "+c.name, func(t *testing.T) {
			env := stubPartitions(t, "")
			m.set(&env.devices[1])
			err := a.run(c.confirm)
			if !errors.Is(err, bridgeipc.ErrConflict) || !strings.Contains(err.Error(), c.want) || len(env.calls) != 0 {
				t.Fatalf("err = %v, calls = %q, want a conflict naming %q and nothing written", err, env.calls, c.want)
			}
		})
	}
	t.Run(a.name+" "+m.name+" confirmed", func(t *testing.T) {
		env := stubPartitions(t, "")
		m.set(&env.devices[1])
		if err := a.run([]apischema.DiskMemberConfirmed{m.member}); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(env.calls, a.writes) {
			t.Fatalf("calls = %q, want %q", env.calls, a.writes)
		}
	})
}

// memberWrite reports whether call wrote to a device.
func memberWrite(call string) bool {
	return strings.HasPrefix(call, "format ") || strings.HasPrefix(call, "delete ") || strings.HasPrefix(call, "initialize ")
}

// TestMembersAreCheckedAgainUnderTheLock covers Format, Delete and
// Initialize of /dev/sdb1 when it became a member of an md array after the
// request read the inventory: while the request waited for the disk lock
// with udev seeing it, or with udev not having probed it at all, which only
// the device's own signature shows. Neither is erased without a
// confirmation.
func TestMembersAreCheckedAgainUnderTheLock(t *testing.T) {
	ctx := context.Background()
	actions := []struct {
		name, what string
		run        func(confirm []apischema.DiskMemberConfirmed) error
	}{
		{"format", "confirm erasing it to format /dev/sdb1", func(confirm []apischema.DiskMemberConfirmed) error {
			return FormatPartition(ctx, apischema.PartitionFormatRequest{Device: "/dev/sdb1", FSType: "ext4", Overwrite: true, ConfirmMembers: confirm}, func(string, float64) {})
		}},
		{"delete", "confirm deleting it", func(confirm []apischema.DiskMemberConfirmed) error {
			_, err := DeletePartition(ctx, apischema.PartitionDeleteRequest{Device: "/dev/sdb1", ConfirmMembers: confirm})
			return err
		}},
		{"initialize", "confirm erasing it to initialize /dev/sdb", func(confirm []apischema.DiskMemberConfirmed) error {
			_, err := InitializeDisk(ctx, apischema.DiskInitializeRequest{Device: "/dev/sdb", ConfirmMembers: confirm})
			return err
		}},
	}
	refused := func(t *testing.T, env *partitionEnv, err error, want string) {
		t.Helper()
		if !errors.Is(err, bridgeipc.ErrConflict) || !strings.Contains(err.Error(), want) || slices.ContainsFunc(env.calls, memberWrite) {
			t.Fatalf("err = %v, calls = %q; want a conflict naming %q and nothing written", err, env.calls, want)
		}
	}
	for _, a := range actions {
		t.Run(a.name+" while waiting for the lock", func(t *testing.T) {
			env := stubPartitions(t, "")
			lock := lockDisk
			lockDisk = func(ctx context.Context, device string) (func(), error) {
				env.devices[1].FSType, env.devices[1].UUID, env.devices[1].Label = "linux_raid_member", "a1", "host:data"
				return lock(ctx, device)
			}
			refused(t, env, a.run(nil), "/dev/sdb1: Member of md array host:data; "+a.what)
		})
		t.Run(a.name+" udev has not probed", func(t *testing.T) {
			env := stubPartitions(t, "")
			env.signatures = map[string]btrfs.MemberSignature{"/dev/sdb1": {Device: "/dev/sdb1", Kind: "md", UUID: "a9"}}
			refused(t, env, a.run(nil), "/dev/sdb1: Member of md array a9; "+a.what)
		})
		t.Run(a.name+" another array since the confirmation", func(t *testing.T) {
			env := stubPartitions(t, "")
			env.devices[1].FSType, env.devices[1].UUID = "linux_raid_member", "a1"
			env.signatures = map[string]btrfs.MemberSignature{"/dev/sdb1": {Device: "/dev/sdb1", Kind: "md", UUID: "a9"}}
			refused(t, env, a.run([]apischema.DiskMemberConfirmed{{Device: "/dev/sdb1", Kind: "md", UUID: "a1"}}), "/dev/sdb1: Member of md array a9; "+a.what)
		})
		// A signature udev reported that the read does not find is kept, so
		// the confirmation that named it still holds.
		t.Run(a.name+" a confirmed member the read misses", func(t *testing.T) {
			env := stubPartitions(t, "")
			env.devices[1].FSType, env.devices[1].UUID = "LVM2_member", "pv1"
			if err := a.run([]apischema.DiskMemberConfirmed{{Device: "/dev/sdb1", Kind: "lvm", UUID: "pv1"}}); err != nil {
				t.Fatal(err)
			}
			if !slices.ContainsFunc(env.calls, memberWrite) {
				t.Fatalf("calls = %q, want the write", env.calls)
			}
		})
	}
}

// TestWholeDeviceChecksReadTheSignature covers md add and replace, btrfs
// add and replace, and pool create: an md or LVM2 signature on the device
// that udev has not reported refuses it as a member, as one udev reported
// would.
func TestWholeDeviceChecksReadTheSignature(t *testing.T) {
	ctx := context.Background()
	stubMdadm(t, []apischema.StorageBlockDevice{{Path: "/dev/sdz", Type: "disk", Mountpoints: []string{}}})
	stubSignatures(t, map[string]btrfs.MemberSignature{"/dev/sdz": {Device: "/dev/sdz", Kind: "lvm", UUID: "pv9"}})
	const want = "/dev/sdz: Member of an LVM volume group (physical volume pv9)"
	for name, err := range map[string]error{
		"md":    mdDeviceFree(ctx, "/dev/sdz"),
		"btrfs": ensureBtrfsDeviceFree(ctx, "/dev/sdz", true, ""),
	} {
		if !errors.Is(err, bridgeipc.ErrConflict) || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want a conflict naming %q", name, err, want)
		}
	}

	env := stubPoolCreate(t)
	stubSignatures(t, map[string]btrfs.MemberSignature{"/dev/sdc": {Device: "/dev/sdc", Kind: "md", UUID: "a9"}})
	err := canceledCreate(t, []string{"/dev/sdb", "/dev/sdc"}, nil)
	if !errors.Is(err, bridgeipc.ErrConflict) || !strings.Contains(err.Error(), "/dev/sdc: Member of md array a9") || len(env.calls) != 0 {
		t.Fatalf("pool create: err = %v, calls = %q; want a conflict and nothing written", err, env.calls)
	}
}

// TestALegLeftOutOfAPoolIsStillRefused covers /dev/sdb1, a leg of an md
// array whose metadata sits at the end, so it shows the btrfs superblock of
// the pool on the array: the pool listing leaves it out (btrfs
// partOfAnother), so no write refuses it as a disk of that pool. Every write
// still refuses it, as held while the array runs, and as an md member while
// the array is stopped or udev has not probed it: Format, Delete, Initialize
// of its disk, Mount, md add and replace, and btrfs add, replace and pool
// create, whose device check ensureBtrfsDeviceFree is. As for any member of
// a stopped array, Format, Delete and Initialize erase it once the request
// confirms that md member (TestFormatAndDeleteConfirmMembers); the
// confirmation names the array, not the pool on it.
func TestALegLeftOutOfAPoolIsStillRefused(t *testing.T) {
	ctx := context.Background()
	writes := []struct {
		name string
		run  func() error
	}{
		{"format", func() error {
			return FormatPartition(ctx, apischema.PartitionFormatRequest{Device: "/dev/sdb1", FSType: "ext4", Overwrite: true}, func(string, float64) {})
		}},
		{"delete", func() error {
			_, err := DeletePartition(ctx, apischema.PartitionDeleteRequest{Device: "/dev/sdb1"})
			return err
		}},
		{"initialize", func() error {
			_, err := InitializeDisk(ctx, apischema.DiskInitializeRequest{Device: "/dev/sdb"})
			return err
		}},
		{"mount", func() error {
			_, err := MountPartition(ctx, apischema.PartitionMountRequest{Device: "/dev/sdb1", Mountpoint: filepath.Join(t.TempDir(), "leg")})
			return err
		}},
		{"md add or replace", func() error { return mdDeviceFree(ctx, "/dev/sdb1") }},
		{"btrfs add, replace or pool create", func() error { return ensureBtrfsDeviceFree(ctx, "/dev/sdb1", true, "") }},
	}
	legs := []struct {
		name, want string
		set        func(env *partitionEnv)
	}{
		{"held by the running array", "Used by /dev/md127", func(env *partitionEnv) {
			env.devices[1].FSType, env.devices[1].UUID, env.devices[1].Busy = "linux_raid_member", "a1", "Used by /dev/md127"
		}},
		{"of a stopped array", "md array", func(env *partitionEnv) {
			env.devices[1].FSType, env.devices[1].UUID = "linux_raid_member", "a1"
		}},
		{"udev has not probed", "md array a9", func(env *partitionEnv) {
			env.devices[1].FSType, env.devices[1].UUID = "", ""
			env.signatures = map[string]btrfs.MemberSignature{"/dev/sdb1": {Device: "/dev/sdb1", Kind: "md", UUID: "a9"}}
		}},
	}
	for _, leg := range legs {
		for _, w := range writes {
			t.Run(w.name+" a leg "+leg.name, func(t *testing.T) {
				env := stubPartitions(t, "")
				env.offline = []btrfs.BtrfsOfflinePool{{UUID: "p1", Label: "data", NumDevices: 2, Devices: []string{"/dev/md127"}, Missing: 1}}
				leg.set(env)
				err := w.run()
				if err == nil || slices.ContainsFunc(env.calls, func(c string) bool { return memberWrite(c) || strings.HasPrefix(c, "mount ") }) {
					t.Fatalf("err = %v, calls = %q; want a refusal and nothing written", err, env.calls)
				}
				// Mount refuses what is no filesystem before it looks at use.
				if w.name != "mount" && (!errors.Is(err, bridgeipc.ErrConflict) || !strings.Contains(err.Error(), leg.want)) {
					t.Fatalf("err = %v, want a conflict naming %q", err, leg.want)
				}
			})
		}
	}
}

// TestAMemberOfAnotherKindStays covers a device udev reports as a member of
// a firmware RAID that also carries an LVM2 label: libblkid stops at the
// first RAID signature, so the outer set is its membership, and the read
// does not replace it. Its own confirmation erases it; none refuses it.
func TestAMemberOfAnotherKindStays(t *testing.T) {
	ctx := context.Background()
	isw := apischema.DiskMemberConfirmed{Device: "/dev/sdb1", Kind: "other", UUID: "f1"}
	run := func(confirm []apischema.DiskMemberConfirmed) (*partitionEnv, error) {
		env := stubPartitions(t, "")
		env.devices[1].FSType, env.devices[1].UUID = "isw_raid_member", "f1"
		env.signatures = map[string]btrfs.MemberSignature{"/dev/sdb1": {Device: "/dev/sdb1", Kind: "lvm", UUID: "pv9"}}
		err := FormatPartition(ctx, apischema.PartitionFormatRequest{Device: "/dev/sdb1", FSType: "ext4", Overwrite: true, ConfirmMembers: confirm}, func(string, float64) {})
		return env, err
	}
	if env, err := run([]apischema.DiskMemberConfirmed{isw}); err != nil || !slices.ContainsFunc(env.calls, memberWrite) {
		t.Fatalf("confirmed: err = %v, calls = %q; want the format", err, env.calls)
	}
	if env, err := run(nil); !errors.Is(err, bridgeipc.ErrConflict) || !strings.Contains(err.Error(), "Member of another storage set (isw_raid_member)") || slices.ContainsFunc(env.calls, memberWrite) {
		t.Fatalf("not confirmed: err = %v, calls = %q; want a conflict naming the firmware RAID", err, env.calls)
	}
}

// TestAnMDOrLVMMemberIsReplacedByTheOther covers a partition udev still
// reports as an old LVM physical volume on which mdadm --create has written
// a superblock udev has not probed, the reverse, an md member zeroed and
// made a physical volume, and a stale btrfs signature under a new md
// superblock: the read checks md and LVM2 as libblkid does, before any
// filesystem, so it is the truth, and a confirmation of the old member
// erases nothing.
func TestAnMDOrLVMMemberIsReplacedByTheOther(t *testing.T) {
	for _, c := range []struct {
		fsType, uuid string
		confirm      apischema.DiskMemberConfirmed
		read         btrfs.MemberSignature
		names        string
	}{
		{"LVM2_member", "pv1", apischema.DiskMemberConfirmed{Device: "/dev/sdb1", Kind: "lvm", UUID: "pv1"},
			btrfs.MemberSignature{Device: "/dev/sdb1", Kind: "md", UUID: "a9"}, "/dev/sdb1: Member of md array a9"},
		{"linux_raid_member", "a1", apischema.DiskMemberConfirmed{Device: "/dev/sdb1", Kind: "md", UUID: "a1"},
			btrfs.MemberSignature{Device: "/dev/sdb1", Kind: "lvm", UUID: "pv9"}, "/dev/sdb1: Member of an LVM volume group (physical volume pv9)"},
		// A stale btrfs signature of no pool the bridge knows, then mdadm
		// --create: libblkid reads RAID signatures before filesystems.
		{"btrfs", "s1", apischema.DiskMemberConfirmed{Device: "/dev/sdb1", Kind: "btrfs", UUID: "s1"},
			btrfs.MemberSignature{Device: "/dev/sdb1", Kind: "md", UUID: "a9"}, "/dev/sdb1: Member of md array a9"},
	} {
		env := stubPartitions(t, "")
		env.devices[1].FSType, env.devices[1].UUID = c.fsType, c.uuid
		env.signatures = map[string]btrfs.MemberSignature{"/dev/sdb1": c.read}
		err := FormatPartition(context.Background(), apischema.PartitionFormatRequest{
			Device: "/dev/sdb1", FSType: "ext4", Overwrite: true, ConfirmMembers: []apischema.DiskMemberConfirmed{c.confirm},
		}, func(string, float64) {})
		if !errors.Is(err, bridgeipc.ErrConflict) || !strings.Contains(err.Error(), c.names) || slices.ContainsFunc(env.calls, memberWrite) {
			t.Fatalf("udev %s, read %s: err = %v, calls = %q; want a conflict naming %q and nothing written", c.fsType, c.read.Kind, err, env.calls, c.names)
		}
	}
}

func TestWithSignaturesRefusesADeviceItDoesNotList(t *testing.T) {
	devs := partitionDevices()
	_, err := withSignatures(devs, []btrfs.MemberSignature{{Device: "/dev/sdb3", Kind: "md", UUID: "a9"}})
	if !errors.Is(err, bridgeipc.ErrConflict) || !strings.Contains(err.Error(), "/dev/sdb3 appeared after the disk was read") {
		t.Fatalf("err = %v, want a conflict naming /dev/sdb3", err)
	}
}

func TestSignatureFreeBusyIsAConflict(t *testing.T) {
	orig := readMemberSignature
	t.Cleanup(func() { readMemberSignature = orig })
	readMemberSignature = func(device string) (btrfs.MemberSignature, error) {
		return btrfs.MemberSignature{}, fmt.Errorf("%s is in use: %w", device, unix.EBUSY)
	}
	if err := signatureFree("/dev/sdz"); !errors.Is(err, bridgeipc.ErrConflict) || !errors.Is(err, unix.EBUSY) {
		t.Fatalf("err = %v, want a conflict", err)
	}
}

// TestRefusalsAskUdevToProbe covers a Format or Delete refused under the lock
// while the partition's own signature showed a member: the dialog confirms
// only what udev reports, so once the lock is released the bridge asks udev
// to probe the partition again. Nothing else asks.
func TestRefusalsAskUdevToProbe(t *testing.T) {
	ctx := context.Background()
	runs := map[string]func() error{
		"format": func() error {
			return FormatPartition(ctx, apischema.PartitionFormatRequest{Device: "/dev/sdb1", FSType: "ext4", Overwrite: true}, func(string, float64) {})
		},
		"delete": func() error {
			_, err := DeletePartition(ctx, apischema.PartitionDeleteRequest{Device: "/dev/sdb1"})
			return err
		},
	}
	for name, run := range runs {
		env := stubPartitions(t, "")
		env.signatures = map[string]btrfs.MemberSignature{"/dev/sdb1": {Device: "/dev/sdb1", Kind: "md", UUID: "a9"}}
		if err := run(); !errors.Is(err, bridgeipc.ErrConflict) {
			t.Fatalf("%s: err = %v, want a conflict", name, err)
		}
		if want := []string{"lock /dev/sdb", "check /dev/sdb1", "unlock /dev/sdb", "probe /dev/sdb1"}; !slices.Equal(env.calls, want) {
			t.Fatalf("%s: calls = %q, want %q", name, env.calls, want)
		}
		env = stubPartitions(t, "")
		if err := run(); err != nil || slices.ContainsFunc(env.calls, isProbe) {
			t.Fatalf("%s without a signature: err = %v, calls = %q; want no probe", name, err, env.calls)
		}
		// Refused under the lock for another reason, with no signature found.
		env = stubPartitions(t, "")
		lock := lockDisk
		lockDisk = func(ctx context.Context, device string) (func(), error) {
			env.devices[1].Busy = "Mounted at /srv" // while the request waited
			return lock(ctx, device)
		}
		if err := run(); !errors.Is(err, bridgeipc.ErrConflict) || slices.ContainsFunc(env.calls, isProbe) {
			t.Fatalf("%s refused without a signature: err = %v, calls = %q; want a conflict and no probe", name, err, env.calls)
		}
	}
	env := stubPartitions(t, "")
	env.signatures = map[string]btrfs.MemberSignature{"/dev/sdb1": {Device: "/dev/sdb1", Kind: "md", UUID: "a9"}}
	if _, err := InitializeDisk(ctx, apischema.DiskInitializeRequest{Device: "/dev/sdb"}); !errors.Is(err, bridgeipc.ErrConflict) || slices.ContainsFunc(env.calls, isProbe) {
		t.Fatalf("initialize: err = %v, calls = %q; want a conflict and no probe, its close makes udev probe", err, env.calls)
	}
}

// isProbe reports whether call asked udev to probe a device.
func isProbe(call string) bool { return strings.HasPrefix(call, "probe ") }

func TestAskUdevToProbeWritesChange(t *testing.T) {
	orig := sysClassBlockRoot
	t.Cleanup(func() { sysClassBlockRoot = orig })
	sysClassBlockRoot = t.TempDir()
	uevent := filepath.Join(sysClassBlockRoot, "sdb1", "uevent")
	if err := os.MkdirAll(filepath.Dir(uevent), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(uevent, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	askUdevToProbe("/dev/sdb1")
	if got, err := os.ReadFile(uevent); err != nil || string(got) != "change" {
		t.Fatalf("uevent = %q, %v; want change", got, err)
	}
	askUdevToProbe("/dev/sdz9") // no such device: logged, nothing else
}

// TestVanishedDeviceIsNotFound covers a partition removed while the request
// waited for the disk lock, and an Initialize whose partition node went
// while it read the disk.
func TestVanishedDeviceIsNotFound(t *testing.T) {
	ctx := context.Background()
	gone := &os.PathError{Op: "open", Path: "/dev/sdb1", Err: unix.ENOENT}
	env := stubPartitions(t, "")
	env.busyErr = gone
	if err := FormatPartition(ctx, apischema.PartitionFormatRequest{Device: "/dev/sdb1", FSType: "ext4", Overwrite: true}, func(string, float64) {}); !errors.Is(err, bridgeipc.ErrNotFound) {
		t.Errorf("format: err = %v, want not found", err)
	}
	env = stubPartitions(t, "")
	env.busyErr = gone
	if _, err := DeletePartition(ctx, apischema.PartitionDeleteRequest{Device: "/dev/sdb1"}); !errors.Is(err, bridgeipc.ErrNotFound) {
		t.Errorf("delete: err = %v, want not found", err)
	}
	env = stubPartitions(t, "")
	env.initErr = gone
	if _, err := InitializeDisk(ctx, apischema.DiskInitializeRequest{Device: "/dev/sdb"}); !errors.Is(err, bridgeipc.ErrConflict) {
		t.Errorf("initialize: err = %v, want a conflict", err)
	}
}

// TestDestroyLeavesOutADiskThatDroppedOut: the kernel keeps a dropped disk's
// old path, which names no device of the pool and, once the pool is
// unmounted, may name another disk. A disk the kernel found again after a
// mount without it is the pool's own and is wiped.
func TestDestroyLeavesOutADiskThatDroppedOut(t *testing.T) {
	fs := btrfs.BtrfsFilesystem{Devices: []btrfs.BtrfsDevice{
		{ID: 1, Path: "/dev/linuxio-test-a"},
		{ID: 2, Path: "/dev/linuxio-test-b", Missing: true, DroppedOut: true},
		{ID: 3, Path: "/dev/linuxio-test-c", Missing: true},
		{ID: 4, Missing: true},
	}}
	if got, want := destroyDevices(fs), []string{"/dev/linuxio-test-a", "/dev/linuxio-test-c"}; !slices.Equal(got, want) {
		t.Fatalf("devices to wipe = %v, want %v", got, want)
	}
}

// TestDestroyRefusesAHeldDiskBeforeChangingFstab covers a pool whose disk
// something still holds after the unmount, such as a mount in another mount
// namespace: the destroy is refused before fstab changes or any disk is
// wiped, and says its mounts here are unmounted.
func TestDestroyRefusesAHeldDiskBeforeChangingFstab(t *testing.T) {
	const uuid = "0d3c2b1a-0000-4000-8000-000000000001"
	const fstab = "UUID=" + uuid + " /srv/tank btrfs defaults,nofail 0 0\n"
	env := stubPartitions(t, fstab)
	env.offline = []btrfs.BtrfsOfflinePool{{UUID: uuid, Devices: []string{"/dev/linuxio-test-a", "/dev/linuxio-test-b"}}}
	origMounts := btrfsListMounts
	t.Cleanup(func() { btrfsListMounts = origMounts })
	btrfsListMounts = func(context.Context) ([]btrfs.Mount, error) { return nil, nil }
	checkNotBusy = func(device string) error {
		if device == "/dev/linuxio-test-b" {
			return fmt.Errorf("%s is in use: %w", device, unix.EBUSY)
		}
		return nil
	}
	err := DestroyBtrfsPool(context.Background(), uuid)
	if !errors.Is(err, bridgeipc.ErrConflict) || !strings.Contains(err.Error(), "its mounts here are unmounted and nothing was erased") {
		t.Fatalf("err = %v, want a conflict saying its mounts here are unmounted and nothing was erased", err)
	}
	if got := readFstab(t); got != fstab {
		t.Fatalf("fstab = %q, want it untouched", got)
	}
}
