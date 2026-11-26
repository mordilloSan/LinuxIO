package wireguard

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/netip"
	"slices"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

// A connection check proves an interface works for a client, or names the hop
// that does not: LinuxIO adds a throwaway peer to the running device, builds
// that peer's side in an anonymous network namespace of the server, with its
// endpoint on 127.0.0.1, and sends real traffic through the tunnel. Nothing is
// written to the config, and the namespace dies with the process. See
// docs/wireguard.md, "Connection check".

const (
	// checkTimeout bounds the setup and every probe; the probes' own waits add
	// up to 17 s at most.
	checkTimeout   = 20 * time.Second
	handshakeWait  = 5 * time.Second
	pingWait       = 2 * time.Second
	dnsWait        = 2 * time.Second
	maxDNSServers  = 3
	cleanupTimeout = 10 * time.Second
	// maxProbeHosts bounds the search for a free address in a large subnet.
	maxProbeHosts = 1 << 16
)

// probePlan is what the kernel part needs to build the probe.
type probePlan struct {
	iface      string
	serverKey  wgtypes.Key
	listenPort int
	probe      netip.Addr
	// tunnel is the server's address in the tunnel.
	tunnel     netip.Addr
	privateKey wgtypes.Key
	// stale are probe peers an earlier check left on the device.
	stale []wgtypes.Key
}

// checkKernel is the part of a check that touches the kernel; tests replace it.
type checkKernel interface {
	// device reads the running interface; an error wrapping fs.ErrNotExist
	// means it is down.
	device(name string) (*wgtypes.Device, error)
	// hostAddresses are this host's own IPv4 addresses.
	hostAddresses() ([]netip.Addr, error)
	// open adds the temporary peer and builds the probe's side. On an error
	// it has undone what it did.
	open(ctx context.Context, plan probePlan) (probeEnv, error)
}

// probeEnv is a built probe. Each probe sends from inside its namespace.
type probeEnv interface {
	// handshake sends a packet through the tunnel and waits for the server
	// to record a handshake with the temporary peer.
	handshake(ctx context.Context, wait time.Duration) error
	ping(ctx context.Context, target netip.Addr, wait time.Duration) error
	// hostPing pings from the host's own namespace, not through the tunnel.
	hostPing(ctx context.Context, target netip.Addr, wait time.Duration) error
	queryDNS(ctx context.Context, server netip.Addr, wait time.Duration) error
	// close removes the temporary peer and the namespace.
	close(ctx context.Context) error
}

var newCheckKernel = func() checkKernel { return kernelProbe{} }

// checkTargets are the addresses a check probes.
type checkTargets struct {
	tunnel  netip.Addr
	gateway netip.Addr // invalid when the host has no default gateway
	dns     []netip.Addr
	own     []netip.Addr
}

// CheckInterface runs wireguard.check_interface. It holds configMu for its
// whole run, so no edit or sync removes the temporary peer under it, and one
// check runs at a time.
func CheckInterface(ctx context.Context, req apischema.NameRequest) (apischema.WireGuardCheckResult, error) {
	name := req.Name
	if err := validateInterfaceName(name); err != nil {
		return apischema.WireGuardCheckResult{}, fmt.Errorf("%w: invalid interface name: %v", bridgeipc.ErrInvalidArgs, err)
	}
	if err := ctx.Err(); err != nil {
		return apischema.WireGuardCheckResult{}, err
	}
	// sync.Mutex.Lock is not cancellable once entered.
	configMu.Lock()
	defer configMu.Unlock()
	// A check cancelled while it waited for the lock adds no peer.
	if err := ctx.Err(); err != nil {
		return apischema.WireGuardCheckResult{}, err
	}

	cfg, err := ParseWireGuardConfig(configPath(name))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return apischema.WireGuardCheckResult{}, fmt.Errorf("%w: interface %s not found", bridgeipc.ErrNotFound, name)
		}
		return apischema.WireGuardCheckResult{}, fmt.Errorf("read config: %w", err)
	}
	kernel := newCheckKernel()
	exported, err := loadExportedPeers(ctx, name)
	if err != nil {
		return apischema.WireGuardCheckResult{}, fmt.Errorf("read exported peers of %s: %w", name, err)
	}
	plan, targets, err := planCheck(name, cfg, exported, kernel)
	if err != nil {
		return apischema.WireGuardCheckResult{}, err
	}

	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	slog.Debug("starting WireGuard connection check", "interface", name, "probe", plan.probe, "stale_peers", len(plan.stale))
	env, err := kernel.open(ctx, plan)
	if err != nil {
		return apischema.WireGuardCheckResult{}, fmt.Errorf("set up the check: %w", err)
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
		defer cancel()
		if closeErr := env.close(cleanupCtx); closeErr != nil {
			slog.Warn("failed to clean up after a WireGuard connection check", "interface", name, "error", closeErr)
		}
	}()

	steps, err := runCheck(ctx, env, targets)
	if err != nil {
		return apischema.WireGuardCheckResult{}, err
	}
	slog.Debug("checked WireGuard connection", "interface", name, "steps", steps)
	return apischema.WireGuardCheckResult{Steps: steps}, nil
}

