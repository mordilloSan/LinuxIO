package network

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/vishvananda/netlink"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/dbusclient"
	"github.com/mordilloSan/LinuxIO/backend/common/utils"
)

const (
	bridgeBackendNetworkManager = "nmconnection"
	bridgeBackendNetworkd       = "systemd-networkd"
	bridgeBackendNetplan        = "netplan"
	maxLinuxInterfaceNameBytes  = 15
)

// BridgeCandidate describes one physical link that can safely be used by the
// Stage 2a bridge flow. Reasons are populated for ineligible links.
type BridgeCandidate struct {
	Name            string
	MAC             string
	Backend         string
	Eligible        bool
	Reasons         []string
	HandoffEligible bool
	HandoffReasons  []string
}

type BridgeOptions struct {
	Candidates []BridgeCandidate
	Warnings   []string
}

type BridgePlan struct {
	Name   string
	Member string
}

type BridgeResult struct {
	Name    string
	Member  string
	Backend string
}

var (
	errBridgeNameInvalid = errors.New("invalid bridge name")
	errBridgeMember      = errors.New("invalid bridge member")
)

func GetBridgeOptions(ctx context.Context, env Environment) (BridgeOptions, error) {
	if err := ctx.Err(); err != nil {
		return BridgeOptions{}, err
	}
	probes, err := interfaceProbes(ctx, env)
	if err != nil {
		return BridgeOptions{}, fmt.Errorf("inspect network interfaces: %w", err)
	}

	options := BridgeOptions{
		Candidates: make([]BridgeCandidate, 0, len(probes)),
	}
	options.Warnings = firewallWarnings(ctx, env)
	for _, probe := range probes {
		if err := ctx.Err(); err != nil {
			return BridgeOptions{}, err
		}
		candidate := inspectBridgeCandidate(ctx, env, probe)
		handoff := inspectBridgeHandoffCandidate(ctx, env, probe)
		candidate.HandoffEligible = handoff.HandoffEligible
		candidate.HandoffReasons = handoff.HandoffReasons
		options.Candidates = append(options.Candidates, candidate)
	}
	return options, nil
}

func CreateBridge(ctx context.Context, env Environment, plan BridgePlan) (BridgeResult, error) {
	if err := ctx.Err(); err != nil {
		return BridgeResult{}, err
	}
	if err := validateBridgePlan(plan); err != nil {
		return BridgeResult{}, err
	}

	options, err := GetBridgeOptions(ctx, env)
	if err != nil {
		return BridgeResult{}, err
	}
	candidate, err := bridgeCandidateForPlan(options, plan)
	if err != nil {
		return BridgeResult{}, err
	}

	switch candidate.Backend {
	case bridgeBackendNetworkManager:
		return createNetworkManagerBridge(ctx, env, plan)
	case bridgeBackendNetworkd:
		return createNetworkdBridge(ctx, env, plan, candidate.MAC)
	case bridgeBackendNetplan:
		return createNetplanBridge(ctx, env, plan)
	default:
		return BridgeResult{}, fmt.Errorf("%w: %s", ErrUnsupportedBackend, candidate.Backend)
	}
}

func validateBridgePlan(plan BridgePlan) error {
	if err := validateBridgeName(plan.Name); err != nil {
		return err
	}
	if strings.TrimSpace(plan.Member) == "" {
		return fmt.Errorf("%w: member is required", errBridgeMember)
	}
	if plan.Name == plan.Member {
		return fmt.Errorf("%w: bridge and member must differ", errBridgeMember)
	}
	return nil
}

