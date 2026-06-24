package virt

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	libvirt "github.com/digitalocean/go-libvirt"
	"libvirt.org/go/libvirtxml"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

type createdVMVolume struct {
	Volume libvirt.StorageVol
	Name   string
	Path   string
	SizeGB int
}

type createdVMStorage struct {
	Boot createdVMVolume
	Seed *createdVMVolume
}

type vmCreateReporter func(apischema.VMCreateProgress)

func CreateVM(ctx context.Context, req apischema.VMCreateRequest) (apischema.VirtualMachine, error) {
	return CreateVMWithProgress(ctx, req, nil)
}

func CreateVMWithProgress(ctx context.Context, req apischema.VMCreateRequest, report vmCreateReporter) (apischema.VirtualMachine, error) {
	reportVMCreateProgress(report, "validating", "Validating VM request", "", nil)
	if validateErr := validateCreateRequest(req); validateErr != nil {
		return apischema.VirtualMachine{}, validateErr
	}
	sourceType := normalizedVMSourceType(req.SourceType)
	req.SourceType = sourceType
	preflightReq := apischema.VMPreflightRequest{
		ImagePresetID: req.ImagePresetID,
		SourceType:    sourceType,
	}
	if sourceType == vmSourceTypeISO {
		isoPath, mediaErr := validateInstallMedia(req.ISOPath)
		if mediaErr != nil {
			return apischema.VirtualMachine{}, mediaErr
		}
		req.ISOPath = isoPath
		preflightReq.ISOPath = &isoPath
	}
	reportVMCreateProgress(report, "preflight", "Checking host virtualization prerequisites", "", nil)
	preflight, preflightErr := runPreflight(ctx, preflightReq)
	if preflightErr != nil {
		return apischema.VirtualMachine{}, preflightErr
	}
	if readyErr := preflightReadyForCreate(preflight, sourceType); readyErr != nil {
		return apischema.VirtualMachine{}, readyErr
	}

	var created apischema.VirtualMachine
	reportVMCreateProgress(report, "connecting", "Connecting to libvirt", "", nil)
	connErr := withLibvirtConn(ctx, func(conn libvirtConn) error {
		var createErr error
		created, createErr = createVMWithConn(ctx, conn, req, preflight.Firmware, report)
		return createErr
	})
	return created, connErr
}

func reportVMCreateProgress(report vmCreateReporter, phase, message, path string, percent *int) {
	if report == nil {
		return
	}
	report(apischema.VMCreateProgress{
		Phase:   phase,
		Message: message,
		Path:    path,
		Percent: percent,
	})
}

func progressPercent(value int) *int {
	if value < 0 {
		value = 0
	}
	if value > 100 {
		value = 100
	}
	return &value
}

func createVMWithConn(ctx context.Context, conn libvirtConn, req apischema.VMCreateRequest, firmware apischema.VMPreflightFirmware, report vmCreateReporter) (apischema.VirtualMachine, error) {
	reportVMCreateProgress(report, "checking", "Checking for existing VM", "", nil)
	if _, lookupErr := conn.DomainLookupByName(req.Name); lookupErr == nil {
		return apischema.VirtualMachine{}, conflictf("VM %q already exists", req.Name)
	} else if !isDomainMissing(lookupErr) {
		return apischema.VirtualMachine{}, fmt.Errorf("check existing VM: %w", lookupErr)
	}

	reportVMCreateProgress(report, "storage", "Preparing default storage pool", defaultPoolPath, nil)
	pool, poolErr := ensureDefaultPoolActive(conn)
	if poolErr != nil {
		return apischema.VirtualMachine{}, poolErr
	}
	reportVMCreateProgress(report, "storage", "Creating managed VM folders", managedRootPath, nil)
	if mkdirErr := ensureManagedStorageDirectories(); mkdirErr != nil {
		return apischema.VirtualMachine{}, mkdirErr
	}
	reportVMCreateProgress(report, "network", "Preparing default NAT network", defaultNetworkName, nil)
	if networkErr := ensureDefaultNetworkActive(conn); networkErr != nil {
		return apischema.VirtualMachine{}, networkErr
	}

	volumeName := managedVolumeName(req.Name)
	reportVMCreateProgress(report, "storage", "Preparing VM disk", volumeName, nil)
	storage, storageErr := createManagedStorage(ctx, conn, pool, volumeName, req, report)
	if storageErr != nil {
		return apischema.VirtualMachine{}, storageErr
	}

	reportVMCreateProgress(report, "define", "Defining libvirt domain", req.Name, nil)
	domain, domainErr := defineDomainWithRollback(conn, req, firmware, storage)
	if domainErr != nil {
		return apischema.VirtualMachine{}, domainErr
	}
	if req.Start {
		reportVMCreateProgress(report, "start", "Starting VM", req.Name, nil)
		if startErr := conn.DomainCreate(domain); startErr != nil {
			rollbackFailedStartedCreate(conn, domain, storage)
			return apischema.VirtualMachine{}, fmt.Errorf("start VM: %w", startErr)
		}
	}
	reportVMCreateProgress(report, "inspect", "Reading created VM state", req.Name, nil)
	vm, vmErr := virtualMachineFromDomain(conn, domain)
	if vmErr != nil {
		return apischema.VirtualMachine{}, vmErr
	}
	reportVMCreateProgress(report, "complete", "VM created", req.Name, progressPercent(100))
	return vm, nil
}

