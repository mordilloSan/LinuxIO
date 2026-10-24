package virt

import (
	"strings"
	"testing"

	libvirt "github.com/digitalocean/go-libvirt"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func TestUUIDString(t *testing.T) {
	tests := []struct {
		name  string
		value libvirt.UUID
		want  string
	}{
		{name: "zero"},
		{
			name:  "canonical",
			value: libvirt.UUID{0x12, 0x3e, 0x45, 0x67, 0xe8, 0x9b, 0x12, 0xd3, 0xa4, 0x56, 0x42, 0x66, 0x14, 0x17, 0x40, 0x00},
			want:  "123e4567-e89b-12d3-a456-426614174000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := uuidString(tt.value); got != tt.want {
				t.Fatalf("uuidString(%v) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

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
		"<loader stateless=\"no\"></loader>",
		"<feature enabled=\"no\" name=\"secure-boot\"></feature>",
		"<feature enabled=\"no\" name=\"enrolled-keys\"></feature>",
		"<features>",
		"<acpi></acpi>",
		"<apic></apic>",
		"linuxio-test-vm.qcow2",
		"/isos/debian.iso",
		"<graphics type=\"vnc\" socket=\"/var/lib/libvirt/qemu/linuxio-test-vm.vnc\">",
		"<listen type=\"socket\" socket=\"/var/lib/libvirt/qemu/linuxio-test-vm.vnc\"></listen>",
		"org.qemu.guest_agent.0",
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
		`os="homeassistant"`,
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

func TestParseVirtualMachineReadsOS(t *testing.T) {
	domainXML := func(metadata string) string {
		return `<domain type="kvm"><name>os-test</name><metadata>` + metadata + `</metadata><memory unit="MiB">1024</memory><vcpu>1</vcpu><os><type arch="x86_64" machine="q35">hvm</type></os></domain>`
	}
	for _, tc := range []struct {
		name, metadata, want string
	}{
		{"linuxio", `<linuxio xmlns="https://linuxio.local/libvirt/v1" os="ubuntu"></linuxio>`, "ubuntu"},
		{"libosinfo", `<libosinfo:libosinfo xmlns:libosinfo="http://libosinfo.org/xmlns/libvirt/domain/1.0"><libosinfo:os id="http://microsoft.com/win/11"/></libosinfo:libosinfo>`, "win"},
		{"linuxio wins", `<linuxio xmlns="https://linuxio.local/libvirt/v1" os="debian"></linuxio><libosinfo:libosinfo xmlns:libosinfo="http://libosinfo.org/xmlns/libvirt/domain/1.0"><libosinfo:os id="http://fedoraproject.org/fedora/40"/></libosinfo:libosinfo>`, "debian"},
		{"none", ``, ""},
	} {
		vm, err := parseVirtualMachine(testDomain("os-test"), domainXML(tc.metadata), "shutoff", false)
		if err != nil {
			t.Fatalf("%s: parseVirtualMachine: %v", tc.name, err)
		}
		if vm.OS != tc.want {
			t.Fatalf("%s: OS = %q, want %q", tc.name, vm.OS, tc.want)
		}
	}
}
