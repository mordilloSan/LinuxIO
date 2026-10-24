package virt

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	libvirt "github.com/digitalocean/go-libvirt"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func TestCreateVMRejectsDirectoryInstallMediaBeforeLibvirt(t *testing.T) {
	fake := newFakeConn()
	withFakeLibvirt(t, fake)

	_, err := createVMForTest(context.Background(), apischema.VMCreateRequest{
		Name:     "bad-media",
		VCPUs:    2,
		MemoryMB: 2048,
		DiskGB:   16,
		ISOPath:  t.TempDir(),
	})
	if err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("CreateVM error = %v, want directory rejection", err)
	}
	if len(fake.volumesByName) != 0 || len(fake.domains) != 0 {
		t.Fatalf("libvirt was mutated for invalid media: volumes=%#v domains=%#v", fake.volumesByName, fake.domains)
	}
}

func TestCreateVMRollsBackVolumeWhenDefineFails(t *testing.T) {
	fake := newFakeConn()
	fake.defineErr = errors.New("define failed")
	withFakeLibvirt(t, fake)
	withReadyPreflight(t)

	_, err := createVMForTest(context.Background(), apischema.VMCreateRequest{
		Name:     "rollback",
		VCPUs:    1,
		MemoryMB: 1024,
		DiskGB:   8,
		ISOPath:  "/isos/test.iso",
	})
	if err == nil {
		t.Fatal("CreateVM succeeded, want define failure")
	}
	if len(fake.deletedVolumes) != 1 || fake.deletedVolumes[0] != "linuxio-rollback.qcow2" {
		t.Fatalf("deletedVolumes = %#v, want rollback volume", fake.deletedVolumes)
	}
}

func TestCreateVMStartsInactiveDefaultsAndDomain(t *testing.T) {
	fake := newFakeConn()
	fake.poolActive = 0
	fake.networkActive = 0
	withFakeLibvirt(t, fake)
	withReadyPreflight(t)

	vm, err := createVMForTest(context.Background(), apischema.VMCreateRequest{
		Name:     "install-me",
		VCPUs:    2,
		MemoryMB: 2048,
		DiskGB:   16,
		ISOPath:  "/isos/install.iso",
		Start:    true,
	})
	if err != nil {
		t.Fatalf("CreateVM: %v", err)
	}
	if vm.Name != "install-me" || vm.VCPUs != 2 || vm.MemoryMB != 2048 || vm.DiskGB != 16 {
		t.Fatalf("created VM = %#v", vm)
	}
	if fake.poolCreateCount != 1 {
		t.Fatalf("poolCreateCount = %d, want 1", fake.poolCreateCount)
	}
	if fake.networkCreateCount != 1 {
		t.Fatalf("networkCreateCount = %d, want 1", fake.networkCreateCount)
	}
	if fake.networkAutostart != 1 {
		t.Fatalf("networkAutostart = %d, want 1", fake.networkAutostart)
	}
	if fake.domainCreateCount != 1 {
		t.Fatalf("domainCreateCount = %d, want 1", fake.domainCreateCount)
	}
}

func TestCreateVMImportsImagePresetDisk(t *testing.T) {
	fake := newFakeConn()
	withFakeLibvirt(t, fake)
	withReadyPreflight(t)
	var importedPath string
	var importedDiskGB int
	withFakeImageImporter(t, func(ctx context.Context, preset vmImagePreset, templateID, volumePath string, diskGB int, report vmCreateReporter) error {
		importedPath = volumePath
		importedDiskGB = diskGB
		return nil
	})

	vm, err := createVMForTest(context.Background(), apischema.VMCreateRequest{
		Name:          "haos",
		VCPUs:         2,
		MemoryMB:      4096,
		DiskGB:        32,
		SourceType:    vmSourceTypeImagePreset,
		ImagePresetID: vmImagePresetHomeOS,
		Start:         true,
	})
	if err != nil {
		t.Fatalf("CreateVM: %v", err)
	}
	if vm.Name != "haos" || vm.DiskGB != 32 {
		t.Fatalf("created VM = %#v", vm)
	}
	if importedPath != managedCloudPath+"/linuxio-haos.qcow2" || importedDiskGB != 32 {
		t.Fatalf("imported path=%q diskGB=%d", importedPath, importedDiskGB)
	}
	if fake.poolRefreshCount != 1 {
		t.Fatalf("poolRefreshCount = %d, want 1", fake.poolRefreshCount)
	}
	if fake.domainCreateCount != 1 {
		t.Fatalf("domainCreateCount = %d, want 1", fake.domainCreateCount)
	}
	xmlDoc := fake.domainXML["haos"]
	if strings.Contains(xmlDoc, "device=\"cdrom\"") || strings.Contains(xmlDoc, ".iso") {
		t.Fatalf("image preset domain should not include install media:\n%s", xmlDoc)
	}
}

