package storage

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

// fakeHostTree is a temp tree standing in for /sys, /run/udev, /proc and /dev. It
// points every reader seam at itself and restores them on cleanup.
type fakeHostTree struct {
	t    *testing.T
	root string
}

func fakeHost(t *testing.T) *fakeHostTree {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := &fakeHostTree{t: t, root: root}
	for _, d := range []string{"sys/class/block", "sys/devices", "run/udev/data", "dev"} {
		h.mkdir(d)
	}
	oldSys, oldUdev, oldMountinfo, oldSwaps, oldDev, oldFstab := sysClassBlockRoot, udevDataRoot, procMountinfo, procSwaps, devRoot, fstabPath
	t.Cleanup(func() {
		sysClassBlockRoot, udevDataRoot, procMountinfo, procSwaps, devRoot, fstabPath = oldSys, oldUdev, oldMountinfo, oldSwaps, oldDev, oldFstab
	})
	sysClassBlockRoot = filepath.Join(root, "sys/class/block")
	udevDataRoot = filepath.Join(root, "run/udev/data")
	procMountinfo = filepath.Join(root, "mountinfo")
	procSwaps = filepath.Join(root, "swaps")
	devRoot = filepath.Join(root, "dev")
	fstabPath = filepath.Join(root, "fstab")
	return h
}

func (h *fakeHostTree) mkdir(rel string) {
	h.t.Helper()
	if err := os.MkdirAll(filepath.Join(h.root, rel), 0o755); err != nil {
		h.t.Fatal(err)
	}
}

func (h *fakeHostTree) write(rel, content string) {
	h.t.Helper()
	h.mkdir(filepath.Dir(rel))
	if err := os.WriteFile(filepath.Join(h.root, rel), []byte(content), 0o644); err != nil {
		h.t.Fatal(err)
	}
}

// add creates /sys/class/block/<name> as a symlink into sys/devices, nested
// under parent for a partition like the kernel does. attrs are files relative
// to the device directory.
func (h *fakeHostTree) add(parent, name, devno string, sectors int, attrs map[string]string) {
	h.t.Helper()
	rel := filepath.Join("sys/devices", parent, name)
	h.mkdir(rel)
	if err := os.Symlink(filepath.Join(h.root, rel), filepath.Join(sysClassBlockRoot, name)); err != nil {
		h.t.Fatal(err)
	}
	h.write(filepath.Join("sys/class/block", name, "dev"), devno+"\n")
	h.write(filepath.Join("sys/class/block", name, "size"), strconv.Itoa(sectors)+"\n")
	for file, content := range attrs {
		h.write(filepath.Join("sys/class/block", name, file), content+"\n")
	}
}

func (h *fakeHostTree) part(parent, name, devno string, number, start, sectors int) {
	h.add(parent, name, devno, sectors, map[string]string{"partition": strconv.Itoa(number), "start": strconv.Itoa(start)})
}

// relate records a slaves/ or holders/ entry of name.
func (h *fakeHostTree) relate(name, dir, other string) {
	h.write(filepath.Join("sys/class/block", name, dir, other), "")
}

func (h *fakeHostTree) udev(devno string, props ...string) {
	h.write(filepath.Join("run/udev/data", "b"+devno), "I:1\n"+"E:"+strings.Join(props, "\nE:")+"\n")
}

func (h *fakeHostTree) mountinfo(lines ...string) {
	h.write("mountinfo", strings.Join(lines, "\n")+"\n")
}
func (h *fakeHostTree) swaps(lines ...string) {
	h.write("swaps", "Filename\t\t\t\tType\t\tSize\t\tUsed\t\tPriority\n"+strings.Join(lines, "\n")+"\n")
}
func (h *fakeHostTree) fstab(lines ...string) { h.write("fstab", strings.Join(lines, "\n")+"\n") }

func byPath(devices []apischema.StorageBlockDevice) map[string]apischema.StorageBlockDevice {
	m := map[string]apischema.StorageBlockDevice{}
	for _, d := range devices {
		m[d.Path] = d
	}
	return m
}

