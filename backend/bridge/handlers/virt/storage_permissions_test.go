package virt

import (
	"errors"
	"os"
	"os/user"
	"slices"
	"strings"
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
	for _, want := range []os.FileMode{0o755, 0o660, 0o640} {
		if !slices.Contains(modes, want) {
			t.Fatalf("chmod modes = %#v, missing %o", modes, want)
		}
	}
}

func TestMakeManagedFileAccessibleFailsWithoutQEMUAccountAndKeepsMode(t *testing.T) {
	withFakeManagedStoragePermissions(t)
	lookupOSUser = func(string) (*user.User, error) {
		return nil, errors.New("missing")
	}
	chmodFile = func(string, os.FileMode) error {
		t.Fatal("chmod must not run without a qemu account")
		return nil
	}

	err := makeManagedWritableDiskAccessible("/tmp/disk.qcow2")

	if err == nil || !strings.Contains(err.Error(), "libvirt-qemu, qemu") {
		t.Fatalf("error = %v, want one naming the accounts tried", err)
	}
}
