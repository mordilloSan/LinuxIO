package virt

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	libvirt "github.com/digitalocean/go-libvirt"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func TestDeleteVMRemovesOnlyOwnedManagedDisks(t *testing.T) {
	fake := newFakeConn()
	fake.domains["delete-me"] = testDomain("delete-me")
	fake.domainXML["delete-me"] = deleteTestDomainXML()
	fake.volumesByPath["/var/lib/libvirt/images/linuxio-delete-me.qcow2"] = libvirt.StorageVol{
		Pool: defaultPoolName,
		Name: "linuxio-delete-me.qcow2",
		Key:  "/var/lib/libvirt/images/linuxio-delete-me.qcow2",
	}
	withFakeLibvirt(t, fake)

	result, err := DeleteVM(context.Background(), apischema.VMDeleteRequest{Name: "delete-me", DeleteDisks: true})
	if err != nil {
		t.Fatalf("DeleteVM: %v", err)
	}
	if len(result.Removed) != 1 || result.Removed[0] != "/var/lib/libvirt/images/linuxio-delete-me.qcow2" {
		t.Fatalf("removed = %#v", result.Removed)
	}
	if len(result.Preserved) != 1 || result.Preserved[0] != "/srv/external-data.qcow2" {
		t.Fatalf("preserved = %#v", result.Preserved)
	}
	if fake.undefineFlags == 0 ||
		fake.undefineFlags&libvirt.DomainUndefineNvram == 0 ||
		fake.undefineFlags&libvirt.DomainUndefineManagedSave == 0 ||
		fake.undefineFlags&libvirt.DomainUndefineSnapshotsMetadata == 0 ||
		fake.undefineFlags&libvirt.DomainUndefineCheckpointsMetadata == 0 {
		t.Fatalf("undefine flags = %v", fake.undefineFlags)
	}
}

func TestDeleteVMRemovesOwnedCloudInitSeed(t *testing.T) {
	fake := newFakeConn()
	fake.domains["cloud-seed"] = testDomain("cloud-seed")
	fake.domainXML["cloud-seed"] = cloudSeedTestDomainXML()
	for _, vol := range []libvirt.StorageVol{
		{
			Pool: defaultPoolName,
			Name: "linuxio-cloud-seed.qcow2",
			Key:  managedCloudPath + "/linuxio-cloud-seed.qcow2",
		},
		{
			Pool: defaultPoolName,
			Name: "linuxio-cloud-seed-seed.img",
			Key:  managedCloudPath + "/linuxio-cloud-seed-seed.img",
		},
	} {
		fake.volumesByPath[vol.Key] = vol
		fake.volumesByName[vol.Name] = vol
	}
	withFakeLibvirt(t, fake)

	result, err := DeleteVM(context.Background(), apischema.VMDeleteRequest{Name: "cloud-seed", DeleteDisks: true})
	if err != nil {
		t.Fatalf("DeleteVM: %v", err)
	}
	if len(result.Removed) != 2 {
		t.Fatalf("removed = %#v, want boot disk and seed image", result.Removed)
	}
	for _, want := range []string{
		managedCloudPath + "/linuxio-cloud-seed.qcow2",
		managedCloudPath + "/linuxio-cloud-seed-seed.img",
	} {
		if !slices.Contains(result.Removed, want) {
			t.Fatalf("removed = %#v, missing %q", result.Removed, want)
		}
	}
	if len(result.Preserved) != 0 {
		t.Fatalf("preserved = %#v, want none", result.Preserved)
	}
}

func TestDeleteVMUnregisteredCloudDisks(t *testing.T) {
	fake := newFakeConn()
	fake.domains["cloud-seed"] = testDomain("cloud-seed")
	fake.domainXML["cloud-seed"] = cloudSeedTestDomainXML()
	withFakeLibvirt(t, fake)
	dir := t.TempDir()
	paths := []string{
		managedCloudPath + "/linuxio-cloud-seed.qcow2",
		managedCloudPath + "/linuxio-cloud-seed-seed.img",
	}
	for _, path := range paths {
		name := filepath.Base(path)
		if err := os.WriteFile(filepath.Join(dir, name), []byte("VM data"), 0o600); err != nil {
			t.Fatal(err)
		}
		// A same-named volume elsewhere in the pool must survive.
		fake.volumesByName[name] = libvirt.StorageVol{Pool: defaultPoolName, Name: name, Key: defaultPoolPath + "/" + name}
	}
	oldRemove := removeFile
	removeFile = func(path string) error {
		if !slices.Contains(paths, path) {
			t.Fatalf("unexpected file removal: %s", path)
		}
		return os.Remove(filepath.Join(dir, filepath.Base(path)))
	}
	t.Cleanup(func() { removeFile = oldRemove })

	result, err := DeleteVM(context.Background(), apischema.VMDeleteRequest{Name: "cloud-seed", DeleteDisks: true})
	if err != nil {
		t.Fatalf("DeleteVM: %v", err)
	}
	if len(fake.deletedVolumes) != 0 {
		t.Fatalf("deleted unrelated volumes: %v", fake.deletedVolumes)
	}
	for _, path := range paths {
		_, statErr := os.Stat(filepath.Join(dir, filepath.Base(path)))
		if !errors.Is(statErr, os.ErrNotExist) || !slices.Contains(result.Removed, path) {
			t.Fatalf("disk %s remains: stat = %v, result = %+v", path, statErr, result)
		}
	}
}

