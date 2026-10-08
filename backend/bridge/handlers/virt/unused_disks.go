package virt

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	libvirt "github.com/digitalocean/go-libvirt"
	"libvirt.org/go/libvirtxml"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/filelock"
)

const managedSeedSuffix = "-seed.img"

var (
	// Directories where LinuxIO writes VM disks: the default pool (ISO
	// installs) and the cloud image folder (ready images).
	managedDiskDirs = []string{defaultPoolPath, managedCloudPath}
	vmLockDir       = managedRootPath + "/.locks"
)

// withVMNameLock serializes work on one VM name across bridge processes.
func withVMNameLock(ctx context.Context, vmName string, fn func() error) error {
	return filelock.RunExclusive(ctx, filepath.Join(vmLockDir, "vm-"+vmName), fn,
		filelock.WithDirPermissions(0o700))
}

// managedDiskVMName returns the VM a LinuxIO disk or seed file name belongs to.
func managedDiskVMName(fileName string) (string, bool) {
	rest, ok := strings.CutPrefix(fileName, managedDiskPrefix)
	if !ok {
		return "", false
	}
	for _, suffix := range []string{managedSeedSuffix, managedDiskSuffix} {
		if name, found := strings.CutSuffix(rest, suffix); found && validateVMName(name) == nil {
			return name, true
		}
	}
	return "", false
}

type referencedDisks struct {
	files map[string]struct{}
	vms   map[string]struct{}
}

// inUse is deliberately broad: a disk counts as used when any VM references a
// file with its name in any folder, or when a VM with its owner's name exists.
func (r referencedDisks) inUse(fileName, vmName string) bool {
	_, file := r.files[fileName]
	_, vm := r.vms[vmName]
	return file || vm
}

func listReferencedDisks(conn libvirtConn) (referencedDisks, error) {
	flags := libvirt.ConnectListDomainsActive | libvirt.ConnectListDomainsInactive
	domains, _, err := conn.ConnectListAllDomains(1, flags)
	if err != nil {
		return referencedDisks{}, fmt.Errorf("list VMs: %w", err)
	}
	out := referencedDisks{files: map[string]struct{}{}, vms: map[string]struct{}{}}
	for _, domain := range domains {
		out.vms[domain.Name] = struct{}{}
		xmlDoc, err := conn.DomainGetXMLDesc(domain, libvirt.DomainXMLInactive)
		if err != nil {
			return referencedDisks{}, fmt.Errorf("read VM %s: %w", domain.Name, err)
		}
		var parsed libvirtxml.Domain
		if err := parsed.Unmarshal(xmlDoc); err != nil {
			return referencedDisks{}, fmt.Errorf("parse VM %s: %w", domain.Name, err)
		}
		if parsed.Devices == nil {
			continue
		}
		for _, disk := range parsed.Devices.Disks {
			if path := mapDisk(disk, nil, nil).Path; path != "" {
				out.files[filepath.Base(path)] = struct{}{}
			}
		}
	}
	return out, nil
}

func ListUnusedDisks(ctx context.Context) ([]apischema.VMUnusedDisk, error) {
	out := []apischema.VMUnusedDisk{}
	err := withLibvirtConn(ctx, func(conn libvirtConn) error {
		used, err := listReferencedDisks(conn)
		if err != nil {
			return err
		}
		for _, dir := range managedDiskDirs {
			disks, err := unusedDisksIn(dir, used)
			if err != nil {
				return err
			}
			out = append(out, disks...)
		}
		return nil
	})
	return out, err
}

func unusedDisksIn(dir string, used referencedDisks) ([]apischema.VMUnusedDisk, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", dir, err)
	}
	var out []apischema.VMUnusedDisk
	for _, entry := range entries {
		vmName, ok := managedDiskVMName(entry.Name())
		if !ok || !entry.Type().IsRegular() || used.inUse(entry.Name(), vmName) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		out = append(out, apischema.VMUnusedDisk{
			Name:       entry.Name(),
			Path:       filepath.Join(dir, entry.Name()),
			VMName:     vmName,
			SizeBytes:  allocatedBytes(info),
			ModifiedAt: info.ModTime().UTC().Format(time.RFC3339),
		})
	}
	return out, nil
}

// allocatedBytes reports the space a sparse disk image really uses on disk.
func allocatedBytes(info os.FileInfo) int64 {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return stat.Blocks * 512
	}
	return info.Size()
}

// DeleteUnusedDisk removes one LinuxIO disk file after re-checking, under the
// owning VM's name lock, that no VM uses it.
func DeleteUnusedDisk(ctx context.Context, path string) error {
	clean := filepath.Clean(path)
	if clean != path || !slices.Contains(managedDiskDirs, filepath.Dir(clean)) {
		return badRequestf("%q is not a LinuxIO VM disk", path)
	}
	vmName, ok := managedDiskVMName(filepath.Base(clean))
	if !ok {
		return badRequestf("%q is not a LinuxIO VM disk", path)
	}
	return withVMNameLock(ctx, vmName, func() error {
		return withLibvirtConn(ctx, func(conn libvirtConn) error {
			return deleteUnusedDiskWithConn(conn, clean, vmName)
		})
	})
}

func deleteUnusedDiskWithConn(conn libvirtConn, path, vmName string) error {
	name := filepath.Base(path)
	used, err := listReferencedDisks(conn)
	if err != nil {
		return err
	}
	if used.inUse(name, vmName) {
		return conflictf("disk %s is used by a VM", name)
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return notFoundf("disk %s no longer exists", name)
	}
	if err != nil {
		return fmt.Errorf("inspect disk %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return badRequestf("%q is not a LinuxIO VM disk", path)
	}
	// Prefer libvirt so its pool inventory stays in sync; the cloud image
	// folder is not a pool, so fall back to unlinking.
	if vol, lookupErr := conn.StorageVolLookupByPath(path); lookupErr == nil {
		if conn.StorageVolDelete(vol, libvirt.StorageVolDeleteNormal) == nil {
			return nil
		}
	}
	if err := removeFile(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete disk %s: %w", path, err)
	}
	return nil
}