func TestCreateVMImportsCloudImagePresetAndCreatesSeedISO(t *testing.T) {
	fake := newFakeConn()
	withFakeLibvirt(t, fake)
	withReadyPreflight(t)
	var importedPath string
	var seedPath string
	withFakeImageImporter(t, func(ctx context.Context, preset vmImagePreset, templateID, volumePath string, diskGB int, report vmCreateReporter) error {
		importedPath = volumePath
		return nil
	})
	withFakeCloudInitSeedCreator(t, func(ctx context.Context, req apischema.VMCreateRequest, preset vmImagePreset, destination string) error {
		seedPath = destination
		return nil
	})

	vm, err := createVMForTest(context.Background(), apischema.VMCreateRequest{
		Name:              "debian",
		VCPUs:             2,
		MemoryMB:          2048,
		DiskGB:            20,
		SourceType:        vmSourceTypeImagePreset,
		ImagePresetID:     vmImagePresetDebian,
		CloudInitUsername: "linuxio",
		CloudInitPassword: "secret",
		Start:             true,
	})
	if err != nil {
		t.Fatalf("CreateVM: %v", err)
	}
	if vm.Name != "debian" || vm.DiskGB != 20 {
		t.Fatalf("created VM = %#v", vm)
	}
	if importedPath != managedCloudPath+"/linuxio-debian.qcow2" {
		t.Fatalf("importedPath = %q", importedPath)
	}
	if seedPath != managedCloudPath+"/linuxio-debian-seed.img" {
		t.Fatalf("seedPath = %q", seedPath)
	}
	if fake.poolRefreshCount != 2 {
		t.Fatalf("poolRefreshCount = %d, want image and seed refreshes", fake.poolRefreshCount)
	}
	xmlDoc := fake.domainXML["debian"]
	for _, want := range []string{"linuxio-debian.qcow2", "linuxio-debian-seed.img", "device=\"disk\""} {
		if !strings.Contains(xmlDoc, want) {
			t.Fatalf("domain XML missing %q:\n%s", want, xmlDoc)
		}
	}
	if fake.domainCreateCount != 1 {
		t.Fatalf("domainCreateCount = %d, want 1", fake.domainCreateCount)
	}
}

func TestCreateVMReportsProgress(t *testing.T) {
	fake := newFakeConn()
	withFakeLibvirt(t, fake)
	withReadyPreflight(t)
	withFakeImageImporter(t, func(ctx context.Context, preset vmImagePreset, templateID, volumePath string, diskGB int, report vmCreateReporter) error {
		reportVMCreateProgress(report, "download", "Downloading Debian Server image", volumePath, progressPercent(42))
		return nil
	})
	withFakeCloudInitSeedCreator(t, func(ctx context.Context, req apischema.VMCreateRequest, preset vmImagePreset, destination string) error {
		return nil
	})

	var phases []string
	_, err := CreateVMWithProgress(context.Background(), apischema.VMCreateRequest{
		Name:              "progress",
		VCPUs:             2,
		MemoryMB:          2048,
		DiskGB:            20,
		SourceType:        vmSourceTypeImagePreset,
		ImagePresetID:     vmImagePresetDebian,
		CloudInitUsername: "linuxio",
		CloudInitPassword: "secret",
		Start:             true,
	}, func(progress apischema.VMCreateProgress) {
		phases = append(phases, progress.Phase)
	})
	if err != nil {
		t.Fatalf("CreateVMWithProgress: %v", err)
	}
	for _, want := range []string{"validating", "preflight", "storage", "download", "seed", "define", "start", "complete"} {
		if !slices.Contains(phases, want) {
			t.Fatalf("progress phases = %#v, missing %q", phases, want)
		}
	}
}

func TestCreateVMRollsBackDomainAndVolumeWhenStartFails(t *testing.T) {
	fake := newFakeConn()
	fake.domainCreateErr = errors.New("unsupported configuration: vnc port must be in range [5900,65535]")
	fake.domainIsActiveErr = libvirtErr(libvirt.ErrInternalError, "active state unavailable")
	withFakeLibvirt(t, fake)
	withReadyPreflight(t)

	_, err := createVMForTest(context.Background(), apischema.VMCreateRequest{
		Name:     "start-fail",
		VCPUs:    2,
		MemoryMB: 2048,
		DiskGB:   16,
		ISOPath:  "/isos/install.iso",
		Start:    true,
	})
	if err == nil || !strings.Contains(err.Error(), "start VM") {
		t.Fatalf("CreateVM error = %v, want start VM error", err)
	}
	if _, ok := fake.domains["start-fail"]; ok {
		t.Fatalf("domain still exists after start rollback: %#v", fake.domains["start-fail"])
	}
	if len(fake.deletedVolumes) != 1 || fake.deletedVolumes[0] != "linuxio-start-fail.qcow2" {
		t.Fatalf("deletedVolumes = %#v, want failed VM volume removed", fake.deletedVolumes)
	}
	if fake.undefineFlags == 0 {
		t.Fatal("domain was not undefined after start rollback")
	}
}

