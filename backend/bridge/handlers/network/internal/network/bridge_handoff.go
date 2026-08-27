package network

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	godbus "github.com/godbus/dbus/v5"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/dbusclient"
)

const (
	BridgeHandoffConfirmationTimeout = 90 * time.Second
	handoffVerifyTimeout             = 20 * time.Second
	netplanHandoffCleanupTimeout     = 10 * time.Second
	networkdHandoffUnsupported       = "systemd-networkd has no transactional rollback; define the bridge in networkd configuration and LinuxIO will attach VMs to it"
)

var (
	startNetworkManagerHandoffNative   = startNetworkManagerHandoff
	confirmNetworkManagerHandoffNative = func(ctx context.Context, path godbus.ObjectPath) error {
		return (networkManagerMutation{checkpoint: path}).commit(ctx)
	}
	revertNetworkManagerHandoffNative = func(ctx context.Context, path godbus.ObjectPath) error {
		return (networkManagerMutation{checkpoint: path}).rollback(ctx)
	}
	startNetplanHandoffNative   = startNetplanHandoff
	confirmNetplanHandoffNative = applyNetplanConfig
	revertNetplanHandoffNative  = cancelNetplanConfig
)

type netplanConfigObject interface {
	CallWithContext(context.Context, string, godbus.Flags, ...any) *godbus.Call
}

func inspectBridgeHandoffCandidate(ctx context.Context, env Environment, probe InterfaceProbe) BridgeCandidate {
	candidate := BridgeCandidate{Name: probe.Name, MAC: probe.MAC}
	candidate.HandoffReasons = physicalBridgeLinkReasons(probe)
	if len(candidate.HandoffReasons) > 0 {
		return candidate
	}
	if len(nonLinkLocalAddresses(probe.Addresses)) == 0 && !probe.DefaultRoute {
		candidate.HandoffReasons = append(candidate.HandoffReasons, "interface does not carry host IP configuration")
	}
	backend, err := bridgeHandoffBackend(ctx, env, probe.Name)
	candidate.Backend = backend
	if err != nil {
		candidate.HandoffReasons = append(candidate.HandoffReasons, err.Error())
	}
	candidate.HandoffEligible = len(candidate.HandoffReasons) == 0 && backend != ""
	return candidate
}

func bridgeHandoffBackend(ctx context.Context, env Environment, iface string) (string, error) {
	owner, err := managerForInterface(ctx, env, iface)
	if err != nil {
		return "", err
	}
	netplan, err := detectNetplanBackend(env, iface)
	if err != nil {
		return "", fmt.Errorf("inspect Netplan configuration: %w", err)
	}
	if netplan != nil {
		typed, ok := netplan.(*netplanBackend)
		if !ok || typed.kind != "ethernets" {
			return bridgeBackendNetplan, unsupportedf("Netplan interface %s is not a physical Ethernet device", iface)
		}
		usesNetworkd, err := netplanUsesNetworkd(typed)
		if err != nil {
			return bridgeBackendNetplan, err
		}
		expectedOwner := bridgeBackendNetworkManager
		if usesNetworkd {
			expectedOwner = bridgeBackendNetworkd
		}
		if owner != expectedOwner {
			return bridgeBackendNetplan, fmt.Errorf("netplan renders %s through %s but runtime owner is %s", iface, expectedOwner, owner)
		}
		return bridgeBackendNetplan, nil
	}
	switch owner {
	case bridgeBackendNetworkManager:
		if err := validateNetworkManagerHandoff(ctx, iface); err != nil {
			return owner, err
		}
		return owner, nil
	case bridgeBackendNetworkd:
		return owner, errors.New(networkdHandoffUnsupported)
	default:
		return owner, fmt.Errorf("%w: %s owns %s", ErrUnsupportedBackend, owner, iface)
	}
}

func validateBridgeHandoffPlan(plan BridgeHandoffPlan) error {
	if err := validateBridgeName(plan.Name); err != nil {
		return err
	}
	if strings.TrimSpace(plan.Member) == "" {
		return fmt.Errorf("%w: member is required", errBridgeMember)
	}
	if plan.Name == plan.Member {
		return fmt.Errorf("%w: bridge and member must differ", errBridgeMember)
	}
	if !plan.ConsoleAcknowledged {
		return errors.New("console acknowledgement is required before a management IP handoff")
	}
	return nil
}

