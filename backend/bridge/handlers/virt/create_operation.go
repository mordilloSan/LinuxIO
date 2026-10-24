package virt

import (
	"context"
	"fmt"

	libvirt "github.com/digitalocean/go-libvirt"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

type vmCreateReporter func(apischema.VMCreateProgress)

func CreateVMWithProgress(ctx context.Context, req apischema.VMCreateRequest, report vmCreateReporter) (apischema.VirtualMachine, error) {
	reportVMCreateProgress(report, "validating", "Validating VM request", "", nil)
	if validateErr := validateCreateRequest(req); validateErr != nil {
		return apischema.VirtualMachine{}, validateErr
	}
	sourceType := normalizedVMSourceType(req.SourceType)
	req.SourceType = sourceType
	networkName := normalizeVMNetwork(req.Network)
	preflightReq := apischema.VMPreflightRequest{
		ImagePresetID: req.ImagePresetID,
		Network:       networkName,
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
	if sourceType == vmSourceTypeImport {
		importPath, importErr := validateImportDisk(req.ImportPath)
		if importErr != nil {
			return apischema.VirtualMachine{}, importErr
		}
		req.ImportPath = importPath
		preflightReq.ImportPath = &importPath
	}
	reportVMCreateProgress(report, "preflight", "Checking host virtualization prerequisites", "", nil)
	preflight, preflightErr := runPreflight(ctx, preflightReq)
	if preflightErr != nil {
		return apischema.VirtualMachine{}, preflightErr
	}
	if readyErr := preflightReadyForNetwork(preflight, sourceType, networkName); readyErr != nil {
		return apischema.VirtualMachine{}, readyErr
	}

	var created apischema.VirtualMachine
	reportVMCreateProgress(report, "connecting", "Connecting to libvirt", "", nil)
	// The name lock keeps unused-disk cleanup away from a disk this create has
	// written but not yet attached to a defined VM.
	connErr := withVMNameLock(ctx, req.Name, func() error {
		return withLibvirtConn(ctx, func(conn libvirtConn) error {
			var createErr error
			created, createErr = createVMWithConn(ctx, conn, req, preflight.Firmware, report)
			return createErr
		})
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
	if networkErr := validateNetworkSelection(ctx, conn, req.Network); networkErr != nil {
		return apischema.VirtualMachine{}, networkErr
	}
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
	networkName := normalizeVMNetwork(req.Network)
	reportVMCreateProgress(report, "network", "Preparing VM network", networkName, nil)
	if networkName == defaultNetworkName {
		if networkErr := ensureDefaultNetworkActive(conn); networkErr != nil {
			return apischema.VirtualMachine{}, networkErr
		}
	}

	volumeName := managedVolumeName(req.Name)
	reportVMCreateProgress(report, "storage", "Preparing VM disk", volumeName, nil)
	storage, storageErr := createManagedStorage(ctx, conn, pool, volumeName, req, report)
	if storageErr != nil {
		return apischema.VirtualMachine{}, storageErr
	}

	switch req.Firmware {
	case vmFirmwareUEFI:
		if !firmware.UEFIAvailable {
			deleteCreatedStorage(conn, storage)
			return apischema.VirtualMachine{}, conflictf("UEFI firmware was requested but OVMF is not installed")
		}
	case vmFirmwareBIOS:
		firmware.UEFIAvailable = false
	default:
		if storage.LegacyBoot {
			firmware.UEFIAvailable = false
		}
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
	vms := []apischema.VirtualMachine{vm}
	enrichBridgeAddresses(ctx, conn, []libvirt.Domain{domain}, vms)
	vm = vms[0]
	reportVMCreateProgress(report, "complete", "VM created", req.Name, progressPercent(100))
	return vm, nil
}

func rollbackFailedStartedCreate(conn libvirtConn, domain libvirt.Domain, storage createdVMStorage) {
	_ = forceOffActiveDomain(conn, domain)
	_ = undefineDomain(conn, domain)
	deleteCreatedStorage(conn, storage)
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
