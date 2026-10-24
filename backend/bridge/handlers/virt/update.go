package virt

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	libvirt "github.com/digitalocean/go-libvirt"
	"libvirt.org/go/libvirtxml"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

var validVMOS = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

const (
	libvirtNVRAMDir = "/var/lib/libvirt/qemu/nvram/"
	// linuxioMetadataNS matches linuxioMetadata's XMLName tag.
	linuxioMetadataNS  = "https://linuxio.local/libvirt/v1"
	libvirtNVRAMSuffix = "_VARS.fd"
)

// UpdateVM redefines a shut-off VM with new settings.
func UpdateVM(ctx context.Context, req apischema.VMUpdateRequest) (apischema.VirtualMachine, error) {
	req.NewName = strings.TrimSpace(req.NewName)
	if req.NewName == "" {
		req.NewName = req.Name
	}
	if err := validateUpdateRequest(req); err != nil {
		return apischema.VirtualMachine{}, err
	}
	var vm apischema.VirtualMachine
	// The name locks keep unused-disk cleanup away from disks being renamed.
	err := withVMNameLocks(ctx, req.Name, req.NewName, func() error {
		return withLibvirtConn(ctx, func(conn libvirtConn) error {
			var updateErr error
			vm, updateErr = updateVMWithConn(ctx, conn, req)
			return updateErr
		})
	})
	return vm, err
}

func validateUpdateRequest(req apischema.VMUpdateRequest) error {
	if err := validateVMName(req.Name); err != nil {
		return err
	}
	if err := validateVMName(req.NewName); err != nil {
		return err
	}
	if err := validateVMResources(req.VCPUs, req.MemoryMB); err != nil {
		return err
	}
	if req.DiskGB < 0 {
		return badRequestf("diskGB must not be negative")
	}
	if err := validateVMNetworkName(req.Network); err != nil {
		return err
	}
	if req.OS != "" && !validVMOS.MatchString(req.OS) {
		return badRequestf("os must be a lowercase distro id such as \"ubuntu\"")
	}
	return validateVMFirmware(req.Firmware, true)
}

// withVMNameLocks takes both name locks in a fixed order, so two opposite
// renames cannot deadlock.
func withVMNameLocks(ctx context.Context, a, b string, fn func() error) error {
	if a == b {
		return withVMNameLock(ctx, a, fn)
	}
	if b < a {
		a, b = b, a
	}
	return withVMNameLock(ctx, a, func() error {
		return withVMNameLock(ctx, b, fn)
	})
}

func updateVMWithConn(ctx context.Context, conn libvirtConn, req apischema.VMUpdateRequest) (apischema.VirtualMachine, error) {
	domain, err := lookupEditableDomain(conn, req)
	if err != nil {
		return apischema.VirtualMachine{}, err
	}
	parsed, err := readInactiveDomain(conn, domain)
	if err != nil {
		return apischema.VirtualMachine{}, err
	}
	setDomainResources(&parsed, req.VCPUs, req.MemoryMB)
	staleNVRAM, err := updateDomainFirmware(parsed.OS, req.Firmware)
	if err != nil {
		return apischema.VirtualMachine{}, err
	}
	if err = updateDomainNetwork(ctx, conn, &parsed, req.Network); err != nil {
		return apischema.VirtualMachine{}, err
	}
	meta := parseLinuxIOMetadata(parsed.Metadata)
	if req.OS != "" {
		meta.OS = req.OS
	}
	grown, err := growManagedBootDisk(ctx, &meta, req.Name, req.DiskGB)
	if err != nil {
		return apischema.VirtualMachine{}, err
	}
	renaming := req.NewName != req.Name
	undo := func() error { return nil }
	if renaming {
		if undo, err = renameDomain(conn, domain, &parsed, &meta, req.Name, req.NewName); err != nil {
			return apischema.VirtualMachine{}, err
		}
	}
	redefined, err := defineEditedDomain(conn, &parsed, meta)
	if err != nil {
		return apischema.VirtualMachine{}, errors.Join(err, undo())
	}
	// libvirt only removes NVRAM on undefine; drop the stale vars file so a
	// later switch back to UEFI starts clean.
	if staleNVRAM != "" {
		if err = removeFile(staleNVRAM); err != nil && !errors.Is(err, os.ErrNotExist) {
			return apischema.VirtualMachine{}, fmt.Errorf("remove stale NVRAM %s: %w", staleNVRAM, err)
		}
	}
	if renaming || grown {
		refreshDefaultPool(conn)
	}
	return virtualMachineFromDomain(conn, redefined)
}

