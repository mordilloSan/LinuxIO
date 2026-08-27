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
	"libvirt.org/go/libvirtxml"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func TestListVMNetworksEnumeratesAndDeduplicatesBridges(t *testing.T) {
	root := t.TempDir()
	makeBridgeFixture(t, root, "br-lan", "up")
	makeBridgeFixture(t, root, "br-down", "down")
	makeBridgeFixture(t, root, "virbr0", "up")
	makeBridgeFixture(t, root, "br-isolated", "up")
	makeInterfaceFixture(t, root, "eth0")
	withNetworkSysfsRoot(t, root)

	fake := newFakeConn()
	fake.libvirtNetworks = []libvirt.Network{
		{Name: "default"},
		{Name: "isolated"},
	}
	fake.networkActiveByName["default"] = 1
	fake.networkActiveByName["isolated"] = 0
	fake.networkXML["default"] = `<network><name>default</name><forward mode="nat"></forward><bridge name="virbr0"></bridge></network>`
	fake.networkXML["isolated"] = `<network><name>isolated</name><forward mode="none"></forward><bridge name="br-isolated"></bridge></network>`
	withFakeLibvirt(t, fake)

	got, err := ListVMNetworks(context.Background())
	if err != nil {
		t.Fatalf("ListVMNetworks: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("networks = %#v, want three entries", got)
	}
	if got[0] != (apischema.VMNetwork{Name: "br-down", Type: bridgeNetworkType, Active: false}) {
		t.Fatalf("first network = %#v", got[0])
	}
	if got[1] != (apischema.VMNetwork{Name: "br-lan", Type: bridgeNetworkType, Active: true}) {
		t.Fatalf("second network = %#v", got[1])
	}
	if got[2] != (apischema.VMNetwork{Name: "default", Type: libvirtNetworkType, Active: true}) {
		t.Fatalf("default network = %#v", got[2])
	}
	for _, network := range got {
		if network.Name == "br-isolated" || network.Name == "isolated" {
			t.Fatalf("unsupported libvirt network exposed: %#v", got)
		}
	}
}

func TestListVMNetworksReturnsLibvirtErrors(t *testing.T) {
	fake := newFakeConn()
	fake.networkListErr = errors.New("list failed")
	withFakeLibvirt(t, fake)

	_, err := ListVMNetworks(context.Background())
	if err == nil || !strings.Contains(err.Error(), "list libvirt networks") {
		t.Fatalf("ListVMNetworks error = %v", err)
	}
}

func TestValidateNetworkSelection(t *testing.T) {
	root := t.TempDir()
	makeBridgeFixture(t, root, "br-lan", "up")
	makeBridgeFixture(t, root, "br-down", "down")
	withNetworkSysfsRoot(t, root)
	fake := newFakeConn()

	if err := validateNetworkSelection(context.Background(), fake, ""); err != nil {
		t.Fatalf("empty network: %v", err)
	}
	if err := validateNetworkSelection(context.Background(), fake, " default "); err != nil {
		t.Fatalf("default network: %v", err)
	}
	if err := validateNetworkSelection(context.Background(), fake, "br-lan"); err != nil {
		t.Fatalf("up bridge: %v", err)
	}
	if err := validateNetworkSelection(context.Background(), fake, "br-down"); errorCode(err, 0) != 409 {
		t.Fatalf("down bridge error = %v, want 409", err)
	}
	if err := validateNetworkSelection(context.Background(), fake, "missing"); errorCode(err, 0) != 409 {
		t.Fatalf("missing bridge error = %v, want 409", err)
	}
	if err := validateNetworkSelection(context.Background(), fake, "bad/name"); errorCode(err, 0) != 400 {
		t.Fatalf("malformed bridge error = %v, want 400", err)
	}
}

func TestValidateNetworkSelectionRejectsHiddenLibvirtBackingBridge(t *testing.T) {
	root := t.TempDir()
	makeBridgeFixture(t, root, "virbr0", "up")
	makeBridgeFixture(t, root, "br-lan", "up")
	withNetworkSysfsRoot(t, root)

	fake := newFakeConn()
	fake.libvirtNetworks = []libvirt.Network{{Name: "default"}}
	fake.networkActiveByName["default"] = 1
	fake.networkXML["default"] = `<network><name>default</name><bridge name="virbr0"></bridge></network>`

	if err := validateNetworkSelection(context.Background(), fake, "virbr0"); errorCode(err, 0) != 409 {
		t.Fatalf("backing bridge error = %v, want 409", err)
	}
	if err := validateNetworkSelection(context.Background(), fake, "br-lan"); err != nil {
		t.Fatalf("selectable bridge: %v", err)
	}
}

func TestPreflightReadyForNetworkAllowsBridgeWithoutDefaultNAT(t *testing.T) {
	ready := readyPreflight()
	ready.DefaultNetworkExists = false
	if err := preflightReadyForNetwork(ready, vmSourceTypeISO, "br-lan"); err != nil {
		t.Fatalf("bridge preflight = %v, want ready without default NAT", err)
	}
	if err := preflightReadyForNetwork(ready, vmSourceTypeISO, "default"); err == nil {
		t.Fatal("default preflight succeeded without default NAT")
	}
}

func TestPreflightWithoutNetworkDoesNotRequireDefaultNAT(t *testing.T) {
	fake := newFakeConn()
	fake.networkLookupErr = libvirtErr(libvirt.ErrNoNetwork, "network missing")
	withFakeLibvirt(t, fake)

	out, err := Preflight(context.Background(), apischema.VMPreflightRequest{})
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	for _, message := range out.Errors {
		if strings.Contains(message, "default NAT network") {
			t.Fatalf("omitted network preflight unexpectedly requires NAT: %#v", out.Errors)
		}
	}

	out, err = Preflight(context.Background(), apischema.VMPreflightRequest{Network: defaultNetworkName})
	if err != nil {
		t.Fatalf("explicit default Preflight: %v", err)
	}
	if !slices.ContainsFunc(out.Errors, func(message string) bool {
		return strings.Contains(message, "default NAT network")
	}) {
		t.Fatalf("explicit default preflight errors = %#v, want default NAT error", out.Errors)
	}
}

func TestCreateVMWithConnRevalidatesBridgeBeforeStorage(t *testing.T) {
	root := t.TempDir()
	makeBridgeFixture(t, root, "br-down", "down")
	withNetworkSysfsRoot(t, root)
	fake := newFakeConn()

	_, err := createVMWithConn(context.Background(), fake, apischema.VMCreateRequest{
		Name:     "bridge-vm",
		VCPUs:    1,
		MemoryMB: 1024,
		DiskGB:   8,
		ISOPath:  "/isos/test.iso",
		Network:  "br-down",
	}, apischema.VMPreflightFirmware{BIOSAvailable: true}, nil)
	if errorCode(err, 0) != 409 {
		t.Fatalf("create error = %v, want bridge conflict", err)
	}
	if len(fake.volumesByName) != 0 || len(fake.domains) != 0 {
		t.Fatalf("create mutated storage before bridge validation: volumes=%#v domains=%#v", fake.volumesByName, fake.domains)
	}
}

func TestCreateVMWithConnSkipsDefaultNATForBridge(t *testing.T) {
	root := t.TempDir()
	makeBridgeFixture(t, root, "br-lan", "up")
	withNetworkSysfsRoot(t, root)
	fake := newFakeConn()
	fake.defineErr = errors.New("define failed")
	withFakeMkdirAll(t)

	_, err := createVMWithConn(context.Background(), fake, apischema.VMCreateRequest{
		Name:     "bridge-vm",
		VCPUs:    1,
		MemoryMB: 1024,
		DiskGB:   8,
		ISOPath:  "/isos/test.iso",
		Network:  "br-lan",
	}, apischema.VMPreflightFirmware{BIOSAvailable: true}, nil)
	if err == nil || !strings.Contains(err.Error(), "define") {
		t.Fatalf("create error = %v, want define failure", err)
	}
	if fake.networkCreateCount != 0 {
		t.Fatalf("networkCreateCount = %d, want bridge create to skip default NAT", fake.networkCreateCount)
	}
}

func TestBuildDomainXMLForHostBridge(t *testing.T) {
	domain, err := buildDomain(apischema.VMCreateRequest{
		Name:     "bridge-vm",
		VCPUs:    2,
		MemoryMB: 1024,
		DiskGB:   8,
		ISOPath:  "/isos/test.iso",
		Network:  "br-lan",
	}, testCreatedStorage("linuxio-bridge-vm.qcow2", "/var/lib/libvirt/images/linuxio-bridge-vm.qcow2", 8), apischema.VMPreflightFirmware{BIOSAvailable: true})
	if err != nil {
		t.Fatalf("buildDomain: %v", err)
	}
	xmlDoc, err := domain.Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(xmlDoc, `<interface type="bridge">`) || !strings.Contains(xmlDoc, `<source bridge="br-lan"></source>`) {
		t.Fatalf("bridge XML missing bridge source:\n%s", xmlDoc)
	}
	if strings.Contains(xmlDoc, `source network="default"`) {
		t.Fatalf("bridge XML unexpectedly contains default network:\n%s", xmlDoc)
	}
}

func TestMapNICAttachmentType(t *testing.T) {
	nat := mapNIC(libvirtxml.DomainInterface{
		Source: &libvirtxml.DomainInterfaceSource{Network: &libvirtxml.DomainInterfaceSourceNetwork{Network: "default"}},
	})
	if nat.Network != "default" || nat.AttachmentType != "nat" {
		t.Fatalf("NAT NIC = %#v", nat)
	}
	bridge := mapNIC(libvirtxml.DomainInterface{
		Source: &libvirtxml.DomainInterfaceSource{Bridge: &libvirtxml.DomainInterfaceSourceBridge{Bridge: "br-lan"}},
	})
	if bridge.Network != "br-lan" || bridge.AttachmentType != "bridge" {
		t.Fatalf("bridge NIC = %#v", bridge)
	}
	external := mapNIC(libvirtxml.DomainInterface{
		Source: &libvirtxml.DomainInterfaceSource{Network: &libvirtxml.DomainInterfaceSourceNetwork{Network: "external"}},
	})
	if external.AttachmentType != "network" {
		t.Fatalf("external NIC = %#v", external)
	}
}

func TestDomainBridgeAddressesQueriesEachSourceOnce(t *testing.T) {
	fake := newFakeConn()
	arpMAC := "52:54:00:7d:a3:19"
	agentMAC := "52:54:00:7d:a3:20"
	fake.domainInterfaceAddresses[uint32(libvirt.DomainInterfaceAddressesSrcArp)] = []libvirt.DomainInterface{
		{Hwaddr: libvirt.OptString{arpMAC}, Addrs: []libvirt.DomainIPAddr{{Addr: "192.0.2.10"}, {Addr: "192.0.2.10"}}},
	}
	fake.domainInterfaceAddresses[uint32(libvirt.DomainInterfaceAddressesSrcAgent)] = []libvirt.DomainInterface{
		{Hwaddr: libvirt.OptString{agentMAC}, Addrs: []libvirt.DomainIPAddr{{Addr: "192.0.2.11"}}},
	}
	vm := apischema.VirtualMachine{
		State: "running",
		NICs: []apischema.VMNIC{
			{AttachmentType: "bridge", MAC: arpMAC},
			{AttachmentType: "bridge", MAC: agentMAC},
		},
	}
	enrichDomainBridgeAddresses(context.Background(), fake, testDomain("mixed"), &vm)

	if !slices.Equal(vm.NICs[0].IPAddresses, []string{"192.0.2.10"}) {
		t.Fatalf("ARP addresses = %#v", vm.NICs[0].IPAddresses)
	}
	if !slices.Equal(vm.NICs[1].IPAddresses, []string{"192.0.2.11"}) {
		t.Fatalf("agent addresses = %#v", vm.NICs[1].IPAddresses)
	}
	for _, source := range []uint32{uint32(libvirt.DomainInterfaceAddressesSrcArp), uint32(libvirt.DomainInterfaceAddressesSrcAgent)} {
		if fake.domainInterfaceAddressCalls[source] != 1 {
			t.Fatalf("source %d calls = %d, want 1", source, fake.domainInterfaceAddressCalls[source])
		}
	}
}

func TestDomainBridgeAddressesFallsBackToAgentAfterARPError(t *testing.T) {
	fake := newFakeConn()
	mac := "52:54:00:7d:a3:21"
	fake.domainInterfaceAddressErr[uint32(libvirt.DomainInterfaceAddressesSrcArp)] = errors.New("ARP unavailable")
	fake.domainInterfaceAddresses[uint32(libvirt.DomainInterfaceAddressesSrcAgent)] = []libvirt.DomainInterface{
		{Hwaddr: libvirt.OptString{mac}, Addrs: []libvirt.DomainIPAddr{{Addr: "192.0.2.12"}}},
	}
	vm := apischema.VirtualMachine{
		State: "running",
		NICs:  []apischema.VMNIC{{AttachmentType: "bridge", MAC: mac}},
	}
	enrichDomainBridgeAddresses(context.Background(), fake, testDomain("agent"), &vm)
	if !slices.Equal(vm.NICs[0].IPAddresses, []string{"192.0.2.12"}) {
		t.Fatalf("agent addresses = %#v", vm.NICs[0].IPAddresses)
	}
}

func TestCloudInitUserDataInstallsGuestAgent(t *testing.T) {
	data, err := buildCloudInitUserData(apischema.VMCreateRequest{
		Name:              "cloud-vm",
		CloudInitUsername: "linuxio",
		CloudInitPassword: "secret",
	}, vmImagePresets[vmImagePresetDebian])
	if err != nil {
		t.Fatalf("buildCloudInitUserData: %v", err)
	}
	if !strings.Contains(string(data), "qemu-guest-agent") {
		t.Fatalf("cloud-init user data missing qemu-guest-agent:\n%s", data)
	}
}

func TestListHostBridgesChecksContext(t *testing.T) {
	root := t.TempDir()
	makeBridgeFixture(t, root, "br-lan", "up")
	withNetworkSysfsRoot(t, root)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := listHostBridges(ctx, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("listHostBridges error = %v, want cancellation", err)
	}
}

func withNetworkSysfsRoot(t *testing.T, root string) {
	t.Helper()
	old := networkSysfsRoot
	networkSysfsRoot = root
	t.Cleanup(func() { networkSysfsRoot = old })
}

func makeBridgeFixture(t *testing.T, root, name, state string) {
	t.Helper()
	path := filepath.Join(root, name, "bridge")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir bridge fixture: %v", err)
	}
	flags := "0x1002"
	if state == "up" {
		flags = "0x1003"
	}
	if err := os.WriteFile(filepath.Join(root, name, "flags"), []byte(flags), 0o644); err != nil {
		t.Fatalf("write bridge fixture state: %v", err)
	}
}

func makeInterfaceFixture(t *testing.T, root, name string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
		t.Fatalf("mkdir interface fixture: %v", err)
	}
}
