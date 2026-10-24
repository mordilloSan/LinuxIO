package wireguard

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"

	"github.com/skip2/go-qrcode"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

var unsafeFilenameChars = regexp.MustCompile(`[^A-Za-z0-9._ -]`)

// peerExportFilename names a downloaded client config after the peer's
// display name, falling back to its id.
func peerExportFilename(name, id string) string {
	base := strings.TrimSpace(unsafeFilenameChars.ReplaceAllString(strings.TrimSpace(name), "_"))
	if base == "" {
		base = id
	}
	return base + configExt
}

// readPeerExport returns the peer file and the client-facing config text:
// the file with LinuxIO metadata lines removed.
func readPeerExport(ctx context.Context, req apischema.WireGuardPeerRequest) (peerFile, string, error) {
	if req.InterfaceName == "" || req.PeerID == "" {
		return peerFile{}, "", fmt.Errorf("usage: <interface> <peerId>")
	}
	if err := validateInterfaceName(req.InterfaceName); err != nil {
		return peerFile{}, "", fmt.Errorf("invalid interface name: %w", err)
	}
	if err := validateInterfaceName(req.PeerID); err != nil {
		return peerFile{}, "", fmt.Errorf("invalid peer id: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return peerFile{}, "", err
	}

	peerPath := peerConfigPath(req.InterfaceName, req.PeerID)
	raw, err := os.ReadFile(peerPath)
	if err != nil {
		return peerFile{}, "", fmt.Errorf("read peer config: %w", err)
	}
	file, err := readPeerFile(peerPath)
	if err != nil {
		return peerFile{}, "", err
	}
	return file, stripPeerMetadata(string(raw)), nil
}

func PeerQRCode(ctx context.Context, req apischema.WireGuardPeerRequest) (apischema.QRCodeResponse, error) {
	_, content, err := readPeerExport(ctx, req)
	if err != nil {
		return apischema.QRCodeResponse{}, err
	}

	png, err := qrcode.Encode(content, qrcode.Medium, 256)
	if err != nil {
		return apischema.QRCodeResponse{}, fmt.Errorf("generate QR code for %s/%s: %w", req.InterfaceName, req.PeerID, err)
	}

	dataURI := "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
	slog.Info("generated peer QR code", "interface", req.InterfaceName, "peer", req.PeerID)
	return apischema.QRCodeResponse{QRCode: dataURI}, nil
}

func PeerConfigDownload(ctx context.Context, req apischema.WireGuardPeerRequest) (apischema.PeerConfigDownload, error) {
	file, content, err := readPeerExport(ctx, req)
	if err != nil {
		return apischema.PeerConfigDownload{}, err
	}
	slog.Info("served peer config download", "interface", req.InterfaceName, "peer", req.PeerID, "size", len(content))
	return apischema.PeerConfigDownload{
		Content:  content,
		Filename: peerExportFilename(file.Name, file.ID),
	}, nil
}