// planCheck picks what the check probes: the server's tunnel address, a free
// address for the probe, the gateway and the clients' DNS servers.
func planCheck(name string, cfg WireGuardConfig, exported []PeerInfo, kernel checkKernel) (probePlan, checkTargets, error) {
	subnet, ok := firstIPv4Prefix(cfg.Address)
	if !ok {
		return probePlan{}, checkTargets{}, fmt.Errorf("%w: interface %s has no IPv4 address", bridgeipc.ErrConflict, name)
	}
	dev, err := kernel.device(name)
	if errors.Is(err, fs.ErrNotExist) {
		return probePlan{}, checkTargets{}, fmt.Errorf("%w: interface %s is down", bridgeipc.ErrConflict, name)
	}
	if err != nil {
		return probePlan{}, checkTargets{}, fmt.Errorf("read device %s: %w", name, err)
	}
	if dev.ListenPort == 0 {
		return probePlan{}, checkTargets{}, fmt.Errorf("%w: interface %s has no listen port", bridgeipc.ErrConflict, name)
	}
	// The probe uses the key of the interface that is up, so one left
	// running under another key would pass every step.
	if private, keyErr := wgtypes.ParseKey(cfg.PrivateKey); keyErr == nil && !keyMatches(private, dev.PublicKey) {
		return probePlan{}, checkTargets{}, fmt.Errorf("%w: interface %s is running under another key than its config, so no device of the config can connect: turn it off and on", bridgeipc.ErrConflict, name)
	}
	// A disabled peer keeps its address in its exported file only.
	reserved := make([]string, 0, len(exported))
	for _, peer := range exported {
		reserved = append(reserved, peer.Address)
	}
	probe, stale, ok := pickProbeAddress(subnet, cfg, reserved, dev.Peers)
	if !ok {
		return probePlan{}, checkTargets{}, fmt.Errorf("%w: interface %s has no free address in %s for a check", bridgeipc.ErrConflict, name, subnet.Masked())
	}
	own, err := kernel.hostAddresses()
	if err != nil {
		return probePlan{}, checkTargets{}, fmt.Errorf("read the host's addresses: %w", err)
	}
	privateKey, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		return probePlan{}, checkTargets{}, fmt.Errorf("generate a key: %w", err)
	}

	targets := checkTargets{tunnel: subnet.Addr(), own: own}
	if gateway, err := getDefaultGatewayIPv4Func(); err == nil {
		if addr, err := netip.ParseAddr(gateway); err == nil && addr.Is4() {
			targets.gateway = addr
		}
	}
	targets.dns = clientDNSServers(defaultClientDNS(cfg))

	plan := probePlan{
		iface:      name,
		serverKey:  dev.PublicKey,
		listenPort: dev.ListenPort,
		probe:      probe,
		tunnel:     targets.tunnel,
		privateKey: privateKey,
		stale:      stale,
	}
	return plan, targets, nil
}

// firstIPv4Prefix is the interface's first IPv4 address with its subnet.
func firstIPv4Prefix(addresses []string) (netip.Prefix, bool) {
	for _, address := range addresses {
		prefix, err := netip.ParsePrefix(address)
		if err == nil && prefix.Addr().Is4() {
			return prefix, true
		}
	}
	return netip.Prefix{}, false
}

// clientDNSServers keeps the IPv4 addresses of a client DNS list (a wg-quick
// DNS line may also hold search domains), at most maxDNSServers of them.
func clientDNSServers(values []string) []netip.Addr {
	var servers []netip.Addr
	for _, value := range values {
		addr, err := netip.ParseAddr(value)
		if err != nil || !addr.Is4() {
			continue
		}
		servers = append(servers, addr)
		if len(servers) == maxDNSServers {
			break
		}
	}
	return servers
}

