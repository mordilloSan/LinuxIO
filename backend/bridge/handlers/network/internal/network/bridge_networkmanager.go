package network

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"time"
	"uuid"

	godbus "github.com/godbus/dbus/v5"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/dbusclient"
)

const (
	networkManagerBusName             = "org.freedesktop.NetworkManager"
	networkManagerPath                = "/org/freedesktop/NetworkManager"
	networkManagerIface               = "org.freedesktop.NetworkManager"
	networkManagerDeviceIface         = "org.freedesktop.NetworkManager.Device"
	networkManagerActiveConnection    = "org.freedesktop.NetworkManager.Connection.Active"
	networkManagerSettingsBusName     = "org.freedesktop.NetworkManager"
	networkManagerSettingsPath        = "/org/freedesktop/NetworkManager/Settings"
	networkManagerSettingsIface       = "org.freedesktop.NetworkManager.Settings"
	networkManagerSettingsConnection  = "org.freedesktop.NetworkManager.Settings.Connection"
	networkManagerCheckpointTimeout   = uint32(BridgeHandoffConfirmationTimeout / time.Second)
	networkManagerCheckpointFlags     = uint32(0x02 | 0x04) // delete new profiles and disconnect new devices on rollback
	networkManagerAutoconnectPriority = int32(1<<31 - 1)
)

func createNetworkManagerBridge(ctx context.Context, env Environment, plan BridgePlan) (BridgeResult, error) {
	mutation, err := addNetworkManagerBridge(ctx, plan)
	if err != nil {
		return BridgeResult{}, err
	}
	if err := waitForBridge(ctx, env, plan.Name, plan.Member); err != nil {
		return BridgeResult{}, networkManagerBridgeFailure(ctx, fmt.Errorf("verify NetworkManager bridge: %w", err), env, plan.Name, mutation)
	}
	if err := mutation.commit(ctx); err != nil {
		return BridgeResult{}, networkManagerBridgeFailure(ctx, fmt.Errorf("commit NetworkManager checkpoint: %w", err), env, plan.Name, mutation)
	}
	return BridgeResult{Name: plan.Name, Member: plan.Member, Backend: bridgeBackendNetworkManager}, nil
}

func networkManagerBridgeFailure(ctx context.Context, operationErr error, env Environment, bridge string, mutation networkManagerMutation) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	cleanupErr := errors.Join(mutation.rollback(cleanupCtx), removeRuntimeBridge(cleanupCtx, env, bridge))
	if cleanupErr != nil {
		return fmt.Errorf("%w; NetworkManager cleanup failed: %v", operationErr, cleanupErr)
	}
	return operationErr
}

type networkManagerMutation struct {
	checkpoint godbus.ObjectPath
}

func addNetworkManagerBridge(ctx context.Context, plan BridgePlan) (networkManagerMutation, error) {
	memberDevice, err := networkManagerDevice(ctx, plan.Member)
	if err != nil {
		return networkManagerMutation{}, err
	}
	return addNetworkManagerProfiles(ctx, memberDevice, networkManagerBridgeSettings(plan.Name, uuid.NewV4().String()), networkManagerSlaveSettings(plan.Name, plan.Member, uuid.NewV4().String()))
}

