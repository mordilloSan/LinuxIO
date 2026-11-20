package storage

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
)

const (
	sparePool   = "5b1e0c2a-0000-4000-8000-000000000001"
	otherPool   = "5b1e0c2a-0000-4000-8000-000000000002"
	mountedPool = "5b1e0c2a-0000-4000-8000-000000000003"
	coldPool    = "5b1e0c2a-0000-4000-8000-000000000004"
	spareLabel  = "tank"
)

// stubSpareDisks makes btrfs.SpareDisks find spares, device to pool UUID.
func stubSpareDisks(t *testing.T, spares map[string]string) {
	t.Helper()
	orig := btrfsSpareDisks
	t.Cleanup(func() { btrfsSpareDisks = orig })
	btrfsSpareDisks = func() (map[string]string, error) { return spares, nil }
}

// spareEnv is the partition env with linuxio-test-spare, an empty whole
// disk, beside /dev/sdb and its partitions, and linuxio-test-tank, a disk of
// pool tank; /dev/sdb and linuxio-test-spare are both kept as tank's hot
// spare. The whole-disk paths name no device, so a check that let one
// through would reach no disk.
func spareEnv(t *testing.T) *partitionEnv {
	t.Helper()
	env := stubPartitions(t, "")
	env.devices = append(env.devices,
		apischema.StorageBlockDevice{Path: "/dev/linuxio-test-spare", Type: "disk", SizeBytes: 8 << 30, Parents: []string{}, Mountpoints: []string{}},
		apischema.StorageBlockDevice{
			Path: "/dev/linuxio-test-tank", Type: "disk", SizeBytes: 4 << 30, FSType: "btrfs", UUID: sparePool, Label: spareLabel, Parents: []string{}, Mountpoints: []string{"/srv/tank"},
			Member: &apischema.StorageMember{Kind: "btrfs", Name: spareLabel, UUID: sparePool},
		},
	)
	env.fss = []btrfs.BtrfsFilesystem{{
		UUID: sparePool, Label: spareLabel, Devices: []btrfs.BtrfsDevice{{ID: 1, Path: "/dev/linuxio-test-tank", SizeBytes: 4 << 30}},
		Allocation: []btrfs.BtrfsAllocation{{Kind: "data", Profile: "raid1"}},
	}}
	stubSpareDisks(t, map[string]string{"/dev/sdb": sparePool, "/dev/linuxio-test-spare": sparePool})
	return env
}

func TestDeviceInUseRefusesAHotSpare(t *testing.T) {
	dev := partitionDevices()[0]
	dev.HotSpareOf = &apischema.StoragePoolRef{UUID: sparePool, Label: spareLabel}
	err := deviceInUse(dev, nil, nil, false, false)
	if !errors.Is(err, bridgeipc.ErrConflict) || !strings.Contains(err.Error(), "Hot spare of pool tank") {
		t.Fatalf("err = %v, want a conflict naming the pool", err)
	}
	dev.HotSpareOf.Label = ""
	if err = deviceInUse(dev, nil, nil, false, false); !strings.Contains(err.Error(), "Hot spare of pool "+sparePool) {
		t.Errorf("unlabeled pool: err = %v, want its UUID", err)
	}
}