func TestCreateVMRejectsExistingDomainAndVolume(t *testing.T) {
	t.Run("domain exists", func(t *testing.T) {
		fake := newFakeConn()
		fake.domains["existing"] = testDomain("existing")
		withFakeLibvirt(t, fake)
		withReadyPreflight(t)

		_, err := createVMForTest(context.Background(), apischema.VMCreateRequest{
			Name:     "existing",
			VCPUs:    1,
			MemoryMB: 1024,
			DiskGB:   8,
			ISOPath:  "/isos/test.iso",
		})
		if err == nil || !strings.Contains(err.Error(), "already exists") {
			t.Fatalf("CreateVM error = %v, want already exists", err)
		}
		if code := errorCode(err, 0); code != 409 {
			t.Fatalf("error code = %d, want 409", code)
		}
	})

	t.Run("volume exists", func(t *testing.T) {
		fake := newFakeConn()
		fake.volumesByName["linuxio-existing-vol.qcow2"] = libvirt.StorageVol{
			Pool: defaultPoolName,
			Name: "linuxio-existing-vol.qcow2",
			Key:  "/var/lib/libvirt/images/linuxio-existing-vol.qcow2",
		}
		withFakeLibvirt(t, fake)
		withReadyPreflight(t)

		_, err := createVMForTest(context.Background(), apischema.VMCreateRequest{
			Name:     "existing-vol",
			VCPUs:    1,
			MemoryMB: 1024,
			DiskGB:   8,
			ISOPath:  "/isos/test.iso",
		})
		if err == nil || !strings.Contains(err.Error(), "managed volume") {
			t.Fatalf("CreateVM error = %v, want managed volume", err)
		}
		if code := errorCode(err, 0); code != 409 {
			t.Fatalf("error code = %d, want 409", code)
		}
	})
}

func TestCreateVMPropagatesLookupErrors(t *testing.T) {
	t.Run("domain lookup", func(t *testing.T) {
		fake := newFakeConn()
		fake.domainLookupErr = libvirtErr(libvirt.ErrInternalError, "lookup failed")
		withFakeLibvirt(t, fake)
		withReadyPreflight(t)

		_, err := createVMForTest(context.Background(), apischema.VMCreateRequest{
			Name:     "lookup-fail",
			VCPUs:    1,
			MemoryMB: 1024,
			DiskGB:   8,
			ISOPath:  "/isos/test.iso",
		})
		if err == nil || !strings.Contains(err.Error(), "check existing VM") {
			t.Fatalf("CreateVM error = %v, want lookup failure", err)
		}
		if len(fake.volumesByName) != 0 || len(fake.domains) != 0 {
			t.Fatalf("libvirt mutated after lookup failure: volumes=%#v domains=%#v", fake.volumesByName, fake.domains)
		}
	})

	t.Run("volume lookup", func(t *testing.T) {
		fake := newFakeConn()
		fake.storageVolLookupErr = libvirtErr(libvirt.ErrInternalError, "volume lookup failed")
		withFakeLibvirt(t, fake)
		withReadyPreflight(t)

		_, err := createVMForTest(context.Background(), apischema.VMCreateRequest{
			Name:     "volume-lookup-fail",
			VCPUs:    1,
			MemoryMB: 1024,
			DiskGB:   8,
			ISOPath:  "/isos/test.iso",
		})
		if err == nil || !strings.Contains(err.Error(), "check managed volume") {
			t.Fatalf("CreateVM error = %v, want volume lookup failure", err)
		}
		if len(fake.volumesByName) != 0 || len(fake.domains) != 0 {
			t.Fatalf("libvirt mutated after volume lookup failure: volumes=%#v domains=%#v", fake.volumesByName, fake.domains)
		}
	})

	t.Run("pool lookup", func(t *testing.T) {
		fake := newFakeConn()
		fake.poolLookupErr = libvirtErr(libvirt.ErrInternalError, "pool lookup failed")
		withFakeLibvirt(t, fake)
		withReadyPreflight(t)

		_, err := createVMForTest(context.Background(), apischema.VMCreateRequest{
			Name:     "pool-lookup-fail",
			VCPUs:    1,
			MemoryMB: 1024,
			DiskGB:   8,
			ISOPath:  "/isos/test.iso",
		})
		if err == nil || !strings.Contains(err.Error(), "look up default storage pool") {
			t.Fatalf("CreateVM error = %v, want pool lookup failure", err)
		}
		if len(fake.definedPoolXML) != 0 || len(fake.volumesByName) != 0 {
			t.Fatalf("libvirt mutated after pool lookup failure: pools=%#v volumes=%#v", fake.definedPoolXML, fake.volumesByName)
		}
	})
}
