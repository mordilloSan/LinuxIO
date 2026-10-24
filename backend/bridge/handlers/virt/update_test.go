package virt

import (
	"context"
	"errors"
	"os/exec"
	"slices"
	"strings"
	"testing"

	libvirt "github.com/digitalocean/go-libvirt"
	"libvirt.org/go/libvirtxml"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func editTestVM(t *testing.T, name, xmlDoc string) *fakeConn {
	t.Helper()
	fake := newFakeConn()
	fake.domains[name] = testDomain(name)
	fake.domainXML[name] = xmlDoc
	withFakeLibvirt(t, fake)
	return fake
}

func editRequest(name string) apischema.VMUpdateRequest {
	return apischema.VMUpdateRequest{Name: name, VCPUs: 1, MemoryMB: 1024, Firmware: vmFirmwareBIOS}
}

func withRecordedRenames(t *testing.T) *[][2]string {
	t.Helper()
	var renamed [][2]string
	old := renameFile
	renameFile = func(from, to string) error {
		renamed = append(renamed, [2]string{from, to})
		return nil
	}
	t.Cleanup(func() { renameFile = old })
	return &renamed
}

func parsedDomain(t *testing.T, xmlDoc string) libvirtxml.Domain {
	t.Helper()
	var domain libvirtxml.Domain
	if err := domain.Unmarshal(xmlDoc); err != nil {
		t.Fatalf("parse domain XML: %v", err)
	}
	return domain
}

func TestUpdateVMSetsResources(t *testing.T) {
	fake := editTestVM(t, "edit-me", strings.ReplaceAll(deleteTestDomainXML(), "delete-me", "edit-me"))
	req := editRequest("edit-me")
	req.VCPUs, req.MemoryMB = 1, 2048

	vm, err := UpdateVM(context.Background(), req)
	if err != nil {
		t.Fatalf("UpdateVM: %v", err)
	}
	if vm.VCPUs != 1 || vm.MemoryMB != 2048 {
		t.Fatalf("vm = %d vCPU / %d MB, want 1 / 2048", vm.VCPUs, vm.MemoryMB)
	}
	if domain := parsedDomain(t, fake.domainXML["edit-me"]); domain.CurrentMemory == nil || domain.CurrentMemory.Value != 2048 {
		t.Fatalf("currentMemory not updated:\n%s", fake.domainXML["edit-me"])
	}
}

func TestUpdateVMSwitchesToUEFI(t *testing.T) {
	fake := editTestVM(t, "fw", strings.ReplaceAll(deleteTestDomainXML(), "delete-me", "fw"))
	withOVMF(t, true)
	req := editRequest("fw")
	req.Firmware = vmFirmwareUEFI

	vm, err := UpdateVM(context.Background(), req)
	if err != nil {
		t.Fatalf("UpdateVM: %v", err)
	}
	if vm.Firmware != vmFirmwareUEFI || !strings.Contains(fake.domainXML["fw"], `<loader stateless="no">`) {
		t.Fatalf("vm.Firmware=%q xml:\n%s", vm.Firmware, fake.domainXML["fw"])
	}
}

func TestUpdateVMSwitchesToBIOSAndRemovesNVRAM(t *testing.T) {
	fake := editTestVM(t, "fw", uefiTestDomainXML("fw"))
	var removed []string
	old := removeFile
	removeFile = func(path string) error { removed = append(removed, path); return nil }
	t.Cleanup(func() { removeFile = old })

	vm, err := UpdateVM(context.Background(), editRequest("fw"))
	if err != nil {
		t.Fatalf("UpdateVM: %v", err)
	}
	xmlDoc := fake.domainXML["fw"]
	if vm.Firmware != vmFirmwareBIOS || strings.Contains(xmlDoc, "efi") || strings.Contains(xmlDoc, "nvram") || strings.Contains(xmlDoc, "loader") {
		t.Fatalf("vm.Firmware=%q xml:\n%s", vm.Firmware, xmlDoc)
	}
	if len(removed) != 1 || removed[0] != libvirtNVRAMDir+"fw_VARS.fd" {
		t.Fatalf("removed = %v, want NVRAM file", removed)
	}
	removed = nil
	escaping := strings.Replace(uefiTestDomainXML("fw"), libvirtNVRAMDir+"fw_VARS.fd", libvirtNVRAMDir+"../../../../etc/passwd", 1)
	fake.domainXML["fw"] = escaping
	if _, err := UpdateVM(context.Background(), editRequest("fw")); err != nil || len(removed) != 0 {
		t.Fatalf("escaping NVRAM: err=%v removed=%v, want nothing removed", err, removed)
	}
}

func TestUpdateVMRejectsRunningVMAndBadInput(t *testing.T) {
	fake := editTestVM(t, "fw", strings.ReplaceAll(deleteTestDomainXML(), "delete-me", "fw"))
	fake.domainState = int32(libvirt.DomainRunning)
	before := fake.domainXML["fw"]
	if _, err := UpdateVM(context.Background(), editRequest("fw")); err == nil || !strings.Contains(err.Error(), "shut off") || fake.domainXML["fw"] != before {
		t.Fatalf("UpdateVM on running VM = %v", err)
	}

	fake.domainState = int32(libvirt.DomainShutoff)
	withOVMF(t, false)
	req := editRequest("fw")
	req.Firmware = vmFirmwareUEFI
	if _, err := UpdateVM(context.Background(), req); err == nil || !strings.Contains(err.Error(), "OVMF") {
		t.Fatalf("UpdateVM without OVMF = %v, want conflict", err)
	}
	for _, bad := range []func(*apischema.VMUpdateRequest){
		func(r *apischema.VMUpdateRequest) { r.Firmware = "zbios" },
		func(r *apischema.VMUpdateRequest) { r.VCPUs = 0 },
		func(r *apischema.VMUpdateRequest) { r.MemoryMB = 128 },
		func(r *apischema.VMUpdateRequest) { r.NewName = "../evil" },
		func(r *apischema.VMUpdateRequest) { r.DiskGB = 8 },
	} {
		req := editRequest("fw")
		bad(&req)
		if _, err := UpdateVM(context.Background(), req); err == nil {
			t.Fatalf("UpdateVM accepted %+v", req)
		}
	}
	if fake.domainXML["fw"] != before {
		t.Fatalf("rejected edits changed the VM:\n%s", fake.domainXML["fw"])
	}
}

func TestUpdateVMRenamesManagedStorage(t *testing.T) {
	fake := editTestVM(t, "edit-me", strings.ReplaceAll(cloudSeedTestDomainXML(), "cloud-seed", "edit-me"))
	renamed := withRecordedRenames(t)
	req := editRequest("edit-me")
	req.NewName = "edited"

	vm, err := UpdateVM(context.Background(), req)
	if err != nil {
		t.Fatalf("UpdateVM: %v", err)
	}
	wantMoves := [][2]string{
		{managedCloudPath + "/linuxio-edit-me.qcow2", managedCloudPath + "/linuxio-edited.qcow2"},
		{managedCloudPath + "/linuxio-edit-me-seed.img", managedCloudPath + "/linuxio-edited-seed.img"},
	}
	if !slices.Equal(*renamed, wantMoves) {
		t.Fatalf("renamed = %v, want %v", *renamed, wantMoves)
	}
	if _, ok := fake.domains["edit-me"]; ok || vm.Name != "edited" {
		t.Fatalf("domains=%v vm.Name=%q, want only edited", fake.domains, vm.Name)
	}
	xmlDoc := fake.domainXML["edited"]
	if strings.Contains(xmlDoc, "edit-me") || !strings.Contains(xmlDoc, vncSocketPath("edited")) {
		t.Fatalf("XML still references the old name:\n%s", xmlDoc)
	}
	if len(vm.OwnedDisks) != 2 || fake.poolRefreshCount != 1 {
		t.Fatalf("ownedDisks=%v poolRefreshCount=%d", vm.OwnedDisks, fake.poolRefreshCount)
	}
}

func TestUpdateVMRenameRollsBackWhenRedefineFails(t *testing.T) {
	fake := editTestVM(t, "edit-me", strings.ReplaceAll(deleteTestDomainXML(), "delete-me", "edit-me"))
	renamed := withRecordedRenames(t)
	fake.defineErr = errors.New("define failed")
	req := editRequest("edit-me")
	req.NewName = "edited"

	if _, err := UpdateVM(context.Background(), req); err == nil || !strings.Contains(err.Error(), "define failed") {
		t.Fatalf("UpdateVM = %v, want define failure", err)
	}
	if _, ok := fake.domains["edit-me"]; !ok {
		t.Fatalf("domains=%v, want the old name restored", fake.domains)
	}
	last := (*renamed)[len(*renamed)-1]
	if len(*renamed) != 2 || last[1] != defaultPoolPath+"/linuxio-edit-me.qcow2" {
		t.Fatalf("renamed = %v, want the disk moved back", *renamed)
	}
}

func TestUpdateVMRejectsTakenName(t *testing.T) {
	fake := editTestVM(t, "edit-me", strings.ReplaceAll(deleteTestDomainXML(), "delete-me", "edit-me"))
	fake.domains["edited"] = testDomain("edited")
	req := editRequest("edit-me")
	req.NewName = "edited"
	if _, err := UpdateVM(context.Background(), req); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("UpdateVM = %v, want name conflict", err)
	}
}