// pickProbeAddress is the first host address of the interface's subnet, from
// offset 2 as for peers, that is not the server's and that no peer of the
// config or the device holds. A device peer that is not in the config and
// holds exactly that address as a /32 is a probe peer a crashed check left
// behind: it is returned in stale, to be removed, and does not hold the
// address. A peer an administrator added by hand with `wg set`, not saved to
// the config, that holds exactly that /32 is removed too. reserved are
// addresses of exported peers.
func pickProbeAddress(subnet netip.Prefix, cfg WireGuardConfig, reserved []string, device []wgtypes.Peer) (netip.Addr, []wgtypes.Key, bool) {
	server := subnet.Addr()
	subnet = subnet.Masked()
	taken, strays := heldAddresses(cfg, reserved, device)

	addr := subnet.Addr().Next()
	for range maxProbeHosts {
		addr = addr.Next()
		if !addr.IsValid() || !subnet.Contains(addr) || !subnet.Contains(addr.Next()) {
			// Past the last host: the next address is the broadcast.
			return netip.Addr{}, nil, false
		}
		if addr == server || holds(taken, subnet, addr) {
			continue
		}
		return addr, strays[addr], true
	}
	return netip.Addr{}, nil, false
}

// heldAddresses are the allowed IPs of the config's peers, the reserved
// addresses and the device's peers, and apart from them the device peers
// that may be probes left behind (strays): in no config, with one /32.
func heldAddresses(cfg WireGuardConfig, reserved []string, device []wgtypes.Peer) ([]netip.Prefix, map[netip.Addr][]wgtypes.Key) {
	configured := make(map[string]bool, len(cfg.Peers))
	var taken []netip.Prefix
	for _, peer := range cfg.Peers {
		configured[peer.PublicKey] = true
		taken = appendPrefixes(taken, peer.AllowedIPs...)
	}
	taken = appendPrefixes(taken, reserved...)
	strays := map[netip.Addr][]wgtypes.Key{}
	for _, peer := range device {
		if addr, ok := strayProbe(peer, configured); ok {
			strays[addr] = append(strays[addr], peer.PublicKey)
			continue
		}
		for _, allowed := range peer.AllowedIPs {
			taken = appendPrefixes(taken, allowed.String())
		}
	}
	return taken, strays
}

