package wireguard

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/systemd"
)

func EnableInterface(ctx context.Context, req apischema.NameRequest) (any, error) {
	if req.Name == "" {
		return nil, fmt.Errorf("usage: enable_interface <name>")
	}

	name := req.Name
	if err := validateInterfaceName(name); err != nil {
		return nil, fmt.Errorf("invalid interface name: %w", err)
	}

	serviceName := fmt.Sprintf("wg-quick@%s.service", name)
	if err := systemd.EnableUnit(ctx, serviceName); err != nil {
		return nil, fmt.Errorf("enable %s: %w", serviceName, err)
	}
	slog.Info("enabled WireGuard interface for boot persistence", "interface", name, "service", serviceName)
	return map[string]any{
		"status": "enabled",
		"output": "enabled via systemd D-Bus",
	}, nil
}

func DisableInterface(ctx context.Context, req apischema.NameRequest) (any, error) {
	if req.Name == "" {
		return nil, fmt.Errorf("usage: disable_interface <name>")
	}

	name := req.Name
	if err := validateInterfaceName(name); err != nil {
		return nil, fmt.Errorf("invalid interface name: %w", err)
	}

	serviceName := fmt.Sprintf("wg-quick@%s.service", name)
	if err := systemd.DisableUnit(ctx, serviceName); err != nil {
		return nil, fmt.Errorf("disable %s: %w", serviceName, err)
	}
	slog.Info("disabled WireGuard interface from boot persistence", "interface", name, "service", serviceName)
	return map[string]any{
		"status": "disabled",
		"output": "disabled via systemd D-Bus",
	}, nil
}

func isInterfaceEnabled(ctx context.Context, name string) bool {
	serviceName := fmt.Sprintf("wg-quick@%s.service", name)
	state, err := systemd.GetUnitFileState(ctx, serviceName)
	if err != nil {
		slog.Debug("failed to query interface enabled state", "component", "wireguard", "interface", name, "service", serviceName, "error", err)
		return false
	}

	switch state {
	case "enabled", "enabled-runtime", "linked", "linked-runtime", "alias":
		return true
	default:
		return false
	}
}
