package virt

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/user"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	libvirt "github.com/digitalocean/go-libvirt"
	"libvirt.org/go/libvirtxml"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
)

func TestBuildDomainXML(t *testing.T) {
	req := apischema.VMCreateRequest{
		Name:     "test-vm",
		VCPUs:    2,
		MemoryMB: 2048,
		DiskGB:   20,
		ISOPath:  "/isos/debian.iso",
	}
	domain, err := buildDomain(req, testCreatedStorage("linuxio-test-vm.qcow2", "/var/lib/libvirt/images/linuxio-test-vm.qcow2", 20), apischema.VMPreflightFirmware{UEFIAvailable: true, BIOSAvailable: true})
	if err != nil {
		t.Fatalf("buildDomain: %v", err)
	}
	xmlDoc, err := domain.Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{
		"<name>test-vm</name>",
		"<memory unit=\"MiB\">2048</memory>",
		"<vcpu>2</vcpu>",
		"firmware=\"efi\"",
		"<feature enabled=\"no\" name=\"secure-boot\"></feature>",
		"<feature enabled=\"no\" name=\"enrolled-keys\"></feature>",
		"<features>",
		"<acpi></acpi>",
		"<apic></apic>",
		"linuxio-test-vm.qcow2",
		"/isos/debian.iso",
		"<graphics type=\"vnc\" socket=\"/var/lib/libvirt/qemu/linuxio-test-vm.vnc\">",
		"<listen type=\"socket\" socket=\"/var/lib/libvirt/qemu/linuxio-test-vm.vnc\"></listen>",
		"<linuxio xmlns=\"https://linuxio.local/libvirt/v1\"><disk volume=\"linuxio-test-vm.qcow2\" path=\"/var/lib/libvirt/images/linuxio-test-vm.qcow2\" sizeGB=\"20\"></disk></linuxio>",
	} {
		if !strings.Contains(xmlDoc, want) {
			t.Fatalf("domain XML missing %q:\n%s", want, xmlDoc)
		}
	}
	if strings.Contains(xmlDoc, "autoport") || strings.Contains(xmlDoc, "port=") {
		t.Fatalf("domain XML should use a VNC socket without TCP port attributes:\n%s", xmlDoc)
	}
}

func TestBuildDomainXMLForImagePreset(t *testing.T) {
	req := apischema.VMCreateRequest{
		Name:          "haos",
		VCPUs:         2,
		MemoryMB:      4096,
		DiskGB:        32,
		SourceType:    vmSourceTypeImagePreset,
		ImagePresetID: vmImagePresetHomeOS,
	}
	domain, err := buildDomain(req, testCreatedStorage("linuxio-haos.qcow2", "/var/lib/libvirt/images/linuxio-haos.qcow2", 32), apischema.VMPreflightFirmware{UEFIAvailable: true, BIOSAvailable: true})
	if err != nil {
		t.Fatalf("buildDomain: %v", err)
	}
	xmlDoc, err := domain.Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{
		"<name>haos</name>",
		"firmware=\"efi\"",
		"<feature enabled=\"no\" name=\"secure-boot\"></feature>",
		"<feature enabled=\"no\" name=\"enrolled-keys\"></feature>",
		"<boot dev=\"hd\"></boot>",
		"linuxio-haos.qcow2",
	} {
		if !strings.Contains(xmlDoc, want) {
			t.Fatalf("domain XML missing %q:\n%s", want, xmlDoc)
		}
	}
	for _, unwanted := range []string{"device=\"cdrom\"", "<boot dev=\"cdrom\">", ".iso"} {
		if strings.Contains(xmlDoc, unwanted) {
			t.Fatalf("domain XML unexpectedly contains %q:\n%s", unwanted, xmlDoc)
		}
	}
}

func TestBuildDomainXMLForCloudImagePresetAddsSeedISO(t *testing.T) {
	req := apischema.VMCreateRequest{
		Name:              "debian",
		VCPUs:             2,
		MemoryMB:          2048,
		DiskGB:            20,
		SourceType:        vmSourceTypeImagePreset,
		ImagePresetID:     vmImagePresetDebian,
		CloudInitUsername: "linuxio",
		CloudInitPassword: "secret",
	}
	storage := testCreatedStorage("linuxio-debian.qcow2", managedCloudPath+"/linuxio-debian.qcow2", 20)
	seedName := "linuxio-debian-seed.img"
	seedPath := managedCloudPath + "/" + seedName
	storage.Seed = &createdVMVolume{
		Volume: libvirt.StorageVol{Pool: defaultPoolName, Name: seedName, Key: seedPath},
		Name:   seedName,
		Path:   seedPath,
	}

	domain, err := buildDomain(req, storage, apischema.VMPreflightFirmware{UEFIAvailable: true, BIOSAvailable: true})
	if err != nil {
		t.Fatalf("buildDomain: %v", err)
	}
	xmlDoc, err := domain.Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{
		"<boot dev=\"hd\"></boot>",
		"linuxio-debian.qcow2",
		"linuxio-debian-seed.img",
		"device=\"disk\"",
		"<readonly></readonly>",
		"<disk volume=\"linuxio-debian-seed.img\" path=\"" + managedCloudPath + "/linuxio-debian-seed.img\"></disk>",
	} {
		if !strings.Contains(xmlDoc, want) {
			t.Fatalf("domain XML missing %q:\n%s", want, xmlDoc)
		}
	}
	if strings.Contains(xmlDoc, "<boot dev=\"cdrom\">") {
		t.Fatalf("cloud image domain should not boot from seed image:\n%s", xmlDoc)
	}
}

func TestParseVirtualMachineUsesLinuxIOMetadataDiskSize(t *testing.T) {
	vm, err := parseVirtualMachine(testDomain("delete-me"), deleteTestDomainXML(), "shutoff", false)
	if err != nil {
		t.Fatalf("parseVirtualMachine: %v", err)
	}
	if vm.DiskGB != 12 {
		t.Fatalf("DiskGB = %d, want 12", vm.DiskGB)
	}
	if len(vm.Disks) == 0 || !vm.Disks[0].Owned || vm.Disks[0].SizeGB != 12 {
		t.Fatalf("managed disk = %#v", vm.Disks)
	}
}

func TestParseVirtualMachineIncludesOwnedCloudInitSeedWithoutCountingDisk(t *testing.T) {
	vm, err := parseVirtualMachine(testDomain("cloud-seed"), cloudSeedTestDomainXML(), "shutoff", false)
	if err != nil {
		t.Fatalf("parseVirtualMachine: %v", err)
	}
	if vm.DiskGB != 12 {
		t.Fatalf("DiskGB = %d, want 12", vm.DiskGB)
	}
	if len(vm.OwnedDisks) != 2 {
		t.Fatalf("OwnedDisks = %#v, want boot disk and seed image", vm.OwnedDisks)
	}
	if len(vm.Disks) != 2 {
		t.Fatalf("Disks = %#v, want boot disk and seed image only", vm.Disks)
	}
	if vm.Disks[1].Device != "disk" || !vm.Disks[1].Owned || vm.Disks[1].Path != managedCloudPath+"/linuxio-cloud-seed-seed.img" {
		t.Fatalf("seed disk = %#v", vm.Disks[1])
	}
}