func TestReadBlockDevicesTree(t *testing.T) {
	h := fakeHost(t)
	h.add("", "sda", "8:0", 2000000, map[string]string{"ro": "0", "queue/rotational": "1"})
	h.part("sda", "sda1", "8:1", 1, 2048, 1000000)
	h.part("sda", "sda2", "8:2", 2, 1002048, 100000)
	h.udev("8:1", "ID_FS_TYPE=ext4")
	h.add("", "sdb", "8:16", 1000000, nil)
	h.relate("sdb", "holders", "md0")
	h.add("", "md0", "9:0", 900000, map[string]string{"md/level": "raid1"})
	h.relate("md0", "slaves", "sda3")
	h.relate("md0", "slaves", "sdb")
	h.add("", "dm-0", "253:0", 800000, map[string]string{"dm/name": "vg-root", "dm/uuid": "LVM-x"})
	h.relate("dm-0", "slaves", "md0")
	h.add("", "nvme0n1", "259:0", 4000000, map[string]string{"queue/logical_block_size": "512"})
	h.part("nvme0n1", "nvme0n1p2", "259:2", 2, 2048, 3000000)
	h.udev("259:2", "ID_FS_TYPE=btrfs")
	h.add("", "loop0", "7:0", 100, nil)
	h.add("", "ram0", "1:0", 100, nil)
	h.add("", "sr0", "11:0", 0, nil)
	h.add("", "nvme0c0n1", "259:9", 100, map[string]string{"hidden": "1"})
	h.mountinfo(
		`30 1 8:1 / /data rw,relatime - ext4 /dev/sda1 rw`,
		`44 1 0:31 / / rw,relatime - btrfs /dev/nvme0n1p2 rw,subvol=/`,
		`45 1 0:22 / /proc rw - proc proc rw`,
	)
	h.swaps("/dev/sda2                               partition\t1000\t0\t-2")

	devices, err := ListBlockDevices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := byPath(devices)
	for _, absent := range []string{"/dev/loop0", "/dev/ram0", "/dev/nvme0c0n1"} {
		if _, ok := got[absent]; ok {
			t.Errorf("%s must be skipped", absent)
		}
	}
	if len(devices) != 9 {
		t.Fatalf("want 9 devices, got %v", slices.Sorted(maps.Keys(got)))
	}
	for _, d := range devices {
		if d.Parents == nil || d.Mountpoints == nil {
			t.Errorf("%s has a nil slice: %#v", d.Path, d)
		}
	}

	type summary struct {
		Name, KernelName, Type, FSType, Busy string
		PartNumber                           int
		StartBytes, SizeBytes                uint64
		Mountpoints, Parents                 []string
	}
	for path, want := range map[string]summary{
		"/dev/sda":            {"sda", "sda", "disk", "", "", 0, 0, 2000000 * 512, []string{}, []string{}},
		"/dev/sda1":           {"sda1", "sda1", "part", "ext4", "Mounted at /data", 1, 2048 * 512, 1000000 * 512, []string{"/data"}, []string{"/dev/sda"}},
		"/dev/sda2":           {"sda2", "sda2", "part", "", "Active swap", 2, 1002048 * 512, 100000 * 512, []string{}, []string{"/dev/sda"}},
		"/dev/sdb":            {"sdb", "sdb", "disk", "", "Used by /dev/md0", 0, 0, 1000000 * 512, []string{}, []string{}},
		"/dev/md0":            {"md0", "md0", "raid1", "", "", 0, 0, 900000 * 512, []string{}, []string{"/dev/sda3", "/dev/sdb"}},
		"/dev/mapper/vg-root": {"vg-root", "dm-0", "lvm", "", "", 0, 0, 800000 * 512, []string{}, []string{"/dev/md0"}},
		"/dev/sr0":            {"sr0", "sr0", "rom", "", "", 0, 0, 0, []string{}, []string{}},
		"/dev/nvme0n1":        {"nvme0n1", "nvme0n1", "disk", "", "", 0, 0, 4000000 * 512, []string{}, []string{}},
		"/dev/nvme0n1p2":      {"nvme0n1p2", "nvme0n1p2", "part", "btrfs", "Mounted at /", 2, 2048 * 512, 3000000 * 512, []string{"/"}, []string{"/dev/nvme0n1"}},
	} {
		d := got[path]
		have := summary{d.Name, d.KernelName, d.Type, d.FSType, d.Busy, d.PartNumber, d.StartBytes, d.SizeBytes, d.Mountpoints, d.Parents}
		if !reflect.DeepEqual(have, want) {
			t.Errorf("%s:\n have %+v\n want %+v", path, have, want)
		}
	}
}

