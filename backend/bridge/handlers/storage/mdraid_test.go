package storage

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

func writeSysfs(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// fakeMD builds md127 (raid1, two in_sync members and one spare) under a temp sysfs.
func fakeMD(t *testing.T, extra map[string]string) string {
	t.Helper()
	root := t.TempDir()
	orig := mdSysfsRoot
	mdSysfsRoot = root
	t.Cleanup(func() { mdSysfsRoot = orig })
	md := filepath.Join(root, "block", "md127", "md")
	files := map[string]string{
		"array_state": "clean", "level": "raid1", "metadata_version": "1.2", "raid_disks": "2",
		"degraded": "0", "sync_action": "idle", "sync_completed": "100 / 400", "sync_speed": "1000",
		"mismatch_cnt":  "8",
		"dev-sdb1/slot": "1", "dev-sdb1/state": "in_sync", "dev-sdb1/errors": "0",
		"dev-sda1/slot": "0", "dev-sda1/state": "in_sync,write_mostly", "dev-sda1/errors": "3",
		"dev-sdc1/slot": "none", "dev-sdc1/state": "spare", "dev-sdc1/errors": "0",
	}
	maps.Copy(files, extra)
	writeSysfs(t, md, files)
	writeSysfs(t, filepath.Join(root, "block", "md127"), map[string]string{"size": "2048"})
	return md
}

func stubMdadm(t *testing.T, devices []apischema.StorageBlockDevice) *[][]string {
	t.Helper()
	var calls [][]string
	origCmd, origDevs := mdadmCommand, mdBlockDevices
	t.Cleanup(func() { mdadmCommand, mdBlockDevices = origCmd, origDevs })
	mdadmCommand = func(_ context.Context, args ...string) error {
		calls = append(calls, args)
		return nil
	}
	mdBlockDevices = func(context.Context) ([]apischema.StorageBlockDevice, error) { return devices, nil }
	return &calls
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(data))
}

func TestListMDArrays(t *testing.T) {
	fakeMD(t, nil)
	if err := os.MkdirAll(filepath.Join(mdSysfsRoot, "block", "sda"), 0o755); err != nil {
		t.Fatal(err)
	}
	arrays, err := ListMDArrays()
	if err != nil || len(arrays) != 1 {
		t.Fatalf("arrays = %#v, err = %v", arrays, err)
	}
	a := arrays[0]
	if a.Name != "md127" || a.Path != "/dev/md127" || a.Level != "raid1" || a.SizeBytes != 1048576 || a.RaidDisks != 2 || a.MismatchCount != 8 {
		t.Fatalf("array = %#v", a)
	}
	if a.SyncProgress != 25 || a.SyncSpeedBytes != 1024000 {
		t.Fatalf("progress = %v speed = %d", a.SyncProgress, a.SyncSpeedBytes)
	}
	var names []string
	for _, m := range a.Members {
		names = append(names, m.Name)
	}
	if !slices.Equal(names, []string{"sda1", "sdb1", "sdc1"}) {
		t.Fatalf("member order = %v", names)
	}
	if m := a.Members[0]; !slices.Equal(m.State, []string{"in_sync", "write_mostly"}) || m.Errors != 3 || m.Slot != 0 {
		t.Fatalf("member = %#v", m)
	}
	if a.Members[2].Slot != -1 {
		t.Fatalf("spare slot = %d", a.Members[2].Slot)
	}
}

func TestListMDArraysNoneAndEmpty(t *testing.T) {
	fakeMD(t, map[string]string{"sync_completed": "none", "sync_speed": "none"})
	arrays, _ := ListMDArrays()
	if arrays[0].SyncProgress != 0 || arrays[0].SyncSpeedBytes != 0 {
		t.Fatalf("array = %#v", arrays[0])
	}
	mdSysfsRoot = t.TempDir()
	_ = os.MkdirAll(filepath.Join(mdSysfsRoot, "block"), 0o755)
	arrays, err := ListMDArrays()
	if err != nil || arrays == nil || len(arrays) != 0 {
		t.Fatalf("arrays = %#v, err = %v", arrays, err)
	}
}

func TestMDSyncAction(t *testing.T) {
	md := fakeMD(t, nil)
	if _, err := MDSyncAction(apischema.MDSyncActionRequest{Name: "md127", Action: "resync"}); !errors.Is(err, bridgeipc.ErrInvalidArgs) {
		t.Fatalf("bad action err = %v", err)
	}
	a, err := MDSyncAction(apischema.MDSyncActionRequest{Name: "md127", Action: "check"})
	if err != nil || a.Name != "md127" || readFile(t, filepath.Join(md, "sync_action")) != "check" {
		t.Fatalf("err = %v, file = %q", err, readFile(t, filepath.Join(md, "sync_action")))
	}
	writeSysfs(t, md, map[string]string{"sync_action": "check"})
	_, err = MDSyncAction(apischema.MDSyncActionRequest{Name: "md127", Action: "repair"})
	if !errors.Is(err, bridgeipc.ErrConflict) || !strings.Contains(err.Error(), "check") {
		t.Fatalf("busy err = %v", err)
	}
	if _, err := MDSyncAction(apischema.MDSyncActionRequest{Name: "md127", Action: "idle"}); err != nil {
		t.Fatalf("idle err = %v", err)
	}
	if err := os.Remove(filepath.Join(md, "sync_action")); err != nil {
		t.Fatal(err)
	}
	if _, err := MDSyncAction(apischema.MDSyncActionRequest{Name: "md127", Action: "check"}); !errors.Is(err, bridgeipc.ErrInvalidArgs) {
		t.Fatalf("no sync_action err = %v", err)
	}
}