func bridgeCandidateForPlan(options BridgeOptions, plan BridgePlan) (BridgeCandidate, error) {
	var candidate BridgeCandidate
	foundMember := false
	for _, entry := range options.Candidates {
		if entry.Name == plan.Member {
			candidate = entry
			foundMember = true
		}
		if entry.Name == plan.Name {
			return BridgeCandidate{}, fmt.Errorf("bridge %s already exists", plan.Name)
		}
	}
	if !foundMember {
		return BridgeCandidate{}, fmt.Errorf("%w: interface %s was not found", errBridgeMember, plan.Member)
	}
	if !candidate.Eligible {
		return BridgeCandidate{}, fmt.Errorf("interface %s is not eligible: %s", plan.Member, strings.Join(candidate.Reasons, "; "))
	}
	return candidate, nil
}
func inspectBridgeCandidate(ctx context.Context, env Environment, probe InterfaceProbe) BridgeCandidate {
	hardReasons := physicalBridgeLinkReasons(probe)
	candidate := BridgeCandidate{
		Name:    probe.Name,
		MAC:     probe.MAC,
		Reasons: append(hardReasons, bridgeL3Reasons(probe)...),
	}
	if len(hardReasons) > 0 {
		return candidate
	}
	inspectBridgeOwnership(ctx, env, probe.Name, &candidate)
	candidate.Eligible = len(candidate.Reasons) == 0 && candidate.Backend != ""
	return candidate
}

func physicalBridgeLinkReasons(probe InterfaceProbe) []string {
	var reasons []string
	if probe.Loopback {
		reasons = append(reasons, "loopback interface")
	}
	if !probe.Ethernet {
		reasons = append(reasons, "not a wired Ethernet interface")
	}
	if probe.Wireless {
		reasons = append(reasons, "wireless interfaces cannot be bridged safely")
	}
	if probe.Bridge {
		reasons = append(reasons, "interface is already a bridge")
	}
	if strings.TrimSpace(probe.Master) != "" {
		reasons = append(reasons, fmt.Sprintf("interface is already enslaved to %s", probe.Master))
	}
	return reasons
}

func bridgeL3Reasons(probe InterfaceProbe) []string {
	var reasons []string
	for _, address := range probe.Addresses {
		if !isLinkLocalAddress(address) {
			reasons = append(reasons, "interface has a non-link-local address")
			break
		}
	}
	if probe.DefaultRoute {
		reasons = append(reasons, "interface carries a default route")
	}
	return reasons
}

func inspectBridgeOwnership(ctx context.Context, env Environment, iface string, candidate *BridgeCandidate) {
	backend, config, configured, configErr := bridgeInterfaceConfig(env, iface)
	candidate.Backend = backend
	switch {
	case configErr != nil:
		candidate.Reasons = append(candidate.Reasons, configErr.Error())
	case configured:
		candidate.Reasons = append(candidate.Reasons, configuredBridgeOwnershipReasons(ctx, env, iface, backend, config)...)
	default:
		owner, reasons := unconfiguredBridgeOwnership(ctx, env, iface)
		candidate.Backend = owner
		candidate.Reasons = append(candidate.Reasons, reasons...)
	}
}

func configuredBridgeOwnershipReasons(ctx context.Context, env Environment, iface, backend string, config InterfaceConfig) []string {
	reasons := unsafeL3Reasons(config)
	owner, err := managerForInterface(ctx, env, iface)
	if err != nil {
		return append(reasons, err.Error())
	}
	if owner != bridgeRuntimeOwner(backend) {
		reasons = append(reasons, fmt.Sprintf("persistent configuration uses %s but runtime owner is %s", backend, owner))
	}
	return reasons
}

func unconfiguredBridgeOwnership(ctx context.Context, env Environment, iface string) (string, []string) {
	owner, err := managerForInterface(ctx, env, iface)
	if err != nil {
		return "", []string{err.Error()}
	}
	if owner == bridgeBackendNetworkManager && env.ManagerForInterface == nil {
		if err := requireNetworkManagerSpareInterface(ctx, iface); err != nil {
			return owner, []string{err.Error()}
		}
	}
	return owner, nil
}

