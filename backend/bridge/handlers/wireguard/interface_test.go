package wireguard

import (
	"context"
	"path/filepath"
	"slices"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func TestAddInterfaceStoresDNSAndHostInHeaderNotSidecar(t *testing.T) {
	newWireGuardTestEnv(t)
	dns := "1.1.1.1"
	host := "vpn.example.org"
	numPeers := "1"

	_, err := AddInterface(context.Background(), apischema.WireGuardAddInterfaceRequest{
		Name:       "wgtest",
		Addresses:  "10.7.0.1/24",
		ListenPort: "51820",
		EgressNic:  "eth0",
		DNS:        &dns,
		Host:       &host,
		NumPeers:   &numPeers,
	})
	if err != nil {
		t.Fatalf("AddInterface returned error: %v", err)
	}

	cfg, err := ParseWireGuardConfig(configPath("wgtest"))
	if err != nil {
		t.Fatalf("ParseWireGuardConfig returned error: %v", err)
	}
	if cfg.Host != "vpn.example.org" || !slices.Equal(cfg.DNS, []string{"1.1.1.1"}) {
		t.Fatalf("header = host %q dns %v", cfg.Host, cfg.DNS)
	}
	assertPathMissing(t, filepath.Join(wgConfigDir, "wgtest.dns"))

	peer, err := readPeerFile(peerConfigPath("wgtest", "Peer2"))
	if err != nil {
		t.Fatalf("readPeerFile returned error: %v", err)
	}
	if peer.Endpoint != "vpn.example.org:51820" {
		t.Fatalf("endpoint = %q, want the host override", peer.Endpoint)
	}
}

func TestListInterfacesReportsHeaderAndHooks(t *testing.T) {
	newWireGuardTestEnv(t)
	cfg := writeTestInterface(t, "wgtest", "10.8.0.1/29")
	cfg.Host = "vpn.example.org"
	cfg.DNS = []string{"9.9.9.9"}
	cfg.MTU = 1400
	cfg.PreUp = []string{"echo pre"}
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
	if !slices.Equal(iface.PreUp, []string{"echo pre"}) || len(iface.PostUp) != len(cfg.PostUp) || len(iface.PostDown) != len(cfg.PostDown) || iface.PreDown == nil {
		t.Fatalf("hooks = %+v", iface)
	}
}