// lookupEditableDomain returns the VM when it is shut off and its new name,
// if any, is free.
func lookupEditableDomain(conn libvirtConn, req apischema.VMUpdateRequest) (libvirt.Domain, error) {
	domain, err := lookupDomain(conn, req.Name)
	if err != nil {
		return domain, err
	}
	active, err := conn.DomainIsActive(domain)
	if err != nil {
		return domain, fmt.Errorf("check VM active state: %w", err)
	}
	if active != 0 {
		return domain, conflictf("VM %q must be shut off to edit it", req.Name)
	}
	if req.NewName == req.Name {
		return domain, nil
	}
	if _, err = conn.DomainLookupByName(req.NewName); err == nil {
		return domain, conflictf("VM %q already exists", req.NewName)
	} else if !isDomainMissing(err) {
		return domain, fmt.Errorf("check existing VM: %w", err)
	}
	return domain, nil
}

func readInactiveDomain(conn libvirtConn, domain libvirt.Domain) (libvirtxml.Domain, error) {
	var parsed libvirtxml.Domain
	xmlDoc, err := conn.DomainGetXMLDesc(domain, libvirt.DomainXMLInactive)
	if err != nil {
		return parsed, fmt.Errorf("read VM XML: %w", err)
	}
	if err = parsed.Unmarshal(xmlDoc); err != nil {
		return parsed, fmt.Errorf("parse VM XML: %w", err)
	}
	if parsed.OS == nil {
		parsed.OS = &libvirtxml.DomainOS{}
	}
	return parsed, nil
}

// renameDomain renames the VM in libvirt and its LinuxIO files on disk. The
// returned undo restores both.
func renameDomain(conn libvirtConn, domain libvirt.Domain, parsed *libvirtxml.Domain, meta *linuxioMetadata, oldName, newName string) (func() error, error) {
	undoStorage, err := renameDomainStorage(parsed, meta, oldName, newName)
	if err != nil {
		return nil, err
	}
	if _, err = conn.DomainRename(domain, libvirt.OptString{newName}, 0); err != nil {
		return nil, errors.Join(fmt.Errorf("rename VM: %w", err), undoStorage())
	}
	parsed.Name = newName
	return func() error {
		renamed, lookupErr := conn.DomainLookupByName(newName)
		if lookupErr != nil {
			return errors.Join(fmt.Errorf("look up renamed VM: %w", lookupErr), undoStorage())
		}
		if _, renameErr := conn.DomainRename(renamed, libvirt.OptString{oldName}, 0); renameErr != nil {
			return errors.Join(fmt.Errorf("restore VM name: %w", renameErr), undoStorage())
		}
		return undoStorage()
	}, nil
}

func defineEditedDomain(conn libvirtConn, parsed *libvirtxml.Domain, meta linuxioMetadata) (libvirt.Domain, error) {
	if len(meta.Disks) > 0 || meta.OS != "" {
		if err := setLinuxIOMetadata(parsed, meta); err != nil {
			return libvirt.Domain{}, err
		}
	}
	updated, err := parsed.Marshal()
	if err != nil {
		return libvirt.Domain{}, err
	}
	redefined, err := conn.DomainDefineXML(updated)
	if err != nil {
		return libvirt.Domain{}, fmt.Errorf("redefine VM: %w", err)
	}
	return redefined, nil
}

func setDomainResources(domain *libvirtxml.Domain, vcpus, memoryMB int) {
	if domain.VCPU == nil {
		domain.VCPU = &libvirtxml.DomainVCPU{}
	}
	domain.VCPU.Value = uint(vcpus)
	// A lower current count would keep the added vCPUs offline.
	domain.VCPU.Current = 0
	if domain.Memory == nil {
		domain.Memory = &libvirtxml.DomainMemory{}
	}
	domain.Memory.Value, domain.Memory.Unit = uint(memoryMB), "MiB"
	domain.CurrentMemory = &libvirtxml.DomainCurrentMemory{Value: uint(memoryMB), Unit: "MiB"}
}

