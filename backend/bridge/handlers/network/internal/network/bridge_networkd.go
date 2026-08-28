package network

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	ini "gopkg.in/ini.v1"
)

func createNetworkdBridge(ctx context.Context, env Environment, plan BridgePlan, memberMAC string) (BridgeResult, error) {
	if env.WriteFile == nil {
		return BridgeResult{}, errors.New("network file writer is unavailable")
	}
	bridgeNetdev := filepath.Join(env.NetworkdDir, "90-linuxio-"+plan.Name+".netdev")
	bridgeNetwork := filepath.Join(env.NetworkdDir, "90-linuxio-"+plan.Name+".network")
	memberPath := filepath.Join(env.NetworkdDir, "90-linuxio-"+plan.Name+"-member.network")
	memberPathOwned := false
	if backend, err := detectNetworkdBackend(env, plan.Member); err != nil {
		return BridgeResult{}, err
	} else if backend != nil {
		networkd, ok := backend.(*networkdBackend)
		if !ok {
			return BridgeResult{}, errors.New("invalid systemd-networkd backend implementation")
		}
		memberPath = networkd.path
		memberPathOwned = true
	}
	snapshots, err := snapshotFiles(env, bridgeNetdev, bridgeNetwork, memberPath)
	if err != nil {
		return BridgeResult{}, err
	}
	conflicts := []string{bridgeNetdev, bridgeNetwork}
	if !memberPathOwned {
		conflicts = append(conflicts, memberPath)
	}
	if slices.ContainsFunc(snapshots, func(snapshot fileSnapshot) bool {
		return snapshot.exists && slices.Contains(conflicts, snapshot.path)
	}) {
		return BridgeResult{}, fmt.Errorf("networkd bridge configuration already exists for %s", plan.Name)
	}
	memberData, err := networkdMemberConfig(env, memberPath, plan.Member, plan.Name)
	if err != nil {
		return BridgeResult{}, err
	}
	bridgeData := networkdBridgeConfig(plan.Name)
	devData := networkdNetdevConfig(plan.Name, memberMAC)
	for _, file := range []struct {
		path string
		data []byte
	}{
		{bridgeNetdev, devData},
		{bridgeNetwork, bridgeData},
		{memberPath, memberData},
	} {
		if err := env.WriteFile(file.path, file.data, existingMode(file.path, 0o644)); err != nil {
			restoreErr := restoreFileSnapshots(env, snapshots)
			writeErr := fmt.Errorf("write networkd configuration %s: %w", file.path, err)
			if restoreErr != nil {
				return BridgeResult{}, fmt.Errorf("%w; restore failed: %v", writeErr, restoreErr)
			}
			return BridgeResult{}, writeErr
		}
	}

	applyErr := applyNetworkdBridge(ctx, env, plan)
	if applyErr == nil {
		applyErr = waitForBridge(ctx, env, plan.Name, plan.Member)
	}
	if applyErr == nil {
		return BridgeResult{Name: plan.Name, Member: plan.Member, Backend: bridgeBackendNetworkd}, nil
	}
	operationErr := fmt.Errorf("apply networkd bridge: %w", applyErr)
	if rollbackErr := rollbackNetworkdBridge(ctx, env, plan, snapshots); rollbackErr != nil {
		return BridgeResult{}, fmt.Errorf("%w; rollback failed: %v", operationErr, rollbackErr)
	}
	return BridgeResult{}, operationErr
}

func rollbackNetworkdBridge(ctx context.Context, env Environment, plan BridgePlan, snapshots []fileSnapshot) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	return errors.Join(
		restoreFileSnapshots(env, snapshots),
		removeRuntimeBridge(cleanupCtx, env, plan.Name),
		runNetworkCommand(cleanupCtx, env, "networkctl", "reload"),
		runNetworkCommand(cleanupCtx, env, "networkctl", "reconfigure", plan.Member),
	)
}

func networkdNetdevConfig(name, mac string) []byte {
	var builder strings.Builder
	builder.WriteString("[NetDev]\nName=")
	builder.WriteString(name)
	builder.WriteString("\nKind=bridge\n")
	if strings.TrimSpace(mac) != "" {
		builder.WriteString("MACAddress=")
		builder.WriteString(mac)
		builder.WriteString("\n")
	}
	builder.WriteString("\n[Bridge]\n")
	return []byte(builder.String())
}

func networkdBridgeConfig(name string) []byte {
	return []byte("[Match]\nName=" + name + "\n\n[Network]\nDHCP=no\nConfigureWithoutCarrier=yes\n")
}

func networkdMemberConfig(env Environment, path, member, bridge string) ([]byte, error) {
	var cfg *ini.File
	if data, err := readEnvironmentFile(env, path); err == nil {
		parsed, parseErr := loadINI(data)
		if parseErr != nil {
			return nil, fmt.Errorf("parse networkd member configuration %s: %w", path, parseErr)
		}
		cfg = parsed
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	} else {
		cfg = ini.Empty()
		cfg.Section("Match").Key("Name").SetValue(member)
	}
	match := cfg.Section("Match")
	if strings.TrimSpace(match.Key("Name").String()) == "" {
		match.Key("Name").SetValue(member)
	}
	network := cfg.Section("Network")
	network.Key("Bridge").SetValue(bridge)
	// Stage 2a only accepts a member with no L3 configuration. Explicitly
	// disabling DHCP in the generated member profile prevents a manager from
	// acquiring an address after the bridge is attached.
	network.Key("DHCP").SetValue("no")
	for _, key := range []string{"Address", "Gateway", "DNS"} {
		network.DeleteKey(key)
	}
	cfg.DeleteSection("Address")
	cfg.DeleteSection("Route")
	return renderINI(cfg)
}

func applyNetworkdBridge(ctx context.Context, env Environment, plan BridgePlan) error {
	if err := runNetworkCommand(ctx, env, "networkctl", "reload"); err != nil {
		return err
	}
	if err := runNetworkCommand(ctx, env, "networkctl", "reconfigure", plan.Name); err != nil {
		return err
	}
	return runNetworkCommand(ctx, env, "networkctl", "reconfigure", plan.Member)
}
