package virt

import (
	"context"
	"strings"
	"testing"

	libvirt "github.com/digitalocean/go-libvirt"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func withOVMF(t *testing.T, available bool) {
	t.Helper()
	old := findOVMFCodePath
	findOVMFCodePath = func() string {
		if available {
			return "/usr/share/OVMF/OVMF_CODE_4M.fd"
		}
		return ""
	}
	t.Cleanup(func() { findOVMFCodePath = old })
}

func uefiTestDomainXML(name string) string {
	return strings.ReplaceAll(strings.Replace(deleteTestDomainXML(),
		`<os><type arch="x86_64" machine="q35">hvm</type></os>`,
		`<os firmware="efi"><type arch="x86_64" machine="q35">hvm</type><loader readonly="yes" type="pflash">/usr/share/OVMF/OVMF_CODE_4M.fd</loader><nvram>`+libvirtNVRAMDir+`delete-me_VARS.fd</nvram></os>`, 1),
		"delete-me", name)
}

func TestCreateVMHonorsRequestedFirmware(t *testing.T) {
	for _, tc := range []struct {
		firmware apischema.VMFirmware
		wantEFI  bool
	}{{vmFirmwareBIOS, false}, {vmFirmwareUEFI, true}, {"", true}} {
		t.Run(string(tc.firmware), func(t *testing.T) {
			fake := newFakeConn()
			withFakeLibvirt(t, fake)
			withReadyPreflight(t)
			vm, err := createVMForTest(context.Background(), apischema.VMCreateRequest{
				Name: "fw", VCPUs: 1, MemoryMB: 1024, DiskGB: 8, ISOPath: "/isos/x.iso", Firmware: tc.firmware,
			})
			if err != nil {
				t.Fatalf("CreateVM: %v", err)
			}
			if gotEFI := strings.Contains(fake.domainXML["fw"], `firmware="efi"`); gotEFI != tc.wantEFI {
				t.Fatalf("efi=%t, want %t:\n%s", gotEFI, tc.wantEFI, fake.domainXML["fw"])
			}
			want := vmFirmwareBIOS
			if tc.wantEFI {
				want = vmFirmwareUEFI
			}
			if vm.Firmware != want {
				t.Fatalf("vm.Firmware = %q, want %q", vm.Firmware, want)
			}
		})
	}
}

func TestCreateVMRejectsUEFIWithoutOVMF(t *testing.T) {
	fake := newFakeConn()
	withFakeLibvirt(t, fake)
	withReadyPreflight(t)
	old := runPreflight
	runPreflight = func(context.Context, apischema.VMPreflightRequest) (apischema.VMPreflight, error) {
		p := readyPreflight()
		p.Firmware.UEFIAvailable = false
		return p, nil
	}
	t.Cleanup(func() { runPreflight = old })

	_, err := createVMForTest(context.Background(), apischema.VMCreateRequest{
		Name: "fw", VCPUs: 1, MemoryMB: 1024, DiskGB: 8, ISOPath: "/isos/x.iso", Firmware: vmFirmwareUEFI,
	})
	if err == nil || !strings.Contains(err.Error(), "OVMF") {
		t.Fatalf("CreateVM error = %v, want OVMF conflict", err)
	}
	if len(fake.domains) != 0 || len(fake.deletedVolumes) != 1 {
		t.Fatalf("domains=%d deletedVolumes=%v, want rollback", len(fake.domains), fake.deletedVolumes)
	}
}

func TestCollectImportPreflightDetectsOVAFirmware(t *testing.T) {
	fakeImportCommands(t, "1")
	for ovf, want := range map[string]apischema.VMFirmware{
		`<Envelope><vmw:Config vmw:key="firmware" vmw:value="bios"/></Envelope>`: vmFirmwareBIOS,
		`<Envelope><VirtualSystem tnas:use_ovmf="true"/></Envelope>`:             "",
	} {
		path := writeOVA(t, ovf)
		out := apischema.VMPreflight{}
		collectImportPreflight(apischema.VMPreflightRequest{ImportPath: &path}, &out)
		if len(out.Errors) != 0 || out.Firmware.Detected != want {
			t.Fatalf("ovf %s: errors=%v detected=%q, want %q", ovf, out.Errors, out.Firmware.Detected, want)
		}
	}
}

func TestSetVMFirmwareSwitchesToUEFI(t *testing.T) {
	fake := newFakeConn()
	fake.domains["fw"] = testDomain("fw")
	fake.domainXML["fw"] = strings.ReplaceAll(deleteTestDomainXML(), "delete-me", "fw")
	withFakeLibvirt(t, fake)
	withOVMF(t, true)

	vm, err := SetVMFirmware(context.Background(), apischema.VMFirmwareRequest{Name: "fw", Firmware: vmFirmwareUEFI})
	if err != nil {
		t.Fatalf("SetVMFirmware: %v", err)
	}
	if vm.Firmware != vmFirmwareUEFI || !strings.Contains(fake.domainXML["fw"], `<loader stateless="no">`) {
		t.Fatalf("vm.Firmware=%q xml:\n%s", vm.Firmware, fake.domainXML["fw"])
	}
}

func TestSetVMFirmwareSwitchesToBIOSAndRemovesNVRAM(t *testing.T) {
	fake := newFakeConn()
	fake.domains["fw"] = testDomain("fw")
	fake.domainXML["fw"] = uefiTestDomainXML("fw")
	withFakeLibvirt(t, fake)
	var removed []string
	old := removeFile
	removeFile = func(path string) error { removed = append(removed, path); return nil }
	t.Cleanup(func() { removeFile = old })

	vm, err := SetVMFirmware(context.Background(), apischema.VMFirmwareRequest{Name: "fw", Firmware: vmFirmwareBIOS})
	if err != nil {
		t.Fatalf("SetVMFirmware: %v", err)
	}
	xmlDoc := fake.domainXML["fw"]
	if vm.Firmware != vmFirmwareBIOS || strings.Contains(xmlDoc, "efi") || strings.Contains(xmlDoc, "nvram") || strings.Contains(xmlDoc, "loader") {
		t.Fatalf("vm.Firmware=%q xml:\n%s", vm.Firmware, xmlDoc)
	}
	if len(removed) != 1 || removed[0] != libvirtNVRAMDir+"fw_VARS.fd" {
		t.Fatalf("removed = %v, want NVRAM file", removed)
	}
}

func TestSetVMFirmwareRejectsRunningVMAndMissingOVMF(t *testing.T) {
	fake := newFakeConn()
	fake.domains["fw"] = testDomain("fw")
	fake.domainXML["fw"] = strings.ReplaceAll(deleteTestDomainXML(), "delete-me", "fw")
	fake.domainState = int32(libvirt.DomainRunning)
	withFakeLibvirt(t, fake)
	withOVMF(t, true)

	before := fake.domainXML["fw"]
	_, err := SetVMFirmware(context.Background(), apischema.VMFirmwareRequest{Name: "fw", Firmware: vmFirmwareUEFI})
	if err == nil || !strings.Contains(err.Error(), "shut off") || fake.domainXML["fw"] != before {
		t.Fatalf("SetVMFirmware on running VM = %v", err)
	}

	withOVMF(t, false)
	if _, err := SetVMFirmware(context.Background(), apischema.VMFirmwareRequest{Name: "fw", Firmware: vmFirmwareUEFI}); err == nil || !strings.Contains(err.Error(), "OVMF") {
		t.Fatalf("SetVMFirmware without OVMF = %v, want conflict", err)
	}
	if _, err := SetVMFirmware(context.Background(), apischema.VMFirmwareRequest{Name: "fw", Firmware: "zbios"}); err == nil {
		t.Fatal("SetVMFirmware accepted an unknown firmware")
	}
}

func TestSetVMAutostart(t *testing.T) {
	fake := newFakeConn()
	fake.domains["fw"] = testDomain("fw")
	fake.domainXML["fw"] = strings.ReplaceAll(deleteTestDomainXML(), "delete-me", "fw")
	withFakeLibvirt(t, fake)

	vm, err := SetVMAutostart(context.Background(), apischema.VMAutostartRequest{Name: "fw", Autostart: true})
	if err != nil || !vm.Autostart || fake.domainAutostart != 1 {
		t.Fatalf("enable: err=%v vm.Autostart=%v raw=%d", err, vm.Autostart, fake.domainAutostart)
	}
	vm, err = SetVMAutostart(context.Background(), apischema.VMAutostartRequest{Name: "fw"})
	if err != nil || vm.Autostart || fake.domainAutostart != 0 {
		t.Fatalf("disable: err=%v vm.Autostart=%v raw=%d", err, vm.Autostart, fake.domainAutostart)
	}
	if _, err = SetVMAutostart(context.Background(), apischema.VMAutostartRequest{Name: "missing", Autostart: true}); err == nil {
		t.Fatal("expected not-found error")
	}
}
