package wireguard

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func ListInterfaces(ctx context.Context) ([]apischema.WireGuardInterface, error) {
	slog.Debug("listing interfaces", "component", "wireguard")

	pattern := filepath.Join(wgConfigDir, "*"+configExt)
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("list interfaces: %w", err)
	}

	interfaces := make([]apischema.WireGuardInterface, 0, len(files))

	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		name := strings.TrimSuffix(filepath.Base(f), configExt)
		cfg, err := ParseWireGuardConfig(f)
		if err != nil {
			slog.Warn("failed to parse WireGuard interface config", "interface", name, "path", f, "error", err)
			continue
		}

		status := "Inactive"
		if isInterfaceUpFunc(name) {
			status = "Active"
		}

		interfaces = append(interfaces, apischema.WireGuardInterface{
			Name:        name,
			IsConnected: status,
			Address:     strings.Join(cfg.Address, ", "),
			Port:        cfg.ListenPort,
			PeerCount:   len(cfg.Peers),
			IsEnabled:   isInterfaceEnabled(ctx, name),
			Host:        cfg.Host,
			DNS:         nonNil(cfg.DNS),
			MTU:         cfg.MTU,
			PreUp:       nonNil(cfg.PreUp),
			PostUp:      nonNil(cfg.PostUp),
			PreDown:     nonNil(cfg.PreDown),
			PostDown:    nonNil(cfg.PostDown),
		})
	}
	slog.Debug("listed WireGuard interfaces", "count", len(interfaces))
	return interfaces, nil
}