func TestReadMountinfoUnescapes(t *testing.T) {
	h := fakeHost(t)
	h.add("", "sda", "8:0", 2000, nil)
	h.part("sda", "sda1", "8:1", 1, 2048, 1000)
	h.mountinfo(`31 1 8:1 / /mnt/my\040disk rw - ext4 /dev/sda1 rw`)
	devices, err := ListBlockDevices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if mp := byPath(devices)["/dev/sda1"].Mountpoints; !reflect.DeepEqual(mp, []string{"/mnt/my disk"}) {
		t.Fatalf("mountpoints = %q", mp)
	}
}

func TestUdevEncodedLabel(t *testing.T) {
	h := fakeHost(t)
	h.add("", "sda", "8:0", 2000, nil)
	h.part("sda", "sda1", "8:1", 1, 2048, 1000)
	h.udev("8:1", `ID_FS_LABEL_ENC=my\x20data`, "ID_FS_UUID_ENC=u-1", "ID_PART_ENTRY_UUID=p-1", "ID_PART_ENTRY_NAME=boot", "ID_PART_ENTRY_SCHEME=gpt")
	// Model: ID_MODEL_ENC, then ID_MODEL, then sysfs.
	h.add("", "sdb", "8:16", 2000, map[string]string{"device/model": "sysfs model "})
	h.udev("8:16", `ID_MODEL_ENC=Enc\x20Model\x20\x20`, "ID_MODEL=plain", "SCSI_IDENT_SERIAL=s1", "ID_SERIAL=s5", "ID_PART_TABLE_TYPE=gpt")
	h.add("", "sdc", "8:32", 2000, map[string]string{"device/model": "sysfs model ", "device/serial": " sys-serial "})
	h.udev("8:32", "ID_MODEL=plain", "ID_SCSI_SERIAL=s2", "ID_SERIAL_SHORT=s3", "ID_SERIAL=s5")
	h.add("", "sdd", "8:48", 2000, map[string]string{"device/model": "sysfs model ", "device/serial": " sys-serial "})
	h.udev("8:48", "ID_SERIAL_SHORT=s3", "ID_SERIAL=s5")
	h.add("", "sde", "8:64", 2000, map[string]string{"device/model": "sysfs model ", "device/serial": " sys-serial "})
	h.udev("8:64", "ID_SERIAL=s5")
	// No udev file at all: sysfs only, no error.
	h.add("", "sdf", "8:80", 2000, map[string]string{"device/model": "sysfs model ", "device/serial": " sys-serial "})
	h.add("", "sdg", "8:96", 2000, nil)

	devices, err := ListBlockDevices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := byPath(devices)
	p := got["/dev/sda1"]
	if p.Label != "my data" || p.UUID != "u-1" || p.PartUUID != "p-1" || p.PartLabel != "boot" || p.PTType != "gpt" {
		t.Errorf("sda1 = %#v", p)
	}
	for path, want := range map[string][2]string{
		"/dev/sdb": {"Enc Model", "s1"},
		"/dev/sdc": {"plain", "s2"},
		"/dev/sdd": {"sysfs model", "s3"},
		"/dev/sde": {"sysfs model", "s5"},
		"/dev/sdf": {"sysfs model", "sys-serial"},
		"/dev/sdg": {"", ""},
	} {
		if d := got[path]; d.Model != want[0] || d.Serial != want[1] {
			t.Errorf("%s model/serial = %q/%q, want %q/%q", path, d.Model, d.Serial, want[0], want[1])
		}
	}
	if got["/dev/sdb"].PTType != "gpt" || got["/dev/sdg"].FSType != "" {
		t.Errorf("sdb/sdg = %#v / %#v", got["/dev/sdb"], got["/dev/sdg"])
	}
}

