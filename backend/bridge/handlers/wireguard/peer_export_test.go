package wireguard

import (
	"context"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

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
