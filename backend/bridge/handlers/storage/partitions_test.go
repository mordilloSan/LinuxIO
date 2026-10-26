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
	devices                                 []apischema.StorageBlockDevice
	fss                                     []btrfs.BtrfsFilesystem
	offline                                 []btrfs.BtrfsOfflinePool
	calls                                   []string
	busyErr, formatErr, deleteErr, mountErr error
}

func stubPartitions(t *testing.T, fstab string) *partitionEnv {
	t.Helper()
	origDevices, origFss, origOffline := storageBlockDevices, btrfsFilesystems, offlinePools
	origCreate, origDelete, origInit, origFormat := createPartition, deletePartition, initializeDisk, formatDevice
	origBusy, origMount, origLock := checkNotBusy, mountDevice, lockDisk
	t.Cleanup(func() {
		storageBlockDevices, btrfsFilesystems, offlinePools = origDevices, origFss, origOffline
		createPartition, deletePartition, initializeDisk, formatDevice = origCreate, origDelete, origInit, origFormat
		checkNotBusy, mountDevice, lockDisk = origBusy, origMount, origLock
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
	createPartition = func(_ context.Context, device string, start, size uint64) (string, error) {
		record("create %s %d %d", device, start, size)
		return "/dev/sdb3", nil
	}
	formatDevice = func(_ context.Context, device, fsType, label string, overwrite bool) (string, error) {
		record("format %s %s %q overwrite=%t", device, fsType, label, overwrite)
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
	initializeDisk = func(_ context.Context, device string) error {
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
			_, err := CreatePartition(context.Background(), c.req, func(string) {})
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
	}, func(step string) { steps = append(steps, step) })
	if err != nil || part != "/dev/sdb3" {
		t.Fatalf("part = %q, err = %v", part, err)
	}
	wantSteps := []string{"Partitioning", "Formatting: SSDs are trimmed first, which can take a few minutes", "Mounting at " + mp}
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
	if _, err := CreatePartition(context.Background(), req, func(step string) { steps = append(steps, step) }); err != nil {
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
	_, err := CreatePartition(context.Background(), req, func(string) {})
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
	_, err = CreatePartition(context.Background(), req, func(string) {})
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
			if err := FormatPartition(ctx, c.req, func(string) {}); !errors.Is(err, c.want) {
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
		if err := FormatPartition(ctx, format("/dev/sdb1", true), func(step string) { steps = append(steps, step) }); err != nil {
			t.Fatal(err)
		}
		// The O_EXCL check runs under the lock, right before mkfs, never before
		// a wait for the lock that can outlast another format.
		want := []string{"lock /dev/sdb", "check /dev/sdb1", `format /dev/sdb1 xfs "logs" overwrite=true`, "unlock /dev/sdb"}
		if !slices.Equal(env.calls, want) {
			t.Errorf("calls = %q, want %q", env.calls, want)
		}
		if !slices.Equal(steps, []string{"Formatting: SSDs are trimmed first, which can take a few minutes"}) {
			t.Errorf("steps = %q", steps)
		}
	})
}

func TestDeletePartition(t *testing.T) {
	ctx := context.Background()
	sdb1 := apischema.BlockDeviceRequest{Device: "/dev/sdb1"}

	env := stubPartitions(t, "")
	if _, err := DeletePartition(ctx, sdb1); err != nil {
		t.Fatal(err)
	}
	if want := []string{"lock /dev/sdb", "delete /dev/sdb 1", "unlock /dev/sdb"}; !slices.Equal(env.calls, want) {
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

	_, err := DeletePartition(ctx, apischema.BlockDeviceRequest{Device: "/dev/sdb1"})
	if !errors.Is(err, bridgeipc.ErrConflict) || !strings.Contains(err.Error(), "another operation is running on /dev/sdb") {
		t.Fatalf("delete: err = %v, want a conflict naming the disk", err)
	}
	_, err = InitializeDisk(ctx, apischema.BlockDeviceRequest{Device: "/dev/sdb"})
	if !errors.Is(err, bridgeipc.ErrConflict) {
		t.Fatalf("initialize: err = %v, want a conflict", err)
	}
	if len(env.calls) != 0 {
		t.Fatalf("calls = %q, want no write", env.calls)
	}

	// A request that ended on its own keeps its own error.
	ended, end := context.WithCancel(context.Background())
	end()
	if _, err = DeletePartition(ended, apischema.BlockDeviceRequest{Device: "/dev/sdb1"}); !errors.Is(err, context.Canceled) || errors.Is(err, bridgeipc.ErrConflict) {
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
	sdb := apischema.BlockDeviceRequest{Device: "/dev/sdb"}

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