func unsafeL3Reasons(config InterfaceConfig) []string {
	var reasons []string
	if configuredL3Method(config.IPv4Method) {
		reasons = append(reasons, "interface has configured IPv4 DHCP or static L3")
	}
	if configuredL3Method(config.IPv6Method) {
		reasons = append(reasons, "interface has configured IPv6 DHCP or static L3")
	}
	if len(config.IPv4Addresses) > 0 || len(config.IPv6Addresses) > 0 {
		reasons = append(reasons, "interface has configured addresses")
	}
	if strings.TrimSpace(config.Gateway) != "" {
		reasons = append(reasons, "interface has a configured gateway")
	}
	if len(config.DNS) > 0 {
		reasons = append(reasons, "interface has configured DNS")
	}
	return reasons
}

func configuredL3Method(method string) bool {
	switch strings.ToLower(strings.TrimSpace(method)) {
	case "auto", "dhcp", "manual", "static":
		return true
	default:
		return false
	}
}

func bridgeInterfaceConfig(env Environment, iface string) (string, InterfaceConfig, bool, error) {
	backend, err := OpenBackend(env, iface)
	if err != nil {
		if errors.Is(err, ErrUnsupportedBackend) {
			return "", InterfaceConfig{}, false, nil
		}
		return "", InterfaceConfig{}, false, fmt.Errorf("inspect persistent configuration: %w", err)
	}
	config, err := backend.Read()
	if err != nil {
		return "", InterfaceConfig{}, false, fmt.Errorf("read %s configuration: %w", backend.Name(), err)
	}
	name := backend.Name()
	if name == bridgeBackendNetworkManager {
		return name, config, false, unsupportedf(
			"existing NetworkManager profile for %s must be removed before spare-NIC bridge creation",
			iface,
		)
	}
	if err := validateBridgeBackendConfiguration(backend, iface); err != nil {
		return "", InterfaceConfig{}, false, err
	}
	return name, config, true, nil
}

func validateBridgeBackendConfiguration(backend ConfigBackend, iface string) error {
	switch typed := backend.(type) {
	case *netplanBackend:
		if typed.kind != "ethernets" {
			return unsupportedf("Netplan %s interface %s is not a physical Ethernet device", typed.kind, iface)
		}
		networkd, err := netplanUsesNetworkd(typed)
		if err != nil {
			return err
		}
		if !networkd {
			return unsupportedf("Netplan renderer for %s is not systemd-networkd", iface)
		}
		return nil
	case *networkdBackend:
		cfg, err := readINIFile(typed.path)
		if err != nil {
			return fmt.Errorf("read systemd-networkd configuration: %w", err)
		}
		for _, key := range []string{"Bridge", "Bond", "VRF", "VLAN"} {
			if len(sectionShadowValues(cfg.Section("Network"), key)) > 0 {
				return unsupportedf("networkd interface %s already has %s configuration", iface, key)
			}
		}
		return ensureSimpleNetworkdLayout(cfg)
	default:
		return unsupportedf("%s owns %s", backend.Name(), iface)
	}
}

func bridgeRuntimeOwner(backend string) string {
	if backend == bridgeBackendNetplan {
		return bridgeBackendNetworkd
	}
	return backend
}

func netplanUsesNetworkd(backend *netplanBackend) (bool, error) {
	doc, err := backend.load()
	if err != nil {
		return false, fmt.Errorf("read Netplan configuration: %w", err)
	}
	network, ok := doc.root["network"].(map[string]any)
	if !ok {
		return true, nil
	}
	kindMap, ok := network[backend.kind].(map[string]any)
	if ok {
		if iface, ok := kindMap[backend.iface].(map[string]any); ok {
			if renderer, ok := iface["renderer"].(string); ok && strings.TrimSpace(renderer) != "" {
				return strings.EqualFold(strings.TrimSpace(renderer), "networkd"), nil
			}
		}
	}
	if renderer, ok := network["renderer"].(string); ok && strings.TrimSpace(renderer) != "" {
		return strings.EqualFold(strings.TrimSpace(renderer), "networkd"), nil
	}
	return true, nil
}