func createManagedStorage(ctx context.Context, conn libvirtConn, pool libvirt.StoragePool, volumeName string, req apischema.VMCreateRequest, report vmCreateReporter) (createdVMStorage, error) {
	if normalizedVMSourceType(req.SourceType) == vmSourceTypeImagePreset {
		return createManagedImageStorage(ctx, conn, pool, volumeName, req, report)
	}
	volume, path, volumeErr := createManagedVolume(conn, pool, volumeName, req.DiskGB)
	if volumeErr != nil {
		return createdVMStorage{}, volumeErr
	}
	return createdVMStorage{
		Boot: createdVMVolume{
			Volume: volume,
			Name:   volumeName,
			Path:   path,
			SizeGB: req.DiskGB,
		},
	}, nil
}

func ensureManagedVolumeAbsent(conn libvirtConn, pool libvirt.StoragePool, name string) error {
	if _, lookupErr := conn.StorageVolLookupByName(pool, name); lookupErr == nil {
		return conflictf("managed volume %q already exists", name)
	} else if !isStorageVolMissing(lookupErr) {
		return fmt.Errorf("check managed volume %q: %w", name, lookupErr)
	}
	return nil
}

func rollbackFailedStartedCreate(conn libvirtConn, domain libvirt.Domain, storage createdVMStorage) {
	_ = forceOffActiveDomain(conn, domain)
	_ = undefineDomain(conn, domain)
	deleteCreatedStorage(conn, storage)
}

func createManagedVolume(conn libvirtConn, pool libvirt.StoragePool, volumeName string, diskGB int) (libvirt.StorageVol, string, error) {
	if err := ensureManagedVolumeAbsent(conn, pool, volumeName); err != nil {
		return libvirt.StorageVol{}, "", err
	}
	volumeXML, xmlErr := buildVolumeXML(volumeName, diskGB)
	if xmlErr != nil {
		return libvirt.StorageVol{}, "", xmlErr
	}
	volume, createErr := conn.StorageVolCreateXML(pool, volumeXML, 0)
	if createErr != nil {
		return libvirt.StorageVol{}, "", fmt.Errorf("create volume: %w", createErr)
	}
	return volume, resolvedVolumePath(conn, volume, volumeName), nil
}

func ensureManagedStorageDirectories() error {
	for _, path := range []string{managedRootPath, managedISOPath, managedCloudPath} {
		if mkdirErr := mkdirAll(path, 0o755); mkdirErr != nil {
			return fmt.Errorf("create managed VM storage directory %s: %w", path, mkdirErr)
		}
		if chmodErr := chmodFile(path, 0o755); chmodErr != nil {
			return fmt.Errorf("set managed VM storage directory permissions %s: %w", path, chmodErr)
		}
	}
	return nil
}

func resolvedVolumePath(conn libvirtConn, volume libvirt.StorageVol, volumeName string) string {
	volumePath := volume.Key
	if volumePath == "" {
		volumePath = defaultPoolPath + "/" + volumeName
	}
	desc, descErr := conn.StorageVolGetXMLDesc(volume, 0)
	if descErr != nil {
		return volumePath
	}
	if path := storageVolumePath(desc); path != "" {
		return path
	}
	return volumePath
}

func defineDomainWithRollback(conn libvirtConn, req apischema.VMCreateRequest, firmware apischema.VMPreflightFirmware, storage createdVMStorage) (libvirt.Domain, error) {
	domainXML, buildErr := buildDomain(req, storage, firmware)
	if buildErr != nil {
		deleteCreatedStorage(conn, storage)
		return libvirt.Domain{}, buildErr
	}
	xmlDoc, marshalErr := domainXML.Marshal()
	if marshalErr != nil {
		deleteCreatedStorage(conn, storage)
		return libvirt.Domain{}, marshalErr
	}
	domain, defineErr := conn.DomainDefineXML(xmlDoc)
	if defineErr != nil {
		deleteCreatedStorage(conn, storage)
		return libvirt.Domain{}, fmt.Errorf("define VM: %w", defineErr)
	}
	return domain, nil
}

