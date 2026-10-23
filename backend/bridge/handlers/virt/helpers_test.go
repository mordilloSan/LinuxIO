package virt

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"os/user"
	"strings"
	"testing"
	"unicode/utf16"

	libvirt "github.com/digitalocean/go-libvirt"
	"libvirt.org/go/libvirtxml"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

func createVMForTest(ctx context.Context, req apischema.VMCreateRequest) (apischema.VirtualMachine, error) {
	return CreateVMWithProgress(ctx, req, nil)
}

func withFakeLibvirt(t *testing.T, fake libvirtConn) {
	t.Helper()
	old := withLibvirtConn
	withLibvirtConn = func(ctx context.Context, fn func(libvirtConn) error) error {
		return fn(fake)
	}
	oldLockDir := vmLockDir
	vmLockDir = t.TempDir()
	t.Cleanup(func() {
		withLibvirtConn = old
		vmLockDir = oldLockDir
	})
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

func withFakeImageImporter(t *testing.T, fn func(context.Context, vmImagePreset, string, string, int, vmCreateReporter) error) {
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
	domains                     map[string]libvirt.Domain
	domainXML                   map[string]string
	volumesByName               map[string]libvirt.StorageVol
	volumesByPath               map[string]libvirt.StorageVol
	deletedVolumes              []string
	defineErr                   error
	domainRenameErr             error
	domainCreateErr             error
	domainIsActiveErr           error
	domainLookupErr             error
	definedPoolXML              []string
	domainCreateCount           int
	domainDestroyCount          int
	domainState                 int32
	domainAutostart             int32
	networkActive               int32
	networkActivateOnCreateErr  bool
	networkAutostart            int32
	networkCreateCount          int
	networkCreateErr            error
	networkLeases               map[string][]libvirt.NetworkDhcpLease
	networkLookupErr            error
	libvirtNetworks             []libvirt.Network
	networkXML                  map[string]string
	networkActiveByName         map[string]int32
	networkListErr              error
	networkXMLReadErr           error
	domainInterfaceAddresses    map[uint32][]libvirt.DomainInterface
	domainInterfaceAddressErr   map[uint32]error
	domainInterfaceAddressCalls map[uint32]int
	poolActive                  int32
	poolAutostart               int32
	poolCreateCount             int
	poolLookupErr               error
	poolRefreshCount            int
	storageVolLookupErr         error
	storageVolPathLookupErr     error
	undefineFlags               libvirt.DomainUndefineFlagsValues
	domainStats                 []libvirt.DomainStatsRecord
	domainStatsErr              error
	pools                       []libvirt.StoragePool
	poolXML                     map[string]string
	poolInfo                    map[string][3]uint64
	poolActiveByName            map[string]int32
}

func newFakeConn() *fakeConn {
	return &fakeConn{
		domains:                     make(map[string]libvirt.Domain),
		domainState:                 int32(libvirt.DomainShutoff),
		domainXML:                   make(map[string]string),
		networkActive:               1,
		networkLeases:               make(map[string][]libvirt.NetworkDhcpLease),
		networkXML:                  make(map[string]string),
		networkActiveByName:         make(map[string]int32),
		domainInterfaceAddresses:    make(map[uint32][]libvirt.DomainInterface),
		domainInterfaceAddressErr:   make(map[uint32]error),
		domainInterfaceAddressCalls: make(map[uint32]int),
		poolActive:                  1,
		volumesByName:               make(map[string]libvirt.StorageVol),
		volumesByPath:               make(map[string]libvirt.StorageVol),
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

func (f *fakeConn) ConnectGetAllDomainStats([]libvirt.Domain, uint32, uint32) ([]libvirt.DomainStatsRecord, error) {
	return f.domainStats, f.domainStatsErr
}

func (f *fakeConn) ConnectListAllStoragePools(int32, libvirt.ConnectListAllStoragePoolsFlags) ([]libvirt.StoragePool, uint32, error) {
	return f.pools, uint32(len(f.pools)), nil
}

func (f *fakeConn) StoragePoolGetInfo(pool libvirt.StoragePool) (uint8, uint64, uint64, uint64, error) {
	info := f.poolInfo[pool.Name]
	return uint8(libvirt.StoragePoolRunning), info[0], info[1], info[2], nil
}

func (f *fakeConn) StoragePoolGetXMLDesc(pool libvirt.StoragePool, _ libvirt.StorageXMLFlags) (string, error) {
	return f.poolXML[pool.Name], nil
}

func (f *fakeConn) ConnectListAllDomains(int32, libvirt.ConnectListAllDomainsFlags) ([]libvirt.Domain, uint32, error) {
	out := make([]libvirt.Domain, 0, len(f.domains))
	for _, domain := range f.domains {
		out = append(out, domain)
	}
	return out, uint32(len(out)), nil
}

func (f *fakeConn) ConnectListAllNetworks(int32, libvirt.ConnectListAllNetworksFlags) ([]libvirt.Network, uint32, error) {
	if f.networkListErr != nil {
		return nil, 0, f.networkListErr
	}
	out := append([]libvirt.Network(nil), f.libvirtNetworks...)
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
	return f.domainAutostart, nil
}

func (f *fakeConn) DomainSetAutostart(_ libvirt.Domain, autostart int32) error {
	f.domainAutostart = autostart
	return nil
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

func (f *fakeConn) DomainInterfaceAddresses(domain libvirt.Domain, source uint32, flags uint32) ([]libvirt.DomainInterface, error) {
	f.domainInterfaceAddressCalls[source]++
	if err := f.domainInterfaceAddressErr[source]; err != nil {
		return nil, err
	}
	return f.domainInterfaceAddresses[source], nil
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

func (f *fakeConn) DomainRename(domain libvirt.Domain, newName libvirt.OptString, _ uint32) (int32, error) {
	if f.domainRenameErr != nil {
		return -1, f.domainRenameErr
	}
	name := newName[0]
	f.domains[name] = testDomain(name)
	f.domainXML[name] = strings.Replace(f.domainXML[domain.Name], "<name>"+domain.Name+"</name>", "<name>"+name+"</name>", 1)
	delete(f.domains, domain.Name)
	delete(f.domainXML, domain.Name)
	return 0, nil
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

func (f *fakeConn) NetworkGetXMLDesc(network libvirt.Network, _ uint32) (string, error) {
	if f.networkXMLReadErr != nil {
		return "", f.networkXMLReadErr
	}
	if xmlDoc, ok := f.networkXML[network.Name]; ok {
		return xmlDoc, nil
	}
	return `<network><name>` + network.Name + `</name></network>`, nil
}

func (f *fakeConn) NetworkIsActive(network libvirt.Network) (int32, error) {
	if active, ok := f.networkActiveByName[network.Name]; ok {
		return active, nil
	}
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

func (f *fakeConn) StoragePoolIsActive(pool libvirt.StoragePool) (int32, error) {
	if active, ok := f.poolActiveByName[pool.Name]; ok {
		return active, nil
	}
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

// errorCode reads the bridge error code tests assert on; fallback when the
// error carries none.
func errorCode(err error, fallback int) int {
	var bridgeErr *bridgeipc.Error
	if errors.As(err, &bridgeErr) && bridgeErr.Code != 0 {
		return bridgeErr.Code
	}
	return fallback
}
