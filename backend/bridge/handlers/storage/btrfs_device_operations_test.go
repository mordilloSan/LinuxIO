package storage

import (
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
)

const testBtrfsUUID = "9a2b4c6d-1e3f-4a5b-8c7d-9e0f1a2b3c4d"

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
