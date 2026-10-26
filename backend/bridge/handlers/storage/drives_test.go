package storage

import (
	"context"
	"testing"
)

func TestFetchDriveInfoFromTree(t *testing.T) {
	h := fakeHost(t)
	h.add("", "nvme0n1", "259:0", 1000, map[string]string{"queue/rotational": "0"})
	h.udev("259:0", `ID_MODEL_ENC=Samsung\x20SSD\x20980\x20`)
	h.part("nvme0n1", "nvme0n1p1", "259:1", 1, 2048, 500)
	h.add("", "sda", "8:0", 2000, map[string]string{"queue/rotational": "1", "ro": "1", "device/vendor": "ATA     "})
	h.udev("8:0", "ID_MODEL=WDC WD40EFRX", "ID_SERIAL_SHORT=WD-1")
	h.add("", "md0", "9:0", 1000, map[string]string{"md/level": "raid1"})

	drives, err := FetchDriveInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(drives) != 2 {
		t.Fatalf("want the two disks only, got %+v", drives)
	}
	nvme, hdd := drives[0], drives[1] // sorted by path: nvme0n1 before sda
	if nvme.Name != "nvme0n1" || nvme.Rotational || nvme.Model != "Samsung SSD 980" || nvme.SizeBytes != 1000*512 ||
		nvme.Type == nil || *nvme.Type != "nvme" || nvme.Serial != nil || nvme.Vendor != nil {
		t.Errorf("nvme = %+v", nvme)
	}
	if hdd.Name != "sda" || !hdd.Rotational || !hdd.RO || hdd.SizeBytes != 2000*512 || hdd.Type != nil ||
		hdd.Serial == nil || *hdd.Serial != "WD-1" || hdd.Vendor == nil || *hdd.Vendor != "ATA" {
		t.Errorf("sda = %+v", hdd)
	}
}
