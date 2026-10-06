package wireguard

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func setupTwoPeers(t *testing.T) (*wireGuardTestEnv, string, string) {
	t.Helper()
	env, first := setupPeer(t)
	second, err := AddPeer(context.Background(), apischema.WireGuardAddPeerRequest{InterfaceName: "wgtest", Name: "Bob"})
	if err != nil {
		t.Fatalf("AddPeer returned error: %v", err)
	}
	updatePeer(t, apischema.WireGuardUpdatePeerRequest{PeerID: second, Enabled: new(false), ServerAllowedIPs: []string{"172.16.0.0/24"}})
	env.syncs = nil
	return env, first, second
}

func TestListPeersReportsMetadataAndDerivedKeys(t *testing.T) {
	_, first, second := setupTwoPeers(t)

	peers, err := ListPeers(context.Background(), apischema.InterfaceNameRequest{InterfaceName: "wgtest"})
	if err != nil {
		t.Fatalf("ListPeers returned error: %v", err)
	}
	if len(peers) != 2 {
		t.Fatalf("peer count = %d, want 2", len(peers))
	}
	byID := map[string]PeerInfo{}
	for _, peer := range peers {
		byID[peer.ID] = peer
	}

	alice := byID[first]
	aliceFile, _ := readPeerFile(peerConfigPath("wgtest", first))
	alicePub, _ := aliceFile.publicKey()
	if alice.Name != "Alice" || !alice.Enabled || alice.Address != "10.8.0.2/32" || alice.PublicKey != alicePub {
		t.Fatalf("alice = %+v", alice)
	}
	if !slices.Equal(alice.ClientAllowedIPs, fullTunnelAllowedIPs) || !slices.Equal(alice.DNS, []string{"192.0.2.1"}) || alice.PresharedKey == "" {
		t.Fatalf("alice client settings = %+v", alice)
	}

	bob := byID[second]
	if bob.Name != "Bob" || bob.Enabled || !slices.Equal(bob.ServerAllowedIPs, []string{"172.16.0.0/24"}) {
		t.Fatalf("bob = %+v", bob)
	}
	if bob.PublicKey == "" || bob.PublicKey == alice.PublicKey {
		t.Fatalf("bob public key must be derived even while disabled: %q", bob.PublicKey)
	}
}

func TestRemovePeerMatchesByPublicKeyAndDeletesFile(t *testing.T) {
	env, first, second := setupTwoPeers(t)
	updatePeer(t, apischema.WireGuardUpdatePeerRequest{PeerID: second, Enabled: new(true)})
	env.syncs = nil
	secondFile, _ := readPeerFile(peerConfigPath("wgtest", second))
	secondPub, _ := secondFile.publicKey()

	err := RemovePeer(context.Background(), apischema.WireGuardPeerRequest{InterfaceName: "wgtest", PeerID: first})
	if err != nil {
		t.Fatalf("RemovePeer returned error: %v", err)
	}

	cfg, err := ParseWireGuardConfig(configPath("wgtest"))
	if err != nil {
		t.Fatalf("ParseWireGuardConfig returned error: %v", err)
	}
	if len(cfg.Peers) != 1 || cfg.Peers[0].PublicKey != secondPub {
		t.Fatalf("server peers = %+v, want only %s", cfg.Peers, secondPub)
	}
	assertPathMissing(t, peerConfigPath("wgtest", first))
	assertPathExists(t, peerConfigPath("wgtest", second))
	if !slices.Equal(env.syncs, []string{"wgtest"}) {
		t.Fatalf("syncs = %v", env.syncs)
	}

	if err := RemovePeer(context.Background(), apischema.WireGuardPeerRequest{InterfaceName: "wgtest", PeerID: first}); err == nil {
		t.Fatal("removing a missing peer must fail")
	}
}

func TestPeerConfigDownloadStripsMetadataAndNamesFile(t *testing.T) {
	_, first, _ := setupTwoPeers(t)
	updatePeer(t, apischema.WireGuardUpdatePeerRequest{PeerID: first, Name: new("Alice's phone/2")})

	download, err := PeerConfigDownload(context.Background(), apischema.WireGuardPeerRequest{InterfaceName: "wgtest", PeerID: first})
	if err != nil {
		t.Fatalf("PeerConfigDownload returned error: %v", err)
	}
	if strings.Contains(download.Content, "linuxio-") || !strings.HasPrefix(download.Content, "[Interface]") {
		t.Fatalf("download content not stripped:\n%s", download.Content)
	}
	if download.Filename != "Alice_s phone_2.conf" {
		t.Fatalf("filename = %q", download.Filename)
	}
}

func TestPeerQRCodeReturnsPNGDataURI(t *testing.T) {
	_, first, _ := setupTwoPeers(t)
	qr, err := PeerQRCode(context.Background(), apischema.WireGuardPeerRequest{InterfaceName: "wgtest", PeerID: first})
	if err != nil {
		t.Fatalf("PeerQRCode returned error: %v", err)
	}
	if !strings.HasPrefix(qr.QRCode, "data:image/png;base64,") {
		t.Fatalf("qr = %q", qr.QRCode[:40])
	}
}

func TestPeerExportFilenameFallsBackToID(t *testing.T) {
	if got := peerExportFilename("   ", "Peer2"); got != "Peer2.conf" {
		t.Fatalf("blank name = %q", got)
	}
	if got := peerExportFilename("Bob-laptop.home", "Peer3"); got != "Bob-laptop.home.conf" {
		t.Fatalf("safe name = %q", got)
	}
}
