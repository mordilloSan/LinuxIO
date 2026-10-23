package wireguard

import (
	"context"
	"slices"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

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
