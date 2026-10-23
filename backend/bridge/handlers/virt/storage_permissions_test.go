package virt

import (
	"os"
	"slices"
	"testing"
)

func TestRepairManagedStorageAccessFromDomainXMLFixesDiskModes(t *testing.T) {
	withFakeManagedStoragePermissions(t)

	var modes []os.FileMode
	oldChmodFile := chmodFile
	chmodFile = func(path string, mode os.FileMode) error {
		modes = append(modes, mode)
		return oldChmodFile(path, mode)
	}
	t.Cleanup(func() {
		chmodFile = oldChmodFile
	})

	if err := repairManagedStorageAccessFromDomainXML(cloudSeedTestDomainXML()); err != nil {
		t.Fatalf("repairManagedStorageAccessFromDomainXML: %v", err)
	}
	for _, want := range []os.FileMode{0o755, 0o666, 0o644} {
		if !slices.Contains(modes, want) {
			t.Fatalf("chmod modes = %#v, missing %o", modes, want)
		}
	}
}