func PrepareBridgeHandoff(ctx context.Context, env Environment, plan BridgeHandoffPlan) (BridgeHandoffState, error) {
	if err := ctx.Err(); err != nil {
		return BridgeHandoffState{}, err
	}
	if err := validateBridgeHandoffPlan(plan); err != nil {
		return BridgeHandoffState{}, err
	}
	probes, err := interfaceProbes(ctx, env)
	if err != nil {
		return BridgeHandoffState{}, fmt.Errorf("inspect handoff interfaces: %w", err)
	}
	member, err := bridgeHandoffMember(probes, plan)
	if err != nil {
		return BridgeHandoffState{}, err
	}
	candidate := inspectBridgeHandoffCandidate(ctx, env, member)
	if !candidate.HandoffEligible {
		return BridgeHandoffState{}, fmt.Errorf("interface %s is not eligible for handoff: %s", plan.Member, strings.Join(candidate.HandoffReasons, "; "))
	}
	state := BridgeHandoffState{
		Plan:                 plan,
		Backend:              candidate.Backend,
		MemberMAC:            member.MAC,
		OriginalAddresses:    append([]string(nil), nonLinkLocalAddresses(member.Addresses)...),
		OriginalDefaultRoute: member.DefaultRoute,
	}
	if state.Backend == bridgeBackendNetplan {
		if _, _, err := netplanHandoffDeltas(env, &state); err != nil {
			return BridgeHandoffState{}, err
		}
	}
	return state, nil
}

func bridgeHandoffMember(probes []InterfaceProbe, plan BridgeHandoffPlan) (InterfaceProbe, error) {
	var member *InterfaceProbe
	for i := range probes {
		if probes[i].Name == plan.Name {
			return InterfaceProbe{}, fmt.Errorf("bridge %s already exists", plan.Name)
		}
		if probes[i].Name == plan.Member {
			member = &probes[i]
		}
	}
	if member == nil {
		return InterfaceProbe{}, fmt.Errorf("%w: interface %s was not found", errBridgeMember, plan.Member)
	}
	return *member, nil
}

func nonLinkLocalAddresses(addresses []string) []string {
	result := make([]string, 0, len(addresses))
	for _, address := range addresses {
		if !isLinkLocalAddress(address) {
			result = append(result, address)
		}
	}
	return result
}

func ApplyBridgeHandoff(ctx context.Context, env Environment, state *BridgeHandoffState) error {
	if err := validateHandoffState(state, false); err != nil {
		return err
	}
	var (
		path godbus.ObjectPath
		err  error
	)
	switch state.Backend {
	case bridgeBackendNetworkManager:
		path, err = startNetworkManagerHandoffNative(ctx, state.Plan, state.MemberMAC)
	case bridgeBackendNetplan:
		path, err = startNetplanHandoffNative(ctx, env, state)
	default:
		err = fmt.Errorf("%w: %s", ErrUnsupportedBackend, state.Backend)
	}
	if err != nil {
		return err
	}
	state.Handle = string(path)
	if err := waitForBridgeHandoff(ctx, env, state); err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), netplanHandoffCleanupTimeout)
		defer cancel()
		return errors.Join(err, RevertBridgeHandoff(cleanupCtx, state))
	}
	return nil
}

func ConfirmBridgeHandoff(ctx context.Context, env Environment, state *BridgeHandoffState) error {
	if err := validateHandoffState(state, true); err != nil {
		return err
	}
	ok, err := verifyBridgeHandoff(ctx, env, state)
	if err != nil {
		return fmt.Errorf("verify bridge handoff before confirmation: %w", err)
	}
	if !ok {
		return errors.New("bridge handoff verification failed")
	}
	path := godbus.ObjectPath(state.Handle)
	switch state.Backend {
	case bridgeBackendNetworkManager:
		return confirmNetworkManagerHandoffNative(ctx, path)
	case bridgeBackendNetplan:
		return confirmNetplanHandoffNative(ctx, path)
	default:
		return fmt.Errorf("%w: %s", ErrUnsupportedBackend, state.Backend)
	}
}

func RevertBridgeHandoff(ctx context.Context, state *BridgeHandoffState) error {
	if err := validateHandoffState(state, true); err != nil {
		return err
	}
	path := godbus.ObjectPath(state.Handle)
	switch state.Backend {
	case bridgeBackendNetworkManager:
		return revertNetworkManagerHandoffNative(ctx, path)
	case bridgeBackendNetplan:
		return revertNetplanHandoffNative(ctx, path)
	default:
		return fmt.Errorf("%w: %s", ErrUnsupportedBackend, state.Backend)
	}
}

