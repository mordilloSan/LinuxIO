package wireguard

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func RemoveInterface(ctx context.Context, req apischema.NameRequest) (any, error) {
	if req.Name == "" {
		return nil, fmt.Errorf("usage: remove_interface <name>")
	}

	name := req.Name
	if err := validateInterfaceName(name); err != nil {
		return nil, fmt.Errorf("invalid interface name: %w", err)
	}
	slog.Debug("removing WireGuard interface", "interface", name)

	// Best-effort: bring it down, but don't abort on failure.
	if _, err := runWGQuickCommand(ctx, "down", name); err != nil {
		slog.Warn("failed to bring down interface before removal", "interface", name, "error", err)
	} else {
		slog.Debug("interface brought down before removal", "interface", name)
	}

	// Remove config file
	cfgPath := configPath(name)
	if err := os.Remove(cfgPath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			slog.Warn("interface config already missing", "interface", name, "path", cfgPath)
		} else {
			return nil, fmt.Errorf("remove config: %w", err)
		}
	} else {
		slog.Debug("removed interface config file", "interface", name, "path", cfgPath)
	}

	// Remove peer directory (best-effort)
	peerDir := peerDirPath(name)
	if err := os.RemoveAll(peerDir); err != nil && !errors.Is(err, fs.ErrNotExist) {
		slog.Warn("failed to remove peer directory", "interface", name, "path", peerDir, "error", err)
	} else {
		slog.Debug("removed peer directory", "interface", name, "path", peerDir)
	}
	slog.Info("WireGuard interface removed", "interface", name)
	return "removed", nil
}
