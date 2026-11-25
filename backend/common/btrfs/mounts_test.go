package btrfs

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"unsafe"

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
	uuidOf := func(_ uint64, _, device string) string {
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

// Since 7.1 the kernel marks a member missing when its disk is gone, and a
// mount whose source was that member shows "<missing disk>" there
// (btrfs_show_devname): the mount is asked for its filesystem instead, by
// its mount ID, and a mount whose source names a member is not.
func TestListMountsAsksTheMountWhenItsSourceIsMissing(t *testing.T) {
	dir := t.TempDir()
	for _, member := range []string{"vdb", "vdc", "sda2"} {
		uuid := dataUUID
		if member == "sda2" {
			uuid = rootUUID
		}
		if err := os.MkdirAll(filepath.Join(dir, "sys", uuid, "devices", member), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// A name the source would match if it were looked up as a device: it is
	// not a path, so it is not, nor read as a file of the working directory.
	if err := os.MkdirAll(filepath.Join(dir, "sys", rootUUID, "devices", missingDiskName), 0o755); err != nil {
		t.Fatal(err)
	}
	info := filepath.Join(dir, "mountinfo")
	text := "44 1 0:34 / / rw - btrfs /dev/sda2 rw,subvolid=5,subvol=/\n" +
		"95 44 0:40 / /data rw,noatime - btrfs <missing\\040disk> rw,degraded,subvolid=5,subvol=/\n" +
		"96 44 0:40 /@www /var/www rw,noatime - btrfs <missing\\040disk> rw,degraded,subvolid=256,subvol=/@www\n" +
		"97 44 0:41 / /gone rw - btrfs <missing\\040disk> rw,degraded,subvolid=5,subvol=/\n"
	if err := os.WriteFile(info, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	type asked struct {
		mountpoint string
		id         uint64
	}
	var calls []asked
	oldInfo, oldSys, oldEval, oldAsk := mountinfoPath, sysFSBtrfs, evalSymlinks, mountedUUID
	mountinfoPath, sysFSBtrfs = info, filepath.Join(dir, "sys")
	evalSymlinks = func(p string) (string, error) { return p, nil }
	mountedUUID = func(mountpoint string, id uint64) (string, error) {
		calls = append(calls, asked{mountpoint, id})
		if id == 97 {
			return "", unix.ENOTTY
		}
		return dataUUID, nil
	}
	t.Cleanup(func() { mountinfoPath, sysFSBtrfs, evalSymlinks, mountedUUID = oldInfo, oldSys, oldEval, oldAsk })

	got, err := ListMounts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var uuids, devices []string
	for _, m := range got {
		uuids, devices = append(uuids, m.UUID), append(devices, m.Device)
	}
	if want := []string{rootUUID, dataUUID, dataUUID, ""}; !reflect.DeepEqual(uuids, want) {
		t.Errorf("UUIDs = %v, want %v", uuids, want)
	}
	// The source stays as the kernel shows it, which fstab_bound.go reads.
	if want := []string{"/dev/sda2", missingDiskName, missingDiskName, missingDiskName}; !reflect.DeepEqual(devices, want) {
		t.Errorf("devices = %v, want %v", devices, want)
	}
	if want := []asked{{"/data", 95}, {"/var/www", 96}, {"/gone", 97}}; !reflect.DeepEqual(calls, want) {
		t.Errorf("asked = %v, want %v", calls, want)
	}
}

// A pool mounted only by a subvolume whose source the kernel shows as
// "<missing disk>" gets its top level mounted by a member it still holds.
func TestTopLevelMountsByAPresentMemberWhenTheSourceIsMissing(t *testing.T) {
	stubBlockDevices(t)
	k := raid1Kernel()
	k.devices[1].path = missingDiskName
	// One descriptor of the mount answers every ioctl of the question.
	mountDir, opened := t.TempDir(), []string{}
	oldOpen, oldFD := openMountpoint, ioctlFD
	openMountpoint = func(mountpoint string) (int, error) {
		opened = append(opened, mountpoint)
		return unix.Open(mountDir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	}
	ioctlFD = func(_ int, req uintptr, arg unsafe.Pointer) error { return k.ioctl(mountDir, req, arg) }
	t.Cleanup(func() { openMountpoint, ioctlFD = oldOpen, oldFD })
	stubMounts(t, []Mount{{Mountpoint: "/srv", Device: missingDiskName, UUID: dataUUID, Subvol: "@srv", SubvolID: 256}})
	calls := stubMountFS(t, nil)
	oldDir := TopLevelDir
	TopLevelDir = t.TempDir()
	t.Cleanup(func() { TopLevelDir = oldDir })

	dir, err := TopLevel(context.Background(), dataUUID)
	if err != nil {
		t.Fatalf("TopLevel: %v", err)
	}
	if want := []string{"/dev/sdb1 " + dir + " btrfs subvolid=5"}; !reflect.DeepEqual(*calls, want) {
		t.Fatalf("mounts = %v, want %v", *calls, want)
	}
	if want := []string{"/srv"}; !reflect.DeepEqual(opened, want) {
		t.Fatalf("opened = %v, want %v", opened, want)
	}

	// A mountpoint that now holds another filesystem names no member of
	// this one; the pool's next mount is asked.
	*calls = nil
	stubMounts(t, []Mount{
		{Mountpoint: "/other", Device: missingDiskName, UUID: rootUUID, Subvol: "@", SubvolID: 256},
	})
	if _, err := TopLevel(context.Background(), rootUUID); err == nil || len(*calls) != 0 {
		t.Fatalf("TopLevel through a mount of another filesystem = %v, mounts %v", err, *calls)
	}

	// With no member present there is nothing to mount it by.
	stubMounts(t, []Mount{{Mountpoint: "/srv", Device: missingDiskName, UUID: dataUUID, Subvol: "@srv", SubvolID: 256}})
	k.devices[2].path = missingDiskName
	if _, err := TopLevel(context.Background(), dataUUID); err == nil || len(*calls) != 0 {
		t.Fatalf("TopLevel with every member missing = %v, mounts %v", err, *calls)
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

// stubBlockDevices has every path but a gone one count as a block device.
func stubBlockDevices(t *testing.T, gone ...string) {
	t.Helper()
	old := blockDeviceExists
	blockDeviceExists = func(path string) bool { return !slices.Contains(gone, path) }
	t.Cleanup(func() { blockDeviceExists = old })
}

// A pool mounted on a kernel before 7.1 keeps the old path of a source disk
// that dropped out: its top level is mounted by a member that exists.
func TestTopLevelPassesOverASourceThatIsGone(t *testing.T) {
	k := raid1Kernel()
	mountDir := t.TempDir()
	oldOpen, oldFD := openMountpoint, ioctlFD
	openMountpoint = func(string) (int, error) {
		return unix.Open(mountDir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	}
	ioctlFD = func(_ int, req uintptr, arg unsafe.Pointer) error { return k.ioctl(mountDir, req, arg) }
	t.Cleanup(func() { openMountpoint, ioctlFD = oldOpen, oldFD })
	stubBlockDevices(t, "/dev/sda1")
	stubMounts(t, []Mount{{Mountpoint: "/srv", Device: "/dev/sda1", UUID: dataUUID, Subvol: "@srv", SubvolID: 256}})
	calls := stubMountFS(t, nil)
	oldDir := TopLevelDir
	TopLevelDir = t.TempDir()
	t.Cleanup(func() { TopLevelDir = oldDir })

	dir, err := TopLevel(context.Background(), dataUUID)
	if err != nil {
		t.Fatalf("TopLevel: %v", err)
	}
	if want := []string{"/dev/sdb1 " + dir + " btrfs subvolid=5"}; !reflect.DeepEqual(*calls, want) {
		t.Fatalf("mounts = %v, want %v", *calls, want)
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
	stubBlockDevices(t)
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

// A mount of a subvolume whose name has a space: the root field and the
// subvol option are escaped alike, so SubPath and Subvol must still agree.
func TestParseMountinfoUnescapesTheSubvolume(t *testing.T) {
	got := parseMountinfo("97 44 0:40 /my\\040data /mnt/my rw - btrfs /dev/sdb1 rw,subvolid=260,subvol=/my\\040data\n", func(uint64, string, string) string { return dataUUID })
	if len(got) != 1 || got[0].Subvol != "my data" || got[0].SubPath != got[0].Subvol {
		t.Fatalf("parseMountinfo = %+v", got)
	}
}

// A bind mount of a folder of the top level has subvolume 5 too, but it is
// not the top level: a path from the top level taken from it would name
// another folder.
func TestTopLevelSkipsABindMountOfAFolder(t *testing.T) {
	stubMounts(t, []Mount{
		{Mountpoint: "/mnt/my share", UUID: dataUUID, SubvolID: 5, SubPath: "my share"},
		{Mountpoint: "/data", UUID: dataUUID, SubvolID: 5},
	})
	dir, err := TopLevel(context.Background(), dataUUID)
	if err != nil || dir != "/data" {
		t.Fatalf("TopLevel() = %q, %v, want /data", dir, err)
	}
}