func TestMDMemberFail(t *testing.T) {
	md := fakeMD(t, nil)
	req := apischema.MDMemberRequest{Name: "md127", Member: "sda1"}
	if _, err := MDMemberFail(req); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(md, "dev-sda1", "state")); got != "faulty" {
		t.Fatalf("state = %q", got)
	}
	// sda1 is faulty now; failing the last in_sync member must be refused.
	writeSysfs(t, md, map[string]string{"dev-sda1/state": "faulty"})
	_, err := MDMemberFail(apischema.MDMemberRequest{Name: "md127", Member: "sdb1"})
	if !errors.Is(err, bridgeipc.ErrConflict) {
		t.Fatalf("last member err = %v", err)
	}
	if got := readFile(t, filepath.Join(md, "dev-sdb1", "state")); got != "in_sync" {
		t.Fatalf("state = %q", got)
	}
}

func TestMDMemberRemove(t *testing.T) {
	md := fakeMD(t, nil)
	if _, err := MDMemberRemove(apischema.MDMemberRequest{Name: "md127", Member: "sda1"}); !errors.Is(err, bridgeipc.ErrConflict) {
		t.Fatalf("in_sync err = %v", err)
	}
	if _, err := MDMemberRemove(apischema.MDMemberRequest{Name: "md127", Member: "sdc1"}); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(md, "dev-sdc1", "state")); got != "remove" {
		t.Fatalf("state = %q", got)
	}
}

func TestCheckMDDeviceFree(t *testing.T) {
	devices := []apischema.StorageBlockDevice{
		{Path: "/dev/sdz", Type: "disk"},
		{Path: "/dev/sdm", Type: "disk", Mountpoints: []string{"/mnt"}},
		{Path: "/dev/sdp", Type: "disk"},
		{Path: "/dev/sdp1", Type: "part", Parents: []string{"/dev/sdp"}},
		{Path: "/dev/sdx", Type: "disk", FSType: "ext4"},
		{Path: "/dev/sr0", Type: "rom"},
	}
	for _, tc := range []struct {
		device  string
		wantErr error
		text    string
	}{
		{"/dev/sdz", nil, ""},
		{"/dev/sdm", bridgeipc.ErrConflict, "mounted"},
		{"/dev/sdp", bridgeipc.ErrConflict, "partitions"},
		{"/dev/sdx", bridgeipc.ErrConflict, "ext4 signature; wipe it first"},
		{"/dev/nope", bridgeipc.ErrNotFound, ""},
		{"/dev/sr0", bridgeipc.ErrInvalidArgs, ""},
	} {
		err := checkMDDeviceFree(devices, tc.device)
		if !errors.Is(err, tc.wantErr) || (tc.wantErr == nil) != (err == nil) || (tc.text != "" && !strings.Contains(err.Error(), tc.text)) {
			t.Errorf("%s: err = %v, want %v %q", tc.device, err, tc.wantErr, tc.text)
		}
	}
}

func TestMDMemberAddAndReplace(t *testing.T) {
	md := fakeMD(t, nil)
	calls := stubMdadm(t, []apischema.StorageBlockDevice{{Path: "/dev/sdz", Type: "disk"}})
	want := []string{"--manage", "/dev/md127", "--add", "/dev/sdz"}

	if _, err := MDMemberAdd(t.Context(), apischema.MDMemberAddRequest{Name: "md127", Device: "/dev/sdz"}); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 || !slices.Equal((*calls)[0], want) {
		t.Fatalf("calls = %v", *calls)
	}

	if _, err := MDMemberReplace(t.Context(), apischema.MDMemberReplaceRequest{Name: "md127", Member: "sdc1", Device: "/dev/sdz"}); !errors.Is(err, bridgeipc.ErrConflict) {
		t.Fatalf("spare replace err = %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("mdadm ran for a refused replace: %v", *calls)
	}

	if _, err := MDMemberReplace(t.Context(), apischema.MDMemberReplaceRequest{Name: "md127", Member: "sda1", Device: "/dev/sdz"}); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 2 || !slices.Equal((*calls)[1], want) {
		t.Fatalf("calls = %v", *calls)
	}
	if got := readFile(t, filepath.Join(md, "dev-sda1", "state")); got != "want_replacement" {
		t.Fatalf("state = %q", got)
	}

	// A failed state write must say the device was already added.
	if err := os.RemoveAll(filepath.Join(md, "dev-sdb1")); err != nil {
		t.Fatal(err)
	}
	writeSysfs(t, md, map[string]string{"dev-sdb1/slot": "1", "dev-sdb1/state": "in_sync"})
	if err := os.Chmod(filepath.Join(md, "dev-sdb1", "state"), 0o444); err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() != 0 {
		_, err := MDMemberReplace(t.Context(), apischema.MDMemberReplaceRequest{Name: "md127", Member: "sdb1", Device: "/dev/sdz"})
		if err == nil || !strings.Contains(err.Error(), "added to md127 as a spare") {
			t.Fatalf("err = %v", err)
		}
	}
}

func TestMDInvalidNames(t *testing.T) {
	fakeMD(t, nil)
	for _, name := range []string{"../md127", "md127/md", "sda", "md999", ""} {
		if _, err := MDSyncAction(apischema.MDSyncActionRequest{Name: name, Action: "idle"}); !errors.Is(err, bridgeipc.ErrNotFound) {
			t.Errorf("name %q err = %v", name, err)
		}
	}
	if _, err := MDMemberFail(apischema.MDMemberRequest{Name: "md127", Member: "../sda1"}); !errors.Is(err, bridgeipc.ErrNotFound) {
		t.Fatalf("member err = %v", err)
	}
}
