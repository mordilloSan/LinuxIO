package btrfs

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// swapfileKernel answers FS_INFO and SPACE_INFO for k on every descriptor,
// and records the requests it was asked.
func swapfileKernel(t *testing.T, k *fakeKernel) *[]uintptr {
	t.Helper()
	var asked []uintptr
	old := ioctlFD
	ioctlFD = func(_ int, req uintptr, arg unsafe.Pointer) error {
		asked = append(asked, req)
		return k.ioctl("", req, arg)
	}
	t.Cleanup(func() { ioctlFD = old })
	return &asked
}

func openTemp(t *testing.T) *os.File {
	t.Helper()
	dir, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dir.Close() })
	return dir
}

func singleKernel(spaces ...spaceInfo) *fakeKernel {
	return &fakeKernel{
		maxID:   1,
		devices: map[uint64]*fakeDevice{1: {path: "/dev/vda3", total: 20 << 30}},
		spaces:  spaces,
	}
}

func TestSwapfileLayout(t *testing.T) {
	metadataDup := spaceInfo{Flags: bgMetadata | bgDup, TotalBytes: 1 << 30}
	systemDup := spaceInfo{Flags: bgSystem | bgDup, TotalBytes: 8 << 20}
	reserve := spaceInfo{Flags: bgGlobalRsv, TotalBytes: 16 << 20}
	for name, c := range map[string]struct {
		k    *fakeKernel
		want bool
	}{
		// What mkfs.btrfs makes on one disk: data single, metadata dup.
		"one device, data single":             {singleKernel(spaceInfo{Flags: bgData, TotalBytes: 8 << 30}, metadataDup, systemDup, reserve), true},
		"one device, data single by its flag": {singleKernel(spaceInfo{Flags: bgData | bgSingle, TotalBytes: 8 << 30}, metadataDup), true},
		"one device, data dup":                {singleKernel(spaceInfo{Flags: bgData | bgDup, TotalBytes: 8 << 30}, metadataDup), false},
		// A conversion that has not finished leaves both profiles.
		"one device, data partly dup": {singleKernel(spaceInfo{Flags: bgData, TotalBytes: 4 << 30}, spaceInfo{Flags: bgData | bgDup, TotalBytes: 1 << 30}), false},
		"mixed block groups single":   {singleKernel(spaceInfo{Flags: bgData | bgMetadata, TotalBytes: 1 << 30}), true},
		"mixed block groups dup":      {singleKernel(spaceInfo{Flags: bgData | bgMetadata | bgDup, TotalBytes: 1 << 30}), false},
		"two devices":                 {raid1Kernel(), false},
	} {
		t.Run(name, func(t *testing.T) {
			swapfileKernel(t, c.k)
			got, err := SwapfileLayout(openTemp(t))
			if err != nil || got != c.want {
				t.Fatalf("SwapfileLayout = %v, %v; want %v", got, err, c.want)
			}
		})
	}
}

// Two devices answer without the space info: the layout is already ruled out.
func TestSwapfileLayoutAsksForTheDevicesFirst(t *testing.T) {
	asked := swapfileKernel(t, raid1Kernel())
	if _, err := SwapfileLayout(openTemp(t)); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(*asked, []uintptr{iocFSInfo}) {
		t.Fatalf("asked %#x, want FS_INFO alone", *asked)
	}
}

func TestSwapfileLayoutReportsTheKernelsRefusal(t *testing.T) {
	for _, req := range []uintptr{iocFSInfo, iocSpaceInfo} {
		k := singleKernel(spaceInfo{Flags: bgData})
		k.fail = map[uintptr]error{req: unix.ENOTTY}
		swapfileKernel(t, k)
		if ok, err := SwapfileLayout(openTemp(t)); ok || !errors.Is(err, unix.ENOTTY) {
			t.Fatalf("%#x failing: SwapfileLayout = %v, %v; want ENOTTY", req, ok, err)
		}
	}
}

func TestCreateSubvolumeAtCreatesThroughTheHandle(t *testing.T) {
	k := &opsKernel{}
	stubIoctls(t, k.ioctl)
	// The folder the fake kernel makes is no subvolume root.
	oldNew := isNewSubvolume
	isNewSubvolume = func(*unix.Statx_t, uint32, time.Time) bool { return true }
	t.Cleanup(func() { isNewSubvolume = oldNew })
	oldUmask := unix.Umask(0o077)
	t.Cleanup(func() { unix.Umask(oldUmask) })
	top := realTop(t, "lib")
	dir, err := os.Open(filepath.Join(top, "lib"))
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()

	if err := CreateSubvolumeAt(dir, "swap"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"create " + filepath.Join(top, "lib") + " swap"}; !slices.Equal(k.calls, want) {
		t.Fatalf("calls = %q, want %q", k.calls, want)
	}
	if info, err := os.Stat(filepath.Join(top, "lib", "swap")); err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("new subvolume: %v, %v; want mode 0755", info, err)
	}
}

func TestCreateSubvolumeAtRefusesAPath(t *testing.T) {
	k := &opsKernel{}
	stubIoctls(t, k.ioctl)
	dir := openTemp(t)
	for _, name := range []string{"", ".", "..", "a/b", "/swap", "a\x00b"} {
		if err := CreateSubvolumeAt(dir, name); !errors.Is(err, ErrInvalidSubvolume) {
			t.Errorf("CreateSubvolumeAt(%q) = %v, want ErrInvalidSubvolume", name, err)
		}
	}
	if len(k.calls) != 0 {
		t.Fatalf("calls = %q, want none", k.calls)
	}
}

// A name taken by anything, a folder of the same name included, is the
// kernel's EEXIST, and nothing is chmodded.
func TestCreateSubvolumeAtReportsATakenName(t *testing.T) {
	stubIoctls(t, func(string, uintptr, unsafe.Pointer) error { return unix.EEXIST })
	if err := CreateSubvolumeAt(openTemp(t), "swap"); !errors.Is(err, unix.EEXIST) {
		t.Fatalf("err = %v, want EEXIST", err)
	}
}

func TestSubvolumeRootInode(t *testing.T) {
	if SubvolumeRootInode != 256 {
		t.Fatalf("SubvolumeRootInode = %d, want 256 (BTRFS_FIRST_FREE_OBJECTID)", SubvolumeRootInode)
	}
}
