package storage

import (
	"encoding/json"
	"testing"
)

func TestBuildDriveInfoReportsRotational(t *testing.T) {
	// Trimmed `lsblk -d -O -J` output: one NVMe SSD and one spinning SATA disk.
	const out = `{"blockdevices":[
		{"name":"nvme0n1","model":"Samsung SSD 980 ","size":"476.9G","ro":false,"rota":false,"type":"disk","tran":"nvme"},
		{"name":"sda","model":"WDC WD40EFRX","size":"3.6T","ro":false,"rota":true,"type":"disk","tran":"sata"}
	]}`
	var parsed LSBLKOutput
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	nvme, hdd := buildDriveInfo(parsed.BlockDevices[0]), buildDriveInfo(parsed.BlockDevices[1])
	if nvme.Rotational || nvme.Model != "Samsung SSD 980" {
		t.Errorf("nvme = %+v, want non-rotational with trimmed model", nvme)
	}
	if !hdd.Rotational {
		t.Errorf("sda = %+v, want rotational", hdd)
	}
}
