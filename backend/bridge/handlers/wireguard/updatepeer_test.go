package wireguard

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

// setupPeer creates an up interface with one peer and clears the sync log so
// tests count only what the update triggers.
func setupPeer(t *testing.T) (*wireGuardTestEnv, string) {
	t.Helper()
	env := newWireGuardTestEnv(t)
	env.interfaceUp = true
	writeTestInterface(t, "wgtest", "10.8.0.1/29")
	id, err := AddPeer(context.Background(), apischema.WireGuardAddPeerRequest{InterfaceName: "wgtest", Name: "Alice"})
	if err != nil {
		t.Fatalf("AddPeer returned error: %v", err)
	}
	env.syncs = nil
	return env, id
}

func updatePeer(t *testing.T, req apischema.WireGuardUpdatePeerRequest) {
	t.Helper()
	req.InterfaceName = "wgtest"
	if err := UpdatePeer(context.Background(), req); err != nil {
		t.Fatalf("UpdatePeer returned error: %v", err)
	}
}

func loadPeerAndServer(t *testing.T, id string) (peerFile, WireGuardConfig) {
	t.Helper()
	peer, err := readPeerFile(peerConfigPath("wgtest", id))
	if err != nil {
		t.Fatalf("readPeerFile returned error: %v", err)
	}
	cfg, err := ParseWireGuardConfig(configPath("wgtest"))
	if err != nil {
		t.Fatalf("ParseWireGuardConfig returned error: %v", err)
	}
	return peer, cfg
}

func TestUpdatePeerRenamesFileAndServerComment(t *testing.T) {
	env, id := setupPeer(t)

	updatePeer(t, apischema.WireGuardUpdatePeerRequest{PeerID: id, Name: new("Alice's phone")})

	peer, _ := loadPeerAndServer(t, id)
	if peer.Name != "Alice's phone" || peer.ID != id {
		t.Fatalf("peer = %+v", peer)
	}
	raw, _ := os.ReadFile(configPath("wgtest"))
	if !strings.Contains(string(raw), "# Alice's phone\n[Peer]") || strings.Contains(string(raw), "# Alice\n") {
		t.Fatalf("server comment not updated:\n%s", raw)
	}
	if !slices.Equal(env.syncs, []string{"wgtest"}) {
		t.Fatalf("syncs = %v", env.syncs)
	}
}

func TestUpdatePeerBlankNameResetsToID(t *testing.T) {
	_, id := setupPeer(t)
	updatePeer(t, apischema.WireGuardUpdatePeerRequest{PeerID: id, Name: new("  ")})
	peer, _ := loadPeerAndServer(t, id)
	if peer.Name != id {
		t.Fatalf("name = %q, want %q", peer.Name, id)
	}
}

func TestUpdatePeerDisableRemovesServerSectionAndEnableRestoresIt(t *testing.T) {
	env, id := setupPeer(t)
	updatePeer(t, apischema.WireGuardUpdatePeerRequest{PeerID: id, ServerAllowedIPs: []string{"192.168.50.0/24"}})
	before, _ := loadPeerAndServer(t, id)

	updatePeer(t, apischema.WireGuardUpdatePeerRequest{PeerID: id, Enabled: new(false)})
	peer, cfg := loadPeerAndServer(t, id)
	if peer.Enabled || len(cfg.Peers) != 0 {
		t.Fatalf("after disable: enabled=%v server peers=%+v", peer.Enabled, cfg.Peers)
	}
	if peer.PrivateKey != before.PrivateKey || peer.PresharedKey != before.PresharedKey || !slices.Equal(peer.ServerAllowedIPs, []string{"192.168.50.0/24"}) {
		t.Fatalf("disable must keep the peer file intact: %+v", peer)
	}

	updatePeer(t, apischema.WireGuardUpdatePeerRequest{PeerID: id, Enabled: new(true)})
	peer, cfg = loadPeerAndServer(t, id)
	pub, _ := peer.publicKey()
	if !peer.Enabled || len(cfg.Peers) != 1 || cfg.Peers[0].PublicKey != pub {
		t.Fatalf("after enable: enabled=%v server peers=%+v", peer.Enabled, cfg.Peers)
	}
	if !slices.Equal(cfg.Peers[0].AllowedIPs, []string{"10.8.0.2/32", "192.168.50.0/24"}) {
		t.Fatalf("server allowed ips after enable = %v", cfg.Peers[0].AllowedIPs)
	}
	if !slices.Equal(env.syncs, []string{"wgtest", "wgtest", "wgtest"}) {
		t.Fatalf("syncs = %v", env.syncs)
	}
}

func TestUpdatePeerClientSettingsLandInPeerFile(t *testing.T) {
	_, id := setupPeer(t)

	updatePeer(t, apischema.WireGuardUpdatePeerRequest{
		PeerID:              id,
		ClientAllowedIPs:    []string{"10.8.0.0/29", "192.168.1.0/24"},
		DNS:                 []string{"9.9.9.9"},
		MTU:                 new(1280),
		PersistentKeepalive: new(15),
	})

	peer, cfg := loadPeerAndServer(t, id)
	if !slices.Equal(peer.ClientAllowedIPs, []string{"10.8.0.0/29", "192.168.1.0/24"}) || !slices.Equal(peer.DNS, []string{"9.9.9.9"}) || peer.MTU != 1280 || peer.PersistentKeepalive != 15 {
		t.Fatalf("peer = %+v", peer)
	}
	if cfg.Peers[0].PersistentKeepalive != 15 {
		t.Fatalf("server keepalive = %d", cfg.Peers[0].PersistentKeepalive)
	}
	if !slices.Equal(cfg.Peers[0].AllowedIPs, []string{"10.8.0.2/32"}) {
		t.Fatalf("client routes must not leak into the server section: %v", cfg.Peers[0].AllowedIPs)
	}
}