// Every operation that writes a device gets the reservation from the
// inventory it checks against, so each refuses the spare, or a partition
// of it, naming the pool, before it writes anything.
func TestEveryWritePathRefusesAHotSpare(t *testing.T) {
	ctx := context.Background()
	noProgress := func(string, float64) {}
	cases := []struct {
		name string
		run  func(env *partitionEnv) error
	}{
		{"create partition", func(*partitionEnv) error {
			_, err := CreatePartition(ctx, apischema.PartitionCreateRequest{Device: "/dev/sdb", StartBytes: 1 << 30, SizeBytes: 512 << 20}, noProgress)
			return err
		}},
		{"format", func(*partitionEnv) error {
			return FormatPartition(ctx, apischema.PartitionFormatRequest{Device: "/dev/sdb2", FSType: "ext4"}, noProgress)
		}},
		{"delete partition", func(*partitionEnv) error {
			_, err := DeletePartition(ctx, apischema.PartitionDeleteRequest{Device: "/dev/sdb2"})
			return err
		}},
		{"mount", func(*partitionEnv) error {
			_, err := MountPartition(ctx, apischema.PartitionMountRequest{Device: "/dev/sdb1", Mountpoint: "/mnt/linuxio-test-x"})
			return err
		}},
		{"initialize disk", func(*partitionEnv) error {
			_, err := InitializeDisk(ctx, apischema.DiskInitializeRequest{Device: "/dev/sdb"})
			return err
		}},
		{"create pool", func(*partitionEnv) error {
			_, err := CreateBtrfsPool(ctx, apischema.BtrfsPoolCreateRequest{Label: "new", Mountpoint: "/mnt/new", DataProfile: "single", MetadataProfile: "single", Devices: []string{"/dev/linuxio-test-spare"}}, noProgress)
			return err
		}},
		{"create pool in free space", func(env *partitionEnv) error {
			env.devices[3].FreeBytes = 1 << 30
			_, err := CreateBtrfsPool(ctx, apischema.BtrfsPoolCreateRequest{Label: "new", Mountpoint: "/mnt/new", DataProfile: "single", MetadataProfile: "single", FreeSpace: []string{"/dev/linuxio-test-spare"}}, noProgress)
			return err
		}},
		{"add device", func(*partitionEnv) error {
			_, err := AddBtrfsDevice(ctx, apischema.BtrfsDeviceAddRequest{UUID: sparePool, Device: "/dev/linuxio-test-spare"})
			return err
		}},
		{"replace in another pool", func(*partitionEnv) error {
			_, err := runBtrfsDeviceReplaceTask(ctx, nil, apischema.BtrfsDeviceReplaceRequest{UUID: otherPool, DeviceID: 1, NewDevice: "/dev/linuxio-test-spare"})
			return err
		}},
		{"md add or replace", func(*partitionEnv) error { return mdDeviceFree(ctx, "/dev/linuxio-test-spare") }},
		{"hot spare of another pool", func(env *partitionEnv) error {
			env.fss = append(env.fss, btrfs.BtrfsFilesystem{UUID: otherPool, Allocation: []btrfs.BtrfsAllocation{{Kind: "data", Profile: "raid1"}}})
			return SetBtrfsSpare(ctx, apischema.BtrfsSpareSetRequest{UUID: otherPool, Device: "/dev/linuxio-test-spare", DelayMinutes: 10})
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := spareEnv(t)
			set := stubSetSpare(t, nil)
			err := c.run(env)
			if !errors.Is(err, bridgeipc.ErrConflict) || !strings.Contains(err.Error(), "Hot spare of pool tank") {
				t.Fatalf("err = %v, want a conflict naming the pool", err)
			}
			for _, call := range env.calls {
				if !strings.HasPrefix(call, "check ") {
					t.Errorf("%s ran after the refusal", call)
				}
			}
			if len(*set) > 0 {
				t.Errorf("hot spare set: %v", *set)
			}
		})
	}
}

// A pool's own spare is the one reservation a replace in that pool may take,
// and the spare is no longer its spare once the replace takes it.
func TestReplaceMayTakeThePoolsOwnSpare(t *testing.T) {
	ctx := context.Background()
	spareEnv(t)
	cleared := stubClearSpare(t)
	if err := ensureBtrfsDeviceFree(ctx, "/dev/linuxio-test-spare", false, sparePool); err != nil {
		t.Fatalf("own spare as replace target: %v", err)
	}
	if err := ensureBtrfsDeviceFree(ctx, "/dev/linuxio-test-spare", false, ""); !errors.Is(err, bridgeipc.ErrConflict) {
		t.Errorf("own spare as a device to add: err = %v, want a conflict", err)
	}
	// Only the spare itself is released: a partition of it stays refused.
	if err := ensureBtrfsDeviceFree(ctx, "/dev/sdb2", false, sparePool); !errors.Is(err, bridgeipc.ErrConflict) || !strings.Contains(err.Error(), "Hot spare of pool tank") {
		t.Errorf("partition of the own spare as replace target: err = %v, want a conflict naming the pool", err)
	}

	if err := takeOwnSpare(ctx, sparePool, "/dev/linuxio-test-tank"); err != nil || len(*cleared) != 0 {
		t.Fatalf("another disk as target: err = %v, cleared %v", err, *cleared)
	}
	if err := takeOwnSpare(ctx, sparePool, "/dev/linuxio-test-spare"); err != nil {
		t.Fatal(err)
	}
	if len(*cleared) != 1 || (*cleared)[0] != sparePool {
		t.Errorf("cleared = %v, want the spare of %s", *cleared, sparePool)
	}
}

