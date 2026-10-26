package btrfs

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// superblock builds a primary superblock buffer with the given identity.
func superblock(uuid [16]byte, label string, devid, numDevices, totalBytes uint64) []byte {
	b := make([]byte, superSize)
	copy(b[superFSID:], uuid[:])
	binary.LittleEndian.PutUint64(b[superMagicAt:], superMagic)
	binary.LittleEndian.PutUint64(b[superTotalBytes:], totalBytes)
	binary.LittleEndian.PutUint64(b[superNumDevices:], numDevices)
	binary.LittleEndian.PutUint64(b[superDevID:], devid)
	copy(b[superLabel:], label)
	return b
}

// stubExec records invocations and replaces the command with a no-op.
func stubExec(t *testing.T) *[][]string {
	t.Helper()
	var calls [][]string
	old := execCommand
	execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		calls = append(calls, append([]string{name}, args...))
		return exec.CommandContext(ctx, "true")
	}
	t.Cleanup(func() { execCommand = old })
	return &calls
}

// stubControlIoctl records control-device ioctls instead of issuing them.
func stubControlIoctl(t *testing.T, err error) *[]string {
	t.Helper()
	var calls []string
	old := controlIoctl
	controlIoctl = func(req uintptr, device string) error {
		calls = append(calls, map[uintptr]string{iocScanDev: "scan", iocForgetDev: "forget"}[req]+" "+device)
		return err
	}
	t.Cleanup(func() { controlIoctl = old })
	return &calls
}

func TestParseSuperblock(t *testing.T) {
	uuid := [16]byte{0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0xde, 0xf0, 1, 2, 3, 4, 5, 6, 7, 8}
	sb, err := parseSuperblock(superblock(uuid, "tank", 2, 3, 300))
	if err != nil {
		t.Fatal(err)
	}
	want := Superblock{UUID: "12345678-9abc-def0-0102-030405060708", Label: "tank", DevID: 2, NumDevices: 3, TotalBytes: 300}
	if sb != want {
		t.Fatalf("got %+v, want %+v", sb, want)
	}
	if _, err := parseSuperblock(make([]byte, superSize)); !errors.Is(err, errNotBtrfs) {
		t.Fatalf("blank: err = %v", err)
	}
	if _, err := parseSuperblock(nil); err == nil {
		t.Fatal("short buffer accepted")
	}
}

func TestNewUUIDIsVersion4(t *testing.T) {
	u := newUUID()
	if len(u) != 36 || u[14] != '4' || !strings.ContainsRune("89ab", rune(u[19])) || u == newUUID() {
		t.Fatalf("uuid = %q", u)
	}
}

