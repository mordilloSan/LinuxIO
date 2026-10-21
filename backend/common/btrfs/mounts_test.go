package btrfs

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"golang.org/x/sys/unix"
)

const (
	rootUUID = "3f1c0a52-7d1e-4b8a-9c11-0e5a6b7d8f90"
	dataUUID = "9a2b4c6d-1e3f-4a5b-8c7d-9e0f1a2b3c4d"
)

const mountinfo = "44 1 0:34 /@ / rw,relatime shared:1 - btrfs /dev/nvme0n1p2 rw,ssd,space_cache=v2,subvolid=256,subvol=/@\n" +
	"33 44 0:28 / /run rw,nosuid,nodev shared:13 - tmpfs tmpfs rw,size=4062144k\n" +
	"90 44 0:34 /@/www /var/www rw,relatime shared:1 - btrfs /dev/nvme0n1p2 rw,ssd,space_cache=v2,subvolid=256,subvol=/@\n" +
	"95 44 0:40 / /data rw,noatime shared:20 - btrfs /dev/sdb1 rw,subvolid=5,subvol=/\n" +
	"96 44 0:40 /my\\040share /mnt/my\\040share rw,noatime - btrfs /dev/sdb1 rw,subvolid=5,subvol=/\n" +
	"garbage line\n"

func TestParseMountinfo(t *testing.T) {
	uuidOf := func(device string) string {
		return map[string]string{"/dev/nvme0n1p2": rootUUID, "/dev/sdb1": dataUUID}[device]
	}
	got := parseMountinfo(mountinfo, uuidOf)
	rootOpts := []string{"rw", "relatime", "rw", "ssd", "space_cache=v2", "subvolid=256", "subvol=/@"}
	want := []Mount{
		{Mountpoint: "/", Device: "/dev/nvme0n1p2", UUID: rootUUID, Subvol: "@", SubvolID: 256, SubPath: "@", Options: rootOpts},
		{Mountpoint: "/var/www", Device: "/dev/nvme0n1p2", UUID: rootUUID, Subvol: "@", SubvolID: 256, SubPath: "@/www", Options: rootOpts},
		{Mountpoint: "/data", Device: "/dev/sdb1", UUID: dataUUID, SubvolID: 5, Options: []string{"rw", "noatime", "rw", "subvolid=5", "subvol=/"}},
		{Mountpoint: "/mnt/my share", Device: "/dev/sdb1", UUID: dataUUID, SubvolID: 5, SubPath: "my share", Options: []string{"rw", "noatime", "rw", "subvolid=5", "subvol=/"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseMountinfo =\n%+v\nwant\n%+v", got, want)
	}
	if got := parseMountinfo("", uuidOf); len(got) != 0 {
		t.Fatalf("empty = %v", got)
	}
}

func TestListMountsResolvesUUIDsFromSysfs(t *testing.T) {
	dir := t.TempDir()
	// The second member of a two-disk pool is mounted; sysfs lists both.
	for _, member := range []string{"sdb1", "sdc1"} {
		if err := os.MkdirAll(filepath.Join(dir, "sys", dataUUID, "devices", member), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "sys", "features"), 0o755); err != nil {
		t.Fatal(err)
	}
	info := filepath.Join(dir, "mountinfo")
	if err := os.WriteFile(info, []byte("95 44 0:40 / /data rw,noatime - btrfs /dev/sdc1 rw,subvolid=5,subvol=/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldInfo, oldSys, oldEval := mountinfoPath, sysFSBtrfs, evalSymlinks
	mountinfoPath, sysFSBtrfs = info, filepath.Join(dir, "sys")
	evalSymlinks = func(p string) (string, error) { return p, nil }
	t.Cleanup(func() { mountinfoPath, sysFSBtrfs, evalSymlinks = oldInfo, oldSys, oldEval })

	got, err := ListMounts(context.Background())
	if err != nil || len(got) != 1 || got[0].UUID != dataUUID || got[0].Mountpoint != "/data" {
		t.Fatalf("got %+v err=%v", got, err)
	}
}

func TestSplitMountOptions(t *testing.T) {
	flags, data := splitMountOptions([]string{"rw", "noatime", "nodev", "defaults", "space_cache=v2", "subvol=/data", ""})
	if flags != unix.MS_NOATIME|unix.MS_NODEV || data != "space_cache=v2,subvol=/data" {
		t.Fatalf("flags=%#x data=%q", flags, data)
	}
	if flags, data := splitMountOptions(nil); flags != 0 || data != "" {
		t.Fatalf("empty: flags=%#x data=%q", flags, data)
	}
}

// stubMounts serves the given mounts.
func stubMounts(t *testing.T, mounts []Mount) {
	t.Helper()
	old := listMounts
	listMounts = func(context.Context) ([]Mount, error) { return mounts, nil }
	t.Cleanup(func() { listMounts = old })
}

// stubMountFS records mount calls instead of mounting.
func stubMountFS(t *testing.T, err error) *[]string {
	t.Helper()
	var calls []string
	old := mountFS
	mountFS = func(source, target, fstype string, flags uintptr, data string) error {
		calls = append(calls, source+" "+target+" "+fstype+" "+data)
		return err
	}
	t.Cleanup(func() { mountFS = old })
	return &calls
}

func TestTopLevelReusesExistingMount(t *testing.T) {
	stubMounts(t, []Mount{{Mountpoint: "/data", UUID: dataUUID, SubvolID: 5}})
	calls := stubMountFS(t, nil)

	dir, err := TopLevel(context.Background(), dataUUID)
	if err != nil || dir != "/data" {
		t.Fatalf("dir = %q, err = %v", dir, err)
	}
	if len(*calls) != 0 {
		t.Fatalf("unexpected mounts: %v", *calls)
	}
}

func TestTopLevelMountsWhenMissing(t *testing.T) {
	stubMounts(t, []Mount{{Mountpoint: "/", Device: "/dev/sda2", UUID: rootUUID, Subvol: "@", SubvolID: 256}})
	calls := stubMountFS(t, nil)
	oldDir := TopLevelDir
	TopLevelDir = t.TempDir()
	t.Cleanup(func() { TopLevelDir = oldDir })

	dir, err := TopLevel(context.Background(), rootUUID)
	if err != nil {
		t.Fatalf("TopLevel: %v", err)
	}
	top := filepath.Join(TopLevelDir, rootUUID)
	if dir != top {
		t.Fatalf("dir = %q, want %q", dir, top)
	}
	if info, err := os.Stat(top); err != nil || !info.IsDir() {
		t.Fatalf("top-level dir not created: %v", err)
	}
	if want := []string{"/dev/sda2 " + top + " btrfs subvolid=5"}; !reflect.DeepEqual(*calls, want) {
		t.Fatalf("mounts = %v, want %v", *calls, want)
	}

	// An unmounted filesystem has no device to mount from.
	stubMounts(t, nil)
	if _, err := TopLevel(context.Background(), rootUUID); err == nil {
		t.Fatal("TopLevel of an unmounted filesystem succeeded")
	}
}

func TestTopLevelRejectsBadUUID(t *testing.T) {
	stubMounts(t, nil)
	calls := stubMountFS(t, nil)
	for _, uuid := range []string{"", "../..", "a/b", "/abs"} {
		if _, err := TopLevel(context.Background(), uuid); err == nil {
			t.Errorf("TopLevel(%q) succeeded, want error", uuid)
		}
	}
	if len(*calls) != 0 {
		t.Fatalf("unexpected mounts: %v", *calls)
	}
}
