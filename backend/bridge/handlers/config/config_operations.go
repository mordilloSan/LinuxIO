package config

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgeconfig "github.com/mordilloSan/LinuxIO/backend/bridge/internal/config"
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

func SetConfigForUser(ctx context.Context, req apischema.ConfigSetPayload, username string, store *bridgeconfig.UserStore) (map[string]any, error) {
	payload, err := configSetPayloadFromAPI(req)
	if err != nil {
		return nil, err
	}

	_, cfgPath, err := bridgeconfig.UpdateForUser(ctx, username, store, func(cfg *bridgeconfig.Settings) error {
		return applyConfigPayload(cfg, payload)
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

func configSetPayloadFromAPI(req apischema.ConfigSetPayload) (*configSetPayload, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	var payload configSetPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}
