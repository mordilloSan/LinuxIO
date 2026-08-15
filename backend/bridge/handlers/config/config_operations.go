package config

import (
	"context"
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

func SetConfigForUser(ctx context.Context, req apischema.ConfigSetPayload, username string, store *bridgeconfig.UserStore, privileged bool) (apischema.ConfigSetResult, error) {
	var syncDockerMountOrdering bool
	updated, cfgPath, err := bridgeconfig.UpdateForUser(ctx, username, store, func(cfg *bridgeconfig.Settings) error {
		if privilegeErr := requireDockerMountOrderingPrivilege(cfg, &req, privileged); privilegeErr != nil {
			return privilegeErr
		}
		if applyErr := applyConfigPayload(cfg, &req); applyErr != nil {
			return applyErr
		}
		syncDockerMountOrdering = shouldSyncDockerMountOrdering(cfg, &req)
		return nil
	})
	if err != nil {
		return apischema.ConfigSetResult{}, fmt.Errorf("update config: %w", err)
	}
	if syncDockerMountOrdering {
		if err := syncDockerServiceMountOrdering(ctx, updated.Docker); err != nil {
			return apischema.ConfigSetResult{}, fmt.Errorf("sync docker service mount ordering: %w", err)
		}
	}
	slog.Info("user config updated", "component", "config", "user", username, "path", cfgPath)
	return apischema.ConfigSetResult{Message: "config updated", Path: cfgPath}, nil
}

func requireDockerMountOrderingPrivilege(cfg *bridgeconfig.Settings, payload *apischema.ConfigSetPayload, privileged bool) error {
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

func shouldSyncDockerMountOrdering(cfg *bridgeconfig.Settings, payload *apischema.ConfigSetPayload) bool {
	if payload == nil || payload.Docker == nil {
		return false
	}
	if payload.Docker.RequireMountsForFolders != nil {
		return true
	}
	return payload.Docker.Folders != nil && cfg.Docker.RequireMountsForFolders
}
