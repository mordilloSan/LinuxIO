package wireguard

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
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

func TestAddAndRemoveInterfaceLifecycle(t *testing.T) {
	env := newWireGuardTestEnv(t)
	dns := "1.1.1.1"
	numPeers := "1"

	_, err := AddInterface(context.Background(), apischema.WireGuardAddInterfaceRequest{
		Name:       "wgtest",
		Addresses:  "10.7.0.1/24",
		ListenPort: "51820",
		EgressNic:  "eth0",
		DNS:        &dns,
		NumPeers:   &numPeers,
	})
	if err != nil {
		t.Fatalf("AddInterface returned error: %v", err)
	}

	cfg, err := ParseWireGuardConfig(configPath("wgtest"))
	if err != nil {
		t.Fatalf("ParseWireGuardConfig returned error: %v", err)
	}
	assertNATHooks(t, cfg, "eth0", "10.7.0.1/24")
	if len(cfg.Peers) != 1 {
		t.Fatalf("peer count = %d, want 1", len(cfg.Peers))
	}
	assertPathExists(t, peerConfigPath("wgtest", "Peer2"))
	exported, err := readPeerFile(peerConfigPath("wgtest", "Peer2"))
	if err != nil {
		t.Fatalf("readPeerFile returned error: %v", err)
	}
	if exported.Name != "Peer2" || !exported.Enabled || exported.PresharedKey == "" || !slices.Equal(exported.DNS, []string{"1.1.1.1"}) {
		t.Fatalf("exported peer = %+v", exported)
	}
	if !slices.Equal(env.wgQuick, []wgQuickCall{{action: "up", name: "wgtest"}}) {
		t.Fatalf("wg-quick calls = %v", env.wgQuick)
	}

	_, err = RemoveInterface(context.Background(), apischema.NameRequest{Name: "wgtest"})
	if err != nil {
		t.Fatalf("RemoveInterface returned error: %v", err)
	}
	assertPathMissing(t, configPath("wgtest"))
	assertPathMissing(t, peerDirPath("wgtest"))
	wantWGQuick := []wgQuickCall{{action: "up", name: "wgtest"}, {action: "down", name: "wgtest"}}
	if !slices.Equal(env.wgQuick, wantWGQuick) {
		t.Fatalf("wg-quick calls = %v, want %v", env.wgQuick, wantWGQuick)
	}
}

func TestGeneratePeersAllocatesSequentialHostOffsets(t *testing.T) {
	peers, err := generatePeers("10.7.0.1/29", 3)
	if err != nil {
		t.Fatalf("generatePeers returned error: %v", err)
	}

	wantIPs := []string{"10.7.0.2/32", "10.7.0.3/32", "10.7.0.4/32"}
	wantNames := []string{"Peer2", "Peer3", "Peer4"}
	for i, peer := range peers {
		if peer.Name != wantNames[i] {
			t.Fatalf("peer %d name = %q, want %q", i, peer.Name, wantNames[i])
		}
		if len(peer.AllowedIPs) != 1 || peer.AllowedIPs[0] != wantIPs[i] {
			t.Fatalf("peer %d allowed IPs = %v, want [%s]", i, peer.AllowedIPs, wantIPs[i])
		}
		if peer.PersistentKeepalive != defaultKeepalive {
			t.Fatalf("peer %d keepalive = %d, want %d", i, peer.PersistentKeepalive, defaultKeepalive)
		}
		if peer.PrivateKey == "" || peer.PublicKey == "" {
			t.Fatalf("peer %d missing generated keys", i)
		}
	}
}

func TestGeneratePeersReturnsErrorWhenSubnetFull(t *testing.T) {
	_, err := generatePeers("10.7.0.1/30", 2)
	if err == nil {
		t.Fatal("generatePeers returned nil error")
	}
	if !strings.Contains(err.Error(), "allocate IP for peer 2") {
		t.Fatalf("error = %q, want peer allocation context", err)
	}
}

