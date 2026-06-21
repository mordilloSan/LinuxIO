package virt

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	libvirt "github.com/digitalocean/go-libvirt"
	"github.com/digitalocean/go-libvirt/socket/dialers"
)

const (
	defaultPoolName    = "default"
	defaultPoolPath    = "/var/lib/libvirt/images"
	defaultNetworkName = "default"
	managedRootPath    = defaultPoolPath + "/linuxio"
	managedISOPath     = managedRootPath + "/isos"
	managedCloudPath   = managedRootPath + "/cloud-images"
	managedDiskPrefix  = "linuxio-"
	managedDiskSuffix  = ".qcow2"
	vncSocketDir       = "/var/lib/libvirt/qemu"
)

var validVMName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,62}$`)

type libvirtConn interface {
	ConnectListAllDomains(int32, libvirt.ConnectListAllDomainsFlags) ([]libvirt.Domain, uint32, error)
	DomainCreate(libvirt.Domain) error
	DomainDefineXML(string) (libvirt.Domain, error)
	DomainDestroy(libvirt.Domain) error
	DomainGetAutostart(libvirt.Domain) (int32, error)
	DomainGetInfo(libvirt.Domain) (uint8, uint64, uint64, uint16, uint64, error)
	DomainGetState(libvirt.Domain, uint32) (int32, int32, error)
	DomainGetXMLDesc(libvirt.Domain, libvirt.DomainXMLFlags) (string, error)
	DomainIsActive(libvirt.Domain) (int32, error)
	DomainLookupByName(string) (libvirt.Domain, error)
	DomainReboot(libvirt.Domain, libvirt.DomainRebootFlagValues) error
	DomainResume(libvirt.Domain) error
	DomainShutdown(libvirt.Domain) error
	DomainSuspend(libvirt.Domain) error
	DomainUndefineFlags(libvirt.Domain, libvirt.DomainUndefineFlagsValues) error
	NetworkCreate(libvirt.Network) error
	NetworkGetDhcpLeases(libvirt.Network, libvirt.OptString, int32, uint32) ([]libvirt.NetworkDhcpLease, uint32, error)
	NetworkIsActive(libvirt.Network) (int32, error)
	NetworkLookupByName(string) (libvirt.Network, error)
	NetworkSetAutostart(libvirt.Network, int32) error
	StoragePoolCreate(libvirt.StoragePool, libvirt.StoragePoolCreateFlags) error
	StoragePoolDefineXML(string, uint32) (libvirt.StoragePool, error)
	StoragePoolIsActive(libvirt.StoragePool) (int32, error)
	StoragePoolLookupByName(string) (libvirt.StoragePool, error)
	StoragePoolRefresh(libvirt.StoragePool, uint32) error
	StoragePoolSetAutostart(libvirt.StoragePool, int32) error
	StorageVolCreateXML(libvirt.StoragePool, string, libvirt.StorageVolCreateFlags) (libvirt.StorageVol, error)
	StorageVolDelete(libvirt.StorageVol, libvirt.StorageVolDeleteFlags) error
	StorageVolGetXMLDesc(libvirt.StorageVol, uint32) (string, error)
	StorageVolLookupByName(libvirt.StoragePool, string) (libvirt.StorageVol, error)
	StorageVolLookupByPath(string) (libvirt.StorageVol, error)
}

var withLibvirtConn = withLibvirt
var mkdirAll = os.MkdirAll

func withLibvirt(ctx context.Context, fn func(libvirtConn) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var lastErr error
	for _, socketPath := range []string{"/var/run/libvirt/libvirt-sock", "/var/run/libvirt/virtqemud-sock"} {
		l := libvirt.NewWithDialer(dialers.NewLocal(
			dialers.WithSocket(socketPath),
			dialers.WithLocalTimeout(3*time.Second),
		))
		if err := l.Connect(); err != nil {
			lastErr = err
			continue
		}
		defer func() {
			_ = l.Disconnect()
		}()
		if err := ctx.Err(); err != nil {
			return err
		}
		return fn(l)
	}
	if lastErr == nil {
		lastErr = errors.New("no libvirt socket paths attempted")
	}
	return fmt.Errorf("libvirt not reachable: %w", lastErr)
}

func CheckLibvirtAvailability(ctx context.Context) (bool, error) {
	if err := withLibvirtConn(ctx, func(libvirtConn) error { return nil }); err != nil {
		return false, err
	}
	return true, nil
}

func validateVMName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return badRequestf("VM name is required")
	}
	if !validVMName.MatchString(name) || strings.Contains(name, "..") {
		return badRequestf("VM name must be 1-63 characters and contain only letters, numbers, dots, dashes, and underscores")
	}
	if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "-") {
		return badRequestf("VM name must not start with a dot or dash")
	}
	return nil
}

func managedVolumeName(vmName string) string {
	return managedDiskPrefix + vmName + managedDiskSuffix
}

func managedSeedVolumeName(vmName string) string {
	return managedDiskPrefix + vmName + "-seed.img"
}

func isManagedVolumeName(vmName, volumeName string) bool {
	return volumeName == managedVolumeName(vmName) || volumeName == managedSeedVolumeName(vmName)
}

func isSafeVNCSocket(path string) bool {
	return strings.HasPrefix(path, vncSocketDir+"/") && !strings.Contains(path, "..")
}

func qemuReadable(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}