func TestUpdatePeerEmptyListsResetToDefaults(t *testing.T) {
	_, id := setupPeer(t)
	setTestInterfaceHeader(t, "wgtest", "", []string{"1.1.1.1"})
	updatePeer(t, apischema.WireGuardUpdatePeerRequest{
		PeerID:           id,
		ClientAllowedIPs: []string{"10.8.0.0/29"},
		ServerAllowedIPs: []string{"172.16.0.0/24"},
		DNS:              []string{"9.9.9.9"},
		MTU:              new(1400),
	})

	updatePeer(t, apischema.WireGuardUpdatePeerRequest{
		PeerID:           id,
		ClientAllowedIPs: []string{},
		ServerAllowedIPs: []string{},
		DNS:              []string{},
		MTU:              new(0),
	})

	peer, cfg := loadPeerAndServer(t, id)
	if !slices.Equal(peer.ClientAllowedIPs, fullTunnelAllowedIPs) {
		t.Fatalf("client allowed ips = %v, want full tunnel", peer.ClientAllowedIPs)
	}
	if len(peer.ServerAllowedIPs) != 0 || !slices.Equal(cfg.Peers[0].AllowedIPs, []string{"10.8.0.2/32"}) {
		t.Fatalf("extras not cleared: peer=%v server=%v", peer.ServerAllowedIPs, cfg.Peers[0].AllowedIPs)
	}
	if !slices.Equal(peer.DNS, []string{"1.1.1.1"}) {
		t.Fatalf("dns = %v, want interface default", peer.DNS)
	}
	if peer.MTU != 0 {
		t.Fatalf("mtu = %d, want unset", peer.MTU)
	}
}

func TestUpdatePeerPresharedKeyGenerateAndRemove(t *testing.T) {
	_, id := setupPeer(t)
	original, _ := loadPeerAndServer(t, id)

	updatePeer(t, apischema.WireGuardUpdatePeerRequest{PeerID: id, PresharedKey: "remove"})
	peer, cfg := loadPeerAndServer(t, id)
	if peer.PresharedKey != "" || cfg.Peers[0].PresharedKey != "" {
		t.Fatalf("psk not removed: file=%q server=%q", peer.PresharedKey, cfg.Peers[0].PresharedKey)
	}

	updatePeer(t, apischema.WireGuardUpdatePeerRequest{PeerID: id, PresharedKey: "generate"})
	peer, cfg = loadPeerAndServer(t, id)
	if peer.PresharedKey == "" || peer.PresharedKey == original.PresharedKey || cfg.Peers[0].PresharedKey != peer.PresharedKey {
		t.Fatalf("psk not regenerated: file=%q server=%q", peer.PresharedKey, cfg.Peers[0].PresharedKey)
	}
}

func TestUpdatePeerRegenerateKeysReplacesServerSection(t *testing.T) {
	_, id := setupPeer(t)
	original, _ := loadPeerAndServer(t, id)
	oldPub, _ := original.publicKey()

	updatePeer(t, apischema.WireGuardUpdatePeerRequest{PeerID: id, RegenerateKeys: true})

	peer, cfg := loadPeerAndServer(t, id)
	newPub, _ := peer.publicKey()
	if peer.PrivateKey == original.PrivateKey || newPub == oldPub {
		t.Fatal("keys were not regenerated")
	}
	if len(cfg.Peers) != 1 || cfg.Peers[0].PublicKey != newPub {
		t.Fatalf("server peers = %+v, want only %s", cfg.Peers, newPub)
	}
	if peer.Address != original.Address || peer.PresharedKey != original.PresharedKey {
		t.Fatalf("regeneration must keep address and psk: %+v", peer)
	}
}

func TestUpdatePeerRejectsInvalidValues(t *testing.T) {
	_, id := setupPeer(t)
	cases := map[string]apischema.WireGuardUpdatePeerRequest{
		"bad client cidr": {PeerID: id, ClientAllowedIPs: []string{"10.0.0.1"}},
		"bad server cidr": {PeerID: id, ServerAllowedIPs: []string{"nope/24"}},
		"dns with space":  {PeerID: id, DNS: []string{"1.1.1.1 9.9.9.9"}},
		"mtu too small":   {PeerID: id, MTU: new(1000)},
		"keepalive large": {PeerID: id, PersistentKeepalive: new(70000)},
		"bad psk action":  {PeerID: id, PresharedKey: "rotate"},
		"multi-line name": {PeerID: id, Name: new("a\nb")},
		"unknown peer":    {PeerID: "Peer9"},
		"bad peer id":     {PeerID: "../x"},
	}
	for name, req := range cases {
		req.InterfaceName = "wgtest"
		if err := UpdatePeer(context.Background(), req); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	peer, _ := loadPeerAndServer(t, id)
	if peer.Name != "Alice" || peer.MTU != 0 {
		t.Fatalf("rejected updates must not touch the file: %+v", peer)
	}
}

func TestUpdatePeerSkipsSyncWhenInterfaceDown(t *testing.T) {
	env, id := setupPeer(t)
	env.interfaceUp = false
	updatePeer(t, apischema.WireGuardUpdatePeerRequest{PeerID: id, Name: new("Bob")})
	if len(env.syncs) != 0 {
		t.Fatalf("syncs = %v, want none while down", env.syncs)
	}
}
