package virt

import (
	"strings"
	"testing"
)

func TestBuildCloudInitSeedImageIsFATConfigDrive(t *testing.T) {
	image, err := buildCloudInitSeedImage([]seedImageFile{
		{Name: "user-data", Data: []byte("#cloud-config\nhostname: test\n")},
		{Name: "meta-data", Data: []byte("instance-id: test\n")},
	})
	if err != nil {
		t.Fatalf("buildCloudInitSeedImage: %v", err)
	}
	if len(image) != 2*1024*1024 {
		t.Fatalf("image size = %d, want 2 MiB", len(image))
	}
	for _, want := range []string{"CIDATA", "FAT12"} {
		if !strings.Contains(string(image), want) {
			t.Fatalf("seed image missing %q", want)
		}
	}
	for _, want := range []string{"user-data", "meta-data"} {
		if !containsFATLongName(image, want) {
			t.Fatalf("seed image missing long file name %q", want)
		}
	}
	if image[510] != 0x55 || image[511] != 0xaa {
		t.Fatalf("seed image missing boot sector signature")
	}
}

func TestBuildCloudInitSeedImageRejectsOversizedData(t *testing.T) {
	_, err := buildCloudInitSeedImage([]seedImageFile{
		{Name: "user-data", Data: []byte(strings.Repeat("x", 3*1024*1024))},
	})

	if err == nil || !strings.Contains(err.Error(), "seed data exceeds") {
		t.Fatalf("buildCloudInitSeedImage error = %v, want oversized data rejection", err)
	}
	if code := errorCode(err, 0); code != 400 {
		t.Fatalf("error code = %d, want 400", code)
	}
}