// strayProbe is the address of a device peer that looks like a probe peer: in
// no config, with one IPv4 /32 for its allowed IPs. A peer added by hand with
// `wg set` and not saved to the config looks the same.
func strayProbe(peer wgtypes.Peer, configured map[string]bool) (netip.Addr, bool) {
	if configured[peer.PublicKey.String()] || len(peer.AllowedIPs) != 1 {
		return netip.Addr{}, false
	}
	ones, bits := peer.AllowedIPs[0].Mask.Size()
	addr, ok := netip.AddrFromSlice(peer.AllowedIPs[0].IP)
	if !ok || ones != 32 || bits != 32 {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}

func appendPrefixes(prefixes []netip.Prefix, values ...string) []netip.Prefix {
	for _, value := range values {
		ipNet, err := parseAllowedIP(value)
		if err != nil {
			continue
		}
		addr, ok := netip.AddrFromSlice(ipNet.IP)
		if !ok {
			continue
		}
		ones, _ := ipNet.Mask.Size()
		prefixes = append(prefixes, netip.PrefixFrom(addr.Unmap(), ones))
	}
	return prefixes
}

// holds reports whether a peer holds addr: an allowed IP that contains it and
// is narrower than the subnet. A wider one is a route, not an address.
func holds(taken []netip.Prefix, subnet netip.Prefix, addr netip.Addr) bool {
	for _, prefix := range taken {
		if prefix.Bits() > subnet.Bits() && prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// runCheck runs the probes and reports every step, in the order of
// apischema.WireGuardCheckStepIDs. DNS is asked before "beyond" is decided,
// since an answer from a DNS server outside this host also proves that
// traffic goes beyond it.
func runCheck(ctx context.Context, env probeEnv, t checkTargets) ([]apischema.WireGuardCheckStep, error) {
	handshake := apischema.WireGuardCheckStep{ID: apischema.WireGuardCheckHandshake}
	tunnel := apischema.WireGuardCheckStep{ID: apischema.WireGuardCheckTunnel, Target: t.tunnel.String()}
	skipped := func(id apischema.WireGuardCheckStepID, reason string) apischema.WireGuardCheckStep {
		return apischema.WireGuardCheckStep{ID: id, Status: apischema.WireGuardCheckSkipped, Detail: reason}
	}

	if err := env.handshake(ctx, handshakeWait); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		handshake.Status, handshake.Detail = apischema.WireGuardCheckFailed, err.Error()
		const reason = "no handshake with the server"
		return []apischema.WireGuardCheckStep{handshake, skipped(tunnel.ID, reason), skipped(apischema.WireGuardCheckBeyond, reason), skipped(apischema.WireGuardCheckDNS, reason)}, nil
	}
	handshake.Status = apischema.WireGuardCheckOK

	if err := env.ping(ctx, t.tunnel, pingWait); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		tunnel.Status, tunnel.Detail = apischema.WireGuardCheckFailed, err.Error()
		const reason = "no traffic passes through the tunnel"
		return []apischema.WireGuardCheckStep{handshake, tunnel, skipped(apischema.WireGuardCheckBeyond, reason), skipped(apischema.WireGuardCheckDNS, reason)}, nil
	}
	tunnel.Status = apischema.WireGuardCheckOK

	dns, answered, err := checkDNS(ctx, env, t.dns)
	if err != nil {
		return nil, err
	}
	// A DNS server on this host proves nothing about what lies beyond it.
	if slices.Contains(t.own, answered) {
		answered = netip.Addr{}
	}
	beyond, err := checkBeyond(ctx, env, t.gateway, answered)
	if err != nil {
		return nil, err
	}
	return []apischema.WireGuardCheckStep{handshake, tunnel, beyond, dns}, nil
}

// checkDNS asks the clients' DNS servers in turn and returns the one that
// answered, if any.
func checkDNS(ctx context.Context, env probeEnv, servers []netip.Addr) (apischema.WireGuardCheckStep, netip.Addr, error) {
	step := apischema.WireGuardCheckStep{ID: apischema.WireGuardCheckDNS}
	if len(servers) == 0 {
		step.Status, step.Detail = apischema.WireGuardCheckSkipped, "no DNS server for clients: none is set and the host has no default gateway"
		return step, netip.Addr{}, nil
	}
	var lastErr error
	for _, server := range servers {
		lastErr = env.queryDNS(ctx, server, dnsWait)
		if lastErr == nil {
			step.Status, step.Target = apischema.WireGuardCheckOK, server.String()
			return step, server, nil
		}
		if ctx.Err() != nil {
			return step, netip.Addr{}, ctx.Err()
		}
	}
	step.Status, step.Target, step.Detail = apischema.WireGuardCheckFailed, servers[0].String(), lastErr.Error()
	return step, netip.Addr{}, nil
}

// checkBeyond pings the gateway through the tunnel. A DNS server outside
// this host that answered (outsideDNS) passes the step too. A gateway that
// does not answer through the tunnel is pinged from the host itself: only
// when it answers there is the silence a failure, since some gateways (cloud
// routers) never answer a ping, and then the step cannot be judged.
func checkBeyond(ctx context.Context, env probeEnv, gateway, outsideDNS netip.Addr) (apischema.WireGuardCheckStep, error) {
	step := apischema.WireGuardCheckStep{ID: apischema.WireGuardCheckBeyond}
	if !gateway.IsValid() {
		if outsideDNS.IsValid() {
			step.Status, step.Target = apischema.WireGuardCheckOK, outsideDNS.String()
			return step, nil
		}
		step.Status, step.Detail = apischema.WireGuardCheckSkipped, "the host has no default IPv4 gateway to probe, and no DNS server outside it answered"
		return step, nil
	}
	step.Target = gateway.String()
	tunnelErr := env.ping(ctx, gateway, pingWait)
	switch {
	case tunnelErr == nil:
		step.Status = apischema.WireGuardCheckOK
		return step, nil
	case ctx.Err() != nil:
		return step, ctx.Err()
	case outsideDNS.IsValid():
		step.Status, step.Target = apischema.WireGuardCheckOK, outsideDNS.String()
		return step, nil
	}
	hostErr := env.hostPing(ctx, gateway, pingWait)
	switch {
	case hostErr == nil:
		step.Status, step.Detail = apischema.WireGuardCheckFailed, tunnelErr.Error()+"; the gateway answers this server"
	case ctx.Err() != nil:
		return step, ctx.Err()
	default:
		step.Status, step.Detail = apischema.WireGuardCheckSkipped, fmt.Sprintf("the gateway %s does not answer a ping from this server either, so this step cannot be judged", gateway)
	}
	return step, nil
}