func validateHandoffState(state *BridgeHandoffState, requireHandle bool) error {
	if state == nil {
		return errors.New("bridge handoff state is required")
	}
	if err := validateBridgeHandoffPlan(state.Plan); err != nil {
		return err
	}
	if state.Backend == "" || state.MemberMAC == "" || requireHandle && !godbus.ObjectPath(state.Handle).IsValid() {
		return errors.New("bridge handoff state is incomplete")
	}
	return nil
}

func startNetplanHandoff(ctx context.Context, env Environment, state *BridgeHandoffState) (godbus.ObjectPath, error) {
	memberDelta, bridgeDelta, err := netplanHandoffDeltas(env, state)
	if err != nil {
		return "", err
	}
	var configPath godbus.ObjectPath
	err = dbusclient.UseSystemBusWithOptions(ctx, dbusclient.SystemBusOptions{Subsystem: "netplan", NoRetry: true}, func(ctx context.Context, conn *godbus.Conn) error {
		root := conn.Object(netplanBusName, godbus.ObjectPath(netplanRootPath))
		if configErr := root.CallWithContext(ctx, netplanIface+".Config", 0).Store(&configPath); configErr != nil {
			return fmt.Errorf("create Netplan handoff transaction: %w", configErr)
		}
		config := conn.Object(netplanBusName, configPath)
		return configureNetplanHandoff(ctx, config, state, memberDelta, bridgeDelta)
	})
	if err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), netplanHandoffCleanupTimeout)
		defer cancel()
		return "", errors.Join(err, cancelNetplanConfig(cleanupCtx, configPath))
	}
	return configPath, nil
}

func configureNetplanHandoff(ctx context.Context, config netplanConfigObject, state *BridgeHandoffState, memberDelta, bridgeDelta []byte) error {
	origin := "90-linuxio-handoff-" + state.Plan.Name
	for _, delta := range []struct{ key, value string }{
		{"ethernets." + netplanPathSegment(state.Plan.Member), string(memberDelta)},
		{"bridges." + netplanPathSegment(state.Plan.Name), string(bridgeDelta)},
	} {
		var accepted bool
		if err := config.CallWithContext(ctx, netplanConfigIface+".Set", 0, delta.key+"="+delta.value, origin).Store(&accepted); err != nil {
			return fmt.Errorf("set Netplan handoff %s: %w", delta.key, err)
		}
		if !accepted {
			return fmt.Errorf("netplan rejected handoff setting %s", delta.key)
		}
	}
	var tried bool
	if err := config.CallWithContext(ctx, netplanConfigIface+".Try", 0, uint32(BridgeHandoffConfirmationTimeout/time.Second)).Store(&tried); err != nil {
		return fmt.Errorf("try Netplan handoff: %w", err)
	}
	if !tried {
		return errors.New("netplan rejected handoff transaction")
	}
	return nil
}

func netplanHandoffDeltas(env Environment, state *BridgeHandoffState) ([]byte, []byte, error) {
	backend, err := detectNetplanBackend(env, state.Plan.Member)
	if err != nil {
		return nil, nil, err
	}
	netplan, ok := backend.(*netplanBackend)
	if !ok || netplan == nil {
		return nil, nil, errors.New("netplan member configuration disappeared")
	}
	doc, err := netplan.load()
	if err != nil {
		return nil, nil, err
	}
	memberMap, err := doc.interfaceMap(netplan.kind, state.Plan.Member)
	if err != nil {
		return nil, nil, err
	}
	bridgeMap := cloneAnyMap(memberMap)
	for key := range bridgeMap {
		if !netplanL3Key(key) && key != "mtu" {
			delete(bridgeMap, key)
		}
	}
	memberMap = cloneAnyMap(memberMap)
	for key := range memberMap {
		if netplanL3Key(key) {
			memberMap[key] = nil
		}
	}
	memberMap["dhcp4"] = false
	memberMap["dhcp6"] = false
	memberMap["link-local"] = []string{}
	memberMap["accept-ra"] = false
	bridgeMap["interfaces"] = []string{state.Plan.Member}
	bridgeMap["macaddress"] = state.MemberMAC

	memberDelta, err := json.Marshal(memberMap)
	if err != nil {
		return nil, nil, err
	}
	bridgeDelta, err := json.Marshal(bridgeMap)
	if err != nil {
		return nil, nil, err
	}
	return memberDelta, bridgeDelta, nil
}