func TestGeneratePeersIncludePresharedKeys(t *testing.T) {
	peers, err := generatePeers("10.7.0.1/29", 2)
	if err != nil {
		t.Fatalf("generatePeers returned error: %v", err)
	}
	for _, peer := range peers {
		if peer.PresharedKey == "" {
			t.Fatalf("peer %s has no preshared key", peer.Name)
		}
	}
}

func TestAddInterfaceRejectsInvalidRequestsBeforeWriting(t *testing.T) {
	env := newWireGuardTestEnv(t)
	writeTestInterface(t, "wgother", "10.8.0.1/24")
	badMTU := "100"
	tests := []struct {
		name string
		req  apischema.WireGuardAddInterfaceRequest
	}{
		{"port zero", apischema.WireGuardAddInterfaceRequest{Addresses: "10.7.0.1/24", ListenPort: "0"}},
		{"negative port", apischema.WireGuardAddInterfaceRequest{Addresses: "10.7.0.1/24", ListenPort: "-1"}},
		{"port too large", apischema.WireGuardAddInterfaceRequest{Addresses: "10.7.0.1/24", ListenPort: "70000"}},
		{"port used by another interface", apischema.WireGuardAddInterfaceRequest{Addresses: "10.7.0.1/24", ListenPort: "51820"}},
		{"MTU below minimum", apischema.WireGuardAddInterfaceRequest{Addresses: "10.7.0.1/24", ListenPort: "51821", MTU: &badMTU}},
		{"address without prefix", apischema.WireGuardAddInterfaceRequest{Addresses: "10.7.0.1", ListenPort: "51821"}},
		{"garbage address", apischema.WireGuardAddInterfaceRequest{Addresses: "10.7.0.1/24,nope", ListenPort: "51821"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.req.Name = "wgtest"
			tc.req.EgressNic = "eth0"
			if _, err := AddInterface(context.Background(), tc.req); err == nil {
				t.Fatal("AddInterface returned nil error")
			}
			assertPathMissing(t, configPath("wgtest"))
			assertPathMissing(t, peerDirPath("wgtest"))
			if len(env.wgQuick) != 0 {
				t.Fatalf("wg-quick calls = %v, want none", env.wgQuick)
			}
		})
	}
}

func TestAddInterfaceRefusesExistingInterface(t *testing.T) {
	env := newWireGuardTestEnv(t)
	old := writeTestInterface(t, "wgtest", "10.7.0.1/24")

	_, err := AddInterface(context.Background(), apischema.WireGuardAddInterfaceRequest{
		Name:       "wgtest",
		Addresses:  "10.9.0.1/24",
		ListenPort: "51821",
		EgressNic:  "eth0",
	})
	if bridgeipc.StatusCode(err) != 409 {
		t.Fatalf("error = %v, want a 409 conflict", err)
	}
	cfg, err := ParseWireGuardConfig(configPath("wgtest"))
	if err != nil {
		t.Fatalf("ParseWireGuardConfig returned error: %v", err)
	}
	if cfg.PrivateKey != old.PrivateKey {
		t.Fatal("existing interface private key was replaced")
	}
	if len(env.wgQuick) != 0 {
		t.Fatalf("wg-quick calls = %v, want none", env.wgQuick)
	}
}

func TestAddInterfaceReturnsPeerExportError(t *testing.T) {
	newWireGuardTestEnv(t)
	numPeers := "1"
	if err := os.MkdirAll(peerDirPath("wgtest"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(peerConfigPath("wgtest", "Peer2"), []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := AddInterface(context.Background(), apischema.WireGuardAddInterfaceRequest{
		Name:       "wgtest",
		Addresses:  "10.7.0.1/24",
		ListenPort: "51820",
		EgressNic:  "eth0",
		NumPeers:   &numPeers,
	})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("error = %v, want the peer export failure", err)
	}
	assertPathMissing(t, configPath("wgtest"))
	assertPathMissing(t, peerDirPath("wgtest"))
}
