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

func SetConfigForUser(ctx context.Context, req apischema.ConfigSetPayload, username string, store *bridgeconfig.UserStore, privileged bool) (map[string]any, error) {
	payload, err := configSetPayloadFromAPI(req)
	if err != nil {
		return nil, err
	}

	var syncDockerMountOrdering bool
	updated, cfgPath, err := bridgeconfig.UpdateForUser(ctx, username, store, func(cfg *bridgeconfig.Settings) error {
		if privilegeErr := requireDockerMountOrderingPrivilege(cfg, payload, privileged); privilegeErr != nil {
			return privilegeErr
		}
		if applyErr := applyConfigPayload(cfg, payload); applyErr != nil {
			return applyErr
		}
		syncDockerMountOrdering = shouldSyncDockerMountOrdering(cfg, payload)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("update config: %w", err)
	}
	if syncDockerMountOrdering {
		if err := syncDockerServiceMountOrdering(ctx, updated.Docker); err != nil {
			return nil, fmt.Errorf("sync docker service mount ordering: %w", err)
		}
	}
	slog.Info("user config updated", "component", "config", "user", username, "path", cfgPath)
	return map[string]any{
		"message": "config updated",
		"path":    cfgPath,
	}, nil
}

func requireDockerMountOrderingPrivilege(cfg *bridgeconfig.Settings, payload *configSetPayload, privileged bool) error {
	if privileged || payload == nil || payload.Docker == nil {
		return nil
	}
	if payload.Docker.RequireMountsForFolders != nil {
		return fmt.Errorf("docker.requireMountsForFolders requires a privileged session")
	}
	if payload.Docker.Folders != nil && cfg.Docker.RequireMountsForFolders {
		return fmt.Errorf("docker.folders requires a privileged session when docker.requireMountsForFolders is enabled")
	}
	return nil
}

func shouldSyncDockerMountOrdering(cfg *bridgeconfig.Settings, payload *configSetPayload) bool {
	if payload == nil || payload.Docker == nil {
		return false
	}
	if payload.Docker.RequireMountsForFolders != nil {
		return true
	}
	return payload.Docker.Folders != nil && cfg.Docker.RequireMountsForFolders
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
