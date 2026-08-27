package network

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	godbus "github.com/godbus/dbus/v5"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/dbusclient"
)

const (
	netplanBusName     = "io.netplan.Netplan"
	netplanRootPath    = "/io/netplan/Netplan"
	netplanIface       = "io.netplan.Netplan"
	netplanConfigIface = "io.netplan.Netplan.Config"
)

func createNetplanBridge(ctx context.Context, env Environment, plan BridgePlan) (BridgeResult, error) {
	backend, err := detectNetplanBackend(env, plan.Member)
	if err != nil {
		return BridgeResult{}, err
	}
	if backend == nil {
		return BridgeResult{}, fmt.Errorf("netplan no longer owns %s", plan.Member)
	}

	var configPath godbus.ObjectPath
	runtimeChanged := false
	err = dbusclient.UseSystemBusWithOptions(ctx, dbusclient.SystemBusOptions{
		Subsystem: "netplan",
		NoRetry:   true,
	}, func(ctx context.Context, conn *godbus.Conn) error {
		root := conn.Object(netplanBusName, godbus.ObjectPath(netplanRootPath))
		if callErr := root.CallWithContext(ctx, netplanIface+".Config", 0).Store(&configPath); callErr != nil {
			return fmt.Errorf("create netplan config transaction: %w", callErr)
		}
		config := conn.Object(netplanBusName, configPath)
		delta, deltaErr := netplanBridgeDelta(plan)
		if deltaErr != nil {
			return deltaErr
		}
		var set bool
		if callErr := config.CallWithContext(ctx, netplanConfigIface+".Set", 0, delta, "90-linuxio-"+plan.Name).Store(&set); callErr != nil {
			return fmt.Errorf("set netplan bridge configuration: %w", callErr)
		}
		if !set {
			return errors.New("netplan rejected bridge configuration")
		}
		var tried bool
		if callErr := config.CallWithContext(ctx, netplanConfigIface+".Try", 0, uint32(20)).Store(&tried); callErr != nil {
			return fmt.Errorf("try netplan bridge configuration: %w", callErr)
		}
		if !tried {
			return errors.New("netplan could not apply bridge configuration")
		}
		runtimeChanged = true
		return nil
	})
	if err != nil {
		return BridgeResult{}, netplanBridgeFailure(ctx, err, env, configPath, plan.Name, runtimeChanged)
	}

	if err := waitForBridge(ctx, env, plan.Name, plan.Member); err != nil {
		return BridgeResult{}, netplanBridgeFailure(ctx, fmt.Errorf("verify Netplan bridge: %w", err), env, configPath, plan.Name, true)
	}
	if err := applyNetplanConfig(ctx, configPath); err != nil {
		return BridgeResult{}, netplanBridgeFailure(ctx, fmt.Errorf("accept Netplan bridge configuration: %w", err), env, configPath, plan.Name, true)
	}
	return BridgeResult{Name: plan.Name, Member: plan.Member, Backend: bridgeBackendNetplan}, nil
}

func netplanBridgeDelta(plan BridgePlan) (string, error) {
	value, err := json.Marshal(map[string]any{
		"interfaces": []string{plan.Member},
		"dhcp4":      false,
		"dhcp6":      false,
		"link-local": []string{},
	})
	if err != nil {
		return "", fmt.Errorf("encode netplan bridge configuration: %w", err)
	}
	return "bridges." + netplanPathSegment(plan.Name) + "=" + string(value), nil
}

func netplanPathSegment(name string) string {
	return strings.ReplaceAll(name, ".", `\.`)
}

func netplanBridgeFailure(ctx context.Context, operationErr error, env Environment, path godbus.ObjectPath, bridge string, runtimeChanged bool) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	cleanupErrs := []error{cancelNetplanConfig(cleanupCtx, path)}
	if runtimeChanged {
		cleanupErrs = append(cleanupErrs, removeRuntimeBridge(cleanupCtx, env, bridge))
	}
	cleanupErr := errors.Join(cleanupErrs...)
	if cleanupErr != nil {
		return errors.Join(operationErr, fmt.Errorf("netplan cleanup failed: %w", cleanupErr))
	}
	return operationErr
}

func applyNetplanConfig(ctx context.Context, path godbus.ObjectPath) error {
	return callNetplanConfigBool(ctx, path, "Apply")
}

func cancelNetplanConfig(ctx context.Context, path godbus.ObjectPath) error {
	if path == "" {
		return nil
	}
	return callNetplanConfigBool(ctx, path, "Cancel")
}

func callNetplanConfigBool(ctx context.Context, path godbus.ObjectPath, method string) error {
	var accepted bool
	err := dbusclient.UseSystemBusWithOptions(ctx, dbusclient.SystemBusOptions{
		Subsystem: "netplan",
		NoRetry:   true,
	}, func(ctx context.Context, conn *godbus.Conn) error {
		config := conn.Object(netplanBusName, path)
		if callErr := config.CallWithContext(ctx, netplanConfigIface+"."+method, 0).Store(&accepted); callErr != nil {
			return callErr
		}
		if !accepted {
			return fmt.Errorf("netplan %s rejected the transaction", method)
		}
		return nil
	})
	return err
}
