package virt

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	libvirt "github.com/digitalocean/go-libvirt"
	"libvirt.org/go/libvirtxml"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

const (
	managedRootPath   = defaultPoolPath + "/linuxio"
	managedISOPath    = managedRootPath + "/isos"
	managedCloudPath  = managedRootPath + "/cloud-images"
	managedDiskPrefix = "linuxio-"
	managedDiskSuffix = ".qcow2"
	managedSeedSuffix = "-seed.img"
)

var mkdirAll = os.MkdirAll

type createdVMVolume struct {
	Volume libvirt.StorageVol
	Name   string
	Path   string
	SizeGB int
}

type createdVMStorage struct {
	Boot createdVMVolume
	Seed *createdVMVolume
	// LegacyBoot is set when an imported appliance declares BIOS firmware.
	LegacyBoot bool
}

func createManagedStorage(ctx context.Context, conn libvirtConn, pool libvirt.StoragePool, volumeName string, req apischema.VMCreateRequest, report vmCreateReporter) (createdVMStorage, error) {
	switch normalizedVMSourceType(req.SourceType) {
	case vmSourceTypeImagePreset:
		return createManagedImageStorage(ctx, conn, pool, volumeName, req, report)
	case vmSourceTypeImport:
		return createManagedImportStorage(ctx, conn, pool, volumeName, req, report)
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

func managedVolumeName(vmName string) string {
	return managedDiskPrefix + vmName + managedDiskSuffix
}

func managedSeedVolumeName(vmName string) string {
	return managedDiskPrefix + vmName + managedSeedSuffix
}

func isManagedVolumeName(vmName, volumeName string) bool {
	return volumeName == managedVolumeName(vmName) || volumeName == managedSeedVolumeName(vmName)
}
