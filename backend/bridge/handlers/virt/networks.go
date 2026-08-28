package virt

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"

	libvirt "github.com/digitalocean/go-libvirt"
	"libvirt.org/go/libvirtxml"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

const (
	libvirtNetworkType = "libvirt"
	bridgeNetworkType  = "bridge"
	bridgeSysfsRoot    = "/sys/class/net"
)

var (
	networkSysfsRoot = bridgeSysfsRoot
)

// ListVMNetworks returns the default libvirt NAT network and existing host
// bridges. Bridges used by any libvirt network are not selectable separately.
func ListVMNetworks(ctx context.Context) ([]apischema.VMNetwork, error) {
	var out []apischema.VMNetwork
	err := withLibvirtConn(ctx, func(conn libvirtConn) error {
		libvirtNetworks, backingBridges, err := listLibvirtNetworks(ctx, conn)
		if err != nil {
			return err
		}
		bridges, err := listHostBridges(ctx, backingBridges)
		if err != nil {
			return err
		}
		out = append(out, libvirtNetworks...)
		out = append(out, bridges...)
		return nil
	})
	if out == nil {
		out = []apischema.VMNetwork{}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name == out[j].Name {
			return out[i].Type < out[j].Type
		}
		return out[i].Name < out[j].Name
	})
	return out, err
}

func listLibvirtNetworks(ctx context.Context, conn libvirtConn) ([]apischema.VMNetwork, map[string]struct{}, error) {
	flags := libvirt.ConnectListNetworksActive | libvirt.ConnectListNetworksInactive
	networks, _, err := conn.ConnectListAllNetworks(1, flags)
	if err != nil {
		return nil, nil, fmt.Errorf("list libvirt networks: %w", err)
	}
	out := make([]apischema.VMNetwork, 0, 1)
	backingBridges := make(map[string]struct{})
	for _, network := range networks {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		active, err := conn.NetworkIsActive(network)
		if err != nil {
			return nil, nil, fmt.Errorf("read libvirt network %q state: %w", network.Name, err)
		}
		xmlFlags := uint32(0)
		if active == 0 {
			xmlFlags = uint32(libvirt.NetworkXMLInactive)
		}
		xmlDoc, err := conn.NetworkGetXMLDesc(network, xmlFlags)
		if err != nil {
			return nil, nil, fmt.Errorf("read libvirt network %q XML: %w", network.Name, err)
		}
		var parsed libvirtxml.Network
		if err := parsed.Unmarshal(xmlDoc); err != nil {
			return nil, nil, fmt.Errorf("parse libvirt network %q XML: %w", network.Name, err)
		}
		if parsed.Bridge != nil && parsed.Bridge.Name != "" {
			backingBridges[parsed.Bridge.Name] = struct{}{}
		}
		if network.Name != defaultNetworkName {
			continue
		}
		out = append(out, apischema.VMNetwork{
			Name:   network.Name,
			Type:   libvirtNetworkType,
			Active: active != 0,
		})
	}
	return out, backingBridges, nil
}

func listHostBridges(ctx context.Context, excluded map[string]struct{}) ([]apischema.VMNetwork, error) {
	entries, err := os.ReadDir(networkSysfsRoot)
	if err != nil {
		return nil, fmt.Errorf("list host network interfaces: %w", err)
	}
	out := make([]apischema.VMNetwork, 0, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		name := entry.Name()
		if _, skip := excluded[name]; skip {
			continue
		}
		bridgePath := filepath.Join(networkSysfsRoot, name, "bridge")
		info, statErr := os.Stat(bridgePath)
		if statErr != nil || !info.IsDir() {
			continue
		}
		out = append(out, apischema.VMNetwork{
			Name:   name,
			Type:   bridgeNetworkType,
			Active: hostBridgeActive(name),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func hostBridgeActive(name string) bool {
	data, err := os.ReadFile(filepath.Join(networkSysfsRoot, name, "flags"))
	if err != nil {
		return false
	}
	flags, err := strconv.ParseUint(strings.TrimPrefix(strings.TrimSpace(string(data)), "0x"), 16, 32)
	return err == nil && flags&1 != 0
}

func normalizeVMNetwork(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return defaultNetworkName
	}
	return name
}

func validateVMNetworkName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" || name == defaultNetworkName {
		return nil
	}
	if len(name) > 15 || name == "." || name == ".." {
		return badRequestf("network name %q is invalid", name)
	}
	for _, r := range name {
		if unicode.IsSpace(r) || unicode.IsControl(r) || r == '/' || r == '\\' {
			return badRequestf("network name %q is invalid", name)
		}
	}
	return nil
}

func validateNetworkSelection(ctx context.Context, conn libvirtConn, name string) error {
	name = normalizeVMNetwork(name)
	if err := validateVMNetworkName(name); err != nil {
		return err
	}
	if name == defaultNetworkName {
		return nil
	}
	_, backingBridges, err := listLibvirtNetworks(ctx, conn)
	if err != nil {
		return err
	}
	bridges, err := listHostBridges(ctx, backingBridges)
	if err != nil {
		return err
	}
	for _, bridge := range bridges {
		if bridge.Name != name {
			continue
		}
		if !bridge.Active {
			return conflictf("host bridge %q is down", name)
		}
		return nil
	}
	return conflictf("host bridge %q does not exist", name)
}