func TestTransport(t *testing.T) {
	// scsi gives a disk a device link under host0 and the host's class entries.
	scsi := func(h *fakeHostTree, name string, extra ...string) {
		for _, class := range extra {
			h.mkdir(filepath.Join("sys/class", class))
		}
		h.mkdir("sys/devices/pci0/host0/target0:0:0/0:0:0:0")
		if err := os.Symlink(filepath.Join(h.root, "sys/devices/pci0/host0/target0:0:0/0:0:0:0"), filepath.Join(sysClassBlockRoot, name, "device")); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		name, kname, want string
		setup             func(h *fakeHostTree, kname string)
	}{
		{"spi", "sda", "spi", func(h *fakeHostTree, k string) { scsi(h, k, "spi_host/host0") }},
		{"fc", "sda", "fc", func(h *fakeHostTree, k string) { scsi(h, k, "fc_host/host0") }},
		{"fcoe", "sda", "fcoe", func(h *fakeHostTree, k string) { scsi(h, k, "fc_host/host0", "fcoe_host/host0") }},
		{"sas host", "sda", "sas", func(h *fakeHostTree, k string) { scsi(h, k, "sas_host/host0") }},
		{"sas device", "sda", "sas", func(h *fakeHostTree, k string) {
			scsi(h, k)
			h.write("sys/devices/pci0/host0/target0:0:0/0:0:0:0/sas_device", "")
		}},
		{"sbp", "sda", "sbp", func(h *fakeHostTree, k string) {
			scsi(h, k)
			h.write("sys/devices/pci0/host0/target0:0:0/0:0:0:0/ieee1394_id", "")
		}},
		{"iscsi", "sda", "iscsi", func(h *fakeHostTree, k string) { scsi(h, k, "iscsi_host/host0") }},
		{"usb", "sda", "usb", func(h *fakeHostTree, k string) {
			h.mkdir("sys/devices/pci0/usb1/1-1/host6/target6:0:0/6:0:0:0")
			if err := os.Symlink(filepath.Join(h.root, "sys/devices/pci0/usb1/1-1/host6/target6:0:0/6:0:0:0"), filepath.Join(sysClassBlockRoot, k, "device")); err != nil {
				t.Fatal(err)
			}
		}},
		{"sata", "sda", "sata", func(h *fakeHostTree, k string) {
			scsi(h, k)
			h.write("sys/class/scsi_host/host0/proc_name", "ahci\n")
		}},
		{"sata prefix", "sda", "sata", func(h *fakeHostTree, k string) {
			scsi(h, k)
			h.write("sys/class/scsi_host/host0/proc_name", "sata_nv\n")
		}},
		{"ata", "sda", "ata", func(h *fakeHostTree, k string) {
			scsi(h, k)
			h.write("sys/class/scsi_host/host0/proc_name", "pata_acpi\n")
		}},
		{"scsi unknown", "sda", "", func(h *fakeHostTree, k string) {
			scsi(h, k)
			h.write("sys/class/scsi_host/host0/proc_name", "megaraid\n")
		}},
		{"nvme", "nvme0n1", "nvme", func(*fakeHostTree, string) {}},
		{"virtio", "vda", "virtio", func(*fakeHostTree, string) {}},
		{"mmc", "mmcblk0", "mmc", func(*fakeHostTree, string) {}},
		{"unknown", "xvda", "", func(*fakeHostTree, string) {}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := fakeHost(t)
			h.add("", tc.kname, "8:0", 2000, nil)
			tc.setup(h, tc.kname)
			devices, err := ListBlockDevices(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if len(devices) != 1 || devices[0].Transport != tc.want {
				t.Fatalf("transport = %#v, want %q", devices, tc.want)
			}
		})
	}
}

func TestInFstab(t *testing.T) {
	h := fakeHost(t)
	h.add("", "sda", "8:0", 2000, nil)
	h.part("sda", "sda1", "8:1", 1, 2048, 1000)
	h.udev("8:1", "ID_FS_UUID_ENC=ABCD-1234", `ID_FS_LABEL_ENC=my\x20data`, "ID_PART_ENTRY_UUID=1eae21cc-61ec", `ID_PART_ENTRY_NAME=my\x20part`)
	h.write("dev/sda1", "")
	h.mkdir("dev/disk/by-id")
	if err := os.Symlink("../../sda1", filepath.Join(h.root, "dev/disk/by-id/x")); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		source string
		want   bool
	}{
		{"UUID=ABCD-1234", true},
		{"UUID=abcd-1234", true},
		{"PARTUUID=1EAE21CC-61EC", true},
		{`LABEL=my\040data`, true}, // fstab(5) escapes the space as octal
		{`PARTLABEL=my\040part`, true},
		{"LABEL=data", false},
		{"PARTLABEL=mypart", false},
		{"/dev/sda1", true},
		{"/dev/disk/by-id/x", true},
		{"UUID=other", false},
		{"/dev/sdb1", false},
	} {
		h.fstab("# comment", tc.source+" /mnt ext4 defaults 0 2")
		devices, err := ListBlockDevices(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		got := byPath(devices)
		if got["/dev/sda1"].InFstab != tc.want {
			t.Errorf("%s: InFstab = %v, want %v", tc.source, got["/dev/sda1"].InFstab, tc.want)
		}
		if got["/dev/sda"].InFstab {
			t.Errorf("%s: sda must not be InFstab", tc.source)
		}
	}
}