func TestUpdateVMGrowsManagedDisk(t *testing.T) {
	fake := editTestVM(t, "edit-me", strings.ReplaceAll(deleteTestDomainXML(), "delete-me", "edit-me"))
	var resized []string
	oldLook, oldCommand := execLookPath, execCommand
	execLookPath = func(name string) (string, error) { return name, nil }
	execCommand = func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		resized = args
		return exec.CommandContext(ctx, "true")
	}
	t.Cleanup(func() { execLookPath, execCommand = oldLook, oldCommand })

	req := editRequest("edit-me")
	req.DiskGB = 8
	if _, err := UpdateVM(context.Background(), req); err == nil || !strings.Contains(err.Error(), "only grow") {
		t.Fatalf("shrink = %v, want rejection", err)
	}
	req.DiskGB = 20
	vm, err := UpdateVM(context.Background(), req)
	if err != nil {
		t.Fatalf("UpdateVM: %v", err)
	}
	if want := []string{"resize", "-f", "qcow2", defaultPoolPath + "/linuxio-edit-me.qcow2", "20G"}; !slices.Equal(resized, want) {
		t.Fatalf("qemu-img args = %v, want %v", resized, want)
	}
	if vm.DiskGB != 20 || !strings.Contains(fake.domainXML["edit-me"], `sizeGB="20"`) {
		t.Fatalf("vm.DiskGB=%d xml:\n%s", vm.DiskGB, fake.domainXML["edit-me"])
	}
}