// updateDomainFirmware switches osConfig to firmware and returns the NVRAM
// file a dropped UEFI mode leaves behind.
func updateDomainFirmware(osConfig *libvirtxml.DomainOS, firmware apischema.VMFirmware) (string, error) {
	if domainFirmware(osConfig) == firmware {
		return "", nil
	}
	if firmware == vmFirmwareUEFI && !ovmfAvailable() {
		return "", conflictf("OVMF firmware is not installed on this host")
	}
	staleNVRAM := ""
	if nvram := osConfig.NVRam; nvram != nil && filepath.Clean(nvram.NVRam) == nvram.NVRam && strings.HasPrefix(nvram.NVRam, libvirtNVRAMDir) {
		staleNVRAM = nvram.NVRam
	}
	setDomainFirmware(osConfig, firmware == vmFirmwareUEFI)
	return staleNVRAM, nil
}

// updateDomainNetwork moves the first NIC to network, keeping its MAC.
func updateDomainNetwork(ctx context.Context, conn libvirtConn, domain *libvirtxml.Domain, network string) error {
	if network == "" {
		return nil
	}
	if domain.Devices == nil || len(domain.Devices.Interfaces) == 0 {
		return badRequestf("VM has no network interface to change")
	}
	nic := &domain.Devices.Interfaces[0]
	networkName := normalizeVMNetwork(network)
	if mapNIC(*nic).Network == networkName {
		return nil
	}
	if err := validateNetworkSelection(ctx, conn, networkName); err != nil {
		return err
	}
	if networkName == defaultNetworkName {
		if err := ensureDefaultNetworkActive(conn); err != nil {
			return err
		}
	}
	nic.Source = vmInterfaceSource(networkName)
	return nil
}

// growManagedBootDisk grows the LinuxIO boot disk to diskGB. Disks never
// shrink, since that would cut off guest data.
func growManagedBootDisk(ctx context.Context, meta *linuxioMetadata, vmName string, diskGB int) (bool, error) {
	if diskGB == 0 {
		return false, nil
	}
	for i := range meta.Disks {
		disk := &meta.Disks[i]
		if disk.Volume != managedVolumeName(vmName) {
			continue
		}
		switch {
		case diskGB == disk.SizeGB:
			return false, nil
		case diskGB < disk.SizeGB:
			return false, badRequestf("disk can only grow; it is already %d GB", disk.SizeGB)
		case !slices.Contains(managedDiskDirs, filepath.Dir(disk.Path)):
			return false, badRequestf("%q is not a LinuxIO VM disk", disk.Path)
		}
		if err := resizeQCOW2Image(ctx, disk.Path, diskGB); err != nil {
			return false, fmt.Errorf("grow VM disk: %w", err)
		}
		disk.SizeGB = diskGB
		return true, nil
	}
	return false, badRequestf("VM %q has no LinuxIO-managed disk to resize", vmName)
}

// fileMoves renames files and remembers them so undo can move them back.
type fileMoves [][2]string

func (m *fileMoves) move(from, to string) error {
	if _, err := os.Lstat(to); err == nil {
		return conflictf("%s already exists", to)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("check %s: %w", to, err)
	}
	if err := renameFile(from, to); err != nil {
		return fmt.Errorf("rename %s: %w", from, err)
	}
	*m = append(*m, [2]string{from, to})
	return nil
}

func (m *fileMoves) undo() error {
	var errs []error
	for i := range slices.Backward(*m) {
		if err := renameFile((*m)[i][1], (*m)[i][0]); err != nil {
			errs = append(errs, fmt.Errorf("restore %s: %w", (*m)[i][0], err))
		}
	}
	return errors.Join(errs...)
}

