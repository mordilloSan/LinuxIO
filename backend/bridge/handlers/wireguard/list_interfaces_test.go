package wireguard

import (
	"context"
	"slices"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func TestListInterfacesReportsHeaderAndHooks(t *testing.T) {
	newWireGuardTestEnv(t)
	cfg := writeTestInterface(t, "wgtest", "10.8.0.1/29")
	cfg.Host = "vpn.example.org"
	cfg.DNS = []string{"9.9.9.9"}
	cfg.MTU = 1400
	cfg.Hooks[apischema.WireGuardPreUp] = []string{"echo pre"}
	if err := WriteWireGuardConfig(configPath("wgtest"), cfg); err != nil {
		t.Fatalf("WriteWireGuardConfig returned error: %v", err)
	}

	interfaces, err := ListInterfaces(context.Background())
	if err != nil {
		t.Fatalf("ListInterfaces returned error: %v", err)
	}
	if len(interfaces) != 1 {
		t.Fatalf("interfaces = %+v", interfaces)
	}
	iface := interfaces[0]
	if iface.Host != "vpn.example.org" || !slices.Equal(iface.DNS, []string{"9.9.9.9"}) || iface.MTU != 1400 {
		t.Fatalf("interface = %+v", iface)
	}
	if hooksChanged(iface.Hooks, cfg.Hooks) {
		t.Fatalf("hooks = %v, want %v", iface.Hooks, cfg.Hooks)
	}
	// Every kind is present, as [] when unused, so the client never sees null.
	if lines, ok := iface.Hooks[apischema.WireGuardPreDown]; !ok || lines == nil {
		t.Fatalf("PreDown = %#v, want an empty list", lines)
	}
}