func TestInFstabRepeatedSwapLines(t *testing.T) {
	h := fakeHost(t)
	h.add("", "sda", "8:0", 2000, nil)
	h.part("sda", "sda1", "8:1", 1, 2048, 500)
	h.part("sda", "sda2", "8:2", 2, 4096, 500)
	h.udev("8:1", "ID_FS_UUID_ENC=aaa")
	h.udev("8:2", "ID_FS_UUID_ENC=bbb")
	h.fstab("UUID=aaa none swap sw 0 0", "UUID=bbb none swap sw 0 0")
	devices, err := ListBlockDevices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := byPath(devices)
	if !got["/dev/sda1"].InFstab || !got["/dev/sda2"].InFstab {
		t.Fatalf("both swap lines must count: sda1=%v sda2=%v", got["/dev/sda1"].InFstab, got["/dev/sda2"].InFstab)
	}
}

func TestListBlockDevicesCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ListBlockDevices(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}

func TestAddFreeSpace(t *testing.T) {
	h := fakeHost(t)
	// A 4Kn disk: sysfs still counts 512-byte units. sdd has a GPT but no
	// logical_block_size entry; sdc is not GPT at all.
	h.write("sys/class/block/nvme0n1/queue/logical_block_size", "512")
	h.write("sys/class/block/sdb/queue/logical_block_size", "4096")
	h.write("sys/class/block/sde/queue/logical_block_size", "512")
	// The host from the field: EFI, a 64 GiB md member, then the rest free.
	devices := []apischema.StorageBlockDevice{
		{Path: "/dev/nvme0n1", KernelName: "nvme0n1", Type: "disk", SizeBytes: 1024209543168, PTType: "gpt"},
		{Path: "/dev/nvme0n1p1", KernelName: "nvme0n1p1", Type: "part", Parents: []string{"/dev/nvme0n1"}, StartBytes: 2048 * 512, SizeBytes: 2097152 * 512},
		{Path: "/dev/nvme0n1p2", KernelName: "nvme0n1p2", Type: "part", Parents: []string{"/dev/nvme0n1"}, StartBytes: 2099200 * 512, SizeBytes: 134322176 * 512},
		{Path: "/dev/md0", KernelName: "md0", Type: "raid1", Parents: []string{"/dev/nvme0n1p2"}},
		// 1 MiB EFI at LBA 256-511, the rest free.
		{Path: "/dev/sdb", KernelName: "sdb", Type: "disk", SizeBytes: 1073741824, PTType: "gpt"},
		{Path: "/dev/sdb1", KernelName: "sdb1", Type: "part", Parents: []string{"/dev/sdb"}, StartBytes: 2048 * 512, SizeBytes: 2048 * 512},
		// 4 GiB with a 1 GiB partition in the middle: a gap on each side.
		{Path: "/dev/sde", KernelName: "sde", Type: "disk", SizeBytes: 4 << 30, PTType: "gpt"},
		{Path: "/dev/sde1", KernelName: "sde1", Type: "part", Parents: []string{"/dev/sde"}, StartBytes: 1 << 30, SizeBytes: 1 << 30},
		{Path: "/dev/sdc", KernelName: "sdc", Type: "disk", SizeBytes: 1073741824, PTType: "dos"},
		{Path: "/dev/sdd", KernelName: "sdd", Type: "disk", SizeBytes: 1073741824, PTType: "gpt"},
	}
	addFreeSpace(devices)
	got := byPath(devices)
	wantFree := map[string]uint64{
		"/dev/nvme0n1": (2000408575 - 136421376 + 1) * 512, // LBA 136421376 up to the last aligned sector
		"/dev/sdb":     (261888 - 512) * 4096,              // 4Kn: usable LBAs 6-262138, EFI at 256-511
		"/dev/sde":     (8386560 - 4194304) * 512,          // the larger of the two gaps
	}
	for path, d := range got {
		if d.FreeBytes != wantFree[path] {
			t.Errorf("%s FreeBytes = %d, want %d", path, d.FreeBytes, wantFree[path])
		}
	}
	if r := got["/dev/sde"].FreeRegions; !reflect.DeepEqual(r, []apischema.StorageRegion{
		{StartBytes: 1 << 20, SizeBytes: (2097152 - 2048) * 512},
		{StartBytes: 4194304 * 512, SizeBytes: (8386560 - 4194304) * 512},
	}) {
		t.Errorf("sde regions = %#v", r)
	}
	if r := got["/dev/sdb"].FreeRegions; !reflect.DeepEqual(r, []apischema.StorageRegion{{StartBytes: 512 * 4096, SizeBytes: wantFree["/dev/sdb"]}}) {
		t.Errorf("sdb regions = %#v", r)
	}
	if r := got["/dev/nvme0n1"].FreeRegions; len(r) != 1 || r[0].StartBytes != 136421376*512 || r[0].SizeBytes != wantFree["/dev/nvme0n1"] {
		t.Errorf("nvme0n1 regions = %#v", r)
	}
	for _, path := range []string{"/dev/sdc", "/dev/sdd", "/dev/nvme0n1p1", "/dev/md0"} {
		if got[path].FreeRegions != nil {
			t.Errorf("%s must have no regions: %#v", path, got[path].FreeRegions)
		}
	}
}