// A replace drops the pool's spare only once it installed it, on the first
// run and on a resume, which knows the new disk only by its slot.
func TestDropTakenSpare(t *testing.T) {
	ctx := context.Background()
	stubSpareDisks(t, map[string]string{"/dev/linuxio-test-spare": sparePool})
	cleared := stubClearSpare(t)
	fs := btrfs.BtrfsFilesystem{UUID: sparePool, Devices: []btrfs.BtrfsDevice{{ID: 1, Path: "/dev/linuxio-test-tank"}, {ID: 2, Path: "/dev/linuxio-test-spare"}}}
	dropTakenSpare(ctx, sparePool, 1, fs)
	dropTakenSpare(ctx, sparePool, 3, fs)
	if len(*cleared) != 0 {
		t.Fatalf("cleared = %v, want nothing for another disk or slot", *cleared)
	}
	dropTakenSpare(ctx, sparePool, 2, fs)
	if len(*cleared) != 1 || (*cleared)[0] != sparePool {
		t.Errorf("cleared = %v, want the spare of %s", *cleared, sparePool)
	}
}

// The inventory marks a designated spare and its partitions with the pool
// it is kept for, named as udev names the pool's disks, while a disk of that
// pool is connected.
func TestMarkHotSpares(t *testing.T) {
	stubSpareDisks(t, map[string]string{"/dev/linuxio-test-spare": sparePool, "/dev/linuxio-test-new": otherPool, "/dev/linuxio-test-mounted": mountedPool, "/dev/linuxio-test-cold": coldPool})
	devices := []apischema.StorageBlockDevice{
		{Path: "/dev/linuxio-test-spare", Type: "disk"},
		{Path: "/dev/linuxio-test-spare1", Type: "part", Parents: []string{"/dev/linuxio-test-spare"}},
		{Path: "/dev/linuxio-test-tank", Type: "disk", Member: &apischema.StorageMember{Kind: "btrfs", Name: spareLabel, UUID: sparePool}},
		{Path: "/dev/linuxio-test-new", Type: "disk"},
		{Path: "/dev/linuxio-test-other", Type: "disk"},
		{Path: "/dev/linuxio-test-mounted", Type: "disk"},
		{Path: "/dev/linuxio-test-cold", Type: "disk"},
	}
	// Pools udev has not probed a member of, which the kernel or the
	// superblocks know.
	fss := []btrfs.BtrfsFilesystem{{UUID: mountedPool, Label: "live"}}
	offline := []btrfs.BtrfsOfflinePool{{UUID: coldPool}}
	if err := markHotSpares(devices, fss, offline); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]apischema.StoragePoolRef{
		"/dev/linuxio-test-spare":   {UUID: sparePool, Label: spareLabel},
		"/dev/linuxio-test-spare1":  {UUID: sparePool, Label: spareLabel},
		"/dev/linuxio-test-mounted": {UUID: mountedPool, Label: "live"},
		"/dev/linuxio-test-cold":    {UUID: coldPool},
	} {
		i := slices.IndexFunc(devices, func(d apischema.StorageBlockDevice) bool { return d.Path == path })
		if d := devices[i]; d.HotSpareOf == nil || *d.HotSpareOf != want {
			t.Errorf("%s: hotSpareOf = %v, want %v", d.Path, d.HotSpareOf, want)
		}
	}
	for _, d := range devices[2:5] {
		if d.HotSpareOf != nil {
			t.Errorf("%s: hotSpareOf = %v, want none (the spare of a pool with no disk connected reserves nothing)", d.Path, *d.HotSpareOf)
		}
	}
}

