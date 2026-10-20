package storage

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
	bridgetasks "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

func TestCheckBtrfsDeviceFree(t *testing.T) {
	devices := []apischema.StorageBlockDevice{
		{Path: "/dev/sdb", Type: "disk", Mountpoints: []string{}},
		{Path: "/dev/sdc", Type: "disk", FSType: "ext4", Mountpoints: []string{}},
		{Path: "/dev/sdd", Type: "disk", FSType: "ext4", Mountpoints: []string{"/srv"}},
		{Path: "/dev/sde", Type: "disk", FSType: "btrfs", Mountpoints: []string{}},
		{Path: "/dev/sdf", Type: "disk", Mountpoints: []string{}},
		{Path: "/dev/sdf1", Type: "part", Mountpoints: []string{}, Parents: []string{"/dev/sdf"}},
	}
	filesystems := []btrfs.BtrfsFilesystem{{UUID: testBtrfsUUID, Devices: []btrfs.BtrfsDevice{{ID: 1, Path: "/dev/sde"}}}}
	cases := []struct {
		device  string
		force   bool
		wantErr string
	}{
		{"/dev/sdb", false, ""},
		{"/dev/sdc", true, ""},
		{"/dev/sdc", false, "has a ext4 signature"},
		{"/dev/sdd", true, "is mounted"},
		{"/dev/sde", true, "already belongs to btrfs filesystem"},
		{"/dev/sdf", true, "has partitions or holders"},
		{"/dev/nope", true, "not found"},
	}
	for _, c := range cases {
		err := checkBtrfsDeviceFree(devices, filesystems, c.device, c.force)
		if c.wantErr == "" && err != nil {
			t.Errorf("%s force=%v: err = %v", c.device, c.force, err)
		}
		if c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)) {
			t.Errorf("%s force=%v: err = %v, want %q", c.device, c.force, err, c.wantErr)
		}
	}
}

func fastBtrfsPoll(t *testing.T) {
	t.Helper()
	old := btrfsPollInterval
	btrfsPollInterval = time.Millisecond
	t.Cleanup(func() { btrfsPollInterval = old })
}

func TestPollBtrfsOpReportsUntilDone(t *testing.T) {
	fastBtrfsPoll(t)
	polls := 0
	var reported []BtrfsOpProgress
	err := pollBtrfsOp(context.Background(), func(p bridgetasks.ProgressDetail) {
		if op, ok := p.(BtrfsOpProgress); ok {
			reported = append(reported, op)
		}
	}, "balance", "Balancing", func(context.Context) (bool, float64, error) {
		polls++
		return polls < 3, float64(polls * 10), nil
	})
	if err != nil || polls != 3 || len(reported) != 2 || reported[1].Percent != 20 || reported[1].Op != "balance" {
		t.Fatalf("err=%v polls=%d reported=%+v", err, polls, reported)
	}
}

func TestPollBtrfsOpStopsOnErrorAndCancel(t *testing.T) {
	// The default interval keeps the ticker from racing the canceled ctx.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := pollBtrfsOp(ctx, func(bridgetasks.ProgressDetail) {}, "scrub", "Scrubbing", func(context.Context) (bool, float64, error) {
		t.Fatal("polled after cancel")
		return true, 0, nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want canceled", err)
	}

	fastBtrfsPoll(t)
	boom := errors.New("boom")
	if err := pollBtrfsOp(context.Background(), func(bridgetasks.ProgressDetail) {}, "scrub", "Scrubbing", func(context.Context) (bool, float64, error) {
		return false, 0, boom
	}); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
}