func addNetworkManagerProfiles(ctx context.Context, memberDevice godbus.ObjectPath, bridgeSettings, portSettings map[string]map[string]godbus.Variant) (networkManagerMutation, error) {
	var mutation networkManagerMutation
	err := dbusclient.UseSystemBusWithOptions(ctx, dbusclient.SystemBusOptions{Subsystem: "network-manager", NoRetry: true}, func(ctx context.Context, conn *godbus.Conn) error {
		manager := conn.Object(networkManagerBusName, godbus.ObjectPath(networkManagerPath))
		if err := manager.CallWithContext(ctx, networkManagerIface+".CheckpointCreate", 0, []godbus.ObjectPath{memberDevice}, networkManagerCheckpointTimeout, networkManagerCheckpointFlags).Store(&mutation.checkpoint); err != nil {
			return fmt.Errorf("create NetworkManager checkpoint: %w", err)
		}
		settings := conn.Object(networkManagerSettingsBusName, godbus.ObjectPath(networkManagerSettingsPath))
		bridgePath, err := addNetworkManagerProfile(ctx, settings, bridgeSettings)
		if err != nil {
			return err
		}
		portPath, err := addNetworkManagerProfile(ctx, settings, portSettings)
		if err != nil {
			return err
		}
		var active godbus.ObjectPath
		if err := manager.CallWithContext(ctx, networkManagerIface+".ActivateConnection", 0, bridgePath, godbus.ObjectPath("/"), godbus.ObjectPath("/")).Store(&active); err != nil {
			return fmt.Errorf("activate NetworkManager bridge profile: %w", err)
		}
		if err := manager.CallWithContext(ctx, networkManagerIface+".ActivateConnection", 0, portPath, memberDevice, godbus.ObjectPath("/")).Store(&active); err != nil {
			return fmt.Errorf("activate NetworkManager member profile: %w", err)
		}
		return nil
	})
	if err == nil {
		return mutation, nil
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if rollbackErr := mutation.rollback(cleanupCtx); rollbackErr != nil {
		return networkManagerMutation{}, fmt.Errorf("%w; rollback failed: %v", err, rollbackErr)
	}
	return networkManagerMutation{}, err
}

func networkManagerDevice(ctx context.Context, member string) (godbus.ObjectPath, error) {
	var memberDevice godbus.ObjectPath
	err := dbusclient.DBus.UseSessionWithOptions(ctx, dbusclient.SystemBusOptions{Subsystem: "network-manager", NoRetry: true}, func(session dbusclient.SystemSession) error {
		info, found, err := readNetworkManagerDevice(session, member)
		if err != nil {
			return err
		}
		if !found || !info.managed || info.deviceType != networkManagerEthernetDevice {
			return fmt.Errorf("NetworkManager does not manage %s as an Ethernet device", member)
		}
		if info.activeConnection != "" && info.activeConnection != "/" {
			return fmt.Errorf("NetworkManager has an active connection on %s", member)
		}
		if len(info.availableConnections) > 0 {
			return fmt.Errorf("NetworkManager has existing connection profiles for %s", member)
		}
		memberDevice = info.path
		return nil
	})
	return memberDevice, err
}

func addNetworkManagerProfile(ctx context.Context, settings godbus.BusObject, values map[string]map[string]godbus.Variant) (godbus.ObjectPath, error) {
	var path godbus.ObjectPath
	var result map[string]godbus.Variant
	if err := settings.CallWithContext(ctx, networkManagerSettingsIface+".AddConnection2", 0, values, uint32(1), map[string]godbus.Variant{}).Store(&path, &result); err != nil {
		return "", fmt.Errorf("add NetworkManager connection profile: %w", err)
	}
	return path, nil
}

func networkManagerBridgeSettings(name, profileUUID string) map[string]map[string]godbus.Variant {
	return map[string]map[string]godbus.Variant{
		"connection": {
			"id":                 godbus.MakeVariant(name),
			"uuid":               godbus.MakeVariant(profileUUID),
			"type":               godbus.MakeVariant("bridge"),
			"interface-name":     godbus.MakeVariant(name),
			"autoconnect":        godbus.MakeVariant(true),
			"autoconnect-slaves": godbus.MakeVariant(int32(1)),
		},
		"bridge": {},
		"ipv4":   {"method": godbus.MakeVariant("disabled")},
		"ipv6":   {"method": godbus.MakeVariant("disabled")},
	}
}

func networkManagerSlaveSettings(bridge, member, profileUUID string) map[string]map[string]godbus.Variant {
	return map[string]map[string]godbus.Variant{
		"connection": {
			"id":             godbus.MakeVariant(bridge + "-" + member),
			"uuid":           godbus.MakeVariant(profileUUID),
			"type":           godbus.MakeVariant("802-3-ethernet"),
			"interface-name": godbus.MakeVariant(member),
			"master":         godbus.MakeVariant(bridge),
			"slave-type":     godbus.MakeVariant("bridge"),
			"autoconnect":    godbus.MakeVariant(true),
		},
		"802-3-ethernet": {},
		"ipv4":           {"method": godbus.MakeVariant("disabled")},
		"ipv6":           {"method": godbus.MakeVariant("disabled")},
	}
}

type networkManagerHandoffSource struct {
	device   godbus.ObjectPath
	settings map[string]map[string]godbus.Variant
}

func validateNetworkManagerHandoff(ctx context.Context, member string) error {
	return dbusclient.DBus.UseSessionWithOptions(ctx, dbusclient.SystemBusOptions{Subsystem: "network-manager", NoRetry: true}, func(session dbusclient.SystemSession) error {
		_, err := readNetworkManagerHandoffSource(session, member)
		return err
	})
}

func startNetworkManagerHandoff(ctx context.Context, plan BridgeHandoffPlan, memberMAC string) (godbus.ObjectPath, error) {
	var mutation networkManagerMutation
	err := dbusclient.DBus.UseSessionWithOptions(ctx, dbusclient.SystemBusOptions{Subsystem: "network-manager", NoRetry: true}, func(session dbusclient.SystemSession) error {
		source, err := readNetworkManagerHandoffSource(session, plan.Member)
		if err != nil {
			return err
		}
		manager := session.ObjectFor(networkManagerBusName, godbus.ObjectPath(networkManagerPath))
		if checkpointErr := manager.CallWithContext(session.Context(), networkManagerIface+".CheckpointCreate", 0, []godbus.ObjectPath{source.device}, networkManagerCheckpointTimeout, networkManagerCheckpointFlags).Store(&mutation.checkpoint); checkpointErr != nil {
			return fmt.Errorf("create NetworkManager handoff checkpoint: %w", checkpointErr)
		}
		bridge, port := networkManagerHandoffSettings(plan, memberMAC, source.settings)
		settings := session.ObjectFor(networkManagerSettingsBusName, godbus.ObjectPath(networkManagerSettingsPath))
		bridgePath, err := addNetworkManagerProfile(session.Context(), settings, bridge)
		if err != nil {
			return err
		}
		portPath, err := addNetworkManagerProfile(session.Context(), settings, port)
		if err != nil {
			return err
		}
		var active godbus.ObjectPath
		if err := manager.CallWithContext(session.Context(), networkManagerIface+".ActivateConnection", 0, bridgePath, godbus.ObjectPath("/"), godbus.ObjectPath("/")).Store(&active); err != nil {
			return fmt.Errorf("activate NetworkManager handoff bridge: %w", err)
		}
		if err := manager.CallWithContext(session.Context(), networkManagerIface+".ActivateConnection", 0, portPath, source.device, godbus.ObjectPath("/")).Store(&active); err != nil {
			return fmt.Errorf("activate NetworkManager handoff port: %w", err)
		}
		return nil
	})
	if err == nil {
		return mutation.checkpoint, nil
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	return "", errors.Join(err, mutation.rollback(cleanupCtx))
}

func readNetworkManagerHandoffSource(session dbusclient.SystemSession, member string) (networkManagerHandoffSource, error) {
	info, found, err := readNetworkManagerDevice(session, member)
	if err != nil {
		return networkManagerHandoffSource{}, err
	}
	if !found || !info.managed || info.deviceType != networkManagerEthernetDevice {
		return networkManagerHandoffSource{}, fmt.Errorf("NetworkManager does not manage %s as an Ethernet device", member)
	}
	if info.activeConnection == "" || info.activeConnection == "/" {
		return networkManagerHandoffSource{}, fmt.Errorf("NetworkManager has no active connection on %s", member)
	}
	active := session.ObjectFor(networkManagerBusName, info.activeConnection)
	connectionPath, err := dbusclient.GetProperty[godbus.ObjectPath](session, active, networkManagerActiveConnection, "Connection")
	if err != nil {
		return networkManagerHandoffSource{}, fmt.Errorf("read active NetworkManager profile for %s: %w", member, err)
	}
	var settings map[string]map[string]godbus.Variant
	connection := session.ObjectFor(networkManagerSettingsBusName, connectionPath)
	if err := connection.CallWithContext(session.Context(), networkManagerSettingsConnection+".GetSettings", 0).Store(&settings); err != nil {
		return networkManagerHandoffSource{}, fmt.Errorf("read NetworkManager settings for %s: %w", member, err)
	}
	if err := validateNetworkManagerHandoffSettings(settings); err != nil {
		return networkManagerHandoffSource{}, err
	}
	return networkManagerHandoffSource{device: info.path, settings: settings}, nil
}

func validateNetworkManagerHandoffSettings(settings map[string]map[string]godbus.Variant) error {
	connection, ok := settings["connection"]
	if !ok || variantString(connection["type"]) != "802-3-ethernet" {
		return unsupportedf("active NetworkManager profile is not a wired Ethernet connection")
	}
	if _, ok := settings["802-1x"]; ok {
		return unsupportedf("802.1X NetworkManager profiles cannot be moved because their secrets are not available")
	}
	for setting := range settings {
		switch setting {
		case "connection", "802-3-ethernet", "ipv4", "ipv6":
		default:
			return unsupportedf("NetworkManager setting %q cannot be copied safely", setting)
		}
	}
	for _, setting := range []string{"802-3-ethernet", "ipv4", "ipv6"} {
		if _, ok := settings[setting]; !ok {
			return unsupportedf("active NetworkManager profile is missing its %s settings", setting)
		}
	}
	if err := validateNetworkManagerHandoffConnection(connection); err != nil {
		return err
	}
	if err := validateNetworkManagerHandoffIPv6(settings); err != nil {
		return err
	}
	if err := validateNetworkManagerHandoffIPv4(settings); err != nil {
		return err
	}
	assignedMAC := strings.ToLower(strings.TrimSpace(variantString(settings["802-3-ethernet"]["assigned-mac-address"])))
	clonedMAC := strings.ToLower(strings.TrimSpace(variantString(settings["802-3-ethernet"]["cloned-mac-address"])))
	if assignedMAC == "stable" || assignedMAC == "random" || clonedMAC == "stable" || clonedMAC == "random" {
		return unsupportedf("NetworkManager member profile uses a generated MAC address")
	}
	return nil
}

func validateNetworkManagerHandoffConnection(connection map[string]godbus.Variant) error {
	for key, value := range connection {
		switch key {
		case "id", "uuid", "type", "interface-name", "autoconnect", "autoconnect-priority", "timestamp", "read-only", "autoconnect-slaves", "autoconnect-ports":
		case "master", "controller", "slave-type", "port-type", "secondaries":
		case "auth-retries", "autoconnect-retries", "dns-over-tls", "dnssec", "down-on-poweroff", "gateway-ping-timeout", "ip-ping-addresses", "ip-ping-addresses-require-all", "ip-ping-timeout", "lldp", "llmnr", "mdns", "metered", "mptcp-flags", "multi-connect", "mud-url", "permissions", "stable-id", "zone", "wait-activation-delay", "wait-device-timeout":
		default:
			return unsupportedf("NetworkManager connection setting %q cannot be copied safely", key)
		}
		if key == "zone" || key == "stable-id" {
			if _, ok := value.Value().(string); !ok {
				return unsupportedf("NetworkManager connection setting %q has an unsupported value", key)
			}
		}
	}
	if variantString(connection["master"]) != "" || variantString(connection["controller"]) != "" {
		return unsupportedf("active NetworkManager profile is already a port")
	}
	if values, ok := connection["secondaries"]; ok {
		secondaries, valid := values.Value().([]string)
		if !valid {
			return unsupportedf("NetworkManager secondary connection list has an unsupported value")
		}
		if len(secondaries) > 0 {
			return unsupportedf("NetworkManager profiles with secondary connections cannot be moved")
		}
	}
	return nil
}

func validateNetworkManagerHandoffIPv6(settings map[string]map[string]godbus.Variant) error {
	ipv6 := settings["ipv6"]
	method := strings.ToLower(strings.TrimSpace(variantString(ipv6["method"])))
	if method != "auto" && method != "dhcp" {
		return nil
	}
	addrGen, ok := ipv6["addr-gen-mode"].Value().(int32)
	if !ok || addrGen != 0 {
		return unsupportedf("NetworkManager dynamic IPv6 uses interface-dependent address generation; require explicit eui64")
	}
	privacy, ok := ipv6["ip6-privacy"].Value().(int32)
	if !ok || privacy != 0 {
		return unsupportedf("NetworkManager dynamic IPv6 privacy extensions cannot be preserved across the bridge rename")
	}
	duid, hasDUID := ipv6["dhcp-duid"]
	if !hasDUID || strings.TrimSpace(variantString(duid)) == "" {
		return unsupportedf("NetworkManager dynamic IPv6 has an implicit DHCPv6 lease identity that cannot be moved to the bridge")
	}
	iaid, hasIAID := ipv6["dhcp-iaid"]
	if !hasIAID || strings.TrimSpace(variantString(iaid)) == "" {
		return unsupportedf("NetworkManager dynamic IPv6 has an implicit interface-name DHCPv6 identity")
	}
	if networkManagerStableIDDependsOnRenamedContext(settings) {
		return unsupportedf("NetworkManager dynamic IPv6 identity depends on the source interface or connection")
	}
	duidValue := strings.ToLower(strings.TrimSpace(variantString(duid)))
	if duidValue == "lease" {
		return unsupportedf("NetworkManager DHCPv6 lease identity cannot be moved to the bridge")
	}
	if strings.HasPrefix(duidValue, "stable-") && strings.TrimSpace(variantString(settings["connection"]["stable-id"])) == "" {
		return unsupportedf("NetworkManager stable DHCPv6 identity requires an explicit connection.stable-id")
	}
	iaidValue := strings.ToLower(strings.TrimSpace(variantString(iaid)))
	if iaidValue == "ifname" || iaidValue == "stable" || iaidValue == "perm-mac" {
		return unsupportedf("NetworkManager DHCPv6 identity depends on the source interface")
	}
	return nil
}

func validateNetworkManagerHandoffIPv4(settings map[string]map[string]godbus.Variant) error {
	ipv4 := settings["ipv4"]
	method := strings.ToLower(strings.TrimSpace(variantString(ipv4["method"])))
	if method != "auto" && method != "dhcp" {
		return nil
	}
	clientID := strings.ToLower(strings.TrimSpace(variantString(ipv4["dhcp-client-id"])))
	if clientID == "stable" {
		if strings.TrimSpace(variantString(settings["connection"]["stable-id"])) == "" {
			return unsupportedf("NetworkManager stable DHCP identity requires an explicit connection.stable-id")
		}
		if networkManagerStableIDDependsOnRenamedContext(settings) {
			return unsupportedf("NetworkManager DHCP identity depends on the source interface or connection")
		}
	}
	if clientID != "duid" && clientID != "ipv6-duid" {
		return nil
	}
	iaid, hasIAID := ipv4["dhcp-iaid"]
	if !hasIAID || strings.TrimSpace(variantString(iaid)) == "" {
		return unsupportedf("NetworkManager DHCP identity has an implicit interface-name IAID")
	}
	iaidValue := strings.ToLower(strings.TrimSpace(variantString(iaid)))
	if iaidValue == "ifname" || iaidValue == "stable" || iaidValue == "perm-mac" {
		return unsupportedf("NetworkManager DHCP identity depends on the source interface")
	}
	return nil
}

func networkManagerStableIDDependsOnRenamedContext(settings map[string]map[string]godbus.Variant) bool {
	stableID := variantString(settings["connection"]["stable-id"])
	return strings.Contains(stableID, "${DEVICE}") || strings.Contains(stableID, "${MAC}") || strings.Contains(stableID, "${CONNECTION}") || strings.Contains(stableID, "${NETWORK_SSID}") || strings.Contains(stableID, "${RANDOM}")
}

func networkManagerHandoffSettings(plan BridgeHandoffPlan, memberMAC string, source map[string]map[string]godbus.Variant) (map[string]map[string]godbus.Variant, map[string]map[string]godbus.Variant) {
	bridge := networkManagerBridgeSettings(plan.Name, uuid.NewV4().String())
	bridge["connection"]["autoconnect-priority"] = godbus.MakeVariant(networkManagerAutoconnectPriority)
	bridge["802-3-ethernet"] = map[string]godbus.Variant{"cloned-mac-address": godbus.MakeVariant(memberMAC)}
	bridge["ipv4"] = maps.Clone(source["ipv4"])
	bridge["ipv6"] = maps.Clone(source["ipv6"])
	for _, key := range []string{
		"auth-retries", "autoconnect-retries", "dns-over-tls", "dnssec", "down-on-poweroff", "gateway-ping-timeout",
		"ip-ping-addresses", "ip-ping-addresses-require-all", "ip-ping-timeout",
		"lldp", "llmnr", "mdns", "metered", "mptcp-flags", "multi-connect", "mud-url",
		"permissions", "stable-id", "zone", "wait-activation-delay", "wait-device-timeout",
	} {
		if value, ok := source["connection"][key]; ok {
			bridge["connection"][key] = value
		}
	}
	port := networkManagerSlaveSettings(plan.Name, plan.Member, uuid.NewV4().String())
	port["connection"]["autoconnect-priority"] = godbus.MakeVariant(networkManagerAutoconnectPriority)
	port["802-3-ethernet"] = maps.Clone(source["802-3-ethernet"])
	return bridge, port
}

func variantString(value godbus.Variant) string {
	result, _ := value.Value().(string)
	return result
}

func (m networkManagerMutation) commit(ctx context.Context) error {
	if m.checkpoint == "" {
		return nil
	}
	return dbusclient.UseSystemBusWithOptions(ctx, dbusclient.SystemBusOptions{Subsystem: "network-manager", NoRetry: true}, func(ctx context.Context, conn *godbus.Conn) error {
		manager := conn.Object(networkManagerBusName, godbus.ObjectPath(networkManagerPath))
		return manager.CallWithContext(ctx, networkManagerIface+".CheckpointDestroy", 0, m.checkpoint).Err
	})
}

func (m networkManagerMutation) rollback(ctx context.Context) error {
	if m.checkpoint == "" {
		return nil
	}
	return dbusclient.UseSystemBusWithOptions(ctx, dbusclient.SystemBusOptions{Subsystem: "network-manager", NoRetry: true}, func(ctx context.Context, conn *godbus.Conn) error {
		manager := conn.Object(networkManagerBusName, godbus.ObjectPath(networkManagerPath))
		var results map[string]uint32
		if err := manager.CallWithContext(ctx, networkManagerIface+".CheckpointRollback", 0, m.checkpoint).Store(&results); err != nil {
			return fmt.Errorf("rollback NetworkManager checkpoint: %w", err)
		}
		for device, result := range results {
			if result != 0 {
				return fmt.Errorf("rollback NetworkManager checkpoint failed for %s (result %d)", device, result)
			}
		}
		return nil
	})
}