func managerForInterface(ctx context.Context, env Environment, iface string) (string, error) {
	if env.ManagerForInterface != nil {
		owner, err := env.ManagerForInterface(ctx, iface)
		if err != nil {
			return "", err
		}
		return normalizeBridgeBackend(owner)
	}

	var owners []string
	busErr := dbusclient.DBus.UseSessionWithOptions(ctx, dbusclient.SystemBusOptions{
		Subsystem: "network",
		Timeout:   5 * time.Second,
	}, func(session dbusclient.SystemSession) error {
		var ownerErr error
		owners, ownerErr = runtimeOwnersForInterface(session, iface)
		return ownerErr
	})
	if busErr != nil {
		return "", fmt.Errorf("confirm runtime owner for %s: %w", iface, busErr)
	}
	switch len(owners) {
	case 0:
		return "", fmt.Errorf("no supported runtime manager owns %s", iface)
	case 1:
		return owners[0], nil
	default:
		return "", fmt.Errorf("runtime ownership for %s is ambiguous: %s", iface, strings.Join(owners, ", "))
	}
}

func runtimeOwnersForInterface(session dbusclient.SystemSession, iface string) ([]string, error) {
	var owners []string
	nmState, stateErr := session.BusNameState(networkManagerBusName)
	if stateErr != nil {
		return nil, fmt.Errorf("inspect NetworkManager availability: %w", stateErr)
	}
	if nmState.Active {
		nmManaged, ownerErr := networkManagerOwnsSpareInterface(session, iface)
		if ownerErr != nil {
			return nil, ownerErr
		}
		if nmManaged {
			owners = append(owners, bridgeBackendNetworkManager)
		}
	}

	networkdState, stateErr := session.BusNameState(networkdBusName)
	if stateErr != nil {
		return nil, fmt.Errorf("inspect systemd-networkd availability: %w", stateErr)
	}
	if networkdState.Active {
		networkdManaged, ownerErr := networkdOwnsInterface(session, iface)
		if ownerErr != nil {
			return nil, ownerErr
		}
		if networkdManaged {
			owners = append(owners, bridgeBackendNetworkd)
		}
	}
	return owners, nil
}

func normalizeBridgeBackend(owner string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(owner)) {
	case "nm", "networkmanager", "network-manager", bridgeBackendNetworkManager:
		return bridgeBackendNetworkManager, nil
	case "networkd", bridgeBackendNetworkd:
		return bridgeBackendNetworkd, nil
	case bridgeBackendNetplan:
		return bridgeBackendNetplan, nil
	default:
		return "", fmt.Errorf("unsupported or unknown network owner %q", owner)
	}
}

func interfaceProbes(ctx context.Context, env Environment) ([]InterfaceProbe, error) {
	if env.InterfaceProbes != nil {
		return env.InterfaceProbes(ctx)
	}
	return defaultInterfaceProbes(ctx, env)
}

func defaultInterfaceProbes(ctx context.Context, env Environment) ([]InterfaceProbe, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	routes, err := netlink.RouteList(nil, netlink.FAMILY_ALL)
	if err != nil {
		return nil, fmt.Errorf("list routes: %w", err)
	}
	defaultRoutes := make(map[int]bool)
	for _, route := range routes {
		if route.LinkIndex == 0 || !isDefaultRoute(route) {
			continue
		}
		defaultRoutes[route.LinkIndex] = true
	}
	probes := make([]InterfaceProbe, 0, len(interfaces))
	for _, iface := range interfaces {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		addresses, err := iface.Addrs()
		if err != nil {
			return nil, fmt.Errorf("list addresses for %s: %w", iface.Name, err)
		}
		link, linkErr := netlink.LinkByName(iface.Name)
		if linkErr != nil {
			return nil, fmt.Errorf("inspect link %s: %w", iface.Name, linkErr)
		}
		master := masterLinkName(link.Attrs().MasterIndex, netlink.LinkByIndex)
		probe := InterfaceProbe{
			Name:         iface.Name,
			MAC:          iface.HardwareAddr.String(),
			Ethernet:     iface.Flags&net.FlagLoopback == 0 && link.Type() == "device" && interfaceIsEthernet(env, iface.Name),
			Loopback:     iface.Flags&net.FlagLoopback != 0,
			Wireless:     interfaceIsWireless(env, iface.Name),
			Bridge:       interfaceIsBridge(env, iface.Name),
			Master:       master,
			DefaultRoute: defaultRoutes[link.Attrs().Index],
		}
		for _, address := range addresses {
			probe.Addresses = append(probe.Addresses, address.String())
		}
		probes = append(probes, probe)
	}
	return probes, nil
}

