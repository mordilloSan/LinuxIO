package wireguard

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"slices"
	"strings"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

const (
	presharedKeyGenerate = "generate"
	presharedKeyRemove   = "remove"
	minMTU               = 1280
	maxMTU               = 65535
	maxKeepalive         = 65535
)

// UpdatePeer applies a partial update to one peer: the exported client config
// is rewritten with the new metadata, the server config's [Peer] section is
// replaced (or dropped while the peer is disabled), and a running interface
// is synced. Nothing is written until every field has been validated.
func UpdatePeer(ctx context.Context, req apischema.WireGuardUpdatePeerRequest) error {
	if req.InterfaceName == "" || req.PeerID == "" {
		return fmt.Errorf("usage: update_peer <interface> <peerId> [fields]")
	}
	if err := validateInterfaceName(req.InterfaceName); err != nil {
		return fmt.Errorf("invalid interface name: %w", err)
	}
	if err := validateInterfaceName(req.PeerID); err != nil {
		return fmt.Errorf("invalid peer id: %w", err)
	}

	path := peerConfigPath(req.InterfaceName, req.PeerID)
	file, err := readPeerFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("peer %s not found on %s", req.PeerID, req.InterfaceName)
		}
		return err
	}
	oldPublicKey, err := file.publicKey()
	if err != nil {
		return err
	}
	cfg, err := ParseWireGuardConfig(configPath(req.InterfaceName))
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}

	if err = applyPeerUpdate(&file, req); err != nil {
		return err
	}

	if err = writePeerFile(path, file); err != nil {
		slog.Error("failed to write peer config", "interface", req.InterfaceName, "peer", req.PeerID, "error", err)
		return err
	}

	section, err := file.serverSection()
	if err != nil {
		return err
	}
	if file.Enabled {
		replacePeerSection(&cfg, oldPublicKey, &section)
	} else {
		replacePeerSection(&cfg, oldPublicKey, nil)
	}
	if err := WriteWireGuardConfig(configPath(req.InterfaceName), cfg); err != nil {
		slog.Error("failed to write interface config", "interface", req.InterfaceName, "peer", req.PeerID, "error", err)
		return fmt.Errorf("write config: %w", err)
	}

	if err := syncRunningInterface(ctx, req.InterfaceName); err != nil {
		slog.Error("failed to sync running interface after updating peer", "interface", req.InterfaceName, "peer", req.PeerID, "error", err)
		return err
	}
	slog.Info("WireGuard peer updated", "interface", req.InterfaceName, "peer", req.PeerID, "enabled", file.Enabled)
	return nil
}

func applyPeerUpdate(file *peerFile, req apischema.WireGuardUpdatePeerRequest) error {
	if err := applyPeerIdentity(file, req); err != nil {
		return err
	}
	if err := applyPeerRoutes(file, req); err != nil {
		return err
	}
	if err := applyPeerLinkSettings(file, req); err != nil {
		return err
	}
	return applyPeerKeys(file, req)
}

func applyPeerIdentity(file *peerFile, req apischema.WireGuardUpdatePeerRequest) error {
	if req.Name != nil {
		if err := validatePeerName(*req.Name); err != nil {
			return err
		}
		file.Name = strings.TrimSpace(*req.Name)
		if file.Name == "" {
			file.Name = file.ID
		}
	}
	if req.Enabled != nil {
		file.Enabled = *req.Enabled
	}
	return nil
}

func applyPeerRoutes(file *peerFile, req apischema.WireGuardUpdatePeerRequest) error {
	if req.ClientAllowedIPs != nil {
		ips, err := validateCIDRs(req.ClientAllowedIPs)
		if err != nil {
			return fmt.Errorf("invalid client allowed IPs: %w", err)
		}
		if len(ips) == 0 {
			ips = fullTunnelAllowedIPs
		}
		file.ClientAllowedIPs = ips
	}
	if req.ServerAllowedIPs != nil {
		ips, err := validateCIDRs(req.ServerAllowedIPs)
		if err != nil {
			return fmt.Errorf("invalid server allowed IPs: %w", err)
		}
		file.ServerAllowedIPs = ips
	}
	if req.DNS != nil {
		dns, err := validateDNS(req.DNS)
		if err != nil {
			return err
		}
		if len(dns) == 0 {
			dns = interfaceDefaultDNS(req.InterfaceName)
		}
		file.DNS = dns
	}
	return nil
}

func applyPeerLinkSettings(file *peerFile, req apischema.WireGuardUpdatePeerRequest) error {
	if req.MTU != nil {
		if *req.MTU != 0 && (*req.MTU < minMTU || *req.MTU > maxMTU) {
			return fmt.Errorf("invalid MTU %d: use 0 or %d to %d", *req.MTU, minMTU, maxMTU)
		}
		file.MTU = *req.MTU
	}
	if req.PersistentKeepalive != nil {
		if *req.PersistentKeepalive < 0 || *req.PersistentKeepalive > maxKeepalive {
			return fmt.Errorf("invalid keepalive %d: use 0 to %d", *req.PersistentKeepalive, maxKeepalive)
		}
		file.PersistentKeepalive = *req.PersistentKeepalive
	}
	return nil
}

func applyPeerKeys(file *peerFile, req apischema.WireGuardUpdatePeerRequest) error {
	switch req.PresharedKey {
	case "":
	case presharedKeyGenerate:
		psk, err := wgtypes.GenerateKey()
		if err != nil {
			return fmt.Errorf("generate preshared key: %w", err)
		}
		file.PresharedKey = psk.String()
	case presharedKeyRemove:
		file.PresharedKey = ""
	default:
		return fmt.Errorf("invalid preshared key action %q: use %q or %q", req.PresharedKey, presharedKeyGenerate, presharedKeyRemove)
	}
	if req.RegenerateKeys {
		priv, err := wgtypes.GeneratePrivateKey()
		if err != nil {
			return fmt.Errorf("generate private key: %w", err)
		}
		file.PrivateKey = priv.String()
	}
	return nil
}

// validateCIDRs trims, validates and de-duplicates a list of CIDRs, keeping
// the entries as typed. nil is returned for an empty list.
func validateCIDRs(values []string) ([]string, error) {
	var result []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, _, err := net.ParseCIDR(value); err != nil {
			return nil, fmt.Errorf("%q is not a CIDR", value)
		}
		if !slices.Contains(result, value) {
			result = append(result, value)
		}
	}
	return result, nil
}

func validateDNS(values []string) ([]string, error) {
	var result []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if strings.ContainsAny(value, " \t\r\n,") {
			return nil, fmt.Errorf("invalid DNS entry %q", value)
		}
		result = append(result, value)
	}
	return result, nil
}

// interfaceDefaultDNS is what a peer gets when it has no DNS of its own.
func interfaceDefaultDNS(interfaceName string) []string {
	cfg, err := ParseWireGuardConfig(configPath(interfaceName))
	if err != nil {
		slog.Warn("failed to read interface config for DNS default", "interface", interfaceName, "error", err)
	}
	return defaultClientDNS(cfg)
}
