package wireguard

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

var syncWireGuardConfigFunc = syncWireGuardConfig

func syncRunningInterface(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !isInterfaceUpFunc(name) {
		return nil
	}

	_, err := syncWireGuardConfigFunc(ctx, name)
	if err != nil {
		return fmt.Errorf("sync config: %w", err)
	}
	slog.Debug("synced WireGuard interface from config", "interface", name)
	return nil
}

// syncWireGuardConfig is wg syncconf over wgctrl: the interface keys and
// peers from the config file are applied and peers missing from it are
// removed, so unchanged peers keep their sessions and counters.
func syncWireGuardConfig(ctx context.Context, name string) (string, error) {
	cfg, err := ParseWireGuardConfig(configPath(name))
	if err != nil {
		return "", err
	}
	client, err := wgctrl.New()
	if err != nil {
		return "", fmt.Errorf("open wgctrl: %w", err)
	}
	defer client.Close()

	dev, err := client.Device(name)
	if err != nil {
		return "", fmt.Errorf("read device %s: %w", name, err)
	}
	deviceCfg, err := syncDeviceConfig(ctx, cfg, dev.Peers)
	if err != nil {
		return "", err
	}
	if err := client.ConfigureDevice(name, deviceCfg); err != nil {
		return "", fmt.Errorf("configure %s: %w", name, err)
	}
	return "", nil
}

func syncDeviceConfig(ctx context.Context, cfg WireGuardConfig, current []wgtypes.Peer) (wgtypes.Config, error) {
	privateKey, err := wgtypes.ParseKey(cfg.PrivateKey)
	if err != nil {
		return wgtypes.Config{}, fmt.Errorf("parse private key: %w", err)
	}
	out := wgtypes.Config{PrivateKey: &privateKey}
	if cfg.ListenPort > 0 {
		out.ListenPort = &cfg.ListenPort
	}

	wanted := make(map[wgtypes.Key]bool, len(cfg.Peers))
	for _, peer := range cfg.Peers {
		pc, err := peerDeviceConfig(ctx, peer)
		if err != nil {
			return wgtypes.Config{}, err
		}
		wanted[pc.PublicKey] = true
		out.Peers = append(out.Peers, pc)
	}
	for _, peer := range current {
		if !wanted[peer.PublicKey] {
			out.Peers = append(out.Peers, wgtypes.PeerConfig{PublicKey: peer.PublicKey, Remove: true})
		}
	}
	return out, nil
}

func peerDeviceConfig(ctx context.Context, peer PeerConfig) (wgtypes.PeerConfig, error) {
	publicKey, err := wgtypes.ParseKey(peer.PublicKey)
	if err != nil {
		return wgtypes.PeerConfig{}, fmt.Errorf("parse peer public key: %w", err)
	}
	pc := wgtypes.PeerConfig{PublicKey: publicKey, ReplaceAllowedIPs: true}
	if peer.PresharedKey != "" {
		psk, err := wgtypes.ParseKey(peer.PresharedKey)
		if err != nil {
			return wgtypes.PeerConfig{}, fmt.Errorf("parse preshared key for %s: %w", peer.PublicKey, err)
		}
		pc.PresharedKey = &psk
	}
	for _, allowed := range peer.AllowedIPs {
		ipNet, err := parseAllowedIP(allowed)
		if err != nil {
			return wgtypes.PeerConfig{}, fmt.Errorf("parse allowed IP %q for %s: %w", allowed, peer.PublicKey, err)
		}
		pc.AllowedIPs = append(pc.AllowedIPs, ipNet)
	}
	if peer.Endpoint != "" {
		endpoint, err := resolveEndpoint(ctx, peer.Endpoint)
		if err != nil {
			return wgtypes.PeerConfig{}, fmt.Errorf("resolve endpoint %q for %s: %w", peer.Endpoint, peer.PublicKey, err)
		}
		pc.Endpoint = endpoint
	}
	if peer.PersistentKeepalive > 0 {
		keepalive := time.Duration(peer.PersistentKeepalive) * time.Second
		pc.PersistentKeepaliveInterval = &keepalive
	}
	return pc, nil
}

// parseAllowedIP accepts a CIDR or, like wg, a bare address as a host route.
func parseAllowedIP(value string) (net.IPNet, error) {
	if _, ipNet, err := net.ParseCIDR(value); err == nil {
		return *ipNet, nil
	}
	ip := net.ParseIP(value)
	if ip == nil {
		return net.IPNet{}, fmt.Errorf("invalid address")
	}
	if v4 := ip.To4(); v4 != nil {
		return net.IPNet{IP: v4, Mask: net.CIDRMask(32, 32)}, nil
	}
	return net.IPNet{IP: ip, Mask: net.CIDRMask(128, 128)}, nil
}

func resolveEndpoint(ctx context.Context, endpoint string) (*net.UDPAddr, error) {
	host, portText, err := net.SplitHostPort(endpoint)
	if err != nil {
		return nil, err
	}
	port, err := net.DefaultResolver.LookupPort(ctx, "udp", portText)
	if err != nil {
		return nil, err
	}
	addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("no addresses for %s", host)
	}
	return &net.UDPAddr{IP: addrs[0].AsSlice(), Port: port}, nil
}