func TestDeleteIfManagedDiskFileFallback(t *testing.T) {
	name := managedVolumeName("cloud-seed")
	path := managedCloudPath + "/" + name
	for _, tt := range []struct {
		name      string
		disk      apischema.VMDisk
		lookupErr error
		removeErr error
		wantCall  bool
		wantErr   error
	}{
		{name: "missing file", disk: apischema.VMDisk{Owned: true, Path: path}, removeErr: os.ErrNotExist, wantCall: true},
		{name: "remove failure", disk: apischema.VMDisk{Owned: true, Path: path}, removeErr: os.ErrPermission, wantCall: true, wantErr: os.ErrPermission},
		{name: "lookup failure", disk: apischema.VMDisk{Owned: true, Path: path}, lookupErr: os.ErrPermission, wantErr: os.ErrPermission},
		{name: "unowned", disk: apischema.VMDisk{Path: path}},
		{name: "external path", disk: apischema.VMDisk{Owned: true, Path: "/srv/" + name}},
		{name: "sibling directory", disk: apischema.VMDisk{Owned: true, Path: managedCloudPath + "-other/" + name}},
		{name: "traversal", disk: apischema.VMDisk{Owned: true, Path: managedCloudPath + "/../" + name}},
		{name: "other VM", disk: apischema.VMDisk{Owned: true, Path: managedCloudPath + "/linuxio-other.qcow2"}},
		{name: "mismatched volume", disk: apischema.VMDisk{Owned: true, Path: path, VolumeName: managedSeedVolumeName("cloud-seed")}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fake := newFakeConn()
			fake.storageVolPathLookupErr = tt.lookupErr
			fake.poolLookupErr = libvirtErr(libvirt.ErrNoStoragePool, "pool missing")
			called := false
			oldRemove := removeFile
			removeFile = func(got string) error {
				called = true
				if got != path {
					t.Fatalf("removeFile(%q), want %q", got, path)
				}
				return tt.removeErr
			}
			t.Cleanup(func() { removeFile = oldRemove })

			removed, err := deleteIfManagedDisk(fake, "cloud-seed", tt.disk)
			if !errors.Is(err, tt.wantErr) || called != tt.wantCall || removed != (tt.wantCall && tt.wantErr == nil) {
				t.Fatalf("removed = %t, err = %v, file removal called = %t", removed, err, called)
			}
		})
	}
}

func TestDeleteVMPreservesDisksWhenRequested(t *testing.T) {
	for _, xmlDoc := range []string{deleteTestDomainXML(), strings.ReplaceAll(cloudSeedTestDomainXML(), "cloud-seed", "delete-me")} {
		fake := newFakeConn()
		fake.domains["delete-me"] = testDomain("delete-me")
		fake.domainXML["delete-me"] = xmlDoc
		withFakeLibvirt(t, fake)
		oldRemove := removeFile
		removeFile = func(path string) error {
			t.Fatalf("unexpected file removal: %s", path)
			return nil
		}
		t.Cleanup(func() { removeFile = oldRemove })

		result, err := DeleteVM(context.Background(), apischema.VMDeleteRequest{Name: "delete-me", DeleteDisks: false})
		if err != nil {
			t.Fatalf("DeleteVM: %v", err)
		}
		if len(result.Removed) != 0 {
			t.Fatalf("removed = %#v", result.Removed)
		}
		if len(result.Preserved) != 2 {
			t.Fatalf("preserved = %#v, want two disks", result.Preserved)
		}
		if len(fake.deletedVolumes) != 0 {
			t.Fatalf("deletedVolumes = %#v, want none", fake.deletedVolumes)
		}
	}
}

