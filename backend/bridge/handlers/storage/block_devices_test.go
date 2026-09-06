package storage

import (
	"context"
	"errors"
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
	devices, err := parseBlockDevices(data)
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
		if _, parseErr := parseBlockDevices([]byte(input)); parseErr == nil {
			t.Errorf("expected error for %s", input)
		}
	}
	empty, err := parseBlockDevices([]byte(`{"blockdevices":[]}`))
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
