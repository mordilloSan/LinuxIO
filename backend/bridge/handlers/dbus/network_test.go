package dbus

import (
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/dbus/internal/network"
)

func TestMergeConfiguredStatePrefersManualConfiguredValues(t *testing.T) {
	info := NetworkInterfaceInfo{
		Name:         "eth0",
		IP4Addresses: []string{"192.168.1.5/24"},
		DNS:          []string{"192.168.1.1"},
		Gateway:      "192.168.1.1",
		MTU:          1500,
		IPv4Method:   "unknown",
	}
	cfg := network.InterfaceConfig{
		Backend:       "netplan",
		IPv4Method:    "manual",
		IPv4Addresses: []string{"10.0.0.20/24"},
		DNS:           []string{"1.1.1.1", "8.8.8.8"},
		Gateway:       "10.0.0.1",
	}
	mtu := uint32(9000)
	cfg.MTU = &mtu

	mergeConfiguredState(&info, cfg)

	if got := info.IP4Addresses[0]; got != "10.0.0.20/24" {
		t.Fatalf("expected configured IPv4 address, got %s", got)
	}
	if got := info.Gateway; got != "10.0.0.1" {
		t.Fatalf("expected configured gateway, got %s", got)
	}
	if got := info.DNS[0]; got != "1.1.1.1" {
		t.Fatalf("expected configured DNS, got %v", info.DNS)
	}
	if got := info.MTU; got != 9000 {
		t.Fatalf("expected configured MTU, got %d", got)
	}
	if got := info.IPv4Method; got != "manual" {
		t.Fatalf("expected manual IPv4 method, got %s", got)
	}
}

func TestMergeConfiguredStateBackfillsEmptyLiveValues(t *testing.T) {
	info := NetworkInterfaceInfo{Name: "eth1"}
	cfg := network.InterfaceConfig{
		Backend:       "ifcfg",
		IPv4Method:    "manual",
		IPv4Addresses: []string{"172.16.10.5/24"},
		IPv6Addresses: []string{"fd00::10/64"},
		DNS:           []string{"9.9.9.9"},
		Gateway:       "172.16.10.1",
	}

	mergeConfiguredState(&info, cfg)

	if len(info.IP4Addresses) != 1 || info.IP4Addresses[0] != "172.16.10.5/24" {
		t.Fatalf("expected configured IPv4 addresses, got %v", info.IP4Addresses)
	}
	if len(info.IP6Addresses) != 1 || info.IP6Addresses[0] != "fd00::10/64" {
		t.Fatalf("expected configured IPv6 addresses, got %v", info.IP6Addresses)
	}
	if len(info.DNS) != 1 || info.DNS[0] != "9.9.9.9" {
		t.Fatalf("expected configured DNS, got %v", info.DNS)
	}
	if info.Gateway != "172.16.10.1" {
		t.Fatalf("expected configured gateway, got %s", info.Gateway)
	}
}
