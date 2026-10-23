// Package netdbus holds the netplan and systemd-networkd D-Bus calls shared by
// the interface-configuration writers and the host-bridge engine.
package netdbus

import (
	"context"
	"fmt"
	"strings"
	"time"

	godbus "github.com/godbus/dbus/v5"
	"github.com/vishvananda/netlink"

	"github.com/mordilloSan/LinuxIO/backend/common/dbusclient"
)

const (
	NetplanBusName     = "io.netplan.Netplan"
	NetplanRootPath    = "/io/netplan/Netplan"
	NetplanIface       = "io.netplan.Netplan"
	NetplanConfigIface = "io.netplan.Netplan.Config"

	NetworkdBusName      = "org.freedesktop.network1"
	networkdPath         = "/org/freedesktop/network1"
	networkdManagerIface = "org.freedesktop.network1.Manager"
	networkdLinkIface    = "org.freedesktop.network1.Link"
)

// CallNetplan runs Generate or Apply through netplan-dbus rather than
// executing netplan from the bridge: a child would inherit the bridge's 077
// umask and write /run/systemd/network files systemd-networkd cannot read.
func CallNetplan(ctx context.Context, method string) error {
	return CallNetplanBool(ctx, NetplanRootPath, NetplanIface, method)
}

func CallNetplanBool(ctx context.Context, path godbus.ObjectPath, iface, method string) error {
	var accepted bool
	err := dbusclient.UseSystemBusWithOptions(ctx, dbusclient.SystemBusOptions{
		Subsystem: "netplan",
		NoRetry:   true,
	}, func(ctx context.Context, conn *godbus.Conn) error {
		object := conn.Object(NetplanBusName, path)
		if callErr := object.CallWithContext(ctx, iface+"."+method, 0).Store(&accepted); callErr != nil {
			return callErr
		}
		if !accepted {
			return fmt.Errorf("netplan %s rejected the transaction", method)
		}
		return nil
	})
	return err
}

// CallNetworkd is what networkctl reload and reconfigure do: a method call on
// the systemd-networkd manager object.
func CallNetworkd(ctx context.Context, method, iface string) error {
	var args []any
	if iface != "" {
		link, err := netlink.LinkByName(iface)
		if err != nil {
			return fmt.Errorf("resolve link %s: %w", iface, err)
		}
		args = append(args, int32(link.Attrs().Index))
	}
	return dbusclient.UseSystemBusWithOptions(ctx, dbusclient.SystemBusOptions{
		Subsystem: "networkd",
		Timeout:   30 * time.Second,
	}, func(ctx context.Context, conn *godbus.Conn) error {
		if err := conn.Object(NetworkdBusName, godbus.ObjectPath(networkdPath)).CallWithContext(ctx, networkdManagerIface+"."+method, 0, args...).Err; err != nil {
			return fmt.Errorf("systemd-networkd %s: %w", method, err)
		}
		return nil
	})
}

// NetworkdOwnsInterface reports whether systemd-networkd manages iface.
func NetworkdOwnsInterface(session dbusclient.SystemSession, iface string) (bool, error) {
	type listedLink struct {
		Index int32
		Name  string
		Path  godbus.ObjectPath
	}

	manager := session.ObjectFor(NetworkdBusName, godbus.ObjectPath(networkdPath))
	var links []listedLink
	if err := manager.CallWithContext(session.Context(), networkdManagerIface+".ListLinks", 0).Store(&links); err != nil {
		return false, fmt.Errorf("list systemd-networkd links: %w", err)
	}
	for _, link := range links {
		if link.Name != iface {
			continue
		}
		state, err := dbusclient.GetProperty[string](session, session.ObjectFor(NetworkdBusName, link.Path), networkdLinkIface, "AdministrativeState")
		if err != nil {
			return false, err
		}
		return !strings.EqualFold(strings.TrimSpace(state), "unmanaged"), nil
	}
	return false, nil
}
