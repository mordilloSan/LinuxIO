package wireguard

import (
	"context"
	"slices"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func updateInterface(t *testing.T, req apischema.WireGuardUpdateInterfaceRequest) {
	t.Helper()
	req.Name = "wgtest"
	if err := UpdateInterface(context.Background(), req); err != nil {
		t.Fatalf("UpdateInterface returned error: %v", err)
	}
}

func peerEndpoints(t *testing.T, ids ...string) []string {
	t.Helper()
	endpoints := make([]string, 0, len(ids))
	for _, id := range ids {
		peer, err := readPeerFile(peerConfigPath("wgtest", id))
		if err != nil {
			t.Fatalf("readPeerFile(%s) returned error: %v", id, err)
		}
		endpoints = append(endpoints, peer.Endpoint)
	}
	return endpoints
}

func TestUpdateInterfacePortChangeRestartsWithOldConfigDown(t *testing.T) {
	env, first, second := setupTwoPeers(t)
	env.wgQuick, env.wgQuickPorts = nil, nil

	updateInterface(t, apischema.WireGuardUpdateInterfaceRequest{ListenPort: new(51830)})

	want := []wgQuickCall{{action: "down", name: "wgtest"}, {action: "up", name: "wgtest"}}
	if !slices.Equal(env.wgQuick, want) {
		t.Fatalf("wg-quick calls = %v, want %v", env.wgQuick, want)
	}
	if !slices.Equal(env.wgQuickPorts, []int{51820, 51830}) {
		t.Fatalf("ports seen = %v: down must run on the old config, up on the new", env.wgQuickPorts)
	}
	cfg, _ := ParseWireGuardConfig(configPath("wgtest"))
	if cfg.ListenPort != 51830 || len(cfg.Peers) != 1 {
		t.Fatalf("config = port %d peers %d", cfg.ListenPort, len(cfg.Peers))
	}
	if got := peerEndpoints(t, first, second); !slices.Equal(got, []string{"203.0.113.10:51830", "203.0.113.10:51830"}) {
		t.Fatalf("endpoints = %v", got)
	}
}

func TestUpdateInterfaceHooksChangeRestarts(t *testing.T) {
	env, _, _ := setupTwoPeers(t)
	env.wgQuick = nil

	updateInterface(t, apischema.WireGuardUpdateInterfaceRequest{PreUp: []string{" echo a ", "", "echo b"}, PostUp: []string{}})

	if len(env.wgQuick) != 2 || env.wgQuick[0].action != "down" || env.wgQuick[1].action != "up" {
		t.Fatalf("wg-quick calls = %v", env.wgQuick)
	}
	cfg, _ := ParseWireGuardConfig(configPath("wgtest"))
	if !slices.Equal(cfg.PreUp, []string{"echo a", "echo b"}) || len(cfg.PostUp) != 0 || len(cfg.PostDown) == 0 {
		t.Fatalf("hooks = %+v", cfg)
	}
}

func TestUpdateInterfaceHostOnlyRewritesEndpointsWithoutRestart(t *testing.T) {
	env, first, second := setupTwoPeers(t)
	env.wgQuick = nil

	updateInterface(t, apischema.WireGuardUpdateInterfaceRequest{Host: new("vpn.example.org")})

	if len(env.wgQuick) != 0 {
		t.Fatalf("host change must not restart: %v", env.wgQuick)
	}
	cfg, _ := ParseWireGuardConfig(configPath("wgtest"))
	if cfg.Host != "vpn.example.org" {
		t.Fatalf("host = %q", cfg.Host)
	}
	if got := peerEndpoints(t, first, second); !slices.Equal(got, []string{"vpn.example.org:51820", "vpn.example.org:51820"}) {
		t.Fatalf("endpoints = %v", got)
	}
	second2, _ := readPeerFile(peerConfigPath("wgtest", second))
	if second2.Enabled || second2.Name != "Bob" || !slices.Equal(second2.ServerAllowedIPs, []string{"172.16.0.0/24"}) {
		t.Fatalf("peer metadata must survive the rewrite: %+v", second2)
	}

	updateInterface(t, apischema.WireGuardUpdateInterfaceRequest{Host: new("")})
	cfg, _ = ParseWireGuardConfig(configPath("wgtest"))
	if cfg.Host != "" {
		t.Fatalf("host not cleared: %q", cfg.Host)
	}
	if got := peerEndpoints(t, first); !slices.Equal(got, []string{"203.0.113.10:51820"}) {
		t.Fatalf("endpoints after clearing host = %v", got)
	}
}

func TestUpdateInterfaceDNSChangeRewritesInheritedPeersOnly(t *testing.T) {
	env, first, second := setupTwoPeers(t)
	updatePeer(t, apischema.WireGuardUpdatePeerRequest{PeerID: second, DNS: []string{"8.8.8.8"}})
	env.wgQuick = nil

	updateInterface(t, apischema.WireGuardUpdateInterfaceRequest{DNS: []string{"1.1.1.1"}})

	if len(env.wgQuick) != 0 {
		t.Fatalf("dns change must not restart: %v", env.wgQuick)
	}
	a, _ := readPeerFile(peerConfigPath("wgtest", first))
	b, _ := readPeerFile(peerConfigPath("wgtest", second))
	if !slices.Equal(a.DNS, []string{"1.1.1.1"}) || !slices.Equal(b.DNS, []string{"8.8.8.8"}) {
		t.Fatalf("dns = %v / %v", a.DNS, b.DNS)
	}

	updateInterface(t, apischema.WireGuardUpdateInterfaceRequest{DNS: []string{}})
	a, _ = readPeerFile(peerConfigPath("wgtest", first))
	if !slices.Equal(a.DNS, []string{"192.0.2.1"}) {
		t.Fatalf("clearing dns must fall back to the gateway: %v", a.DNS)
	}
}

func TestUpdateInterfaceKeepsEndpointHostWhenNoneKnown(t *testing.T) {
	env, first, _ := setupTwoPeers(t)
	env.publicIP = ""

	updateInterface(t, apischema.WireGuardUpdateInterfaceRequest{ListenPort: new(51831)})

	if got := peerEndpoints(t, first); !slices.Equal(got, []string{"203.0.113.10:51831"}) {
		t.Fatalf("endpoints = %v, want the old host with the new port", got)
	}
}

func TestUpdateInterfaceWhileDownWritesWithoutWGQuick(t *testing.T) {
	env, _, _ := setupTwoPeers(t)
	env.interfaceUp = false
	env.wgQuick = nil

	updateInterface(t, apischema.WireGuardUpdateInterfaceRequest{ListenPort: new(51832), MTU: new(1400)})

	if len(env.wgQuick) != 0 {
		t.Fatalf("wg-quick calls = %v", env.wgQuick)
	}
	cfg, _ := ParseWireGuardConfig(configPath("wgtest"))
	if cfg.ListenPort != 51832 || cfg.MTU != 1400 {
		t.Fatalf("config = %+v", cfg)
	}
}

func TestUpdateInterfaceRejectsInvalidValues(t *testing.T) {
	env, _, _ := setupTwoPeers(t)
	other := writeTestInterface(t, "wgother", "10.9.0.1/29")
	other.ListenPort = 51821
	if err := WriteWireGuardConfig(configPath("wgother"), other); err != nil {
		t.Fatal(err)
	}
	env.wgQuick = nil

	cases := map[string]apischema.WireGuardUpdateInterfaceRequest{
		"host with space":   {Host: new("vpn example")},
		"host with slash":   {Host: new("vpn.example.org/path")},
		"host bad ipv6":     {Host: new("not:an:ip")},
		"port zero":         {ListenPort: new(0)},
		"port too large":    {ListenPort: new(70000)},
		"port in use":       {ListenPort: new(51821)},
		"mtu too small":     {MTU: new(1000)},
		"dns with space":    {DNS: []string{"1.1.1.1 9.9.9.9"}},
		"unknown interface": {Name: "wgnope", MTU: new(1400)},
		"bad name":          {Name: "../x", MTU: new(1400)},
	}
	for name, req := range cases {
		if req.Name == "" {
			req.Name = "wgtest"
		}
		if err := UpdateInterface(context.Background(), req); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	cfg, _ := ParseWireGuardConfig(configPath("wgtest"))
	if cfg.ListenPort != 51820 || cfg.MTU != 0 || cfg.Host != "" {
		t.Fatalf("rejected updates must not touch the config: %+v", cfg)
	}
	if len(env.wgQuick) != 0 {
		t.Fatalf("rejected updates must not restart: %v", env.wgQuick)
	}
}

func TestAddPeerUsesHostOverrideForEndpoint(t *testing.T) {
	newWireGuardTestEnv(t)
	writeTestInterface(t, "wgtest", "10.8.0.1/29")
	setTestInterfaceHeader(t, "wgtest", "vpn.example.org", nil)

	id, err := AddPeer(context.Background(), apischema.WireGuardAddPeerRequest{InterfaceName: "wgtest"})
	if err != nil {
		t.Fatalf("AddPeer returned error: %v", err)
	}
	if got := peerEndpoints(t, id); !slices.Equal(got, []string{"vpn.example.org:51820"}) {
		t.Fatalf("endpoint = %v", got)
	}
}