func TestDeleteVMDestroysRunningDomainBeforeUndefine(t *testing.T) {
	fake := newFakeConn()
	fake.domainState = int32(libvirt.DomainRunning)
	fake.domains["delete-me"] = testDomain("delete-me")
	fake.domainXML["delete-me"] = deleteTestDomainXML()
	withFakeLibvirt(t, fake)

	_, err := DeleteVM(context.Background(), apischema.VMDeleteRequest{Name: "delete-me"})
	if err != nil {
		t.Fatalf("DeleteVM: %v", err)
	}
	if fake.domainDestroyCount != 1 {
		t.Fatalf("domainDestroyCount = %d, want 1", fake.domainDestroyCount)
	}
	if fake.undefineFlags == 0 {
		t.Fatal("domain was not undefined")
	}
}

func TestDeleteVMDestroysPausedDomainBeforeUndefine(t *testing.T) {
	fake := newFakeConn()
	fake.domainState = int32(libvirt.DomainPaused)
	fake.domains["delete-me"] = testDomain("delete-me")
	fake.domainXML["delete-me"] = deleteTestDomainXML()
	withFakeLibvirt(t, fake)

	_, err := DeleteVM(context.Background(), apischema.VMDeleteRequest{Name: "delete-me"})
	if err != nil {
		t.Fatalf("DeleteVM: %v", err)
	}
	if fake.domainDestroyCount != 1 {
		t.Fatalf("domainDestroyCount = %d, want 1", fake.domainDestroyCount)
	}
	if fake.undefineFlags == 0 {
		t.Fatal("domain was not undefined")
	}
}

func TestDeleteVMPropagatesActiveStateError(t *testing.T) {
	fake := newFakeConn()
	fake.domainIsActiveErr = libvirtErr(libvirt.ErrInternalError, "active state unavailable")
	fake.domains["delete-me"] = testDomain("delete-me")
	fake.domainXML["delete-me"] = deleteTestDomainXML()
	withFakeLibvirt(t, fake)

	_, err := DeleteVM(context.Background(), apischema.VMDeleteRequest{Name: "delete-me"})
	if err == nil || !strings.Contains(err.Error(), "check VM active state") {
		t.Fatalf("DeleteVM error = %v, want active state error", err)
	}
	if fake.undefineFlags != 0 {
		t.Fatalf("undefineFlags = %v, want no undefine after active-state failure", fake.undefineFlags)
	}
}

func TestDeleteVMPropagatesManagedDiskLookupError(t *testing.T) {
	fake := newFakeConn()
	fake.domains["delete-me"] = testDomain("delete-me")
	fake.domainXML["delete-me"] = deleteTestDomainXML()
	fake.storageVolPathLookupErr = libvirtErr(libvirt.ErrInternalError, "volume lookup failed")
	withFakeLibvirt(t, fake)

	_, err := DeleteVM(context.Background(), apischema.VMDeleteRequest{Name: "delete-me", DeleteDisks: true})
	if err == nil || !strings.Contains(err.Error(), "look up disk") {
		t.Fatalf("DeleteVM error = %v, want disk lookup error", err)
	}
}

func TestStartVMRepairsLegacyLinuxIOVNCGraphics(t *testing.T) {
	fake := newFakeConn()
	fake.domains["legacy-vnc"] = testDomain("legacy-vnc")
	fake.domainXML["legacy-vnc"] = strings.ReplaceAll(deleteTestDomainXML(), "delete-me", "legacy-vnc")
	withFakeLibvirt(t, fake)
	withFakeManagedStoragePermissions(t)

	if err := StartVM(context.Background(), "legacy-vnc"); err != nil {
		t.Fatalf("StartVM: %v", err)
	}
	if fake.domainCreateCount != 1 {
		t.Fatalf("domainCreateCount = %d, want 1", fake.domainCreateCount)
	}
	xmlDoc := fake.domainXML["legacy-vnc"]
	if !strings.Contains(xmlDoc, `socket="/var/lib/libvirt/qemu/linuxio-legacy-vnc.vnc"`) {
		t.Fatalf("repaired XML missing VNC socket:\n%s", xmlDoc)
	}
	if strings.Contains(xmlDoc, "autoport") || strings.Contains(xmlDoc, "port=") {
		t.Fatalf("repaired XML should not include VNC TCP port attributes:\n%s", xmlDoc)
	}
}
