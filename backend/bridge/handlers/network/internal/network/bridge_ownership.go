package network

import (
	"context"
	"fmt"
	"strings"
	"time"

	godbus "github.com/godbus/dbus/v5"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/dbusclient"
)

const (
	networkdBusName      = "org.freedesktop.network1"
	networkdPath         = "/org/freedesktop/network1"
	networkdManagerIface = "org.freedesktop.network1.Manager"
	networkdLinkIface    = "org.freedesktop.network1.Link"

	networkManagerEthernetDevice = uint32(1)
)

type networkManagerDeviceInfo struct {
	path                 godbus.ObjectPath
	activeConnection     godbus.ObjectPath
	availableConnections []godbus.ObjectPath
	managed              bool
	deviceType           uint32
}

func networkManagerOwnsSpareInterface(session dbusclient.SystemSession, iface string) (bool, error) {
	info, found, err := readNetworkManagerDevice(session, iface)
	if err != nil || !found {
		return false, err
	}
	return info.managed && info.deviceType == networkManagerEthernetDevice, nil
}

func requireNetworkManagerSpareInterface(ctx context.Context, iface string) error {
	return dbusclient.DBus.UseSessionWithOptions(ctx, dbusclient.SystemBusOptions{
		Subsystem: "network-manager",
		Timeout:   5 * time.Second,
	}, func(session dbusclient.SystemSession) error {
		info, found, err := readNetworkManagerDevice(session, iface)
		if err != nil {
			return err
		}
		if !found || !info.managed || info.deviceType != networkManagerEthernetDevice {
			return fmt.Errorf("NetworkManager does not manage %s as an Ethernet device", iface)
		}
		if info.activeConnection != "" && info.activeConnection != "/" {
			return fmt.Errorf("NetworkManager has an active connection on %s", iface)
		}
		if len(info.availableConnections) > 0 {
			return fmt.Errorf("NetworkManager has existing connection profiles for %s", iface)
		}
		return nil
	})
}

func readNetworkManagerDevice(session dbusclient.SystemSession, iface string) (networkManagerDeviceInfo, bool, error) {
	manager := session.ObjectFor(networkManagerBusName, godbus.ObjectPath(networkManagerPath))
	var devices []godbus.ObjectPath
	if err := manager.CallWithContext(session.Context(), networkManagerIface+".GetDevices", 0).Store(&devices); err != nil {
		return networkManagerDeviceInfo{}, false, fmt.Errorf("list NetworkManager devices: %w", err)
	}
	for _, path := range devices {
		device := session.ObjectFor(networkManagerBusName, path)
		name, err := dbusclient.GetProperty[string](session, device, networkManagerDeviceIface, "Interface")
		if err != nil {
			return networkManagerDeviceInfo{}, false, err
		}
		if name != iface {
			continue
		}
		managed, err := dbusclient.GetProperty[bool](session, device, networkManagerDeviceIface, "Managed")
		if err != nil {
			return networkManagerDeviceInfo{}, false, err
		}
		deviceType, err := dbusclient.GetProperty[uint32](session, device, networkManagerDeviceIface, "DeviceType")
		if err != nil {
			return networkManagerDeviceInfo{}, false, err
		}
		active, err := dbusclient.GetProperty[godbus.ObjectPath](session, device, networkManagerDeviceIface, "ActiveConnection")
		if err != nil {
			return networkManagerDeviceInfo{}, false, err
		}
		available, err := dbusclient.GetProperty[[]godbus.ObjectPath](session, device, networkManagerDeviceIface, "AvailableConnections")
		if err != nil {
			return networkManagerDeviceInfo{}, false, err
		}
		return networkManagerDeviceInfo{
			path:                 path,
			activeConnection:     active,
			availableConnections: available,
			managed:              managed,
			deviceType:           deviceType,
		}, true, nil
	}
	return networkManagerDeviceInfo{}, false, nil
}

func networkdOwnsInterface(session dbusclient.SystemSession, iface string) (bool, error) {
	type listedLink struct {
		Index int32
		Name  string
		Path  godbus.ObjectPath
	}

	manager := session.ObjectFor(networkdBusName, godbus.ObjectPath(networkdPath))
	var links []listedLink
	if err := manager.CallWithContext(session.Context(), networkdManagerIface+".ListLinks", 0).Store(&links); err != nil {
		return false, fmt.Errorf("list systemd-networkd links: %w", err)
	}
	for _, link := range links {
		if link.Name != iface {
			continue
		}
		state, err := dbusclient.GetProperty[string](session, session.ObjectFor(networkdBusName, link.Path), networkdLinkIface, "AdministrativeState")
		if err != nil {
			return false, err
		}
		return !strings.EqualFold(strings.TrimSpace(state), "unmanaged"), nil
	}
	return false, nil
}
