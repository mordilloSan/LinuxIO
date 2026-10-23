package wireguard

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func RemoveInterface(ctx context.Context, req apischema.NameRequest) (any, error) {
	if req.Name == "" {
		slog.Error("invalid remove interface request")
		return nil, fmt.Errorf("usage: remove_interface <name>")
	}

	name := req.Name
	if err := validateInterfaceName(name); err != nil {
		slog.Error("invalid WireGuard interface name", "interface", name, "error", err)
		return nil, fmt.Errorf("invalid interface name: %w", err)
	}
	slog.Info("removing WireGuard interface", "interface", name)

	// Best-effort: bring it down, but don't abort on failure.
	if _, err := DownInterface(ctx, apischema.NameRequest{Name: name}); err != nil {
		slog.Warn("failed to bring down interface before removal", "interface", name, "error", err)
	} else {
		slog.Info("interface brought down before removal", "interface", name)
	}

	// Remove config file
	cfgPath := configPath(name)
	if err := os.Remove(cfgPath); err != nil {
		if os.IsNotExist(err) {
			slog.Warn("interface config already missing", "interface", name, "path", cfgPath)
		} else {
			slog.Error("failed to remove interface config", "interface", name, "path", cfgPath, "error", err)
			return nil, fmt.Errorf("remove config: %w", err)
		}
	} else {
		slog.Info("removed interface config file", "interface", name, "path", cfgPath)
	}

	// Remove peer directory (best-effort)
	peerDir := peerDirPath(name)
	if err := os.RemoveAll(peerDir); err != nil && !os.IsNotExist(err) {
		slog.Warn("failed to remove peer directory", "interface", name, "path", peerDir, "error", err)
	} else {
		slog.Info("removed peer directory", "interface", name, "path", peerDir)
	}
	slog.Info("WireGuard interface removed", "interface", name)
	return "removed", nil
}