func masterLinkName(index int, lookup func(int) (netlink.Link, error)) string {
	if index == 0 {
		return ""
	}
	master, err := lookup(index)
	if err != nil || master == nil || master.Attrs() == nil {
		return ""
	}
	return master.Attrs().Name
}

func isDefaultRoute(route netlink.Route) bool {
	if route.Dst == nil {
		return true
	}
	ones, bits := route.Dst.Mask.Size()
	return bits > 0 && ones == 0
}

func interfaceIsEthernet(env Environment, iface string) bool {
	data, err := readEnvironmentFile(env, filepath.Join("/sys/class/net", iface, "type"))
	if err != nil {
		// A missing type file is unusual on Linux. Keep physical devices
		// eligible rather than making a virtual-test fixture impossible.
		return true
	}
	return strings.TrimSpace(string(data)) == "1"
}

func interfaceIsWireless(env Environment, iface string) bool {
	path := filepath.Join("/sys/class/net", iface, "wireless")
	if env.ReadFile != nil {
		_, err := env.ReadFile(path)
		return err == nil
	}
	_, err := os.Stat(path)
	return err == nil
}

func interfaceIsBridge(env Environment, iface string) bool {
	path := filepath.Join("/sys/class/net", iface, "bridge")
	if env.ReadFile != nil {
		_, err := env.ReadFile(path)
		return err == nil
	}
	_, err := os.Stat(path)
	return err == nil
}

func readEnvironmentFile(env Environment, path string) ([]byte, error) {
	if env.ReadFile != nil {
		return env.ReadFile(path)
	}
	return os.ReadFile(path)
}

func isLinkLocalAddress(value string) bool {
	ip, _, err := net.ParseCIDR(strings.TrimSpace(value))
	return err == nil && ip.IsLinkLocalUnicast()
}

func validateBridgeName(name string) error {
	if strings.TrimSpace(name) == "" || len([]byte(name)) > maxLinuxInterfaceNameBytes || name != strings.TrimSpace(name) || name == "." || name == ".." {
		return fmt.Errorf("%w: must be 1-%d bytes with no surrounding whitespace", errBridgeNameInvalid, maxLinuxInterfaceNameBytes)
	}
	for _, r := range name {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.') {
			return fmt.Errorf("%w: contains an invalid character", errBridgeNameInvalid)
		}
	}
	return nil
}

func firewallWarnings(ctx context.Context, env Environment) []string {
	data, err := readEnvironmentFile(env, "/proc/sys/net/bridge/bridge-nf-call-iptables")
	if err != nil || strings.TrimSpace(string(data)) != "1" || env.Runner == nil {
		return nil
	}
	output, err := env.Runner.Run(ctx, "iptables", "-S", "FORWARD")
	if err != nil {
		return []string{"bridge netfilter is enabled, but the iptables FORWARD policy could not be inspected"}
	}
	if !strings.Contains(string(output), "-P FORWARD DROP") {
		return nil
	}
	return []string{"bridge netfilter is enabled while the iptables FORWARD policy is DROP; VM traffic may be filtered"}
}