// stubSetSpare records btrfs.SetSpare calls and answers err.
func stubSetSpare(t *testing.T, err error) *[]string {
	t.Helper()
	var calls []string
	orig := btrfsSetSpare
	t.Cleanup(func() { btrfsSetSpare = orig })
	btrfsSetSpare = func(_ context.Context, fs btrfs.BtrfsFilesystem, device string, delay int) error {
		calls = append(calls, fmt.Sprintf("%s %s %d", fs.UUID, device, delay))
		return err
	}
	return &calls
}

// stubClearSpare records the pools whose spare btrfs.ClearSpare drops.
func stubClearSpare(t *testing.T) *[]string {
	t.Helper()
	var cleared []string
	orig := btrfsClearSpare
	t.Cleanup(func() { btrfsClearSpare = orig })
	btrfsClearSpare = func(_ context.Context, uuid string) (bool, error) {
		cleared = append(cleared, uuid)
		return true, nil
	}
	return &cleared
}

func TestSetBtrfsSpareValidatesTheDisk(t *testing.T) {
	ctx := context.Background()
	req := func(device string) apischema.BtrfsSpareSetRequest {
		return apischema.BtrfsSpareSetRequest{UUID: sparePool, Device: device, DelayMinutes: 10}
	}
	cases := []struct {
		name string
		req  apischema.BtrfsSpareSetRequest
		edit func(*partitionEnv)
		want error
	}{
		{"no pool", apischema.BtrfsSpareSetRequest{Device: "/dev/linuxio-test-new"}, nil, bridgeipc.ErrInvalidArgs},
		{"no device", apischema.BtrfsSpareSetRequest{UUID: sparePool}, nil, bridgeipc.ErrInvalidArgs},
		{"pool not mounted", apischema.BtrfsSpareSetRequest{UUID: otherPool, Device: "/dev/linuxio-test-new"}, nil, bridgeipc.ErrNotFound},
		{"unknown device", req("/dev/sdz"), nil, bridgeipc.ErrNotFound},
		{"a partition", req("/dev/sdb2"), nil, bridgeipc.ErrInvalidArgs},
		{"read-only", req("/dev/linuxio-test-new"), func(env *partitionEnv) { env.devices[len(env.devices)-1].ReadOnly = true }, bridgeipc.ErrConflict},
		{"busy", req("/dev/linuxio-test-new"), func(env *partitionEnv) { env.devices[len(env.devices)-1].Busy = "Used by /dev/md0" }, bridgeipc.ErrConflict},
		{"in fstab", req("/dev/linuxio-test-new"), func(env *partitionEnv) { env.devices[len(env.devices)-1].InFstab = true }, bridgeipc.ErrConflict},
		{"a filesystem", req("/dev/linuxio-test-new"), func(env *partitionEnv) { env.devices[len(env.devices)-1].FSType = "xfs" }, bridgeipc.ErrConflict},
		{"a member", req("/dev/linuxio-test-new"), func(env *partitionEnv) {
			env.devices[len(env.devices)-1].Member = &apischema.StorageMember{Kind: "md", Name: "host:0"}
		}, bridgeipc.ErrConflict},
		{"partitions", req("/dev/linuxio-test-new"), func(env *partitionEnv) {
			env.devices = append(env.devices, apischema.StorageBlockDevice{Path: "/dev/linuxio-test-new1", Type: "part", Parents: []string{"/dev/linuxio-test-new"}, Mountpoints: []string{}})
		}, bridgeipc.ErrConflict},
		{"a disk of the pool", req("/dev/linuxio-test-tank"), nil, bridgeipc.ErrConflict},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := spareEnv(t)
			env.devices = append(env.devices, apischema.StorageBlockDevice{Path: "/dev/linuxio-test-new", Type: "disk", SizeBytes: 8 << 30, Parents: []string{}, Mountpoints: []string{}})
			if c.edit != nil {
				c.edit(env)
			}
			set := stubSetSpare(t, nil)
			if err := SetBtrfsSpare(ctx, c.req); !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
			if len(*set) > 0 {
				t.Errorf("hot spare set: %v", *set)
			}
		})
	}
}