func TestUpdateVMMovesNICToDefaultNetwork(t *testing.T) {
	xmlDoc := strings.Replace(strings.ReplaceAll(deleteTestDomainXML(), "delete-me", "edit-me"), "<devices>",
		`<devices><interface type="bridge"><mac address="52:54:00:aa:bb:cc"></mac><source bridge="br0"></source><model type="virtio"></model></interface>`, 1)
	fake := editTestVM(t, "edit-me", xmlDoc)
	req := editRequest("edit-me")
	req.Network = defaultNetworkName

	vm, err := UpdateVM(context.Background(), req)
	if err != nil {
		t.Fatalf("UpdateVM: %v", err)
	}
	if len(vm.NICs) != 1 || vm.NICs[0].Network != defaultNetworkName || vm.NICs[0].MAC != "52:54:00:aa:bb:cc" {
		t.Fatalf("nics = %+v, want default network with the same MAC", vm.NICs)
	}
	if strings.Contains(fake.domainXML["edit-me"], "br0") {
		t.Fatalf("bridge still attached:\n%s", fake.domainXML["edit-me"])
	}
}

func TestUpdateVMSetsOS(t *testing.T) {
	xmlDoc := strings.Replace(strings.ReplaceAll(deleteTestDomainXML(), "delete-me", "edit-me"), "<metadata>",
		`<metadata><libosinfo:libosinfo xmlns:libosinfo="http://libosinfo.org/xmlns/libvirt/domain/1.0"><libosinfo:os id="http://debian.org/debian/12"/></libosinfo:libosinfo>`, 1)
	fake := editTestVM(t, "edit-me", xmlDoc)
	req := editRequest("edit-me")
	req.OS = "homeassistant"

	vm, err := UpdateVM(context.Background(), req)
	if err != nil {
		t.Fatalf("UpdateVM: %v", err)
	}
	if vm.OS != "homeassistant" || len(vm.OwnedDisks) != 1 || !strings.Contains(fake.domainXML["edit-me"], "libosinfo:os") {
		t.Fatalf("vm.OS=%q ownedDisks=%v xml:\n%s", vm.OS, vm.OwnedDisks, fake.domainXML["edit-me"])
	}
	req.OS = "Not An ID"
	if _, err := UpdateVM(context.Background(), req); err == nil {
		t.Fatal("UpdateVM accepted an invalid OS id")
	}
}

func TestSetLinuxIOMetadataKeepsOtherElements(t *testing.T) {
	domain := libvirtxml.Domain{Metadata: &libvirtxml.DomainMetadata{
		XML: `<libosinfo:libosinfo xmlns:libosinfo="http://libosinfo.org/xmlns/libvirt/domain/1.0"><libosinfo:os id="http://ubuntu.com/ubuntu/24.04"/></libosinfo:libosinfo><linuxio xmlns="https://linuxio.local/libvirt/v1"><disk volume="old.qcow2"></disk></linuxio>`,
	}}
	if err := setLinuxIOMetadata(&domain, linuxioMetadata{Disks: []linuxioMetadataDisk{{Volume: "new.qcow2"}}}); err != nil {
		t.Fatalf("setLinuxIOMetadata: %v", err)
	}
	got := domain.Metadata.XML
	if !strings.Contains(got, "libosinfo:os") || strings.Contains(got, "old.qcow2") || strings.Count(got, "new.qcow2") != 1 {
		t.Fatalf("metadata = %s", got)
	}
}