// renameDomainStorage moves the VM's LinuxIO disk, seed, NVRAM and console
// socket names to newName, since ownership and cleanup match them by VM name.
// The returned undo moves the files back.
func renameDomainStorage(domain *libvirtxml.Domain, meta *linuxioMetadata, oldName, newName string) (func() error, error) {
	moves := &fileMoves{}
	paths, err := renameManagedDisks(moves, meta, oldName, newName)
	if err != nil {
		return nil, errors.Join(err, moves.undo())
	}
	if nvram := domain.OS.NVRam; nvram != nil && nvram.NVRam == libvirtNVRAMDir+oldName+libvirtNVRAMSuffix {
		path := libvirtNVRAMDir + newName + libvirtNVRAMSuffix
		// A UEFI VM that never booted has no NVRAM file yet.
		if err = moves.move(nvram.NVRam, path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, errors.Join(err, moves.undo())
		}
		nvram.NVRam = path
	}
	retargetDomainDevices(domain, paths, oldName, newName)
	return moves.undo, nil
}

// renameManagedDisks renames the files of the VM's LinuxIO disk and seed and
// returns their old-to-new paths.
func renameManagedDisks(moves *fileMoves, meta *linuxioMetadata, oldName, newName string) (map[string]string, error) {
	paths := map[string]string{}
	for i := range meta.Disks {
		disk := &meta.Disks[i]
		var volume string
		switch disk.Volume {
		case managedVolumeName(oldName):
			volume = managedVolumeName(newName)
		case managedSeedVolumeName(oldName):
			volume = managedSeedVolumeName(newName)
		default:
			continue
		}
		dir := filepath.Dir(disk.Path)
		if !slices.Contains(managedDiskDirs, dir) {
			return nil, badRequestf("%q is not a LinuxIO VM disk", disk.Path)
		}
		path := filepath.Join(dir, volume)
		if err := moves.move(disk.Path, path); err != nil {
			return nil, err
		}
		paths[disk.Path] = path
		disk.Volume, disk.Path = volume, path
	}
	return paths, nil
}

// retargetDomainDevices points disk sources and the LinuxIO console socket at
// their renamed paths.
func retargetDomainDevices(domain *libvirtxml.Domain, paths map[string]string, oldName, newName string) {
	if domain.Devices == nil {
		return
	}
	for _, disk := range domain.Devices.Disks {
		if disk.Source == nil || disk.Source.File == nil {
			continue
		}
		if path, ok := paths[disk.Source.File.File]; ok {
			disk.Source.File.File = path
		}
	}
	for _, graphic := range domain.Devices.Graphics {
		if vnc := graphic.VNC; vnc != nil && vnc.Socket == vncSocketPath(oldName) {
			applyManagedVNCSocket(vnc, vncSocketPath(newName))
		}
	}
}

// setLinuxIOMetadata replaces LinuxIO's element in <metadata>, keeping other
// applications' elements such as libosinfo's.
func setLinuxIOMetadata(domain *libvirtxml.Domain, meta linuxioMetadata) error {
	element, err := meta.marshal()
	if err != nil {
		return err
	}
	if domain.Metadata == nil {
		domain.Metadata = &libvirtxml.DomainMetadata{}
	}
	raw := domain.Metadata.XML
	start, end, err := linuxioMetadataSpan(raw)
	if err != nil {
		return err
	}
	if start < 0 {
		domain.Metadata.XML = raw + element
		return nil
	}
	domain.Metadata.XML = raw[:start] + element + raw[end:]
	return nil
}

// linuxioMetadataSpan finds LinuxIO's element among <metadata>'s children;
// start is -1 when it is absent.
func linuxioMetadataSpan(raw string) (int, int, error) {
	decoder := xml.NewDecoder(strings.NewReader(raw))
	for {
		start := decoder.InputOffset()
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return -1, -1, nil
		}
		if err != nil {
			return -1, -1, fmt.Errorf("parse VM metadata: %w", err)
		}
		el, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		if err = decoder.Skip(); err != nil {
			return -1, -1, fmt.Errorf("parse VM metadata: %w", err)
		}
		if el.Name.Space == linuxioMetadataNS && el.Name.Local == "linuxio" {
			return int(start), int(decoder.InputOffset()), nil
		}
	}
}

// refreshDefaultPool updates libvirt's cached volume list after a disk file
// was renamed or grown outside the storage API.
func refreshDefaultPool(conn libvirtConn) {
	pool, err := conn.StoragePoolLookupByName(defaultPoolName)
	if err == nil {
		err = conn.StoragePoolRefresh(pool, 0)
	}
	if err != nil && !isStoragePoolMissing(err) {
		slog.Warn("refresh default storage pool after VM edit", "error", err)
	}
}
