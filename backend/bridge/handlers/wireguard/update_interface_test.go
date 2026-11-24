package wireguard

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
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

	updateInterface(t, apischema.WireGuardUpdateInterfaceRequest{Hooks: apischema.WireGuardHooks{
		apischema.WireGuardPreUp:  {" echo a ", "", "echo b"},
		apischema.WireGuardPostUp: {},
	}})

	if len(env.wgQuick) != 2 || env.wgQuick[0].action != "down" || env.wgQuick[1].action != "up" {
		t.Fatalf("wg-quick calls = %v", env.wgQuick)
	}
	cfg, _ := ParseWireGuardConfig(configPath("wgtest"))
	if !slices.Equal(cfg.Hooks[apischema.WireGuardPreUp], []string{"echo a", "echo b"}) || len(cfg.Hooks[apischema.WireGuardPostUp]) != 0 || len(cfg.Hooks[apischema.WireGuardPostDown]) == 0 {
		t.Fatalf("hooks = %v", cfg.Hooks)
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

func TestCleanLinesRejectsHooksTheConfigCannotCarry(t *testing.T) {
	for _, bad := range []string{"echo ok # not ok", "echo `date`", "echo a\necho b"} {
		if _, err := cleanLines([]string{bad}); err == nil {
			t.Errorf("cleanLines(%q) = nil error, want rejection", bad)
		}
	}
	got, err := cleanLines([]string{" a; b ", ""})
	if err != nil || !slices.Equal(got, []string{"a; b"}) {
		t.Fatalf("cleanLines = %v, %v", got, err)
	}
}

func readTestPeer(t *testing.T, id string) peerFile {
	t.Helper()
	peer, err := readPeerFile(peerConfigPath("wgtest", id))
	if err != nil {
		t.Fatalf("readPeerFile(%s) returned error: %v", id, err)
	}
	return peer
}

func TestUpdateInterfaceAddressRenumbersPeers(t *testing.T) {
	env, first, second := setupTwoPeers(t)
	setTestInterfaceHeader(t, "wgtest", "", []string{"10.8.0.1"})
	// A split tunnel to the old subnet and a DNS server on the tunnel move
	// with it; an address elsewhere stays.
	updatePeer(t, apischema.WireGuardUpdatePeerRequest{PeerID: first, ClientAllowedIPs: []string{"10.8.0.0/29", "10.8.0.1/32", "192.168.1.0/24"}, DNS: []string{"10.8.0.1", "9.9.9.9"}})
	env.wgQuick = nil

	updateInterface(t, apischema.WireGuardUpdateInterfaceRequest{Address: new("10.9.0.0/29")})

	if want := []wgQuickCall{{action: "down", name: "wgtest"}, {action: "up", name: "wgtest"}}; !slices.Equal(env.wgQuick, want) {
		t.Fatalf("wg-quick calls = %v, want %v", env.wgQuick, want)
	}
	cfg, _ := ParseWireGuardConfig(configPath("wgtest"))
	if !slices.Equal(cfg.Address, []string{"10.9.0.1/29"}) || !slices.Equal(cfg.DNS, []string{"10.9.0.1"}) {
		t.Fatalf("address = %v, dns = %v", cfg.Address, cfg.DNS)
	}
	assertNATHooks(t, cfg, "eth0", "10.9.0.1/29")
	if _, source, _ := natRules(cfg); source != "10.9.0.1/29" || len(cfg.Hooks[apischema.WireGuardPostUp]) != 3 {
		t.Fatalf("PostUp = %v: the NAT lines must be rewritten, not added", cfg.Hooks[apischema.WireGuardPostUp])
	}

	a, b := readTestPeer(t, first), readTestPeer(t, second)
	if a.Address != "10.9.0.2/32" || !slices.Equal(a.ClientAllowedIPs, []string{"10.9.0.0/29", "10.9.0.1/32", "192.168.1.0/24"}) || !slices.Equal(a.DNS, []string{"10.9.0.1", "9.9.9.9"}) {
		t.Fatalf("first peer = %+v", a)
	}
	if b.Address != "10.9.0.3/32" || b.Enabled || !slices.Equal(b.ServerAllowedIPs, []string{"172.16.0.0/24"}) {
		t.Fatalf("second peer = %+v", b)
	}
	// Only the enabled peer is in the server config, at its new address.
	if len(cfg.Peers) != 1 || !slices.Equal(cfg.Peers[0].AllowedIPs, []string{"10.9.0.2/32"}) {
		t.Fatalf("server peers = %+v", cfg.Peers)
	}
}

func TestUpdateInterfaceAddressGivesAPeerOutsideTheSubnetTheFirstFreeHost(t *testing.T) {
	_, first, second := setupTwoPeers(t)
	b := readTestPeer(t, second)
	b.Address = "10.8.0.9/32" // past the /29, as a hand edit could leave it
	if err := writePeerFile(peerConfigPath("wgtest", second), b); err != nil {
		t.Fatal(err)
	}

	updateInterface(t, apischema.WireGuardUpdateInterfaceRequest{Address: new("10.9.0.1/29")})

	if a, b := readTestPeer(t, first), readTestPeer(t, second); a.Address != "10.9.0.2/32" || b.Address != "10.9.0.3/32" {
		t.Fatalf("addresses = %s, %s", a.Address, b.Address)
	}
}

func TestUpdateInterfaceAddressRefusesASubnetWithoutRoom(t *testing.T) {
	env, first, _ := setupTwoPeers(t)
	env.wgQuick = nil

	// A /30 holds the server and one peer.
	err := UpdateInterface(context.Background(), apischema.WireGuardUpdateInterfaceRequest{Name: "wgtest", Address: new("10.9.0.1/30")})
	if !errors.Is(err, bridgeipc.ErrConflict) {
		t.Fatalf("error = %v, want a conflict", err)
	}
	cfg, _ := ParseWireGuardConfig(configPath("wgtest"))
	if !slices.Equal(cfg.Address, []string{"10.8.0.1/29"}) || readTestPeer(t, first).Address != "10.8.0.2/32" || len(env.wgQuick) != 0 {
		t.Fatalf("a refused move changed something: address %v, wg-quick %v", cfg.Address, env.wgQuick)
	}
}

func TestUpdateInterfaceEgressNicMovesTheNATLines(t *testing.T) {
	env, first, _ := setupTwoPeers(t)
	env.wgQuick = nil
	before := readTestPeer(t, first)

	updateInterface(t, apischema.WireGuardUpdateInterfaceRequest{EgressNic: new("br-lan")})

	if len(env.wgQuick) != 2 || env.wgQuick[0].action != "down" || env.wgQuick[1].action != "up" {
		t.Fatalf("wg-quick calls = %v", env.wgQuick)
	}
	cfg, _ := ParseWireGuardConfig(configPath("wgtest"))
	if !slices.Equal(cfg.Hooks[apischema.WireGuardPostUp], natPostUpHooks("br-lan", "10.8.0.1/29")) || !slices.Equal(cfg.Hooks[apischema.WireGuardPostDown], natPostDownHooks("br-lan", "10.8.0.1/29")) {
		t.Fatalf("hooks = %v", cfg.Hooks)
	}
	if after := readTestPeer(t, first); after.Address != before.Address || !slices.Equal(after.DNS, before.DNS) {
		t.Fatalf("a card change must leave the clients alone: %+v", after)
	}
}

func TestUpdateInterfaceEgressNicNeedsLinuxIOsNATLines(t *testing.T) {
	setupTwoPeers(t)
	updateInterface(t, apischema.WireGuardUpdateInterfaceRequest{Hooks: apischema.WireGuardHooks{
		apischema.WireGuardPostUp:   {"echo up"},
		apischema.WireGuardPostDown: {},
	}})

	err := UpdateInterface(context.Background(), apischema.WireGuardUpdateInterfaceRequest{Name: "wgtest", EgressNic: new("br-lan")})
	if !errors.Is(err, bridgeipc.ErrConflict) {
		t.Fatalf("error = %v, want a conflict", err)
	}
}

func TestUpdateInterfaceRejectsInvalidMoves(t *testing.T) {
	env, first, _ := setupTwoPeers(t)
	other := writeTestInterface(t, "wgother", "10.9.0.1/29")
	other.ListenPort = 51821
	if err := WriteWireGuardConfig(configPath("wgother"), other); err != nil {
		t.Fatal(err)
	}
	env.wgQuick = nil

	cases := map[string]apischema.WireGuardUpdateInterfaceRequest{
		"address without prefix":   {Address: new("10.10.0.1")},
		"address ipv6":             {Address: new("fd00::1/64")},
		"address subnet too large": {Address: new("10.10.0.1/16")},
		"address not first host":   {Address: new("10.10.0.5/29")},
		"address overlaps another": {Address: new("10.9.0.0/30")},
		"card with a quote":        {EgressNic: new("eth0'")},
		"card too long":            {EgressNic: new("a-very-long-card-name")},
	}
	for name, req := range cases {
		req.Name = "wgtest"
		err := UpdateInterface(context.Background(), req)
		if !errors.Is(err, bridgeipc.ErrInvalidArgs) && !errors.Is(err, bridgeipc.ErrConflict) {
			t.Errorf("%s: error = %v, want invalid args or a conflict", name, err)
		}
	}
	cfg, _ := ParseWireGuardConfig(configPath("wgtest"))
	if !slices.Equal(cfg.Address, []string{"10.8.0.1/29"}) || readTestPeer(t, first).Address != "10.8.0.2/32" {
		t.Fatalf("rejected moves must not touch the config: %+v", cfg)
	}
	assertNATHooks(t, cfg, "eth0", "10.8.0.1/29")
	if len(env.wgQuick) != 0 {
		t.Fatalf("rejected moves must not restart: %v", env.wgQuick)
	}
}
