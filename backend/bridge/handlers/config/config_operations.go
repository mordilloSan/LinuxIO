package config

import (
	"fmt"
	"log/slog"

	"github.com/mordilloSan/LinuxIO/backend/bridge/settings"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

func GetConfigForUser(username string, store *settings.UserStore) (*settings.Settings, error) {
	cfg, cfgPath, err := settings.SnapshotForUser(username, store)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	cfg.Jobs = settings.EffectiveJobSettings(cfg.Jobs)
	slog.Debug("loaded user config", "component", "config", "user", username, "path", cfgPath)
	return cfg, nil
}

func SetConfigForUser(args []string, username string, store *settings.UserStore) (map[string]any, error) {
	payload, err := bridgeipc.DecodeJSONArg[configSetPayload](args, 0)
	if err != nil {
		return nil, err
	}
	slog.Info("config update requested", "component", "config", "user", username)

	_, cfgPath, err := settings.UpdateForUser(username, store, func(cfg *settings.Settings) error {
		return applyConfigPayload(cfg, &payload)
	})
	if err != nil {
		return nil, fmt.Errorf("update config: %w", err)
	}
	slog.Info("user config updated", "component", "config", "user", username, "path", cfgPath)
	return map[string]any{
		"message": "config updated",
		"path":    cfgPath,
	}, nil
}