func netplanL3Key(key string) bool {
	switch key {
	case "dhcp4", "dhcp6", "dhcp-identifier", "dhcp4-overrides", "dhcp6-overrides",
		"addresses", "gateway4", "gateway6", "routes", "routing-policy", "nameservers",
		"link-local", "accept-ra", "ra-overrides", "ipv6-address-generation", "ipv6-address-token",
		"ipv6-privacy", "ipv6-mtu", "optional-addresses":
		return true
	default:
		return false
	}
}

func cloneAnyMap(source map[string]any) map[string]any {
	result := make(map[string]any, len(source))
	maps.Copy(result, source)
	return result
}

func waitForBridgeHandoff(ctx context.Context, env Environment, state *BridgeHandoffState) error {
	return pollUntil(ctx, handoffVerifyTimeout, 100*time.Millisecond, func() (bool, error) {
		return verifyBridgeHandoff(ctx, env, state)
	}, errors.New("bridge handoff verification timed out"))
}

func verifyBridgeHandoff(ctx context.Context, env Environment, state *BridgeHandoffState) (bool, error) {
	if env.VerifyBridgeHandoff != nil {
		return env.VerifyBridgeHandoff(ctx, state)
	}
	return defaultVerifyBridgeHandoff(ctx, env, state)
}

func defaultVerifyBridgeHandoff(ctx context.Context, env Environment, state *BridgeHandoffState) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	linkPresent, err := handoffLinkPresent(ctx, env, state.Plan.Name, state.Plan.Member)
	if err != nil || !linkPresent {
		return linkPresent, err
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		return false, err
	}
	bridge, member := handoffInterfaces(interfaces, state.Plan.Name, state.Plan.Member)
	if bridge == nil || member == nil || !strings.EqualFold(bridge.HardwareAddr.String(), state.MemberMAC) {
		return false, nil
	}
	addressesOK, err := handoffAddressesPresent(bridge, member, state.OriginalAddresses)
	if err != nil || !addressesOK {
		return addressesOK, err
	}
	if !state.OriginalDefaultRoute {
		return true, nil
	}
	return handoffDefaultRoutePresent(ctx, env, state.Plan.Name)
}

func handoffLinkPresent(ctx context.Context, env Environment, bridge, member string) (bool, error) {
	if env.VerifyBridge != nil {
		return env.VerifyBridge(ctx, bridge, member)
	}
	if _, err := os.Stat(filepath.Join("/sys/class/net", bridge, "bridge")); err != nil {
		return false, nil
	}
	master, err := os.Readlink(filepath.Join("/sys/class/net", member, "master"))
	return err == nil && filepath.Base(master) == bridge, nil
}

func handoffInterfaces(interfaces []net.Interface, bridgeName, memberName string) (*net.Interface, *net.Interface) {
	var bridge, member *net.Interface
	for i := range interfaces {
		switch interfaces[i].Name {
		case bridgeName:
			bridge = &interfaces[i]
		case memberName:
			member = &interfaces[i]
		}
	}
	return bridge, member
}

func handoffAddressesPresent(bridge, member *net.Interface, expected []string) (bool, error) {
	bridgeAddrs, err := bridge.Addrs()
	if err != nil {
		return false, err
	}
	memberAddrs, err := member.Addrs()
	if err != nil {
		return false, err
	}
	for _, address := range memberAddrs {
		if !isLinkLocalAddress(address.String()) {
			return false, nil
		}
	}
	got := make(map[string]bool, len(bridgeAddrs))
	for _, address := range bridgeAddrs {
		got[address.String()] = true
	}
	for _, address := range expected {
		if !got[address] {
			return false, nil
		}
	}
	return true, nil
}

func handoffDefaultRoutePresent(ctx context.Context, env Environment, bridgeName string) (bool, error) {
	probes, err := interfaceProbes(ctx, env)
	if err != nil {
		return false, err
	}
	for _, probe := range probes {
		if probe.Name == bridgeName {
			return probe.DefaultRoute, nil
		}
	}
	return false, nil
}