func TestValidateCreateRequestRejectsInvalidInput(t *testing.T) {
	valid := apischema.VMCreateRequest{
		Name:     "good-vm",
		VCPUs:    2,
		MemoryMB: 1024,
		DiskGB:   20,
		ISOPath:  "/isos/test.iso",
	}
	tests := []struct {
		name string
		req  apischema.VMCreateRequest
	}{
		{name: "empty name", req: func() apischema.VMCreateRequest { r := valid; r.Name = ""; return r }()},
		{name: "path traversal name", req: func() apischema.VMCreateRequest { r := valid; r.Name = "bad..vm"; return r }()},
		{name: "zero vcpus", req: func() apischema.VMCreateRequest { r := valid; r.VCPUs = 0; return r }()},
		{name: "too little memory", req: func() apischema.VMCreateRequest { r := valid; r.MemoryMB = 128; return r }()},
		{name: "zero disk", req: func() apischema.VMCreateRequest { r := valid; r.DiskGB = 0; return r }()},
		{name: "missing iso", req: func() apischema.VMCreateRequest { r := valid; r.ISOPath = ""; return r }()},
		{name: "non default network", req: func() apischema.VMCreateRequest { r := valid; r.Network = "tenant"; return r }()},
		{name: "image preset disk too small", req: apischema.VMCreateRequest{Name: "haos", VCPUs: 2, MemoryMB: 2048, DiskGB: 8, SourceType: vmSourceTypeImagePreset, ImagePresetID: vmImagePresetHomeOS}},
		{name: "cloud image without login", req: apischema.VMCreateRequest{Name: "debian", VCPUs: 2, MemoryMB: 2048, DiskGB: 12, SourceType: vmSourceTypeImagePreset, ImagePresetID: vmImagePresetDebian}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateCreateRequest(tt.req); err == nil {
				t.Fatal("validateCreateRequest succeeded, want error")
			}
		})
	}
}

func TestValidateCreateRequestReturnsBadRequestError(t *testing.T) {
	err := validateCreateRequest(apischema.VMCreateRequest{
		Name:     "good-vm",
		VCPUs:    0,
		MemoryMB: 1024,
		DiskGB:   20,
		ISOPath:  "/isos/test.iso",
	})

	if err == nil {
		t.Fatal("validateCreateRequest succeeded, want error")
	}
	if code := errorCode(err, 0); code != 400 {
		t.Fatalf("error code = %d, want 400", code)
	}
}

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

func TestValidateInstallMediaPathRejectsDirectory(t *testing.T) {
	_, err := validateInstallMediaPath(t.TempDir())

	if err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("validateInstallMediaPath error = %v, want directory rejection", err)
	}
}

func TestValidateInstallMediaPathRejectsNonISOFile(t *testing.T) {
	path := tempFile(t, "installer.img")

	_, err := validateInstallMediaPath(path)

	if err == nil || !strings.Contains(err.Error(), ".iso") {
		t.Fatalf("validateInstallMediaPath error = %v, want .iso rejection", err)
	}
}

func TestValidateInstallMediaPathAcceptsRegularISO(t *testing.T) {
	path := tempFile(t, "installer.iso")

	normalized, err := validateInstallMediaPath("  " + path + "  ")

	if err != nil {
		t.Fatalf("validateInstallMediaPath: %v", err)
	}
	if normalized != path {
		t.Fatalf("normalized path = %q, want %q", normalized, path)
	}
}

