package wireguard

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func writeTestInterface(t *testing.T, name, address string) WireGuardConfig {
	t.Helper()
	privateKey, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatalf("GeneratePrivateKey returned error: %v", err)
	}
	cfg := WireGuardConfig{PrivateKey: privateKey.String(), Address: []string{address}, ListenPort: 51820}
	addNATHooks(&cfg, "eth0", address)
	if err := WriteWireGuardConfig(configPath(name), cfg); err != nil {
		t.Fatalf("WriteWireGuardConfig returned error: %v", err)
	}
	return cfg
}

// setTestInterfaceHeader rewrites the interface config with the given host and
// client DNS in its LinuxIO header.
func setTestInterfaceHeader(t *testing.T, name, host string, dns []string) {
	t.Helper()
	cfg, err := ParseWireGuardConfig(configPath(name))
	if err != nil {
		t.Fatalf("ParseWireGuardConfig returned error: %v", err)
	}
	cfg.Host = host
	cfg.DNS = dns
	if err := WriteWireGuardConfig(configPath(name), cfg); err != nil {
		t.Fatalf("WriteWireGuardConfig returned error: %v", err)
	}
}

func TestAddPeerWritesNamedPeerFileWithPresharedKey(t *testing.T) {
	env := newWireGuardTestEnv(t)
	env.interfaceUp = true
	writeTestInterface(t, "wgtest", "10.8.0.1/29")

	id, err := AddPeer(context.Background(), apischema.WireGuardAddPeerRequest{InterfaceName: "wgtest", Name: "Alice's phone"})
	if err != nil {
		t.Fatalf("AddPeer returned error: %v", err)
	}
	if id != "Peer2" {
		t.Fatalf("peer id = %q, want Peer2", id)
	}

	peer, err := readPeerFile(peerConfigPath("wgtest", id))
	if err != nil {
		t.Fatalf("readPeerFile returned error: %v", err)
	}
	if peer.Name != "Alice's phone" || !peer.Enabled || peer.Address != "10.8.0.2/32" {
		t.Fatalf("peer file = %+v", peer)
	}
	if peer.PresharedKey == "" {
		t.Fatal("new peers must get a preshared key")
	}
	if !slices.Equal(peer.ClientAllowedIPs, []string{"0.0.0.0/0", "::/0"}) {
		t.Fatalf("client allowed ips = %v, want full tunnel", peer.ClientAllowedIPs)
	}
	if peer.Endpoint != "203.0.113.10:51820" || peer.PersistentKeepalive != defaultKeepalive {
		t.Fatalf("peer file = %+v", peer)
	}
	if !slices.Equal(peer.DNS, []string{"192.0.2.1"}) {
		t.Fatalf("dns = %v, want the gateway fallback", peer.DNS)
	}

	cfg, err := ParseWireGuardConfig(configPath("wgtest"))
	if err != nil {
		t.Fatalf("ParseWireGuardConfig returned error: %v", err)
	}
	pub, _ := peer.publicKey()
	if len(cfg.Peers) != 1 || cfg.Peers[0].PublicKey != pub || cfg.Peers[0].PresharedKey != peer.PresharedKey {
		t.Fatalf("server peers = %+v", cfg.Peers)
	}
	if !slices.Equal(cfg.Peers[0].AllowedIPs, []string{"10.8.0.2/32"}) {
		t.Fatalf("server allowed ips = %v", cfg.Peers[0].AllowedIPs)
	}
	raw, _ := os.ReadFile(configPath("wgtest"))
	if !strings.Contains(string(raw), "# Alice's phone\n[Peer]") {
		t.Fatalf("server config lacks the name comment:\n%s", raw)
	}
	if !slices.Equal(env.syncs, []string{"wgtest"}) {
		t.Fatalf("syncs = %v", env.syncs)
	}
}

func TestAddPeerWithoutNameUsesID(t *testing.T) {
	newWireGuardTestEnv(t)
	writeTestInterface(t, "wgtest", "10.8.0.1/29")

	id, err := AddPeer(context.Background(), apischema.WireGuardAddPeerRequest{InterfaceName: "wgtest"})
	if err != nil {
		t.Fatalf("AddPeer returned error: %v", err)
	}
	peer, err := readPeerFile(peerConfigPath("wgtest", id))
	if err != nil {
		t.Fatalf("readPeerFile returned error: %v", err)
	}
	if peer.Name != id {
		t.Fatalf("name = %q, want %q", peer.Name, id)
	}
}

func TestAddPeerPrefersInterfaceDNSOverGateway(t *testing.T) {
	newWireGuardTestEnv(t)
	writeTestInterface(t, "wgtest", "10.8.0.1/29")
	setTestInterfaceHeader(t, "wgtest", "", []string{"1.1.1.1"})

	id, err := AddPeer(context.Background(), apischema.WireGuardAddPeerRequest{InterfaceName: "wgtest"})
	if err != nil {
		t.Fatalf("AddPeer returned error: %v", err)
	}
	peer, err := readPeerFile(peerConfigPath("wgtest", id))
	if err != nil {
		t.Fatalf("readPeerFile returned error: %v", err)
	}
	if !slices.Equal(peer.DNS, []string{"1.1.1.1"}) {
		t.Fatalf("dns = %v", peer.DNS)
	}
}
