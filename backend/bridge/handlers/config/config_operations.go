package config

import (
	"context"
	"fmt"
	"log/slog"

	bridgeconfig "github.com/mordilloSan/LinuxIO/backend/bridge/internal/config"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

func GetConfigForUser(ctx context.Context, username string, store *bridgeconfig.UserStore) (*bridgeconfig.Settings, error) {
	cfg, cfgPath, err := bridgeconfig.SnapshotForUser(ctx, username, store)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	cfg.Jobs = bridgeconfig.EffectiveJobSettings(cfg.Jobs)
	slog.Debug("loaded user config", "component", "config", "user", username, "path", cfgPath)
	return cfg, nil
}

func SetConfigForUser(ctx context.Context, args []string, username string, store *bridgeconfig.UserStore) (map[string]any, error) {
	payload, err := bridgeipc.DecodeJSONArg[configSetPayload](args, 0)
	if err != nil {
		return nil, err
	}

	_, cfgPath, err := bridgeconfig.UpdateForUser(ctx, username, store, func(cfg *bridgeconfig.Settings) error {
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