func verifyBridge(ctx context.Context, env Environment, bridge, member string) (bool, error) {
	if env.VerifyBridge != nil {
		return env.VerifyBridge(ctx, bridge, member)
	}
	bridgePath := filepath.Join("/sys/class/net", bridge, "bridge")
	if _, statErr := os.Stat(bridgePath); statErr != nil {
		return false, nil
	}
	masterPath := filepath.Join("/sys/class/net", member, "master")
	master, err := os.Readlink(masterPath)
	if err != nil || filepath.Base(master) != bridge {
		return false, nil
	}
	probes, err := interfaceProbes(ctx, env)
	if err != nil {
		return false, err
	}
	seenBridge := false
	seenMember := false
	for _, probe := range probes {
		if probe.Name != member && probe.Name != bridge {
			continue
		}
		seenBridge = seenBridge || probe.Name == bridge
		seenMember = seenMember || probe.Name == member
		for _, address := range probe.Addresses {
			if !isLinkLocalAddress(address) {
				return false, nil
			}
		}
		if probe.DefaultRoute {
			return false, nil
		}
	}
	return seenBridge && seenMember, nil
}

func waitForBridge(ctx context.Context, env Environment, bridge, member string) error {
	return pollUntil(ctx, 10*time.Second, 100*time.Millisecond, func() (bool, error) {
		return verifyBridge(ctx, env, bridge, member)
	}, fmt.Errorf("bridge %s was not created with member %s", bridge, member))
}

func runNetworkCommand(ctx context.Context, env Environment, name string, args ...string) error {
	if env.Runner == nil {
		return fmt.Errorf("network command runner is unavailable")
	}
	output, err := env.Runner.Run(ctx, name, args...)
	return utils.CommandOutputError(name, args, output, err)
}

func removeRuntimeBridge(ctx context.Context, env Environment, name string) error {
	if err := validateBridgeName(name); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if env.RemoveBridge != nil {
		return env.RemoveBridge(name)
	}
	link, err := netlink.LinkByName(name)
	if err != nil {
		if _, ok := errors.AsType[netlink.LinkNotFoundError](err); ok {
			return nil
		}
		return fmt.Errorf("find runtime bridge %s: %w", name, err)
	}
	if err := netlink.LinkDel(link); err != nil {
		return fmt.Errorf("delete runtime bridge %s: %w", name, err)
	}
	return nil
}

func restoreFileSnapshots(env Environment, snapshots []fileSnapshot) error {
	var firstErr error
	for _, snapshot := range snapshots {
		err := restoreFileSnapshot(env, snapshot)
		if err != nil && !errors.Is(err, os.ErrNotExist) && firstErr == nil {
			firstErr = fmt.Errorf("restore %s: %w", snapshot.path, err)
		}
	}
	return firstErr
}

func restoreFileSnapshot(env Environment, snapshot fileSnapshot) error {
	switch {
	case snapshot.exists && env.WriteFile == nil:
		return errors.New("network file writer is unavailable")
	case snapshot.exists:
		return env.WriteFile(snapshot.path, snapshot.data, snapshot.mode)
	case env.RemoveFile != nil:
		return env.RemoveFile(snapshot.path)
	default:
		return os.Remove(snapshot.path)
	}
}

type fileSnapshot struct {
	path   string
	data   []byte
	mode   os.FileMode
	exists bool
}

func snapshotFiles(env Environment, paths ...string) ([]fileSnapshot, error) {
	snapshots := make([]fileSnapshot, 0, len(paths))
	for _, path := range slices.Compact(paths) {
		data, err := readEnvironmentFile(env, path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				snapshots = append(snapshots, fileSnapshot{path: path})
				continue
			}
			return nil, fmt.Errorf("snapshot %s: %w", path, err)
		}
		snapshots = append(snapshots, fileSnapshot{path: path, data: data, mode: existingMode(path, 0o644), exists: true})
	}
	return snapshots, nil
}