func TestValidateLabel(t *testing.T) {
	for _, bad := range []string{"", "a/b", "a\nb", strings.Repeat("x", labelSize)} {
		if err := ValidateLabel(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if err := ValidateLabel("tank 1"); err != nil {
		t.Fatal(err)
	}
}

func TestCreatePoolFormatsAndRegisters(t *testing.T) {
	if _, err := findCommand("mkfs.btrfs"); err != nil {
		t.Skip("mkfs.btrfs not installed")
	}
	calls := stubExec(t)
	ioctls := stubControlIoctl(t, nil)
	oldSize, oldDiscard := blockDeviceSize, discardDevice
	t.Cleanup(func() { blockDeviceSize, discardDevice = oldSize, oldDiscard })
	blockDeviceSize = func(string) (uint64, error) { return 1 << 30, nil }
	discardDevice = func(_ context.Context, device string, size uint64, advance func(uint64)) error {
		*ioctls = append(*ioctls, "discard "+device)
		advance(size)
		return nil
	}

	uuid, err := CreatePool(context.Background(), "tank", "raid1", "raid1", []string{"/dev/sdb", "/dev/sdc"}, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 {
		t.Fatalf("calls = %v", *calls)
	}
	want := []string{"-L", "tank", "-U", uuid, "-d", "raid1", "-m", "raid1", "-K", "-f", "/dev/sdb", "/dev/sdc"}
	if got := (*calls)[0][1:]; !reflect.DeepEqual(got, want) {
		t.Fatalf("mkfs args = %v, want %v", got, want)
	}
	if !reflect.DeepEqual(*ioctls, []string{"discard /dev/sdb", "discard /dev/sdc", "scan /dev/sdb", "scan /dev/sdc"}) {
		t.Fatalf("ioctls = %v", *ioctls)
	}
}

func TestCreatePoolRefusesBeforeFormatting(t *testing.T) {
	calls := stubExec(t)
	cases := map[string]func() error{
		"too few devices": func() error {
			_, err := CreatePool(context.Background(), "tank", "raid1", "raid1", []string{"/dev/sdb"}, false, nil)
			return err
		},
		"raid5": func() error {
			_, err := CreatePool(context.Background(), "tank", "raid5", "raid1", []string{"/dev/sdb", "/dev/sdc", "/dev/sdd"}, false, nil)
			return err
		},
		"bad label": func() error {
			_, err := CreatePool(context.Background(), "a/b", "single", "dup", []string{"/dev/sdb"}, false, nil)
			return err
		},
		"no devices": func() error {
			_, err := CreatePool(context.Background(), "tank", "single", "dup", nil, false, nil)
			return err
		},
	}
	for name, run := range cases {
		if err := run(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if len(*calls) != 0 {
		t.Fatalf("mkfs ran: %v", *calls)
	}
}

func TestMountPool(t *testing.T) {
	var got []any
	old := mountFS
	mountFS = func(source, target, fstype string, flags uintptr, data string) error {
		got = []any{source, target, fstype, flags, data}
		return nil
	}
	t.Cleanup(func() { mountFS = old })
	stubPoolGeneration(t, dataUUID, 7, "/dev/sdb", "")
	target := filepath.Join(t.TempDir(), "mnt", "tank")

	// fstab-only options are dropped, VFS ones become flags and the rest,
	// such as subvol=, reaches btrfs.
	options := []string{"defaults", "noatime", "nofail", "x-systemd.device-timeout=10s", "subvol=@data", "compress=zstd"}
	if err := MountPool(context.Background(), "/dev/sdb", target, options, true); err != nil {
		t.Fatal(err)
	}
	want := []any{"/dev/sdb", target, "btrfs", uintptr(unix.MS_NOATIME), "subvol=@data,compress=zstd,degraded"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mount = %v, want %v", got, want)
	}
	if states, err := loadPoolStates(); err != nil || !reflect.DeepEqual(states[dataUUID], poolState{Generation: 7, Degraded: true, Present: []uint64{1}}) {
		t.Fatalf("state = %+v, %v; want generation 7, degraded on devid 1", states, err)
	}
	if info, err := os.Stat(target); err != nil || !info.IsDir() {
		t.Fatalf("mountpoint not created: %v", err)
	}
}

func TestWipeDevice(t *testing.T) {
	ioctls := stubControlIoctl(t, unix.ENOENT)
	device := filepath.Join(t.TempDir(), "disk")
	image := make([]byte, superOffset+superSize)
	copy(image[superOffset:], superblock([16]byte{1}, "tank", 1, 1, 0))
	if err := os.WriteFile(device, image, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := WipeDevice(device); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(device)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseSuperblock(after[superOffset:]); !errors.Is(err, errNotBtrfs) {
		t.Fatalf("magic survived: %v", err)
	}
	if string(after[superOffset+superLabel:superOffset+superLabel+4]) != "tank" {
		t.Fatal("more than the magic was overwritten")
	}
	if !reflect.DeepEqual(*ioctls, []string{"forget " + device}) {
		t.Fatalf("ioctls = %v", *ioctls)
	}

	// A device that is not btrfs is refused untouched.
	if err := WipeDevice(device); !errors.Is(err, errNotBtrfs) {
		t.Fatalf("second wipe: err = %v", err)
	}
}

func TestFindOfflinePools(t *testing.T) {
	stubMounts(t, []Mount{{Mountpoint: "/data", UUID: "mounted-uuid", SubvolID: 5}})
	old := readSuperblock
	readSuperblock = func(device string) (Superblock, error) {
		switch device {
		case "/dev/sdb":
			return Superblock{UUID: "pool-uuid", Label: "tank", NumDevices: 3, TotalBytes: 300, Generation: 8}, nil
		case "/dev/sdc":
			return Superblock{UUID: "pool-uuid", Label: "tank", NumDevices: 3, TotalBytes: 300, Generation: 9}, nil
		case "/dev/sdd":
			return Superblock{UUID: "mounted-uuid", Label: "data", NumDevices: 1}, nil
		}
		return Superblock{}, errNotBtrfs
	}
	t.Cleanup(func() { readSuperblock = old })

	pools, err := FindOfflinePools(context.Background(), []string{"/dev/sdb", "/dev/sdd", "/dev/sde", "/dev/sdc"})
	if err != nil {
		t.Fatal(err)
	}
	want := []BtrfsOfflinePool{{UUID: "pool-uuid", Label: "tank", NumDevices: 3, Devices: []string{"/dev/sdb", "/dev/sdc"}, TotalBytes: 300, devids: []uint64{0, 0}, generations: []uint64{8, 9}}}
	if !reflect.DeepEqual(pools, want) {
		t.Fatalf("got %+v, want %+v", pools, want)
	}
}

func TestBlockDevicesSkipsEmptyAndVirtual(t *testing.T) {
	dir := t.TempDir()
	for name, size := range map[string]string{"sda": "1000", "sda1": "500", "ram0": "10", "zram0": "10", "sr0": "10", "sdb": "0", "loop0": "20", "dm-0": "30"} {
		if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name, "size"), []byte(size+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	old := sysBlockDir
	sysBlockDir = dir
	t.Cleanup(func() { sysBlockDir = old })
	got := BlockDevices()
	if want := []string{"/dev/dm-0", "/dev/loop0", "/dev/sda", "/dev/sda1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
