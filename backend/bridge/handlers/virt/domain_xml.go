package virt

import (
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"path/filepath"
	"strings"

	libvirt "github.com/digitalocean/go-libvirt"
	"libvirt.org/go/libvirtxml"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

type linuxioMetadata struct {
	XMLName xml.Name              `xml:"https://linuxio.local/libvirt/v1 linuxio"`
	Disks   []linuxioMetadataDisk `xml:"disk"`
}

type linuxioMetadataDisk struct {
	Volume string `xml:"volume,attr"`
	Path   string `xml:"path,attr,omitempty"`
	SizeGB int    `xml:"sizeGB,attr,omitempty"`
}

func buildDomain(req apischema.VMCreateRequest, storage createdVMStorage, firmware apischema.VMPreflightFirmware) (libvirtxml.Domain, error) {
	if err := validateCreateRequest(req); err != nil {
		return libvirtxml.Domain{}, err
	}

	metaDisks := []linuxioMetadataDisk{
		{
			Volume: storage.Boot.Name,
			Path:   storage.Boot.Path,
			SizeGB: storage.Boot.SizeGB,
		},
	}
	if storage.Seed != nil {
		metaDisks = append(metaDisks, linuxioMetadataDisk{
			Volume: storage.Seed.Name,
			Path:   storage.Seed.Path,
		})
	}
	metaXML, err := linuxioMetadata{
		Disks: metaDisks,
	}.marshal()
	if err != nil {
		return libvirtxml.Domain{}, err
	}

	osConfig := &libvirtxml.DomainOS{
		Type: &libvirtxml.DomainOSType{
			Arch:    "x86_64",
			Machine: "q35",
			Type:    "hvm",
		},
	}
	if normalizedVMSourceType(req.SourceType) == vmSourceTypeISO {
		osConfig.BootDevices = []libvirtxml.DomainBootDevice{
			{Dev: "cdrom"},
			{Dev: "hd"},
		}
	} else {
		osConfig.BootDevices = []libvirtxml.DomainBootDevice{{Dev: "hd"}}
	}
	if firmware.UEFIAvailable {
		osConfig.Firmware = "efi"
		osConfig.FirmwareInfo = &libvirtxml.DomainOSFirmwareInfo{
			Features: []libvirtxml.DomainOSFirmwareFeature{
				{Enabled: "no", Name: "secure-boot"},
				{Enabled: "no", Name: "enrolled-keys"},
			},
		}
	}

	socketPath := vncSocketPath(req.Name)
	disks := []libvirtxml.DomainDisk{
		{
			Device: "disk",
			Driver: &libvirtxml.DomainDiskDriver{
				Name: "qemu",
				Type: "qcow2",
			},
			Source: &libvirtxml.DomainDiskSource{
				File: &libvirtxml.DomainDiskSourceFile{File: storage.Boot.Path},
			},
			Target: &libvirtxml.DomainDiskTarget{Dev: "vda", Bus: "virtio"},
		},
	}
	if normalizedVMSourceType(req.SourceType) == vmSourceTypeISO {
		disks = append(disks, libvirtxml.DomainDisk{
			Device: "cdrom",
			Driver: &libvirtxml.DomainDiskDriver{
				Name: "qemu",
				Type: "raw",
			},
			Source:   &libvirtxml.DomainDiskSource{File: &libvirtxml.DomainDiskSourceFile{File: req.ISOPath}},
			Target:   &libvirtxml.DomainDiskTarget{Dev: "sda", Bus: "sata"},
			ReadOnly: &libvirtxml.DomainDiskReadOnly{},
		})
	}
	if storage.Seed != nil {
		disks = append(disks, libvirtxml.DomainDisk{
			Device: "disk",
			Driver: &libvirtxml.DomainDiskDriver{
				Name: "qemu",
				Type: "raw",
			},
			Source:   &libvirtxml.DomainDiskSource{File: &libvirtxml.DomainDiskSourceFile{File: storage.Seed.Path}},
			Target:   &libvirtxml.DomainDiskTarget{Dev: "vdb", Bus: "virtio"},
			ReadOnly: &libvirtxml.DomainDiskReadOnly{},
		})
	}

	domain := libvirtxml.Domain{
		Type: "kvm",
		Name: req.Name,
		Metadata: &libvirtxml.DomainMetadata{
			XML: metaXML,
		},
		Memory:        &libvirtxml.DomainMemory{Value: uint(req.MemoryMB), Unit: "MiB"},
		CurrentMemory: &libvirtxml.DomainCurrentMemory{Value: uint(req.MemoryMB), Unit: "MiB"},
		VCPU:          &libvirtxml.DomainVCPU{Value: uint(req.VCPUs)},
		OS:            osConfig,
		Features: &libvirtxml.DomainFeatureList{
			ACPI: &libvirtxml.DomainFeature{},
			APIC: &libvirtxml.DomainFeatureAPIC{},
		},
		Devices: &libvirtxml.DomainDeviceList{
			Disks: disks,
			Interfaces: []libvirtxml.DomainInterface{
				{
					Source: &libvirtxml.DomainInterfaceSource{Network: &libvirtxml.DomainInterfaceSourceNetwork{Network: defaultNetworkName}},
					Model:  &libvirtxml.DomainInterfaceModel{Type: "virtio"},
				},
			},
			Graphics: []libvirtxml.DomainGraphic{
				{VNC: managedVNCGraphic(socketPath)},
			},
			Inputs: []libvirtxml.DomainInput{
				{Type: "tablet", Bus: "usb"},
			},
			Videos: []libvirtxml.DomainVideo{
				{Model: libvirtxml.DomainVideoModel{Type: "virtio"}},
			},
		},
		OnPoweroff: "destroy",
		OnReboot:   "restart",
		OnCrash:    "restart",
	}
	return domain, nil
}

func buildVolumeXML(name string, diskGB int) (string, error) {
	if diskGB <= 0 {
		return "", badRequestf("diskGB must be greater than 0")
	}
	vol := libvirtxml.StorageVolume{
		Type: "file",
		Name: name,
		Capacity: &libvirtxml.StorageVolumeSize{
			Value: uint64(diskGB),
			Unit:  "G",
		},
		Target: &libvirtxml.StorageVolumeTarget{
			Format: &libvirtxml.StorageVolumeTargetFormat{Type: "qcow2"},
		},
	}
	return vol.Marshal()
}

func validateCreateRequest(req apischema.VMCreateRequest) error {
	sourceType := normalizedVMSourceType(req.SourceType)
	if err := validateVMSourceType(req.SourceType); err != nil {
		return err
	}
	if err := validateVMName(req.Name); err != nil {
		return err
	}
	if req.VCPUs <= 0 {
		return badRequestf("vcpus must be greater than 0")
	}
	if req.MemoryMB < 256 {
		return badRequestf("memoryMB must be at least 256")
	}
	if req.DiskGB <= 0 {
		return badRequestf("diskGB must be greater than 0")
	}
	if sourceType == vmSourceTypeISO && req.ISOPath == "" {
		return badRequestf("isoPath is required")
	}
	if sourceType == vmSourceTypeImagePreset {
		preset, err := imagePreset(req.ImagePresetID)
		if err != nil {
			return err
		}
		if preset.MinDiskGB > 0 && req.DiskGB < preset.MinDiskGB {
			return badRequestf("%s requires diskGB to be at least %d", preset.Label, preset.MinDiskGB)
		}
		if preset.NeedsCloudInit {
			if err := validateCloudInitRequest(req, preset); err != nil {
				return err
			}
		}
	}
	if req.Network != "" && req.Network != defaultNetworkName {
		return badRequestf("v1 only supports the default libvirt network")
	}
	return nil
}

func parseVirtualMachine(dom libvirt.Domain, xmlDoc string, state string, autostart bool) (apischema.VirtualMachine, error) {
	var parsed libvirtxml.Domain
	if err := parsed.Unmarshal(xmlDoc); err != nil {
		return apischema.VirtualMachine{}, err
	}
	meta := parseLinuxIOMetadata(parsed.Metadata)
	ownedByVolume := make(map[string]linuxioMetadataDisk, len(meta.Disks))
	ownedByPath := make(map[string]linuxioMetadataDisk, len(meta.Disks))
	for _, disk := range meta.Disks {
		if disk.Volume != "" {
			ownedByVolume[disk.Volume] = disk
		}
		if disk.Path != "" {
			ownedByPath[disk.Path] = disk
		}
	}

	vm := apischema.VirtualMachine{
		Name:      parsed.Name,
		UUID:      parsed.UUID,
		State:     state,
		Autostart: autostart,
	}
	if vm.Name == "" {
		vm.Name = dom.Name
	}
	if vm.UUID == "" {
		vm.UUID = uuidString(dom.UUID)
	}
	if parsed.VCPU != nil {
		vm.VCPUs = int(parsed.VCPU.Value)
	}
	if parsed.Memory != nil {
		vm.MemoryMB = memoryToMB(parsed.Memory.Value, parsed.Memory.Unit)
	}
	if parsed.Devices == nil {
		return vm, nil
	}
	for _, disk := range parsed.Devices.Disks {
		apiDisk := mapDisk(disk, ownedByVolume, ownedByPath)
		if apiDisk.Owned {
			vm.OwnedDisks = append(vm.OwnedDisks, apiDisk.Path)
		}
		if disk.Device != "cdrom" {
			vm.DiskGB += apiDisk.SizeGB
		}
		if disk.Device == "cdrom" && !apiDisk.Owned {
			continue
		}
		vm.Disks = append(vm.Disks, apiDisk)
	}
	for _, nic := range parsed.Devices.Interfaces {
		vm.NICs = append(vm.NICs, mapNIC(nic))
	}
	vm.HasGraphics = len(parsed.Devices.Graphics) > 0
	return vm, nil
}

func mapDisk(disk libvirtxml.DomainDisk, ownedByVolume, ownedByPath map[string]linuxioMetadataDisk) apischema.VMDisk {
	out := apischema.VMDisk{
		Device: disk.Device,
	}
	if disk.Target != nil {
		out.Target = disk.Target.Dev
	}
	if disk.Source != nil {
		switch {
		case disk.Source.File != nil:
			out.Path = disk.Source.File.File
		case disk.Source.Block != nil:
			out.Path = disk.Source.Block.Dev
		case disk.Source.Volume != nil:
			out.VolumeName = disk.Source.Volume.Volume
			out.Path = disk.Source.Volume.Volume
		}
	}
	if out.VolumeName == "" && out.Path != "" {
		out.VolumeName = filepath.Base(out.Path)
	}
	if metaDisk, ok := ownedByVolume[out.VolumeName]; ok {
		out.Owned = true
		out.SizeGB = metaDisk.SizeGB
	}
	if metaDisk, ok := ownedByPath[out.Path]; ok {
		out.Owned = true
		if out.SizeGB == 0 {
			out.SizeGB = metaDisk.SizeGB
		}
	}
	return out
}

func mapNIC(nic libvirtxml.DomainInterface) apischema.VMNIC {
	out := apischema.VMNIC{Model: ""}
	if nic.MAC != nil {
		out.MAC = nic.MAC.Address
	}
	if nic.Model != nil {
		out.Model = nic.Model.Type
	}
	if nic.Source != nil && nic.Source.Network != nil {
		out.Network = nic.Source.Network.Network
	}
	return out
}

func (m linuxioMetadata) marshal() (string, error) {
	data, err := xml.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func parseLinuxIOMetadata(metadata *libvirtxml.DomainMetadata) linuxioMetadata {
	if metadata == nil || metadata.XML == "" {
		return linuxioMetadata{}
	}
	var parsed linuxioMetadata
	if err := xml.Unmarshal([]byte(metadata.XML), &parsed); err != nil {
		return linuxioMetadata{}
	}
	return parsed
}

func normalizeLinuxIOVNCGraphicsXML(xmlDoc string) (string, bool, error) {
	var domain libvirtxml.Domain
	if err := domain.Unmarshal(xmlDoc); err != nil {
		return "", false, err
	}
	hasLinuxIOMetadata := len(parseLinuxIOMetadata(domain.Metadata).Disks) > 0
	hasLinuxIOStorage := hasManagedDomainDisk(domain)
	if !hasLinuxIOMetadata && !hasLinuxIOStorage && !hasManagedVNCGraphics(domain) {
		return "", false, nil
	}
	if !normalizeVNCGraphics(&domain, hasLinuxIOMetadata || hasLinuxIOStorage) {
		return "", false, nil
	}
	normalized, err := domain.Marshal()
	if err != nil {
		return "", false, err
	}
	return normalized, true, nil
}

func hasManagedVNCGraphics(domain libvirtxml.Domain) bool {
	if domain.Devices == nil {
		return false
	}
	for _, graphic := range domain.Devices.Graphics {
		if graphic.VNC != nil && isManagedVNCSocketPath(vncGraphicSocket(graphic.VNC)) {
			return true
		}
	}
	return false
}

func hasManagedDomainDisk(domain libvirtxml.Domain) bool {
	if domain.Devices == nil {
		return false
	}
	for _, disk := range domain.Devices.Disks {
		if disk.Source == nil {
			continue
		}
		switch {
		case disk.Source.File != nil && isManagedDiskName(filepath.Base(disk.Source.File.File)):
			return true
		case disk.Source.Volume != nil && isManagedDiskName(disk.Source.Volume.Volume):
			return true
		}
	}
	return false
}

func normalizeVNCGraphics(domain *libvirtxml.Domain, isLinuxIOManaged bool) bool {
	if domain.Devices == nil {
		return addManagedVNCGraphics(domain, isLinuxIOManaged)
	}
	changed := false
	hasVNCGraphics := false
	for idx := range domain.Devices.Graphics {
		vnc := domain.Devices.Graphics[idx].VNC
		if vnc == nil {
			continue
		}
		hasVNCGraphics = true
		changed = normalizeVNCGraphic(vnc, domain.Name, isLinuxIOManaged) || changed
	}
	if !hasVNCGraphics {
		changed = addManagedVNCGraphics(domain, isLinuxIOManaged) || changed
	}
	return changed
}

func normalizeVNCGraphic(vnc *libvirtxml.DomainGraphicVNC, domainName string, isLinuxIOManaged bool) bool {
	changed := false
	if socket := vncGraphicSocket(vnc); vnc.Socket == "" && isManagedVNCSocketPath(socket) {
		vnc.Socket = socket
		changed = true
	}
	if vnc.Socket == "" && isLinuxIOManaged && domainName != "" {
		vnc.Socket = vncSocketPath(domainName)
		changed = true
	}
	if vnc.Socket == "" {
		return changed
	}
	if !isLinuxIOManaged && !isManagedVNCSocketPath(vnc.Socket) {
		return changed
	}
	if isNormalizedVNCSocketGraphic(vnc) {
		return changed
	}
	applyManagedVNCSocket(vnc, vnc.Socket)
	return true
}

func addManagedVNCGraphics(domain *libvirtxml.Domain, isLinuxIOManaged bool) bool {
	if !isLinuxIOManaged || domain.Name == "" {
		return false
	}
	if domain.Devices == nil {
		domain.Devices = &libvirtxml.DomainDeviceList{}
	}
	domain.Devices.Graphics = append(domain.Devices.Graphics, libvirtxml.DomainGraphic{
		VNC: managedVNCGraphic(vncSocketPath(domain.Name)),
	})
	return true
}

// managedVNCGraphic builds the LinuxIO VNC graphics device backed by a unix
// socket. The explicit <listen type='socket'> child is required: with only the
// socket= attribute, libvirt applies the host's default vnc_listen address and
// starts QEMU with a TCP VNC (-vnc <addr>:0) instead, leaving no unix socket for
// the console to connect to.
func managedVNCGraphic(socketPath string) *libvirtxml.DomainGraphicVNC {
	vnc := &libvirtxml.DomainGraphicVNC{}
	applyManagedVNCSocket(vnc, socketPath)
	return vnc
}

func applyManagedVNCSocket(vnc *libvirtxml.DomainGraphicVNC, socketPath string) {
	vnc.Socket = socketPath
	vnc.Port = 0
	vnc.AutoPort = ""
	vnc.WebSocket = 0
	vnc.Listen = ""
	vnc.Listeners = []libvirtxml.DomainGraphicListener{
		{Socket: &libvirtxml.DomainGraphicListenerSocket{Socket: socketPath}},
	}
}

func isNormalizedVNCSocketGraphic(vnc *libvirtxml.DomainGraphicVNC) bool {
	return vnc.Socket != "" &&
		vnc.Port == 0 &&
		vnc.AutoPort == "" &&
		vnc.WebSocket == 0 &&
		vnc.Listen == "" &&
		hasOnlyManagedSocketListener(vnc)
}

func hasOnlyManagedSocketListener(vnc *libvirtxml.DomainGraphicVNC) bool {
	if len(vnc.Listeners) != 1 {
		return false
	}
	listener := vnc.Listeners[0]
	return listener.Address == nil &&
		listener.Network == nil &&
		listener.Socket != nil &&
		listener.Socket.Socket == vnc.Socket
}

func isManagedVNCSocketPath(path string) bool {
	if !isSafeVNCSocket(path) {
		return false
	}
	name := filepath.Base(path)
	return strings.HasPrefix(name, managedDiskPrefix) && strings.HasSuffix(name, ".vnc")
}

func isManagedDiskName(name string) bool {
	return strings.HasPrefix(name, managedDiskPrefix) && strings.HasSuffix(name, managedDiskSuffix)
}

func vncSocketPath(name string) string {
	return filepath.Join(vncSocketDir, managedDiskPrefix+name+".vnc")
}

func uuidString(uuid libvirt.UUID) string {
	raw := uuid[:]
	if len(raw) != 16 {
		return ""
	}
	encoded := hex.EncodeToString(raw)
	if encoded == "00000000000000000000000000000000" {
		return ""
	}
	return fmt.Sprintf("%s-%s-%s-%s-%s", encoded[0:8], encoded[8:12], encoded[12:16], encoded[16:20], encoded[20:32])
}

func memoryToMB(value uint, unit string) int {
	switch unit {
	case "KiB", "K", "k":
		return int(value / 1024)
	case "GiB", "G", "g":
		return int(value * 1024)
	case "MiB", "M", "m", "":
		return int(value)
	default:
		return int(value)
	}
}