// lsblkDevice is one node of lsblk's JSON tree, kept only for the parity test.
type lsblkDevice struct {
	Name        string         `json:"name"`
	KernelName  string         `json:"kname"`
	Path        string         `json:"path"`
	Type        string         `json:"type"`
	Size        jsontext.Value `json:"size"`
	FSType      string         `json:"fstype"`
	Mountpoints []string       `json:"mountpoints"`
	Model       string         `json:"model"`
	Serial      string         `json:"serial"`
	Transport   string         `json:"tran"`
	ReadOnly    bool           `json:"ro"`
	PTType      string         `json:"pttype"`
	Children    []lsblkDevice  `json:"children"`
}

// parseLsblkTree flattens lsblk's tree the way the old reader did: one entry
// per path, every parent seen, loop devices dropped, "[SWAP]" ignored.
func parseLsblkTree(data []byte) (map[string]apischema.StorageBlockDevice, error) {
	var report struct {
		Devices []lsblkDevice `json:"blockdevices"`
	}
	if err := json.Unmarshal(data, &report, json.MatchCaseInsensitiveNames(true)); err != nil {
		return nil, fmt.Errorf("parse block devices: %w", err)
	}
	devices := map[string]*apischema.StorageBlockDevice{}
	for _, raw := range report.Devices {
		if err := collectLsblk(raw, "", devices); err != nil {
			return nil, err
		}
	}
	result := map[string]apischema.StorageBlockDevice{}
	for path, device := range devices {
		slices.Sort(device.Parents)
		slices.Sort(device.Mountpoints)
		result[path] = *device
	}
	return result, nil
}

func collectLsblk(raw lsblkDevice, parent string, devices map[string]*apischema.StorageBlockDevice) error {
	if raw.Type == "loop" {
		return nil
	}
	device := devices[raw.Path]
	if device == nil {
		size, err := strconv.ParseUint(strings.Trim(string(raw.Size), `"`), 10, 64)
		if err != nil {
			return fmt.Errorf("parse size of %s: %w", raw.Path, err)
		}
		device = &apischema.StorageBlockDevice{
			Path: raw.Path, Name: raw.Name, KernelName: raw.KernelName,
			Type: raw.Type, SizeBytes: size, FSType: raw.FSType,
			Model: strings.TrimSpace(raw.Model), Serial: strings.TrimSpace(raw.Serial),
			Transport: raw.Transport, ReadOnly: raw.ReadOnly, PTType: raw.PTType,
			Mountpoints: []string{}, Parents: []string{},
		}
		devices[raw.Path] = device
	}
	if parent != "" && !slices.Contains(device.Parents, parent) {
		device.Parents = append(device.Parents, parent)
	}
	for _, mount := range raw.Mountpoints {
		if strings.HasPrefix(mount, "/") && !slices.Contains(device.Mountpoints, mount) {
			device.Mountpoints = append(device.Mountpoints, mount)
		}
	}
	for _, child := range raw.Children {
		if err := collectLsblk(child, raw.Path, devices); err != nil {
			return err
		}
	}
	return nil
}