func TestCreateVMRejectsDirectoryInstallMediaBeforeLibvirt(t *testing.T) {
	fake := newFakeConn()
	withFakeLibvirt(t, fake)

	_, err := CreateVM(context.Background(), apischema.VMCreateRequest{
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

func TestPreflightReadyForCreateRejectsMissingPrerequisites(t *testing.T) {
	ready := readyPreflight()
	tests := []struct {
		name string
		mut  func(*apischema.VMPreflight)
		want string
	}{
		{name: "libvirt", mut: func(p *apischema.VMPreflight) { p.LibvirtReachable = false }, want: "libvirt"},
		{name: "kvm", mut: func(p *apischema.VMPreflight) { p.KvmPresent = false }, want: "KVM"},
		{name: "qemu", mut: func(p *apischema.VMPreflight) { p.QemuPresent = false }, want: "qemu"},
		{name: "network", mut: func(p *apischema.VMPreflight) { p.DefaultNetworkExists = false }, want: "network"},
		{name: "iso", mut: func(p *apischema.VMPreflight) { p.ISOReadable = false }, want: "ISO"},
		{name: "firmware", mut: func(p *apischema.VMPreflight) { p.Firmware = apischema.VMPreflightFirmware{} }, want: "firmware"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := ready
			tt.mut(&p)
			err := preflightReadyForCreate(p, vmSourceTypeISO)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("preflightReadyForCreate error = %v, want containing %q", err, tt.want)
			}
		})
	}
	if err := preflightReadyForCreate(ready, vmSourceTypeISO); err != nil {
		t.Fatalf("preflightReadyForCreate ready = %v", err)
	}
	ready.DefaultPoolExists = false
	if err := preflightReadyForCreate(ready, vmSourceTypeISO); err != nil {
		t.Fatalf("preflightReadyForCreate without default pool = %v", err)
	}
	ready.ISOReadable = false
	if err := preflightReadyForCreate(ready, vmSourceTypeImagePreset); err != nil {
		t.Fatalf("preflightReadyForCreate image preset without ISO = %v", err)
	}
}

func TestCreateVMRollsBackVolumeWhenDefineFails(t *testing.T) {
	fake := newFakeConn()
	fake.defineErr = errors.New("define failed")
	withFakeLibvirt(t, fake)
	withReadyPreflight(t)

	_, err := CreateVM(context.Background(), apischema.VMCreateRequest{
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

	vm, err := CreateVM(context.Background(), apischema.VMCreateRequest{
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
	withFakeImageImporter(t, func(ctx context.Context, preset vmImagePreset, volumePath string, diskGB int, report vmCreateReporter) error {
		importedPath = volumePath
		importedDiskGB = diskGB
		return nil
	})

	vm, err := CreateVM(context.Background(), apischema.VMCreateRequest{
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
	withFakeImageImporter(t, func(ctx context.Context, preset vmImagePreset, volumePath string, diskGB int, report vmCreateReporter) error {
		importedPath = volumePath
		return nil
	})
	withFakeCloudInitSeedCreator(t, func(ctx context.Context, req apischema.VMCreateRequest, preset vmImagePreset, destination string) error {
		seedPath = destination
		return nil
	})

	vm, err := CreateVM(context.Background(), apischema.VMCreateRequest{
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
	withFakeImageImporter(t, func(ctx context.Context, preset vmImagePreset, volumePath string, diskGB int, report vmCreateReporter) error {
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

	_, err := CreateVM(context.Background(), apischema.VMCreateRequest{
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

func TestCreateVMReportsDefaultNetworkAddressConflict(t *testing.T) {
	fake := newFakeConn()
	fake.networkActive = 0
	fake.networkCreateErr = errors.New("internal error: Child process (VIR_BRIDGE_NAME=virbr0 /usr/sbin/dnsmasq --conf-file=/var/lib/libvirt/dnsmasq/default.conf --leasefile-ro --dhcp-script=/usr/lib/libvirt/libvirt_leaseshelper) unexpected exit status 2: dnsmasq: failed to create listening socket for 192.168.122.1: Address already in use")
	withFakeLibvirt(t, fake)
	withReadyPreflight(t)

	_, err := CreateVM(context.Background(), apischema.VMCreateRequest{
		Name:     "network-conflict",
		VCPUs:    2,
		MemoryMB: 2048,
		DiskGB:   16,
		ISOPath:  "/isos/install.iso",
	})
	if err == nil {
		t.Fatal("CreateVM succeeded, want network conflict")
	}
	for _, want := range []string{
		"default NAT network cannot start",
		"192.168.122.1 is already in use",
		"reconfigure the libvirt default network address",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("CreateVM error = %v, want containing %q", err, want)
		}
	}
	if len(fake.volumesByName) != 0 {
		t.Fatalf("volumesByName = %#v, want no volume created", fake.volumesByName)
	}
}

func TestEnsureDefaultNetworkActiveHandlesAutostartAndTOCTOU(t *testing.T) {
	t.Run("create failure tolerated when active after recheck", func(t *testing.T) {
		fake := newFakeConn()
		fake.networkActive = 0
		fake.networkCreateErr = libvirtErr(libvirt.ErrOperationInvalid, "network already active")
		fake.networkActivateOnCreateErr = true

		err := ensureDefaultNetworkActive(fake)
		if err != nil {
			t.Fatalf("ensureDefaultNetworkActive: %v", err)
		}
		if fake.networkAutostart != 1 {
			t.Fatalf("networkAutostart = %d, want 1", fake.networkAutostart)
		}
		if fake.networkCreateCount != 1 {
			t.Fatalf("networkCreateCount = %d, want 1", fake.networkCreateCount)
		}
	})

	t.Run("create failure surfaces when inactive after recheck", func(t *testing.T) {
		fake := newFakeConn()
		fake.networkActive = 0
		fake.networkCreateErr = libvirtErr(libvirt.ErrOperationFailed, "network start failed")

		err := ensureDefaultNetworkActive(fake)
		if err == nil || !strings.Contains(err.Error(), "start default NAT network") {
			t.Fatalf("ensureDefaultNetworkActive error = %v, want start failure", err)
		}
		if fake.networkAutostart != 1 {
			t.Fatalf("networkAutostart = %d, want 1", fake.networkAutostart)
		}
	})
}

func TestCreateVMDefinesMissingDefaultPool(t *testing.T) {
	fake := newFakeConn()
	fake.poolLookupErr = libvirtErr(libvirt.ErrNoStoragePool, "pool missing")
	withFakeLibvirt(t, fake)
	withReadyPreflight(t)
	withFakeMkdirAll(t)

	_, err := CreateVM(context.Background(), apischema.VMCreateRequest{
		Name:     "new-pool",
		VCPUs:    2,
		MemoryMB: 2048,
		DiskGB:   16,
		ISOPath:  "/isos/install.iso",
	})
	if err != nil {
		t.Fatalf("CreateVM: %v", err)
	}
	if len(fake.definedPoolXML) != 1 ||
		!strings.Contains(fake.definedPoolXML[0], "<name>default</name>") ||
		!strings.Contains(fake.definedPoolXML[0], defaultPoolPath) {
		t.Fatalf("definedPoolXML = %#v", fake.definedPoolXML)
	}
	if fake.poolAutostart != 1 {
		t.Fatalf("poolAutostart = %d, want 1", fake.poolAutostart)
	}
	if fake.poolCreateCount != 1 {
		t.Fatalf("poolCreateCount = %d, want 1", fake.poolCreateCount)
	}
}

func TestCheckDefaultStoragePoolWarnsWhenMissing(t *testing.T) {
	fake := newFakeConn()
	fake.poolLookupErr = libvirtErr(libvirt.ErrNoStoragePool, "pool missing")
	var preflight apischema.VMPreflight

	checkDefaultStoragePool(fake, &preflight)

	if len(preflight.Errors) != 0 {
		t.Fatalf("errors = %#v, want none", preflight.Errors)
	}
	if len(preflight.Warnings) != 1 || !strings.Contains(preflight.Warnings[0], defaultPoolPath) {
		t.Fatalf("warnings = %#v", preflight.Warnings)
	}
}

func TestPreflightLookupErrorsAreReported(t *testing.T) {
	t.Run("storage pool", func(t *testing.T) {
		fake := newFakeConn()
		fake.poolLookupErr = libvirtErr(libvirt.ErrInternalError, "pool lookup failed")
		var preflight apischema.VMPreflight

		checkDefaultStoragePool(fake, &preflight)

		if len(preflight.Errors) != 1 || !strings.Contains(preflight.Errors[0], "lookup failed") {
			t.Fatalf("errors = %#v, want pool lookup failure", preflight.Errors)
		}
		if len(preflight.Warnings) != 0 {
			t.Fatalf("warnings = %#v, want none", preflight.Warnings)
		}
	})

	t.Run("network", func(t *testing.T) {
		fake := newFakeConn()
		fake.networkLookupErr = libvirtErr(libvirt.ErrInternalError, "network lookup failed")
		var preflight apischema.VMPreflight

		checkDefaultNetwork(fake, &preflight)

		if len(preflight.Errors) != 1 || !strings.Contains(preflight.Errors[0], "lookup failed") {
			t.Fatalf("errors = %#v, want network lookup failure", preflight.Errors)
		}
	})
}

func TestDetectOVMFCodePathUsesFirmwareDescriptors(t *testing.T) {
	dir := t.TempDir()
	plainFirmware := tempFile(t, "OVMF_CODE_4M.fd")
	secureFirmware := tempFile(t, "OVMF_CODE_4M.secboot.fd")
	writeFirmwareDescriptor(t, dir, "40-secure.json", secureFirmware, []string{"secure-boot", "requires-smm"})
	writeFirmwareDescriptor(t, dir, "60-plain.json", plainFirmware, []string{"acpi-s3"})
	oldDirs := firmwareDescriptorDirs
	firmwareDescriptorDirs = []string{dir}
	t.Cleanup(func() { firmwareDescriptorDirs = oldDirs })

	if got := detectOVMFCodePath(); got != plainFirmware {
		t.Fatalf("detectOVMFCodePath = %q, want %q", got, plainFirmware)
	}
}

func TestDetectOVMFCodePathRejectsSecureOnlyDescriptors(t *testing.T) {
	dir := t.TempDir()
	secureFirmware := tempFile(t, "OVMF_CODE_4M.secboot.fd")
	writeFirmwareDescriptor(t, dir, "50-secure.json", secureFirmware, []string{"secure-boot"})
	oldDirs := firmwareDescriptorDirs
	firmwareDescriptorDirs = []string{dir}
	t.Cleanup(func() { firmwareDescriptorDirs = oldDirs })

	if got := detectOVMFCodePath(); got != "" {
		t.Fatalf("detectOVMFCodePath = %q, want empty for secure-only descriptor", got)
	}
}

func TestCreateVMRejectsExistingDomainAndVolume(t *testing.T) {
	t.Run("domain exists", func(t *testing.T) {
		fake := newFakeConn()
		fake.domains["existing"] = testDomain("existing")
		withFakeLibvirt(t, fake)
		withReadyPreflight(t)

		_, err := CreateVM(context.Background(), apischema.VMCreateRequest{
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

		_, err := CreateVM(context.Background(), apischema.VMCreateRequest{
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

		_, err := CreateVM(context.Background(), apischema.VMCreateRequest{
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

		_, err := CreateVM(context.Background(), apischema.VMCreateRequest{
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

		_, err := CreateVM(context.Background(), apischema.VMCreateRequest{
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

func TestManagedVolumeGuardsPropagateLookupErrors(t *testing.T) {
	pool := libvirt.StoragePool{Name: defaultPoolName}

	t.Run("iso volume", func(t *testing.T) {
		fake := newFakeConn()
		fake.storageVolLookupErr = libvirtErr(libvirt.ErrInternalError, "volume lookup failed")

		_, _, err := createManagedVolume(fake, pool, "linuxio-iso.qcow2", 8)
		if err == nil || !strings.Contains(err.Error(), "check managed volume") {
			t.Fatalf("createManagedVolume error = %v, want lookup failure", err)
		}
	})

	t.Run("image volume", func(t *testing.T) {
		fake := newFakeConn()
		fake.storageVolLookupErr = libvirtErr(libvirt.ErrInternalError, "image lookup failed")

		_, _, err := createManagedImageVolume(context.Background(), fake, pool, "linuxio-image.qcow2", apischema.VMCreateRequest{
			Name:          "image",
			VCPUs:         1,
			MemoryMB:      1024,
			DiskGB:        32,
			SourceType:    vmSourceTypeImagePreset,
			ImagePresetID: vmImagePresetHomeOS,
		}, nil)
		if err == nil || !strings.Contains(err.Error(), "check managed volume") {
			t.Fatalf("createManagedImageVolume error = %v, want lookup failure", err)
		}
	})

	t.Run("seed volume", func(t *testing.T) {
		fake := newFakeConn()
		fake.storageVolLookupErr = libvirtErr(libvirt.ErrInternalError, "seed lookup failed")
		preset, err := imagePreset(vmImagePresetDebian)
		if err != nil {
			t.Fatalf("imagePreset: %v", err)
		}

		_, err = createManagedCloudInitSeed(context.Background(), fake, pool, "linuxio-seed-seed.img", apischema.VMCreateRequest{
			Name:              "seed",
			CloudInitUsername: "linuxio",
			CloudInitPassword: "secret",
		}, preset)
		if err == nil || !strings.Contains(err.Error(), "check managed volume") {
			t.Fatalf("createManagedCloudInitSeed error = %v, want lookup failure", err)
		}
	})
}

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
		if !containsString(result.Removed, want) {
			t.Fatalf("removed = %#v, missing %q", result.Removed, want)
		}
	}
	if len(result.Preserved) != 0 {
		t.Fatalf("preserved = %#v, want none", result.Preserved)
	}
}

func TestDeleteVMPreservesDisksWhenRequested(t *testing.T) {
	fake := newFakeConn()
	fake.domains["delete-me"] = testDomain("delete-me")
	fake.domainXML["delete-me"] = deleteTestDomainXML()
	withFakeLibvirt(t, fake)

	result, err := DeleteVM(context.Background(), apischema.VMDeleteRequest{Name: "delete-me", DeleteDisks: false})
	if err != nil {
		t.Fatalf("DeleteVM: %v", err)
	}
	if len(result.Removed) != 0 {
		t.Fatalf("removed = %#v, want none", result.Removed)
	}
	if len(result.Preserved) != 2 {
		t.Fatalf("preserved = %#v, want two disks", result.Preserved)
	}
	if len(fake.deletedVolumes) != 0 {
		t.Fatalf("deletedVolumes = %#v, want none", fake.deletedVolumes)
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

func TestEnsureManagedStorageDirectoriesChmodsManagedDirs(t *testing.T) {
	var mkdirs []string
	var chmods []string
	oldMkdirAll := mkdirAll
	oldChmodFile := chmodFile
	mkdirAll = func(path string, mode os.FileMode) error {
		mkdirs = append(mkdirs, path)
		if mode != 0o755 {
			t.Fatalf("mkdir mode for %s = %o, want 755", path, mode)
		}
		return nil
	}
	chmodFile = func(path string, mode os.FileMode) error {
		chmods = append(chmods, path)
		if mode != 0o755 {
			t.Fatalf("chmod mode for %s = %o, want 755", path, mode)
		}
		return nil
	}
	t.Cleanup(func() {
		mkdirAll = oldMkdirAll
		chmodFile = oldChmodFile
	})

	if err := ensureManagedStorageDirectories(); err != nil {
		t.Fatalf("ensureManagedStorageDirectories: %v", err)
	}
	want := []string{managedRootPath, managedISOPath, managedCloudPath}
	if !slices.Equal(mkdirs, want) {
		t.Fatalf("mkdirs = %#v, want %#v", mkdirs, want)
	}
	if !slices.Equal(chmods, want) {
		t.Fatalf("chmods = %#v, want %#v", chmods, want)
	}
}

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

func TestNormalizeLinuxIOVNCGraphicsRepairsManagedSocketWithoutMetadata(t *testing.T) {
	xmlDoc := `<domain type="kvm"><name>legacy-vnc</name><devices><graphics type="vnc" socket="/var/lib/libvirt/qemu/linuxio-legacy-vnc.vnc" autoport="no"></graphics></devices></domain>`

	normalized, changed, err := normalizeLinuxIOVNCGraphicsXML(xmlDoc)

	if err != nil {
		t.Fatalf("normalizeLinuxIOVNCGraphicsXML: %v", err)
	}
	if !changed {
		t.Fatal("normalizeLinuxIOVNCGraphicsXML changed = false, want true")
	}
	if !strings.Contains(normalized, `socket="/var/lib/libvirt/qemu/linuxio-legacy-vnc.vnc"`) {
		t.Fatalf("normalized XML missing VNC socket:\n%s", normalized)
	}
	if strings.Contains(normalized, "autoport") || strings.Contains(normalized, "port=") {
		t.Fatalf("normalized XML should not include VNC TCP port attributes:\n%s", normalized)
	}
}

func TestNormalizeLinuxIOVNCGraphicsRecreatesMissingManagedSocket(t *testing.T) {
	xmlDoc := `<domain type="kvm"><name>legacy-vnc</name><metadata><linuxio xmlns="https://linuxio.local/libvirt/v1"><disk volume="linuxio-legacy-vnc.qcow2" sizeGB="16"></disk></linuxio></metadata><devices><graphics type="vnc" autoport="no"></graphics></devices></domain>`

	normalized, changed, err := normalizeLinuxIOVNCGraphicsXML(xmlDoc)

	if err != nil {
		t.Fatalf("normalizeLinuxIOVNCGraphicsXML: %v", err)
	}
	if !changed {
		t.Fatal("normalizeLinuxIOVNCGraphicsXML changed = false, want true")
	}
	if !strings.Contains(normalized, `socket="/var/lib/libvirt/qemu/linuxio-legacy-vnc.vnc"`) {
		t.Fatalf("normalized XML missing reconstructed VNC socket:\n%s", normalized)
	}
	if strings.Contains(normalized, "autoport") || strings.Contains(normalized, "port=") {
		t.Fatalf("normalized XML should not include VNC TCP port attributes:\n%s", normalized)
	}
}

func TestNormalizeLinuxIOVNCGraphicsAddsMissingManagedGraphics(t *testing.T) {
	xmlDoc := `<domain type="kvm"><name>headless</name><metadata><linuxio xmlns="https://linuxio.local/libvirt/v1"><disk volume="linuxio-headless.qcow2" sizeGB="16"></disk></linuxio></metadata><devices><disk type="file" device="disk"><source file="/var/lib/libvirt/images/linuxio-headless.qcow2"></source></disk></devices></domain>`

	normalized, changed, err := normalizeLinuxIOVNCGraphicsXML(xmlDoc)
	if err != nil {
		t.Fatalf("normalizeLinuxIOVNCGraphicsXML: %v", err)
	}
	if !changed {
		t.Fatal("normalizeLinuxIOVNCGraphicsXML changed = false, want true")
	}
	if !strings.Contains(normalized, `<graphics type="vnc" socket="/var/lib/libvirt/qemu/linuxio-headless.vnc">`) {
		t.Fatalf("normalized XML missing added VNC graphics:\n%s", normalized)
	}
	if !strings.Contains(normalized, `<listen type="socket" socket="/var/lib/libvirt/qemu/linuxio-headless.vnc"></listen>`) {
		t.Fatalf("normalized XML missing VNC socket listener:\n%s", normalized)
	}
}

func TestNormalizeLinuxIOVNCGraphicsRepairsManagedDiskWithoutMetadata(t *testing.T) {
	xmlDoc := `<domain type="kvm"><name>legacy-vnc</name><devices><disk type="file" device="disk"><source file="/var/lib/libvirt/images/linuxio-legacy-vnc.qcow2"></source></disk><graphics type="vnc" autoport="no"></graphics></devices></domain>`

	normalized, changed, err := normalizeLinuxIOVNCGraphicsXML(xmlDoc)

	if err != nil {
		t.Fatalf("normalizeLinuxIOVNCGraphicsXML: %v", err)
	}
	if !changed {
		t.Fatal("normalizeLinuxIOVNCGraphicsXML changed = false, want true")
	}
	if !strings.Contains(normalized, `socket="/var/lib/libvirt/qemu/linuxio-legacy-vnc.vnc"`) {
		t.Fatalf("normalized XML missing reconstructed VNC socket:\n%s", normalized)
	}
	if strings.Contains(normalized, "autoport") || strings.Contains(normalized, "port=") {
		t.Fatalf("normalized XML should not include VNC TCP port attributes:\n%s", normalized)
	}
}

func TestNormalizeLinuxIOVNCGraphicsIgnoresUnmanagedSocketWithoutMetadata(t *testing.T) {
	xmlDoc := `<domain type="kvm"><name>external</name><devices><graphics type="vnc" socket="/var/lib/libvirt/qemu/external.vnc" autoport="no"></graphics></devices></domain>`

	_, changed, err := normalizeLinuxIOVNCGraphicsXML(xmlDoc)

	if err != nil {
		t.Fatalf("normalizeLinuxIOVNCGraphicsXML: %v", err)
	}
	if changed {
		t.Fatal("normalizeLinuxIOVNCGraphicsXML changed = true, want false")
	}
}

func TestListAndGetVMs(t *testing.T) {
	fake := newFakeConn()
	fake.domains["alpha"] = testDomain("alpha")
	fake.domainXML["alpha"] = strings.ReplaceAll(deleteTestDomainXML(), "delete-me", "alpha")
	withFakeLibvirt(t, fake)

	vms, err := ListVMs(context.Background())
	if err != nil {
		t.Fatalf("ListVMs: %v", err)
	}
	if len(vms) != 1 || vms[0].Name != "alpha" {
		t.Fatalf("ListVMs = %#v", vms)
	}

	vm, err := GetVM(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("GetVM: %v", err)
	}
	if vm.Name != "alpha" || vm.State != "shutoff" {
		t.Fatalf("GetVM = %#v", vm)
	}
}

func TestGetVMReturnsNotFoundError(t *testing.T) {
	withFakeLibvirt(t, newFakeConn())

	_, err := GetVM(context.Background(), "missing")

	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("GetVM error = %v, want not found", err)
	}
	if code := errorCode(err, 0); code != 404 {
		t.Fatalf("error code = %d, want 404", code)
	}
}

func TestDomainLookupErrorsAreNotCollapsedToNotFound(t *testing.T) {
	tests := []struct {
		name string
		run  func(context.Context) error
	}{
		{
			name: "get",
			run: func(ctx context.Context) error {
				_, err := GetVM(ctx, "lookup-fail")
				return err
			},
		},
		{
			name: "lifecycle",
			run: func(ctx context.Context) error {
				return StartVM(ctx, "lookup-fail")
			},
		},
		{
			name: "delete",
			run: func(ctx context.Context) error {
				_, err := DeleteVM(ctx, apischema.VMDeleteRequest{Name: "lookup-fail"})
				return err
			},
		},
		{
			name: "console",
			run: func(ctx context.Context) error {
				return withLibvirtConn(ctx, func(conn libvirtConn) error {
					_, err := consoleEndpointFromDomain(conn, "lookup-fail")
					return err
				})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newFakeConn()
			fake.domainLookupErr = libvirtErr(libvirt.ErrInternalError, "lookup failed")
			withFakeLibvirt(t, fake)

			err := tt.run(context.Background())
			if err == nil || !strings.Contains(err.Error(), "look up VM") {
				t.Fatalf("%s error = %v, want wrapped lookup error", tt.name, err)
			}
			if code := errorCode(err, 0); code == 404 {
				t.Fatalf("%s error code = 404, want non-404 lookup failure", tt.name)
			}
		})
	}
}

func TestListVMsIncludesDHCPLeaseIPAddresses(t *testing.T) {
	fake := newFakeConn()
	fake.domains["lease-vm"] = testDomain("lease-vm")
	fake.domainXML["lease-vm"] = `<domain type="kvm">
  <name>lease-vm</name>
  <metadata><linuxio xmlns="https://linuxio.local/libvirt/v1"><disk volume="linuxio-lease-vm.qcow2" sizeGB="12"></disk></linuxio></metadata>
  <memory unit="MiB">1024</memory>
  <vcpu>1</vcpu>
  <devices>
    <disk type="file" device="disk"><source file="/var/lib/libvirt/images/linuxio-lease-vm.qcow2"></source><target dev="vda" bus="virtio"></target></disk>
    <interface type="network"><mac address="52:54:00:7d:a3:19"></mac><source network="default"></source><model type="virtio"></model></interface>
  </devices>
</domain>`
	fake.networkLeases["default"] = []libvirt.NetworkDhcpLease{
		{Mac: libvirt.OptString{"52:54:00:7d:a3:19"}, Ipaddr: "192.168.122.57"},
		{Mac: libvirt.OptString{"52:54:00:7d:a3:19"}, Ipaddr: "192.168.122.57"},
		{Mac: libvirt.OptString{"52:54:00:00:00:ff"}, Ipaddr: "192.168.122.99"},
	}
	withFakeLibvirt(t, fake)

	vms, err := ListVMs(context.Background())
	if err != nil {
		t.Fatalf("ListVMs: %v", err)
	}
	if len(vms) != 1 || len(vms[0].NICs) != 1 {
		t.Fatalf("ListVMs NICs = %#v", vms)
	}
	if got := vms[0].NICs[0].IPAddresses; !slices.Equal(got, []string{"192.168.122.57"}) {
		t.Fatalf("IPAddresses = %#v", got)
	}
}

func TestConsoleSocketHelpers(t *testing.T) {
	if !isSafeVNCSocket("/var/lib/libvirt/qemu/linuxio-test.vnc") {
		t.Fatal("expected LinuxIO VNC socket path to be safe")
	}
	for _, unsafePath := range []string{
		"/tmp/linuxio-test.vnc",
		"/var/lib/libvirt/qemu/../secret",
		"/var/lib/libvirt/qemu",
	} {
		if isSafeVNCSocket(unsafePath) {
			t.Fatalf("isSafeVNCSocket(%q) = true, want false", unsafePath)
		}
	}

	socketEndpoint := vncEndpointFromDomainXML(`<domain><devices><graphics type="vnc" socket="/var/lib/libvirt/qemu/linuxio-test.vnc" autoport="no"></graphics></devices></domain>`)
	if socketEndpoint.Network != "unix" || socketEndpoint.Address != "/var/lib/libvirt/qemu/linuxio-test.vnc" {
		t.Fatalf("vncEndpointFromDomainXML (socket attr) = %#v", socketEndpoint)
	}

	// libvirt rewrites the socket attribute into a <listen type="socket">
	// child element when the domain is defined and started, so the running
	// XML carries the path on the listener rather than the attribute.
	listenerEndpoint := vncEndpointFromDomainXML(`<domain><devices><graphics type="vnc"><listen type="socket" socket="/var/lib/libvirt/qemu/linuxio-test.vnc"></listen></graphics></devices></domain>`)
	if listenerEndpoint.Network != "unix" || listenerEndpoint.Address != "/var/lib/libvirt/qemu/linuxio-test.vnc" {
		t.Fatalf("vncEndpointFromDomainXML (listener form) = %#v", listenerEndpoint)
	}

	if empty := vncEndpointFromDomainXML(`<domain><devices><graphics type="vnc" autoport="yes"></graphics></devices></domain>`); empty.Network != "" {
		t.Fatalf("vncEndpointFromDomainXML (no endpoint) = %#v, want empty", empty)
	}

	tcpEndpoint := vncEndpointFromDomainXML(`<domain><devices><graphics type="vnc" port="5900" autoport="yes" listen="127.0.0.1"><listen type="address" address="127.0.0.1"></listen></graphics></devices></domain>`)
	if tcpEndpoint.Network != "tcp" || tcpEndpoint.Address != "127.0.0.1:5900" {
		t.Fatalf("vncEndpointFromDomainXML (tcp) = %#v", tcpEndpoint)
	}
	validatedEndpoint, err := validateConsoleEndpoint("test", tcpEndpoint)
	if err != nil {
		t.Fatalf("validateConsoleEndpoint(tcp): %v", err)
	}
	if validatedEndpoint.Address != "127.0.0.1:5900" {
		t.Fatalf("validated tcp address = %q", validatedEndpoint.Address)
	}

	wildcardEndpoint := vncEndpointFromDomainXML(`<domain><devices><graphics type="vnc" port="5901" listen="0.0.0.0"></graphics></devices></domain>`)
	validatedEndpoint, err = validateConsoleEndpoint("test", wildcardEndpoint)
	if err != nil {
		t.Fatalf("validateConsoleEndpoint(wildcard): %v", err)
	}
	if validatedEndpoint.Address != "127.0.0.1:5901" {
		t.Fatalf("validated wildcard tcp address = %q", validatedEndpoint.Address)
	}

	unsafeEndpoint := vncEndpointFromDomainXML(`<domain><devices><graphics type="vnc" port="5902" listen="192.168.1.20"></graphics></devices></domain>`)
	if _, err := validateConsoleEndpoint("test", unsafeEndpoint); err == nil {
		t.Fatal("validateConsoleEndpoint(unsafe tcp) error = nil, want error")
	}
}

func TestHandleConsoleSessionWritesErrorResult(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	if err := client.SetDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("SetDeadline: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- HandleConsoleSession(context.Background(), server, apischema.NameRequest{Name: "../bad"})
	}()

	frame, err := relay.ReadRelayFrame(client)
	if err != nil {
		t.Fatalf("ReadRelayFrame(result): %v", err)
	}
	if frame.Opcode != relay.OpStreamResult || frame.StreamID != 1 {
		t.Fatalf("result frame = opcode 0x%02x stream %d", frame.Opcode, frame.StreamID)
	}
	var result relay.ResultFrame
	if unmarshalErr := json.Unmarshal(frame.Payload, &result); unmarshalErr != nil {
		t.Fatalf("json.Unmarshal(result): %v", unmarshalErr)
	}
	if result.Status != "error" || result.Error == "" {
		t.Fatalf("result = %#v, want error result", result)
	}
	if result.Code != 400 {
		t.Fatalf("result code = %d, want 400", result.Code)
	}

	closeFrame, err := relay.ReadRelayFrame(client)
	if err != nil {
		t.Fatalf("ReadRelayFrame(close): %v", err)
	}
	if closeFrame.Opcode != relay.OpStreamClose || closeFrame.StreamID != 1 {
		t.Fatalf("close frame = opcode 0x%02x stream %d", closeFrame.Opcode, closeFrame.StreamID)
	}

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("HandleConsoleSession error = nil, want validation error")
		}
	case <-time.After(time.Second):
		t.Fatal("HandleConsoleSession did not return")
	}
}

func TestHandleConsoleSessionReturnsOnClientAbort(t *testing.T) {
	fake := newFakeConn()
	fake.domainState = int32(libvirt.DomainRunning)
	fake.domains["console"] = testDomain("console")
	listener, port := listenLoopbackVNCPort(t)
	fake.domainXML["console"] = fmt.Sprintf(`<domain type="kvm">
  <name>console</name>
  <devices><graphics type="vnc" port="%d" listen="127.0.0.1"></graphics></devices>
</domain>`, port)
	withFakeLibvirt(t, fake)

	accepted := make(chan net.Conn, 1)
	acceptErr := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			acceptErr <- err
			return
		}
		accepted <- conn
	}()

	server, client := net.Pipe()
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- HandleConsoleSession(ctx, server, apischema.NameRequest{Name: "console"})
	}()

	select {
	case conn := <-accepted:
		defer conn.Close()
	case err := <-acceptErr:
		t.Fatalf("accept VNC connection: %v", err)
	case <-time.After(time.Second):
		t.Fatal("VNC connection was not accepted")
	}

	if err := relay.WriteRelayFrame(client, &relay.StreamFrame{Opcode: relay.OpStreamAbort, StreamID: 1}); err != nil {
		t.Fatalf("write abort frame: %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("HandleConsoleSession returned error after abort: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("HandleConsoleSession did not return after client abort")
	}
}

func listenLoopbackVNCPort(t *testing.T) (net.Listener, int) {
	t.Helper()
	for port := 5900; port <= 5999; port++ {
		listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			continue
		}
		t.Cleanup(func() {
			_ = listener.Close()
		})
		return listener, port
	}
	t.Fatal("could not allocate loopback VNC test port")
	return nil, 0
}

func TestDomainStateName(t *testing.T) {
	tests := map[libvirt.DomainState]string{
		libvirt.DomainRunning:     "running",
		libvirt.DomainPaused:      "paused",
		libvirt.DomainShutoff:     "shutoff",
		libvirt.DomainCrashed:     "crashed",
		libvirt.DomainPmsuspended: "suspended",
	}
	for state, want := range tests {
		if got := domainStateName(int32(state)); got != want {
			t.Fatalf("domainStateName(%v) = %q, want %q", state, got, want)
		}
	}
	if got := domainStateName(999); got != "unknown" {
		t.Fatalf("domainStateName(999) = %q, want unknown", got)
	}
}

func TestRoutesArePrivileged(t *testing.T) {
	for _, route := range Routes {
		if !route.Privileged {
			t.Fatalf("route %s is not privileged", route.Route)
		}
	}
}

func withFakeLibvirt(t *testing.T, fake *fakeConn) {
	t.Helper()
	old := withLibvirtConn
	withLibvirtConn = func(ctx context.Context, fn func(libvirtConn) error) error {
		return fn(fake)
	}
	t.Cleanup(func() { withLibvirtConn = old })
}

func withReadyPreflight(t *testing.T) {
	t.Helper()
	old := runPreflight
	runPreflight = func(context.Context, apischema.VMPreflightRequest) (apischema.VMPreflight, error) {
		return readyPreflight(), nil
	}
	t.Cleanup(func() { runPreflight = old })
	withAnyInstallMedia(t)
	withFakeMkdirAll(t)
}

func withAnyInstallMedia(t *testing.T) {
	t.Helper()
	old := validateInstallMedia
	validateInstallMedia = func(path string) (string, error) {
		return strings.TrimSpace(path), nil
	}
	t.Cleanup(func() { validateInstallMedia = old })
}

func withFakeMkdirAll(t *testing.T) {
	t.Helper()
	old := mkdirAll
	oldChmod := chmodFile
	mkdirAll = func(string, os.FileMode) error {
		return nil
	}
	chmodFile = func(string, os.FileMode) error {
		return nil
	}
	t.Cleanup(func() {
		mkdirAll = old
		chmodFile = oldChmod
	})
}

func withFakeManagedStoragePermissions(t *testing.T) {
	t.Helper()
	oldMkdirAll := mkdirAll
	oldChmodFile := chmodFile
	oldLookupOSUser := lookupOSUser
	mkdirAll = func(string, os.FileMode) error {
		return nil
	}
	chmodFile = func(string, os.FileMode) error {
		return nil
	}
	lookupOSUser = func(string) (*user.User, error) {
		return nil, errors.New("missing")
	}
	t.Cleanup(func() {
		mkdirAll = oldMkdirAll
		chmodFile = oldChmodFile
		lookupOSUser = oldLookupOSUser
	})
}

func withFakeImageImporter(t *testing.T, fn func(context.Context, vmImagePreset, string, int, vmCreateReporter) error) {
	t.Helper()
	old := importImagePresetDisk
	importImagePresetDisk = fn
	t.Cleanup(func() { importImagePresetDisk = old })
}

func withFakeCloudInitSeedCreator(t *testing.T, fn func(context.Context, apischema.VMCreateRequest, vmImagePreset, string) error) {
	t.Helper()
	old := createCloudInitSeed
	createCloudInitSeed = fn
	t.Cleanup(func() { createCloudInitSeed = old })
}

func readyPreflight() apischema.VMPreflight {
	return apischema.VMPreflight{
		KvmPresent:           true,
		QemuPresent:          true,
		LibvirtReachable:     true,
		DefaultPoolExists:    true,
		DefaultPoolActive:    true,
		DefaultNetworkExists: true,
		DefaultNetworkActive: true,
		ISOReadable:          true,
		Firmware:             apischema.VMPreflightFirmware{UEFIAvailable: true, BIOSAvailable: true},
		ManagedPaths:         apischema.VMManagedPaths{Root: managedRootPath, ISOs: managedISOPath, CloudImages: managedCloudPath},
	}
}

type fakeConn struct {
	domains                    map[string]libvirt.Domain
	domainXML                  map[string]string
	volumesByName              map[string]libvirt.StorageVol
	volumesByPath              map[string]libvirt.StorageVol
	deletedVolumes             []string
	defineErr                  error
	domainCreateErr            error
	domainIsActiveErr          error
	domainLookupErr            error
	definedPoolXML             []string
	domainCreateCount          int
	domainDestroyCount         int
	domainState                int32
	networkActive              int32
	networkActivateOnCreateErr bool
	networkAutostart           int32
	networkCreateCount         int
	networkCreateErr           error
	networkLeases              map[string][]libvirt.NetworkDhcpLease
	networkLookupErr           error
	poolActive                 int32
	poolAutostart              int32
	poolCreateCount            int
	poolLookupErr              error
	poolRefreshCount           int
	storageVolLookupErr        error
	storageVolPathLookupErr    error
	undefineFlags              libvirt.DomainUndefineFlagsValues
}

func newFakeConn() *fakeConn {
	return &fakeConn{
		domains:       make(map[string]libvirt.Domain),
		domainState:   int32(libvirt.DomainShutoff),
		domainXML:     make(map[string]string),
		networkActive: 1,
		networkLeases: make(map[string][]libvirt.NetworkDhcpLease),
		poolActive:    1,
		volumesByName: make(map[string]libvirt.StorageVol),
		volumesByPath: make(map[string]libvirt.StorageVol),
	}
}

func testDomain(name string) libvirt.Domain {
	return libvirt.Domain{Name: name}
}

func testCreatedStorage(name, path string, sizeGB int) createdVMStorage {
	return createdVMStorage{
		Boot: createdVMVolume{
			Volume: libvirt.StorageVol{Pool: defaultPoolName, Name: name, Key: path},
			Name:   name,
			Path:   path,
			SizeGB: sizeGB,
		},
	}
}

func containsString(values []string, want string) bool {
	return slices.Contains(values, want)
}

func containsFATLongName(data []byte, value string) bool {
	for offset := 0; offset+32 <= len(data); offset += 32 {
		entry := data[offset : offset+32]
		if entry[11] != 0x0f {
			continue
		}
		if decodeFATLongNameEntry(entry) == value {
			return true
		}
	}
	return false
}

func libvirtErr(code libvirt.ErrorNumber, message string) libvirt.Error {
	return libvirt.Error{Code: uint32(code), Message: message}
}

func decodeFATLongNameEntry(entry []byte) string {
	positions := []int{1, 3, 5, 7, 9, 14, 16, 18, 20, 22, 24, 28, 30}
	chars := make([]uint16, 0, len(positions))
	for _, pos := range positions {
		value := binary.LittleEndian.Uint16(entry[pos : pos+2])
		if value == 0 || value == 0xffff {
			break
		}
		chars = append(chars, value)
	}
	return string(utf16.Decode(chars))
}

func tempFile(t *testing.T, name string) string {
	t.Helper()
	path := t.TempDir() + "/" + name
	if err := os.WriteFile(path, []byte("test"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

func writeFirmwareDescriptor(t *testing.T, dir, name, executable string, features []string) {
	t.Helper()
	descriptor := map[string]any{
		"interface-types": []string{"uefi"},
		"mapping": map[string]any{
			"executable": map[string]any{
				"filename": executable,
			},
		},
		"targets": []map[string]string{
			{"architecture": "x86_64"},
		},
		"features": features,
	}
	data, err := json.Marshal(descriptor)
	if err != nil {
		t.Fatalf("json.Marshal(firmware descriptor): %v", err)
	}
	if err := os.WriteFile(dir+"/"+name, data, 0o644); err != nil {
		t.Fatalf("write firmware descriptor: %v", err)
	}
}

func (f *fakeConn) ConnectListAllDomains(int32, libvirt.ConnectListAllDomainsFlags) ([]libvirt.Domain, uint32, error) {
	out := make([]libvirt.Domain, 0, len(f.domains))
	for _, domain := range f.domains {
		out = append(out, domain)
	}
	return out, uint32(len(out)), nil
}
func (f *fakeConn) DomainCreate(domain libvirt.Domain) error {
	if f.domainCreateErr != nil {
		return f.domainCreateErr
	}
	f.domainCreateCount++
	f.domainState = int32(libvirt.DomainRunning)
	return nil
}
func (f *fakeConn) DomainDefineXML(xmlDoc string) (libvirt.Domain, error) {
	if f.defineErr != nil {
		return libvirt.Domain{}, f.defineErr
	}
	var domain libvirtxml.Domain
	if err := domain.Unmarshal(xmlDoc); err != nil {
		return libvirt.Domain{}, err
	}
	defined := testDomain(domain.Name)
	f.domains[domain.Name] = defined
	f.domainXML[domain.Name] = xmlDoc
	return defined, nil
}
func (f *fakeConn) DomainDestroy(libvirt.Domain) error {
	f.domainDestroyCount++
	f.domainState = int32(libvirt.DomainShutoff)
	return nil
}
func (f *fakeConn) DomainGetAutostart(libvirt.Domain) (int32, error) {
	return 0, nil
}
func (f *fakeConn) DomainGetInfo(libvirt.Domain) (uint8, uint64, uint64, uint16, uint64, error) {
	return uint8(libvirt.DomainShutoff), 1024 * 1024, 1024 * 1024, 1, 0, nil
}
func (f *fakeConn) DomainGetState(libvirt.Domain, uint32) (int32, int32, error) {
	return f.domainState, 0, nil
}
func (f *fakeConn) DomainGetXMLDesc(domain libvirt.Domain, flags libvirt.DomainXMLFlags) (string, error) {
	xmlDoc, ok := f.domainXML[domain.Name]
	if !ok {
		return "", errors.New("domain XML missing")
	}
	return xmlDoc, nil
}
func (f *fakeConn) DomainIsActive(libvirt.Domain) (int32, error) {
	if f.domainIsActiveErr != nil {
		return 0, f.domainIsActiveErr
	}
	switch libvirt.DomainState(f.domainState) {
	case libvirt.DomainNostate, libvirt.DomainShutoff:
		return 0, nil
	default:
		return 1, nil
	}
}
func (f *fakeConn) DomainLookupByName(name string) (libvirt.Domain, error) {
	if f.domainLookupErr != nil {
		return libvirt.Domain{}, f.domainLookupErr
	}
	domain, ok := f.domains[name]
	if !ok {
		return libvirt.Domain{}, libvirtErr(libvirt.ErrNoDomain, "domain not found")
	}
	return domain, nil
}
func (f *fakeConn) DomainReboot(libvirt.Domain, libvirt.DomainRebootFlagValues) error {
	return nil
}
func (f *fakeConn) DomainResume(libvirt.Domain) error { return nil }
func (f *fakeConn) DomainShutdown(libvirt.Domain) error {
	return nil
}
func (f *fakeConn) DomainSuspend(libvirt.Domain) error { return nil }
func (f *fakeConn) DomainUndefineFlags(domain libvirt.Domain, flags libvirt.DomainUndefineFlagsValues) error {
	f.undefineFlags = flags
	delete(f.domains, domain.Name)
	return nil
}
func (f *fakeConn) NetworkCreate(libvirt.Network) error {
	f.networkCreateCount++
	if f.networkCreateErr != nil {
		if f.networkActivateOnCreateErr {
			f.networkActive = 1
		}
		return f.networkCreateErr
	}
	f.networkActive = 1
	return nil
}
func (f *fakeConn) NetworkGetDhcpLeases(network libvirt.Network, mac libvirt.OptString, _ int32, _ uint32) ([]libvirt.NetworkDhcpLease, uint32, error) {
	leases := f.networkLeases[network.Name]
	if len(mac) == 0 {
		return leases, uint32(len(leases)), nil
	}
	out := make([]libvirt.NetworkDhcpLease, 0, len(leases))
	for _, lease := range leases {
		if leaseMatchesMAC(lease.Mac, mac[0]) {
			out = append(out, lease)
		}
	}
	return out, uint32(len(out)), nil
}
func (f *fakeConn) NetworkIsActive(libvirt.Network) (int32, error) {
	return f.networkActive, nil
}
func (f *fakeConn) NetworkLookupByName(name string) (libvirt.Network, error) {
	if f.networkLookupErr != nil {
		return libvirt.Network{}, f.networkLookupErr
	}
	return libvirt.Network{Name: name}, nil
}
func (f *fakeConn) NetworkSetAutostart(network libvirt.Network, autostart int32) error {
	f.networkAutostart = autostart
	return nil
}
func (f *fakeConn) StoragePoolCreate(libvirt.StoragePool, libvirt.StoragePoolCreateFlags) error {
	f.poolCreateCount++
	f.poolActive = 1
	return nil
}
func (f *fakeConn) StoragePoolDefineXML(xmlDoc string, flags uint32) (libvirt.StoragePool, error) {
	var pool libvirtxml.StoragePool
	if err := pool.Unmarshal(xmlDoc); err != nil {
		return libvirt.StoragePool{}, err
	}
	f.definedPoolXML = append(f.definedPoolXML, xmlDoc)
	f.poolLookupErr = nil
	f.poolActive = 0
	return libvirt.StoragePool{Name: pool.Name}, nil
}
func (f *fakeConn) StoragePoolIsActive(libvirt.StoragePool) (int32, error) {
	return f.poolActive, nil
}
func (f *fakeConn) StoragePoolLookupByName(name string) (libvirt.StoragePool, error) {
	if f.poolLookupErr != nil {
		return libvirt.StoragePool{}, f.poolLookupErr
	}
	return libvirt.StoragePool{Name: name}, nil
}
func (f *fakeConn) StoragePoolRefresh(libvirt.StoragePool, uint32) error {
	f.poolRefreshCount++
	return nil
}
func (f *fakeConn) StoragePoolSetAutostart(pool libvirt.StoragePool, autostart int32) error {
	f.poolAutostart = autostart
	return nil
}
func (f *fakeConn) StorageVolCreateXML(pool libvirt.StoragePool, xmlDoc string, flags libvirt.StorageVolCreateFlags) (libvirt.StorageVol, error) {
	var volume libvirtxml.StorageVolume
	if err := volume.Unmarshal(xmlDoc); err != nil {
		return libvirt.StorageVol{}, err
	}
	vol := libvirt.StorageVol{
		Pool: pool.Name,
		Name: volume.Name,
		Key:  "/var/lib/libvirt/images/" + volume.Name,
	}
	f.volumesByName[vol.Name] = vol
	f.volumesByPath[vol.Key] = vol
	return vol, nil
}
func (f *fakeConn) StorageVolDelete(vol libvirt.StorageVol, flags libvirt.StorageVolDeleteFlags) error {
	f.deletedVolumes = append(f.deletedVolumes, vol.Name)
	delete(f.volumesByName, vol.Name)
	delete(f.volumesByPath, vol.Key)
	return nil
}
func (f *fakeConn) StorageVolGetXMLDesc(vol libvirt.StorageVol, flags uint32) (string, error) {
	return `<volume type="file"><name>` + vol.Name + `</name><key>` + vol.Key + `</key><target><path>` + vol.Key + `</path><format type="qcow2"></format></target></volume>`, nil
}
func (f *fakeConn) StorageVolLookupByName(pool libvirt.StoragePool, name string) (libvirt.StorageVol, error) {
	if f.storageVolLookupErr != nil {
		return libvirt.StorageVol{}, f.storageVolLookupErr
	}
	vol, ok := f.volumesByName[name]
	if !ok {
		return libvirt.StorageVol{}, libvirtErr(libvirt.ErrNoStorageVol, "volume not found")
	}
	return vol, nil
}
func (f *fakeConn) StorageVolLookupByPath(path string) (libvirt.StorageVol, error) {
	if f.storageVolPathLookupErr != nil {
		return libvirt.StorageVol{}, f.storageVolPathLookupErr
	}
	vol, ok := f.volumesByPath[path]
	if !ok {
		return libvirt.StorageVol{}, libvirtErr(libvirt.ErrNoStorageVol, "volume not found")
	}
	return vol, nil
}

func deleteTestDomainXML() string {
	return `<domain type="kvm">
  <name>delete-me</name>
  <metadata><linuxio xmlns="https://linuxio.local/libvirt/v1"><disk volume="linuxio-delete-me.qcow2" path="/var/lib/libvirt/images/linuxio-delete-me.qcow2" sizeGB="12"></disk></linuxio></metadata>
  <memory unit="MiB">1024</memory>
  <vcpu>1</vcpu>
  <os><type arch="x86_64" machine="q35">hvm</type></os>
  <devices>
    <disk type="file" device="disk"><source file="/var/lib/libvirt/images/linuxio-delete-me.qcow2"></source><target dev="vda" bus="virtio"></target></disk>
    <disk type="file" device="disk"><source file="/srv/external-data.qcow2"></source><target dev="vdb" bus="virtio"></target></disk>
    <graphics type="vnc" socket="/var/lib/libvirt/qemu/linuxio-delete-me.vnc" autoport="no"></graphics>
  </devices>
</domain>`
}

func cloudSeedTestDomainXML() string {
	return `<domain type="kvm">
  <name>cloud-seed</name>
  <metadata><linuxio xmlns="https://linuxio.local/libvirt/v1"><disk volume="linuxio-cloud-seed.qcow2" path="` + managedCloudPath + `/linuxio-cloud-seed.qcow2" sizeGB="12"></disk><disk volume="linuxio-cloud-seed-seed.img" path="` + managedCloudPath + `/linuxio-cloud-seed-seed.img"></disk></linuxio></metadata>
  <memory unit="MiB">1024</memory>
  <vcpu>1</vcpu>
  <os><type arch="x86_64" machine="q35">hvm</type></os>
  <devices>
    <disk type="file" device="disk"><source file="` + managedCloudPath + `/linuxio-cloud-seed.qcow2"></source><target dev="vda" bus="virtio"></target></disk>
    <disk type="file" device="disk"><source file="` + managedCloudPath + `/linuxio-cloud-seed-seed.img"></source><target dev="vdb" bus="virtio"></target><readonly></readonly></disk>
    <disk type="file" device="cdrom"><source file="/isos/external-installer.iso"></source><target dev="sdb" bus="sata"></target><readonly></readonly></disk>
    <graphics type="vnc" socket="/var/lib/libvirt/qemu/linuxio-cloud-seed.vnc" autoport="no"></graphics>
  </devices>
</domain>`
}
