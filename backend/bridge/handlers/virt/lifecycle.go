package virt

import (
	"context"
	"fmt"
	"path/filepath"

	libvirt "github.com/digitalocean/go-libvirt"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func StartVM(ctx context.Context, name string) error {
	return lifecycle(ctx, name, func(conn libvirtConn, domain libvirt.Domain) error {
		var repairErr error
		domain, repairErr = repairDomainBeforeStart(conn, domain)
		if repairErr != nil {
			return repairErr
		}
		return conn.DomainCreate(domain)
	})
}

func ShutdownVM(ctx context.Context, name string) error {
	return lifecycle(ctx, name, func(conn libvirtConn, domain libvirt.Domain) error {
		return conn.DomainShutdown(domain)
	})
}

func RebootVM(ctx context.Context, name string) error {
	return lifecycle(ctx, name, func(conn libvirtConn, domain libvirt.Domain) error {
		return conn.DomainReboot(domain, libvirt.DomainRebootDefault)
	})
}

func ForceOffVM(ctx context.Context, name string) error {
	return lifecycle(ctx, name, func(conn libvirtConn, domain libvirt.Domain) error {
		return conn.DomainDestroy(domain)
	})
}

func SuspendVM(ctx context.Context, name string) error {
	return lifecycle(ctx, name, func(conn libvirtConn, domain libvirt.Domain) error {
		return conn.DomainSuspend(domain)
	})
}

func ResumeVM(ctx context.Context, name string) error {
	return lifecycle(ctx, name, func(conn libvirtConn, domain libvirt.Domain) error {
		return conn.DomainResume(domain)
	})
}

func lifecycle(ctx context.Context, name string, fn func(libvirtConn, libvirt.Domain) error) error {
	if err := validateVMName(name); err != nil {
		return err
	}
	return withLibvirtConn(ctx, func(conn libvirtConn) error {
		domain, err := lookupDomain(conn, name)
		if err != nil {
			return err
		}
		return fn(conn, domain)
	})
}

func repairDomainBeforeStart(conn libvirtConn, domain libvirt.Domain) (libvirt.Domain, error) {
	xmlDoc, xmlErr := conn.DomainGetXMLDesc(domain, 0)
	if xmlErr != nil {
		return domain, nil
	}
	if storageErr := repairManagedStorageAccessFromDomainXML(xmlDoc); storageErr != nil {
		return domain, fmt.Errorf("repair VM storage permissions: %w", storageErr)
	}
	normalizedXML, changed, normalizeErr := normalizeLinuxIOVNCGraphicsXML(xmlDoc)
	if normalizeErr != nil {
		return domain, fmt.Errorf("repair VM graphics XML: %w", normalizeErr)
	}
	if !changed {
		return domain, nil
	}
	redefined, defineErr := conn.DomainDefineXML(normalizedXML)
	if defineErr != nil {
		return domain, fmt.Errorf("repair VM graphics XML: %w", defineErr)
	}
	return redefined, nil
}

func DeleteVM(ctx context.Context, req apischema.VMDeleteRequest) (apischema.VMDeleteResult, error) {
	if validateErr := validateVMName(req.Name); validateErr != nil {
		return apischema.VMDeleteResult{}, validateErr
	}
	var result apischema.VMDeleteResult
	connErr := withLibvirtConn(ctx, func(conn libvirtConn) error {
		var deleteErr error
		result, deleteErr = deleteVMWithConn(conn, req)
		return deleteErr
	})
	if result.Removed == nil {
		result.Removed = []string{}
	}
	if result.Preserved == nil {
		result.Preserved = []string{}
	}
	return result, connErr
}

func deleteVMWithConn(conn libvirtConn, req apischema.VMDeleteRequest) (apischema.VMDeleteResult, error) {
	domain, lookupErr := lookupDomain(conn, req.Name)
	if lookupErr != nil {
		return apischema.VMDeleteResult{}, lookupErr
	}
	vm, parseErr := virtualMachineFromDomain(conn, domain)
	if parseErr != nil {
		return apischema.VMDeleteResult{}, parseErr
	}
	if powerErr := forceOffActiveDomain(conn, domain); powerErr != nil {
		return apischema.VMDeleteResult{}, powerErr
	}
	if undefineErr := undefineDomain(conn, domain); undefineErr != nil {
		return apischema.VMDeleteResult{}, undefineErr
	}
	return deleteManagedDisks(conn, req, vm.Disks)
}

func forceOffActiveDomain(conn libvirtConn, domain libvirt.Domain) error {
	active, activeErr := conn.DomainIsActive(domain)
	if activeErr != nil {
		return fmt.Errorf("check VM active state: %w", activeErr)
	}
	if active == 0 {
		return nil
	}
	if destroyErr := conn.DomainDestroy(domain); destroyErr != nil {
		return fmt.Errorf("force off VM before delete: %w", destroyErr)
	}
	return nil
}

func undefineDomain(conn libvirtConn, domain libvirt.Domain) error {
	flags := libvirt.DomainUndefineManagedSave |
		libvirt.DomainUndefineSnapshotsMetadata |
		libvirt.DomainUndefineNvram |
		libvirt.DomainUndefineCheckpointsMetadata
	return conn.DomainUndefineFlags(domain, flags)
}

func deleteManagedDisks(conn libvirtConn, req apischema.VMDeleteRequest, disks []apischema.VMDisk) (apischema.VMDeleteResult, error) {
	var result apischema.VMDeleteResult
	for _, disk := range disks {
		if !req.DeleteDisks {
			result.Preserved = append(result.Preserved, disk.Path)
			continue
		}
		removed, removeErr := deleteIfManagedDisk(conn, req.Name, disk)
		if removeErr != nil {
			return apischema.VMDeleteResult{}, removeErr
		}
		if removed {
			result.Removed = append(result.Removed, disk.Path)
		} else {
			result.Preserved = append(result.Preserved, disk.Path)
		}
	}
	return result, nil
}

func deleteIfManagedDisk(conn libvirtConn, vmName string, disk apischema.VMDisk) (bool, error) {
	if !disk.Owned || disk.Path == "" {
		return false, nil
	}
	expectedName := disk.VolumeName
	if expectedName == "" {
		expectedName = filepath.Base(disk.Path)
	}
	if !isManagedVolumeName(vmName, expectedName) {
		return false, nil
	}
	vol, err := conn.StorageVolLookupByPath(disk.Path)
	if err != nil {
		if !isStorageVolMissing(err) {
			return false, fmt.Errorf("look up disk %s: %w", disk.Path, err)
		}
		pool, poolErr := conn.StoragePoolLookupByName(defaultPoolName)
		if poolErr != nil {
			if !isStoragePoolMissing(poolErr) {
				return false, fmt.Errorf("look up default storage pool: %w", poolErr)
			}
			return false, nil
		}
		vol, err = conn.StorageVolLookupByName(pool, expectedName)
		if err != nil {
			if !isStorageVolMissing(err) {
				return false, fmt.Errorf("look up disk %s: %w", expectedName, err)
			}
			return false, nil
		}
	}
	if vol.Pool != defaultPoolName || vol.Name != expectedName {
		return false, nil
	}
	if err := conn.StorageVolDelete(vol, libvirt.StorageVolDeleteNormal); err != nil {
		return false, fmt.Errorf("delete disk %s: %w", disk.Path, err)
	}
	return true, nil
}
