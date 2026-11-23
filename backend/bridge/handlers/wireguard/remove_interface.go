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
	configMu.Lock()
	defer configMu.Unlock()
	if req.Name == "" {
		return nil, fmt.Errorf("usage: remove_interface <name>")
	}

	name := req.Name
	if err := validateInterfaceName(name); err != nil {
		return nil, fmt.Errorf("invalid interface name: %w", err)
	}
	slog.Debug("removing WireGuard interface", "interface", name)

	// An interface that is still up keeps its config: without it, nothing
	// can take the interface down or undo its hooks.
	if _, err := runWGQuickCommand(ctx, "down", name); err != nil {
		if isInterfaceUpFunc(name) {
			return nil, fmt.Errorf("interface %s is still up, so its config is kept: %w", name, err)
		}
		slog.Warn("failed to bring down interface before removal", "interface", name, "error", err)
	} else {
		slog.Debug("interface brought down before removal", "interface", name)
	}

	// A unit left enabled would fail at every boot once its config is gone,
	// and one left failed, by a start or a stop that failed, would stay
	// listed as a failure for an interface that no longer exists.
	unit := unitName(name)
	if err := units.disable(ctx, unit); err != nil {
		slog.Warn("failed to disable interface unit before removal", "interface", name, "unit", unit, "error", err)
	}
	if state, err := units.activeState(ctx, unit); err != nil {
		slog.Warn("failed to read interface unit state before removal", "interface", name, "unit", unit, "error", err)
	} else if state == "failed" {
		if err := units.resetFailed(ctx, unit); err != nil {
			slog.Warn("failed to reset interface unit before removal", "interface", name, "unit", unit, "error", err)
		}
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
	pruneRates(name, nil)
	slog.Info("WireGuard interface removed", "interface", name)
	return "removed", nil
}
