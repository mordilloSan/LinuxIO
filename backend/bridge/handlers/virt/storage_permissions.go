package virt

import (
	"fmt"
	"os"
	"os/user"
	"strconv"

	"libvirt.org/go/libvirtxml"
)

var (
	chmodFile    = os.Chmod
	chownFile    = os.Chown
	lookupOSUser = user.Lookup
)

func makeManagedWritableDiskAccessible(path string) error {
	return makeManagedFileAccessible(path, 0o660, 0o666)
}

func makeManagedReadOnlyDiskAccessible(path string) error {
	return makeManagedFileAccessible(path, 0o640, 0o644)
}

func makeManagedFileAccessible(path string, ownerMode, fallbackMode os.FileMode) error {
	uid, gid, ok := libvirtQEMUUserAndGroup()
	if ok && os.Geteuid() == 0 {
		if chownErr := chownFile(path, uid, gid); chownErr != nil {
			return fmt.Errorf("set qemu ownership on %s: %w", path, chownErr)
		}
		if chmodErr := chmodFile(path, ownerMode); chmodErr != nil {
			return fmt.Errorf("set qemu permissions on %s: %w", path, chmodErr)
		}
		return nil
	}
	if chmodErr := chmodFile(path, fallbackMode); chmodErr != nil {
		return fmt.Errorf("set qemu-readable permissions on %s: %w", path, chmodErr)
	}
	return nil
}

func libvirtQEMUUserAndGroup() (int, int, bool) {
	for _, username := range []string{"libvirt-qemu", "qemu"} {
		account, lookupErr := lookupOSUser(username)
		if lookupErr != nil {
			continue
		}
		uid, uidErr := strconv.Atoi(account.Uid)
		gid, gidErr := strconv.Atoi(account.Gid)
		if uidErr != nil || gidErr != nil {
			continue
		}
		return uid, gid, true
	}
	return 0, 0, false
}

func repairManagedStorageAccessFromDomainXML(xmlDoc string) error {
	if dirErr := ensureManagedStorageDirectories(); dirErr != nil {
		return dirErr
	}

	var domain libvirtxml.Domain
	if unmarshalErr := domain.Unmarshal(xmlDoc); unmarshalErr != nil {
		return nil
	}
	metadata := parseLinuxIOMetadata(domain.Metadata)
	for _, disk := range metadata.Disks {
		if disk.Path == "" {
			continue
		}
		if !isManagedVolumeName(domain.Name, disk.Volume) {
			continue
		}
		if disk.SizeGB > 0 {
			if accessErr := makeManagedWritableDiskAccessible(disk.Path); accessErr != nil {
				return accessErr
			}
			continue
		}
		if accessErr := makeManagedReadOnlyDiskAccessible(disk.Path); accessErr != nil {
			return accessErr
		}
	}
	return nil
}
