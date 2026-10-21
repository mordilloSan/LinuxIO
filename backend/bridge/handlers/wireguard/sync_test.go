package wireguard

import (
	"context"
	"testing"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

func TestSyncDeviceConfigAppliesFilePeersAndRemovesStale(t *testing.T) {
	serverKey, _ := wgtypes.GeneratePrivateKey()
	keep, _ := wgtypes.GeneratePrivateKey()
	stale, _ := wgtypes.GeneratePrivateKey()
	psk, _ := wgtypes.GenerateKey()

	cfg := WireGuardConfig{
		PrivateKey: serverKey.String(),
		ListenPort: 51820,
		Peers: []PeerConfig{{
			PublicKey:           keep.PublicKey().String(),
			PresharedKey:        psk.String(),
			AllowedIPs:          []string{"10.7.0.2/32", "10.7.0.3", "fd00::2"},
			Endpoint:            "192.0.2.10:51821",
			PersistentKeepalive: 25,
		}},
	}
	current := []wgtypes.Peer{{PublicKey: keep.PublicKey()}, {PublicKey: stale.PublicKey()}}

	got, err := syncDeviceConfig(context.Background(), cfg, current)
	if err != nil {
		t.Fatalf("syncDeviceConfig: %v", err)
	}
	if got.ReplacePeers || *got.PrivateKey != serverKey || *got.ListenPort != 51820 {
		t.Fatalf("interface config = %+v", got)
	}
	if len(got.Peers) != 2 {
		t.Fatalf("peers = %d, want 2", len(got.Peers))
	}
	peer := got.Peers[0]
	if peer.PublicKey != keep.PublicKey() || *peer.PresharedKey != psk || !peer.ReplaceAllowedIPs || peer.Remove {
		t.Fatalf("peer = %+v", peer)
	}
	if peer.Endpoint.String() != "192.0.2.10:51821" || *peer.PersistentKeepaliveInterval != 25*time.Second {
		t.Fatalf("endpoint/keepalive = %v %v", peer.Endpoint, peer.PersistentKeepaliveInterval)
	}
	if len(peer.AllowedIPs) != 3 || peer.AllowedIPs[0].String() != "10.7.0.2/32" || peer.AllowedIPs[1].String() != "10.7.0.3/32" || peer.AllowedIPs[2].String() != "fd00::2/128" {
		t.Fatalf("allowed IPs = %v", peer.AllowedIPs)
	}
	if removed := got.Peers[1]; removed.PublicKey != stale.PublicKey() || !removed.Remove {
		t.Fatalf("stale peer = %+v", removed)
	}
}

func TestSyncDeviceConfigRejectsBadAllowedIP(t *testing.T) {
	serverKey, _ := wgtypes.GeneratePrivateKey()
	peerKey, _ := wgtypes.GeneratePrivateKey()
	cfg := WireGuardConfig{
		PrivateKey: serverKey.String(),
		Peers:      []PeerConfig{{PublicKey: peerKey.PublicKey().String(), AllowedIPs: []string{"not-an-ip"}}},
	}
	if _, err := syncDeviceConfig(context.Background(), cfg, nil); err == nil {
		t.Fatal("syncDeviceConfig accepted an invalid allowed IP")
	}
}