// TestNativeInventoryMatchesLsblk holds the native reader to what lsblk
// reports on the machine running the test. It skips where lsblk or the udev
// database is missing.
func TestNativeInventoryMatchesLsblk(t *testing.T) {
	if _, err := exec.LookPath("lsblk"); err != nil {
		t.Skip("lsblk not installed")
	}
	if _, err := os.Stat(udevDataRoot); err != nil {
		t.Skip("no udev database")
	}
	out, err := exec.Command("lsblk", "--json", "--bytes", "--tree", "--output", "NAME,KNAME,PATH,TYPE,SIZE,FSTYPE,MOUNTPOINTS,MODEL,SERIAL,TRAN,RO,PTTYPE").Output()
	if err != nil {
		t.Skipf("lsblk failed: %v", err)
	}
	want, err := parseLsblkTree(out)
	if err != nil {
		t.Fatal(err)
	}
	devices, err := ListBlockDevices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	type compared struct {
		Path, Name, KernelName, Type, FSType, Model, Serial, Transport, PTType string
		SizeBytes                                                              uint64
		Mountpoints, Parents                                                   []string
		ReadOnly                                                               bool
	}
	pick := func(d apischema.StorageBlockDevice) compared {
		return compared{d.Path, d.Name, d.KernelName, d.Type, d.FSType, d.Model, d.Serial, d.Transport, d.PTType, d.SizeBytes, d.Mountpoints, d.Parents, d.ReadOnly}
	}
	got := byPath(devices)
	for path, w := range want {
		if g, ok := got[path]; !ok {
			t.Errorf("%s: missing from the native inventory", path)
		} else if !reflect.DeepEqual(pick(g), pick(w)) {
			t.Errorf("%s differs:\n native %+v\n lsblk  %+v", path, pick(g), pick(w))
		}
	}
	for path := range got {
		if _, ok := want[path]; !ok {
			t.Errorf("%s: native inventory lists a device lsblk does not", path)
		}
	}
	checkDriveParity(t)
}

func checkDriveParity(t *testing.T) {
	t.Helper()
	out, err := exec.Command("lsblk", "-d", "-b", "-J", "-o", "NAME,MODEL,SERIAL,SIZE,RO,ROTA,TYPE,TRAN,VENDOR").Output()
	if err != nil {
		t.Skipf("lsblk -d failed: %v", err)
	}
	var report struct {
		Devices []struct {
			Name      string         `json:"name"`
			Model     string         `json:"model"`
			Serial    string         `json:"serial"`
			Size      jsontext.Value `json:"size"`
			RO        bool           `json:"ro"`
			Rota      bool           `json:"rota"`
			Type      string         `json:"type"`
			Transport string         `json:"tran"`
			Vendor    string         `json:"vendor"`
		} `json:"blockdevices"`
	}
	if err = json.Unmarshal(out, &report, json.MatchCaseInsensitiveNames(true)); err != nil {
		t.Fatal(err)
	}
	drives, err := FetchDriveInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	str := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	type compared struct {
		Name, Model, Serial, Transport, Vendor string
		SizeBytes                              uint64
		RO, Rotational                         bool
	}
	got := map[string]compared{}
	for _, d := range drives {
		got[d.Name] = compared{d.Name, d.Model, str(d.Serial), str(d.Type), str(d.Vendor), d.SizeBytes, d.RO, d.Rotational}
	}
	want := map[string]compared{}
	for _, w := range report.Devices {
		if w.Type == "disk" {
			size, _ := strconv.ParseUint(strings.Trim(string(w.Size), `"`), 10, 64)
			want[w.Name] = compared{w.Name, strings.TrimSpace(w.Model), strings.TrimSpace(w.Serial), w.Transport, strings.TrimSpace(w.Vendor), size, w.RO, w.Rota}
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("drives differ:\n native %+v\n lsblk  %+v", got, want)
	}
}