func deleteCreatedStorage(conn libvirtConn, storage createdVMStorage) {
	if storage.Seed != nil {
		deleteCreatedVolume(conn, storage.Seed.Volume)
	}
	deleteCreatedVolume(conn, storage.Boot.Volume)
}

func deleteCreatedVolume(conn libvirtConn, volume libvirt.StorageVol) {
	if err := conn.StorageVolDelete(volume, libvirt.StorageVolDeleteNormal); err == nil {
		return
	}
	if volume.Key != "" && strings.HasPrefix(volume.Key, defaultPoolPath+string(filepath.Separator)) && strings.HasPrefix(volume.Name, managedDiskPrefix) {
		_ = removeFile(volume.Key)
	}
}

func ensureDefaultPoolActive(conn libvirtConn) (libvirt.StoragePool, error) {
	pool, lookupErr := conn.StoragePoolLookupByName(defaultPoolName)
	if lookupErr != nil {
		if !isStoragePoolMissing(lookupErr) {
			return libvirt.StoragePool{}, fmt.Errorf("look up default storage pool: %w", lookupErr)
		}
		var defineErr error
		pool, defineErr = defineDefaultStoragePool(conn)
		if defineErr != nil {
			return libvirt.StoragePool{}, defineErr
		}
	}
	active, activeErr := conn.StoragePoolIsActive(pool)
	if activeErr == nil && active != 0 {
		return pool, nil
	}
	if createErr := conn.StoragePoolCreate(pool, libvirt.StoragePoolCreateNormal); createErr != nil {
		return libvirt.StoragePool{}, fmt.Errorf("start default storage pool: %w", createErr)
	}
	return pool, nil
}

func defineDefaultStoragePool(conn libvirtConn) (libvirt.StoragePool, error) {
	if mkdirErr := mkdirAll(defaultPoolPath, 0o755); mkdirErr != nil {
		return libvirt.StoragePool{}, fmt.Errorf("create default storage pool directory: %w", mkdirErr)
	}
	xmlDoc, xmlErr := buildDefaultPoolXML()
	if xmlErr != nil {
		return libvirt.StoragePool{}, xmlErr
	}
	pool, defineErr := conn.StoragePoolDefineXML(xmlDoc, 0)
	if defineErr != nil {
		return libvirt.StoragePool{}, fmt.Errorf("define default storage pool: %w", defineErr)
	}
	if autostartErr := conn.StoragePoolSetAutostart(pool, 1); autostartErr != nil {
		return libvirt.StoragePool{}, fmt.Errorf("enable default storage pool autostart: %w", autostartErr)
	}
	return pool, nil
}

func buildDefaultPoolXML() (string, error) {
	pool := libvirtxml.StoragePool{
		Type: "dir",
		Name: defaultPoolName,
		Target: &libvirtxml.StoragePoolTarget{
			Path: defaultPoolPath,
		},
	}
	return pool.Marshal()
}

func ensureDefaultNetworkActive(conn libvirtConn) error {
	network, err := conn.NetworkLookupByName(defaultNetworkName)
	if err != nil {
		if isNetworkMissing(err) {
			return fmt.Errorf("default NAT network is missing: %w", err)
		}
		return fmt.Errorf("look up default NAT network: %w", err)
	}
	active, err := conn.NetworkIsActive(network)
	if err == nil && active != 0 {
		return nil
	}
	_ = conn.NetworkSetAutostart(network, 1)
	if err := conn.NetworkCreate(network); err != nil {
		active, activeErr := conn.NetworkIsActive(network)
		if activeErr == nil && active != 0 {
			return nil
		}
		return defaultNetworkStartError(err)
	}
	return nil
}

func defaultNetworkStartError(err error) error {
	if defaultNetworkAddressInUse(err) {
		return fmt.Errorf("default NAT network cannot start because 192.168.122.1 is already in use; stop the conflicting dnsmasq/libvirt process or reconfigure the libvirt default network address, then retry: %w", err)
	}
	return fmt.Errorf("start default NAT network: %w", err)
}

func defaultNetworkAddressInUse(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "192.168.122.1") && strings.Contains(msg, "address already in use")
}

func storageVolumePath(xmlDoc string) string {
	var volume libvirtxml.StorageVolume
	if err := volume.Unmarshal(xmlDoc); err != nil {
		return ""
	}
	if volume.Target != nil && volume.Target.Path != "" {
		return volume.Target.Path
	}
	return volume.Key
}
