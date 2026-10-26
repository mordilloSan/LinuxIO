package wireguard

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

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

func TestAddAndRemovePeerLifecyclePreservesNATHooks(t *testing.T) {
	env := newWireGuardTestEnv(t)
	env.interfaceUp = true
	privateKey, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatalf("GeneratePrivateKey returned error: %v", err)
	}

	cfg := WireGuardConfig{
		PrivateKey: privateKey.String(),
		Address:    []string{"10.8.0.1/29"},
		ListenPort: 51820,
	}
	addNATHooks(&cfg, "eth0", "10.8.0.1/29")
	err = WriteWireGuardConfig(configPath("wgtest"), cfg)
	if err != nil {
		t.Fatalf("WriteWireGuardConfig returned error: %v", err)
	}

	peerName, err := AddPeer(context.Background(), apischema.WireGuardAddPeerRequest{InterfaceName: "wgtest"})
	if err != nil {
		t.Fatalf("AddPeer returned error: %v", err)
	}
	if peerName == "" {
		t.Fatal("AddPeer returned an empty peer id")
	}

	cfg, err = ParseWireGuardConfig(configPath("wgtest"))
	if err != nil {
		t.Fatalf("ParseWireGuardConfig returned error: %v", err)
	}
	if len(cfg.Peers) != 1 {
		t.Fatalf("peer count after add = %d, want 1", len(cfg.Peers))
	}
	assertNATHooks(t, cfg, "eth0", "10.8.0.1/29")
	peerPath := filepath.Join(peerDirPath("wgtest"), peerName+configExt)
	assertPathExists(t, peerPath)
	if !slices.Equal(env.syncs, []string{"wgtest"}) {
		t.Fatalf("syncs after add = %v, want [wgtest]", env.syncs)
	}

	err = RemovePeer(context.Background(), apischema.WireGuardPeerRequest{
		InterfaceName: "wgtest",
		PeerID:        peerName,
	})
	if err != nil {
		t.Fatalf("RemovePeer returned error: %v", err)
	}

	cfg, err = ParseWireGuardConfig(configPath("wgtest"))
	if err != nil {
		t.Fatalf("ParseWireGuardConfig after remove returned error: %v", err)
	}
	if len(cfg.Peers) != 0 {
		t.Fatalf("peer count after remove = %d, want 0", len(cfg.Peers))
	}
	assertNATHooks(t, cfg, "eth0", "10.8.0.1/29")
	assertPathMissing(t, peerPath)
	if !slices.Equal(env.syncs, []string{"wgtest", "wgtest"}) {
		t.Fatalf("syncs after remove = %v, want [wgtest wgtest]", env.syncs)
	}
}

func TestAddPeerKeepsDisabledPeerAddressAndFile(t *testing.T) {
	env := newWireGuardTestEnv(t)
	env.interfaceUp = true
	writeTestInterface(t, "wgtest", "10.8.0.1/29")
	ctx := context.Background()

	first, err := AddPeer(ctx, apischema.WireGuardAddPeerRequest{InterfaceName: "wgtest"})
	if err != nil {
		t.Fatalf("AddPeer returned error: %v", err)
	}
	firstFile, err := readPeerFile(peerConfigPath("wgtest", first))
	if err != nil {
		t.Fatalf("readPeerFile returned error: %v", err)
	}
	if updateErr := UpdatePeer(ctx, apischema.WireGuardUpdatePeerRequest{InterfaceName: "wgtest", PeerID: first, Enabled: new(false)}); updateErr != nil {
		t.Fatalf("UpdatePeer returned error: %v", updateErr)
	}

	second, err := AddPeer(ctx, apischema.WireGuardAddPeerRequest{InterfaceName: "wgtest"})
	if err != nil {
		t.Fatalf("AddPeer after disable returned error: %v", err)
	}
	if second == first {
		t.Fatalf("second peer reused id %q of the disabled peer", first)
	}
	secondFile, err := readPeerFile(peerConfigPath("wgtest", second))
	if err != nil {
		t.Fatalf("readPeerFile returned error: %v", err)
	}
	if secondFile.Address == firstFile.Address {
		t.Fatalf("second peer reused address %q of the disabled peer", firstFile.Address)
	}
	again, err := readPeerFile(peerConfigPath("wgtest", first))
	if err != nil || again.PrivateKey != firstFile.PrivateKey {
		t.Fatalf("disabled peer file changed: err=%v key_kept=%v", err, again.PrivateKey == firstFile.PrivateKey)
	}
}

func TestConcurrentAddPeerKeepsEveryPeer(t *testing.T) {
	env := newWireGuardTestEnv(t)
	env.interfaceUp = true
	writeTestInterface(t, "wgtest", "10.8.0.1/29")

	const count = 4
	ids := make([]string, count)
	errs := make([]error, count)
	var wg sync.WaitGroup
	for i := range count {
		wg.Go(func() {
			ids[i], errs[i] = AddPeer(context.Background(), apischema.WireGuardAddPeerRequest{InterfaceName: "wgtest"})
		})
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("AddPeer #%d returned error: %v", i, err)
		}
	}
	cfg, err := ParseWireGuardConfig(configPath("wgtest"))
	if err != nil {
		t.Fatalf("ParseWireGuardConfig returned error: %v", err)
	}
	if len(cfg.Peers) != count {
		t.Fatalf("server config has %d peers, want %d (ids %v)", len(cfg.Peers), count, ids)
	}
}

func TestExportPeerFileRefusesExistingFile(t *testing.T) {
	newWireGuardTestEnv(t)
	if err := os.MkdirAll(peerDirPath("wgtest"), 0o700); err != nil {
		t.Fatal(err)
	}
	path := peerConfigPath("wgtest", "Peer2")
	if err := os.WriteFile(path, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := exportPeerFile("wgtest", peerFile{ID: "Peer2"})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("error = %v, want already exists", err)
	}
	if raw, readErr := os.ReadFile(path); readErr != nil || string(raw) != "keep" {
		t.Fatalf("existing peer file = %q, %v; want it untouched", raw, readErr)
	}
}
