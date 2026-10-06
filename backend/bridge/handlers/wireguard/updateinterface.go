package wireguard

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

const (
	minListenPort = 1
	maxListenPort = 65535
)

// UpdateInterface applies a partial update to an interface. Port, MTU and
// hooks are server-side: when the interface is up it is brought down while
// the old config is still on disk (so the old PostDown hooks undo the old
// PostUp), the new config is written, and it is brought back up. Host and DNS
// only affect exported client configs, which are rewritten without a restart.
func UpdateInterface(ctx context.Context, req apischema.WireGuardUpdateInterfaceRequest) error {
	if req.Name == "" {
		return fmt.Errorf("usage: update_interface <name> [fields]")
	}
	if err := validateInterfaceName(req.Name); err != nil {
		return fmt.Errorf("invalid interface name: %w", err)
	}

	old, err := ParseWireGuardConfig(configPath(req.Name))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("interface %s not found", req.Name)
		}
		return fmt.Errorf("read config: %w", err)
	}
	cfg := old
	if err := applyInterfaceUpdate(&cfg, req); err != nil {
		return err
	}

	serverChanged := cfg.ListenPort != old.ListenPort || cfg.MTU != old.MTU ||
		!slices.Equal(cfg.PreUp, old.PreUp) || !slices.Equal(cfg.PostUp, old.PostUp) ||
		!slices.Equal(cfg.PreDown, old.PreDown) || !slices.Equal(cfg.PostDown, old.PostDown)
	restart := serverChanged && isInterfaceUpFunc(req.Name)

	if restart {
		if out, err := runWGQuickCommand(ctx, "down", req.Name); err != nil {
			slog.Error("failed to bring down WireGuard interface before update", "interface", req.Name, "error", err, "output", out)
			return fmt.Errorf("bring down interface: %w", err)
		}
	}
	if err := WriteWireGuardConfig(configPath(req.Name), cfg); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if restart {
		if out, err := runWGQuickCommand(ctx, "up", req.Name); err != nil {
			slog.Error("failed to bring up WireGuard interface after update", "interface", req.Name, "error", err, "output", out)
			return fmt.Errorf("interface %s is down: bring up failed: %w", req.Name, err)
		}
	}

	if cfg.Host != old.Host || cfg.ListenPort != old.ListenPort || !slices.Equal(cfg.DNS, old.DNS) {
		if err := rewriteExportedPeers(ctx, req.Name, old, cfg); err != nil {
			return err
		}
	}
	slog.Info("WireGuard interface updated", "interface", req.Name, "restarted", restart)
	return nil
}

func applyInterfaceUpdate(cfg *WireGuardConfig, req apischema.WireGuardUpdateInterfaceRequest) error {
	if req.Host != nil {
		host := strings.TrimSpace(*req.Host)
		if err := validateHost(host); err != nil {
			return err
		}
		cfg.Host = host
	}
	if req.DNS != nil {
		dns, err := validateDNS(req.DNS)
		if err != nil {
			return err
		}
		cfg.DNS = dns
	}
	if req.MTU != nil {
		if *req.MTU != 0 && (*req.MTU < minMTU || *req.MTU > maxMTU) {
			return fmt.Errorf("invalid MTU %d: use 0 or %d to %d", *req.MTU, minMTU, maxMTU)
		}
		cfg.MTU = *req.MTU
	}
	if req.ListenPort != nil {
		if *req.ListenPort < minListenPort || *req.ListenPort > maxListenPort {
			return fmt.Errorf("invalid listen port %d", *req.ListenPort)
		}
		if other := interfaceUsingPort(req.Name, *req.ListenPort); other != "" {
			return fmt.Errorf("port %d is already used by %s", *req.ListenPort, other)
		}
		cfg.ListenPort = *req.ListenPort
	}
	applyInterfaceHooks(cfg, req)
	return nil
}

func applyInterfaceHooks(cfg *WireGuardConfig, req apischema.WireGuardUpdateInterfaceRequest) {
	for _, hook := range []struct {
		target *[]string
		lines  []string
	}{{&cfg.PreUp, req.PreUp}, {&cfg.PostUp, req.PostUp}, {&cfg.PreDown, req.PreDown}, {&cfg.PostDown, req.PostDown}} {
		if hook.lines != nil {
			*hook.target = cleanLines(hook.lines)
		}
	}
}

// interfaceUsingPort returns the name of another interface config that
// listens on port, or "".
func interfaceUsingPort(self string, port int) string {
	files, _ := filepath.Glob(filepath.Join(wgConfigDir, "*"+configExt))
	for _, file := range files {
		name := strings.TrimSuffix(filepath.Base(file), configExt)
		if name == self {
			continue
		}
		if other, err := ParseWireGuardConfig(file); err == nil && other.ListenPort == port {
			return name
		}
	}
	return ""
}

func cleanLines(lines []string) []string {
	var result []string
	for _, line := range lines {
		if line = strings.TrimSpace(line); line != "" {
			result = append(result, line)
		}
	}
	return result
}

// peerRewrite is what an interface change pushes into exported client configs.
type peerRewrite struct {
	port       int    // new listen port; 0 when the endpoint is unchanged
	host       string // resolved endpoint host; "" keeps each peer's current host
	dnsChanged bool
	oldDefault []string
	newDefault []string
}

// rewriteExportedPeers pushes a changed endpoint or DNS default into the
// exported client configs. Peers keep their own DNS unless it equalled the
// old interface default; the endpoint host falls back to whatever the peer
// already had when neither an override nor a public IP is known.
func rewriteExportedPeers(ctx context.Context, interfaceName string, old, cfg WireGuardConfig) error {
	peers, err := loadExportedPeers(ctx, interfaceName)
	if err != nil {
		return err
	}
	change := peerRewrite{dnsChanged: !slices.Equal(cfg.DNS, old.DNS)}
	if cfg.Host != old.Host || cfg.ListenPort != old.ListenPort {
		change.port = cfg.ListenPort
		change.host = resolveEndpointHost(cfg, "UpdateInterface")
	}
	if change.dnsChanged {
		change.oldDefault = defaultClientDNS(old)
		change.newDefault = defaultClientDNS(cfg)
	}

	for _, peer := range peers {
		file := peer.peerFile
		if !change.apply(&file) {
			continue
		}
		if err := writePeerFile(peerConfigPath(interfaceName, file.ID), file); err != nil {
			return fmt.Errorf("rewrite peer %s: %w", file.ID, err)
		}
	}
	return nil
}

// apply updates one peer file in memory and reports whether it changed.
func (c peerRewrite) apply(file *peerFile) bool {
	changed := false
	if c.port > 0 {
		host := c.host
		if host == "" {
			host, _, _ = net.SplitHostPort(file.Endpoint)
		}
		if host != "" {
			file.Endpoint = net.JoinHostPort(host, strconv.Itoa(c.port))
			changed = true
		} else {
			slog.Warn("no endpoint host known; leaving peer endpoint unchanged", "peer", file.ID)
		}
	}
	if c.dnsChanged && slices.Equal(file.DNS, c.oldDefault) {
		file.DNS = c.newDefault
		changed = true
	}
	return changed
}

// defaultClientDNS is the DNS a peer without its own gets: the header DNS,
// else the default gateway, else nothing.
func defaultClientDNS(cfg WireGuardConfig) []string {
	if len(cfg.DNS) > 0 {
		return cfg.DNS
	}
	if gateway, _ := getDefaultGatewayIPv4Func(); gateway != "" {
		return []string{gateway}
	}
	return nil
}
