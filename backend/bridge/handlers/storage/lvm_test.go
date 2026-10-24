package storage

import (
	"context"
	"errors"
	"os/exec"
	"testing"

	"github.com/shirou/gopsutil/v4/disk"
)

func stubLVMCommand(t *testing.T, fn func(context.Context, string, ...string) ([]byte, error)) {
	t.Helper()
	orig := lvmCommand
	t.Cleanup(func() { lvmCommand = orig })
	lvmCommand = fn
}

func TestListLogicalVolumesReturnsCommandError(t *testing.T) {
	boom := errors.New("permission denied")
	stubLVMCommand(t, func(context.Context, string, ...string) ([]byte, error) { return nil, boom })

	if _, err := ListLogicalVolumes(t.Context()); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
}

func TestListLVMAbsentToolReturnsEmptyList(t *testing.T) {
	stubLVMCommand(t, func(context.Context, string, ...string) ([]byte, error) { return nil, exec.ErrNotFound })

	pvs, err := ListPhysicalVolumes(t.Context())
	if err != nil || pvs == nil || len(pvs) != 0 {
		t.Fatalf("pvs = %#v, err = %v, want empty non-nil list", pvs, err)
	}
}

func TestListLogicalVolumesMatchesMountByMapperPath(t *testing.T) {
	stubLVMCommand(t, func(context.Context, string, ...string) ([]byte, error) {
		return []byte(`{"report":[{"lv":[{"lv_name":"root","vg_name":"vg0","lv_size":"1024","lv_path":"/dev/vg0/root","lv_dm_path":"/dev/mapper/vg0-root","lv_attr":"-wi-ao----"}]}]}`), nil
	})
	origParts := listPartitions
	t.Cleanup(func() { listPartitions = origParts })
	listPartitions = func(bool) ([]disk.PartitionStat, error) {
		return []disk.PartitionStat{{Device: "/dev/mapper/vg0-root", Mountpoint: t.TempDir(), Fstype: "ext4"}}, nil
	}

	lvs, err := ListLogicalVolumes(t.Context())
	if err != nil {
		t.Fatalf("ListLogicalVolumes: %v", err)
	}
	if len(lvs) != 1 || lvs[0].FSType != "ext4" || lvs[0].Mountpoint == "" || lvs[0].Path != "/dev/vg0/root" {
		t.Fatalf("lvs = %#v, want mounted ext4 volume with display path /dev/vg0/root", lvs)
	}
}