func TestSetBtrfsSpare(t *testing.T) {
	ctx := context.Background()
	spareEnv(t)
	set := stubSetSpare(t, nil)
	// The pool's own spare may be set again, with another delay.
	if err := SetBtrfsSpare(ctx, apischema.BtrfsSpareSetRequest{UUID: sparePool, Device: "/dev/linuxio-test-spare", DelayMinutes: 0}); err != nil {
		t.Fatal(err)
	}
	if len(*set) != 1 || (*set)[0] != sparePool+" /dev/linuxio-test-spare 0" {
		t.Fatalf("set = %v", *set)
	}

	for _, c := range []struct {
		from, want error
	}{
		{fmt.Errorf("%w: too small", btrfs.ErrSpareUnsuitable), bridgeipc.ErrInvalidArgs},
		{fmt.Errorf("%w: has partitions", btrfs.ErrSpareInUse), bridgeipc.ErrConflict},
	} {
		stubSetSpare(t, c.from)
		if err := SetBtrfsSpare(ctx, apischema.BtrfsSpareSetRequest{UUID: sparePool, Device: "/dev/linuxio-test-spare", DelayMinutes: 10}); !errors.Is(err, c.want) || !errors.Is(err, c.from) {
			t.Errorf("btrfs answering %v: err = %v, want %v", c.from, err, c.want)
		}
	}
}

func TestBtrfsSpareDelayAndClear(t *testing.T) {
	ctx := context.Background()
	orig := btrfsSetSpareDelay
	t.Cleanup(func() { btrfsSetSpareDelay = orig })
	var got []string
	btrfsSetSpareDelay = func(_ context.Context, uuid string, delay int) error {
		got = append(got, fmt.Sprintf("%s %d", uuid, delay))
		switch {
		case uuid == otherPool:
			return fmt.Errorf("%w: %s", btrfs.ErrNoSpare, uuid)
		case delay < 0:
			return fmt.Errorf("%w: delay", btrfs.ErrSpareUnsuitable)
		}
		return nil
	}
	if err := SetBtrfsSpareDelay(ctx, apischema.BtrfsSpareDelayRequest{UUID: sparePool, DelayMinutes: 30}); err != nil {
		t.Fatal(err)
	}
	if err := SetBtrfsSpareDelay(ctx, apischema.BtrfsSpareDelayRequest{UUID: otherPool, DelayMinutes: 30}); !errors.Is(err, bridgeipc.ErrNotFound) {
		t.Errorf("pool without a spare: err = %v, want not found", err)
	}
	if err := SetBtrfsSpareDelay(ctx, apischema.BtrfsSpareDelayRequest{UUID: sparePool, DelayMinutes: -1}); !errors.Is(err, bridgeipc.ErrInvalidArgs) {
		t.Errorf("negative delay: err = %v, want invalid arguments", err)
	}
	if err := SetBtrfsSpareDelay(ctx, apischema.BtrfsSpareDelayRequest{DelayMinutes: 1}); !errors.Is(err, bridgeipc.ErrInvalidArgs) {
		t.Errorf("no pool: err = %v, want invalid arguments", err)
	}
	if len(got) != 3 || got[0] != sparePool+" 30" {
		t.Errorf("delay calls = %v", got)
	}

	cleared := stubClearSpare(t)
	if err := ClearBtrfsSpare(ctx, apischema.UUIDRequest{UUID: sparePool}); err != nil {
		t.Fatal(err)
	}
	if err := ClearBtrfsSpare(ctx, apischema.UUIDRequest{}); !errors.Is(err, bridgeipc.ErrInvalidArgs) {
		t.Errorf("no pool: err = %v, want invalid arguments", err)
	}
	if len(*cleared) != 1 || (*cleared)[0] != sparePool {
		t.Errorf("cleared = %v", *cleared)
	}
}

// The routes write host state as root, so a non-sudo session is refused.
func TestBtrfsSpareRoutesArePrivileged(t *testing.T) {
	for _, route := range btrfsSpareBindings().Routes() {
		if !route.Privileged {
			t.Errorf("%s is not privileged", route.Route)
		}
	}
	if n := len(btrfsSpareBindings().Routes()); n != 3 {
		t.Errorf("%d spare routes, want set, delay and clear", n)
	}
}
