package virt

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	libvirt "github.com/digitalocean/go-libvirt"
	"github.com/digitalocean/go-libvirt/socket/dialers"
)

type libvirtConn interface {
	ConnectGetAllDomainStats([]libvirt.Domain, uint32, uint32) ([]libvirt.DomainStatsRecord, error)
	ConnectListAllDomains(int32, libvirt.ConnectListAllDomainsFlags) ([]libvirt.Domain, uint32, error)
	ConnectListAllNetworks(int32, libvirt.ConnectListAllNetworksFlags) ([]libvirt.Network, uint32, error)
	ConnectListAllStoragePools(int32, libvirt.ConnectListAllStoragePoolsFlags) ([]libvirt.StoragePool, uint32, error)
	DomainCreate(libvirt.Domain) error
	DomainDefineXML(string) (libvirt.Domain, error)
	DomainDestroy(libvirt.Domain) error
	DomainGetAutostart(libvirt.Domain) (int32, error)
	DomainGetInfo(libvirt.Domain) (uint8, uint64, uint64, uint16, uint64, error)
	DomainGetState(libvirt.Domain, uint32) (int32, int32, error)
	DomainGetXMLDesc(libvirt.Domain, libvirt.DomainXMLFlags) (string, error)
	DomainInterfaceAddresses(libvirt.Domain, uint32, uint32) ([]libvirt.DomainInterface, error)
	DomainIsActive(libvirt.Domain) (int32, error)
	DomainLookupByName(string) (libvirt.Domain, error)
	DomainReboot(libvirt.Domain, libvirt.DomainRebootFlagValues) error
	DomainResume(libvirt.Domain) error
	DomainSetAutostart(libvirt.Domain, int32) error
	DomainShutdown(libvirt.Domain) error
	DomainSuspend(libvirt.Domain) error
	DomainUndefineFlags(libvirt.Domain, libvirt.DomainUndefineFlagsValues) error
	NetworkCreate(libvirt.Network) error
	NetworkGetDhcpLeases(libvirt.Network, libvirt.OptString, int32, uint32) ([]libvirt.NetworkDhcpLease, uint32, error)
	NetworkGetXMLDesc(libvirt.Network, uint32) (string, error)
	NetworkIsActive(libvirt.Network) (int32, error)
	NetworkLookupByName(string) (libvirt.Network, error)
	NetworkSetAutostart(libvirt.Network, int32) error
	StoragePoolCreate(libvirt.StoragePool, libvirt.StoragePoolCreateFlags) error
	StoragePoolDefineXML(string, uint32) (libvirt.StoragePool, error)
	StoragePoolGetInfo(libvirt.StoragePool) (uint8, uint64, uint64, uint64, error)
	StoragePoolGetXMLDesc(libvirt.StoragePool, libvirt.StorageXMLFlags) (string, error)
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

type interruptibleLibvirtConn struct {
	libvirtConn
	interrupt func()
}

func (c *interruptibleLibvirtConn) closeTransport() {
	c.interrupt()
}

func withLibvirt(ctx context.Context, fn func(libvirtConn) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var lastErr error
	for _, socketPath := range []string{"/var/run/libvirt/libvirt-sock", "/var/run/libvirt/virtqemud-sock"} {
		if err := ctx.Err(); err != nil {
			return err
		}
		raw, err := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "unix", socketPath)
		if err != nil {
			lastErr = err
			continue
		}
		stopCancellation := interruptOnContextDone(ctx, func() { _ = raw.Close() })
		l := libvirt.NewWithDialer(dialers.NewAlreadyConnected(raw))
		if err := l.Connect(); err != nil {
			_ = raw.Close()
			stopCancellation()
			lastErr = err
			continue
		}
		disconnected := l.Disconnected()
		defer func() {
			_ = l.Disconnect() // clean CONNECT_CLOSE; a bare socket close makes libvirtd log "End of file while reading data"
			_ = raw.Close()
			<-disconnected
			stopCancellation()
		}()
		if err := ctx.Err(); err != nil {
			return err
		}
		callErr := fn(&interruptibleLibvirtConn{
			libvirtConn: l,
			interrupt:   func() { _ = raw.Close() },
		})
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return callErr
	}
	if err := ctx.Err(); err != nil {
		return err
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
