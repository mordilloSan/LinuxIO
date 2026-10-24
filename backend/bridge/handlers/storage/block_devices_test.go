package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseBlockDevices(t *testing.T) {
	data := []byte(`{"blockdevices":[
		{"name":"sdb","kname":"sdb","path":"/dev/sdb","type":"disk","size":"2000","model":" Disk B ","children":[
			{"name":"md0","kname":"md0","path":"/dev/md0","type":"raid1","size":1000,"mountpoints":[null,"/data","/data"]}
		]},
		{"name":"sda","kname":"sda","path":"/dev/sda","type":"disk","size":2000,"children":[
			{"name":"sda1","kname":"sda1","path":"/dev/sda1","type":"part","size":1000,"children":[
				{"name":"md0","kname":"md0","path":"/dev/md0","type":"raid1","size":1000,"mountpoints":["/data"]}
			]}
		]},
		{"name":"loop0","type":"loop","size":500}
	]}`)
	devices, _, err := parseBlockDevices(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 4 {
		t.Fatalf("want 4 unique devices, got %#v", devices)
	}
	raid := devices[0]
	if raid.Path != "/dev/md0" || !reflect.DeepEqual(raid.Parents, []string{"/dev/sda1", "/dev/sdb"}) || !reflect.DeepEqual(raid.Mountpoints, []string{"/data"}) {
		t.Fatalf("lost RAID parents or mounts: %#v", raid)
	}
	if devices[3].SizeBytes != 2000 || devices[3].Model != "Disk B" || devices[3].Parents == nil || devices[3].Mountpoints == nil {
		t.Fatalf("incorrect metadata or nil arrays: %#v", devices[3])
	}
	for _, input := range []string{`{`, `{"blockdevices":[{"path":"/dev/sda","kname":"sda","size":"bad"}]}`, `{"blockdevices":[{"size":0}]}`} {
		if _, _, parseErr := parseBlockDevices([]byte(input)); parseErr == nil {
			t.Errorf("expected error for %s", input)
		}
	}
	empty, _, err := parseBlockDevices([]byte(`{"blockdevices":[]}`))
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty inventory: %#v, %v", empty, err)
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
	root := t.TempDir()
	write := func(kname, file, value string) {
		path := filepath.Join(root, kname, file)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(value+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// The host from the field: EFI, a 64 GiB md member, then the rest free.
	write("nvme0n1", "queue/logical_block_size", "512")
	write("nvme0n1p1", "start", "2048")
	write("nvme0n1p1", "size", "2097152")
	write("nvme0n1p2", "start", "2099200")
	write("nvme0n1p2", "size", "134322176")
	// A 4Kn disk: sysfs still counts 512-byte units. 1 MiB EFI, rest free.
	write("sdb", "queue/logical_block_size", "4096")
	write("sdb1", "start", "2048")
	write("sdb1", "size", "2048")
	// sdd has a GPT but no sysfs entry; sdc is not GPT at all.
	t.Cleanup(func(old string) func() { return func() { sysClassBlockRoot = old } }(sysClassBlockRoot))
	sysClassBlockRoot = root

	data := []byte(`{"blockdevices":[
		{"name":"nvme0n1","kname":"nvme0n1","path":"/dev/nvme0n1","type":"disk","size":1024209543168,"pttype":"gpt","children":[
			{"name":"nvme0n1p1","kname":"nvme0n1p1","path":"/dev/nvme0n1p1","type":"part","size":1073741824},
			{"name":"nvme0n1p2","kname":"nvme0n1p2","path":"/dev/nvme0n1p2","type":"part","size":68772953088,"children":[
				{"name":"md0","kname":"md0","path":"/dev/md0","type":"raid1","size":68772953088}
			]}
		]},
		{"name":"sdb","kname":"sdb","path":"/dev/sdb","type":"disk","size":1073741824,"pttype":"gpt","children":[
			{"name":"sdb1","kname":"sdb1","path":"/dev/sdb1","type":"part","size":1048576}
		]},
		{"name":"sdc","kname":"sdc","path":"/dev/sdc","type":"disk","size":1073741824,"pttype":"dos"},
		{"name":"sdd","kname":"sdd","path":"/dev/sdd","type":"disk","size":1073741824,"pttype":"gpt"}
	]}`)
	devices, gptDisks, err := parseBlockDevices(data)
	if err != nil {
		t.Fatal(err)
	}
	addFreeSpace(devices, gptDisks)
	got := map[string]uint64{}
	for _, d := range devices {
		got[d.Path] = d.FreeBytes
	}
	want := map[string]uint64{
		"/dev/nvme0n1": (2000408575 - 136421376 + 1) * 512, // LBA 136421376 up to the last aligned sector
		"/dev/sdb":     (261888 - 512) * 4096,              // 4Kn: usable LBAs 6-262138, EFI at 256-511
		"/dev/md0":     0, "/dev/nvme0n1p1": 0, "/dev/nvme0n1p2": 0, "/dev/sdb1": 0, "/dev/sdc": 0, "/dev/sdd": 0,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("free bytes = %v, want %v", got, want)
	}
}
