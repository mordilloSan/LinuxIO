package virt

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	libvirt "github.com/digitalocean/go-libvirt"
	"github.com/vishvananda/netlink"
	"libvirt.org/go/libvirtxml"
)

const (
	defaultPoolName    = "default"
	defaultPoolPath    = "/var/lib/libvirt/images"
	defaultNetworkName = "default"
)

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
		removed, removeErr := removeStaleDefaultBridge(conn, network, err)
		if removeErr != nil {
			return errors.Join(defaultNetworkStartError(err), removeErr)
		}
		if !removed {
			return defaultNetworkStartError(err)
		}
		if retryErr := conn.NetworkCreate(network); retryErr != nil {
			return defaultNetworkStartError(retryErr)
		}
	}
	return nil
}

// deleteHostLink removes a host network interface; tests replace it.
var deleteHostLink = func(name string) error {
	link, err := netlink.LinkByName(name)
	if err != nil {
		return fmt.Errorf("find interface %s: %w", name, err)
	}
	if err := netlink.LinkDel(link); err != nil {
		return fmt.Errorf("delete interface %s: %w", name, err)
	}
	return nil
}

// removeStaleDefaultBridge deletes the bridge an inactive default network is
// blocked by, which happens when libvirt loses its /run state (a WSL or
// daemon restart) but the kernel keeps the bridge. It only acts when libvirt
// names the network's own bridge as the conflict and nothing is attached to
// it, so it never touches a bridge that is in use.
func removeStaleDefaultBridge(conn libvirtConn, network libvirt.Network, startErr error) (bool, error) {
	_, after, found := strings.Cut(startErr.Error(), "already in use by interface ")
	fields := strings.Fields(after)
	if !found || len(fields) == 0 {
		return false, nil
	}
	conflict := fields[0]
	xmlDoc, err := conn.NetworkGetXMLDesc(network, uint32(libvirt.NetworkXMLInactive))
	if err != nil {
		return false, fmt.Errorf("read default NAT network XML: %w", err)
	}
	var parsed libvirtxml.Network
	if err = parsed.Unmarshal(xmlDoc); err != nil {
		return false, fmt.Errorf("parse default NAT network XML: %w", err)
	}
	if parsed.Bridge == nil || parsed.Bridge.Name != conflict {
		return false, nil
	}
	if info, statErr := os.Stat(filepath.Join(networkSysfsRoot, conflict, "bridge")); statErr != nil || !info.IsDir() {
		return false, nil
	}
	members, err := os.ReadDir(filepath.Join(networkSysfsRoot, conflict, "brif"))
	if err != nil || len(members) > 0 {
		return false, nil
	}
	if err := deleteHostLink(conflict); err != nil {
		return false, fmt.Errorf("remove stale default NAT bridge: %w", err)
	}
	return true, nil
}

func defaultNetworkStartError(err error) error {
	if defaultNetworkAddressInUse(err) {
		return fmt.Errorf("default NAT network cannot start because 192.168.122.1 is already in use; stop the conflicting dnsmasq/libvirt process or reconfigure the libvirt default network address, then retry: %w", err)
	}
	if strings.Contains(strings.ToLower(err.Error()), "network is already in use by interface") {
		return fmt.Errorf("default NAT network cannot start because its subnet is already assigned to a host interface, often a bridge left behind by an earlier libvirt run (for example after a WSL or daemon restart); delete that interface if no VM uses it, or reconfigure the libvirt default network address, then retry: %w", err)
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
