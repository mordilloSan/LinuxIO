package config

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/docker"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/userconfig"
)

func GetConfigForUser(ctx context.Context, username string, store *userconfig.UserStore) (*userconfig.Settings, error) {
	cfg, cfgPath, err := userconfig.SnapshotForUser(ctx, username, store)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	cfg.Jobs = userconfig.EffectiveJobSettings(cfg.Jobs)
	cfg.Assistant.Defaults = userconfig.EffectiveAssistantDefaults(cfg.Assistant.Defaults)
	slog.Debug("loaded user config", "component", "config", "user", username, "path", cfgPath)
	return cfg, nil
}

func GetUIConfigForUser(ctx context.Context, username string, store *userconfig.UserStore) (*userconfig.UIPreferences, error) {
	ui, uiPath, err := userconfig.UISnapshotForUser(ctx, username, store)
	if err != nil {
		return nil, fmt.Errorf("load UI config: %w", err)
	}
	slog.Debug("loaded user UI config", "component", "config", "user", username, "path", uiPath)
	return ui, nil
}

func SetConfigForUser(ctx context.Context, req apischema.ConfigSetPayload, username string, store *userconfig.UserStore, privileged bool) (apischema.ConfigSetResult, error) {
	var syncDockerMountOrdering, syncDockerHostIPWait bool
	updated, cfgPath, err := userconfig.UpdateForUser(ctx, username, store, func(cfg *userconfig.Settings) error {
		if privilegeErr := requireDockerMountOrderingPrivilege(cfg, &req, privileged); privilegeErr != nil {
			return privilegeErr
		}
		if applyErr := applyConfigPayload(cfg, &req); applyErr != nil {
			return applyErr
		}
		syncDockerMountOrdering = shouldSyncDockerMountOrdering(cfg, &req, store.StorageMode())
		syncDockerHostIPWait = shouldSyncDockerHostIPWait(cfg, &req, store.StorageMode())
		if syncDockerHostIPWait {
			// Before the config is stored: a host without a tool the drop-in
			// needs must not end up with the setting on and no drop-in.
			if checkErr := docker.CheckDaemonHostIPWait(cfg.Docker); checkErr != nil {
				return fmt.Errorf("docker service host IP wait: %w", checkErr)
			}
		}
		return nil
	})
	if err != nil {
		return apischema.ConfigSetResult{}, fmt.Errorf("update config: %w", err)
	}
	// The config is stored; each drop-in is brought in line with it even when
	// the other one fails.
	var syncErrs []error
	if syncDockerMountOrdering {
		if err := docker.SyncDaemonMountOrdering(ctx, updated.Docker); err != nil {
			syncErrs = append(syncErrs, fmt.Errorf("sync docker service mount ordering: %w", err))
		}
	}
	if syncDockerHostIPWait {
		if err := docker.SyncDaemonHostIPWait(ctx, updated.Docker); err != nil {
			syncErrs = append(syncErrs, fmt.Errorf("sync docker service host IP wait: %w", err))
		}
	}
	if err := errors.Join(syncErrs...); err != nil {
		return apischema.ConfigSetResult{}, err
	}
	slog.Info("user config updated", "component", "config", "user", username, "path", cfgPath)
	return apischema.ConfigSetResult{Message: "config updated", Path: cfgPath}, nil
}

func SetUIConfigForUser(ctx context.Context, req apischema.ConfigUISetPayload, username string, store *userconfig.UserStore) (apischema.ConfigSetResult, error) {
	ui := userconfig.DefaultUIPreferences()
	if err := applyUISettingsUpdate(&ui, &req); err != nil {
		return apischema.ConfigSetResult{}, fmt.Errorf("update UI config: %w", err)
	}
	_, uiPath, err := userconfig.ReplaceUIForUser(ctx, username, store, ui)
	if err != nil {
		return apischema.ConfigSetResult{}, fmt.Errorf("update UI config: %w", err)
	}
	slog.Info("user UI config updated", "component", "config", "user", username, "path", uiPath)
	return apischema.ConfigSetResult{Message: "config updated", Path: uiPath}, nil
}

// requireDockerMountOrderingPrivilege refuses, for a session without sudo, the
// Docker fields that end in a systemd drop-in root writes. The host IP fields
// always need sudo, since the list has no use but that drop-in; the folders
// need it only while the mount setting is on.
func requireDockerMountOrderingPrivilege(cfg *userconfig.Settings, payload *apischema.ConfigSetPayload, privileged bool) error {
	if privileged || payload == nil || payload.Docker == nil {
		return nil
	}
	if payload.Docker.RequireMountsForFolders != nil {
		return fmt.Errorf("%w: docker.requireMountsForFolders requires a privileged session", bridgeipc.ErrForbidden)
	}
	if payload.Docker.Folders != nil && cfg.Docker.RequireMountsForFolders {
		return fmt.Errorf("%w: docker.folders requires a privileged session when docker.requireMountsForFolders is enabled", bridgeipc.ErrForbidden)
	}
	if payload.Docker.WaitForHostIPs != nil {
		return fmt.Errorf("%w: docker.waitForHostIPs requires a privileged session", bridgeipc.ErrForbidden)
	}
	if payload.Docker.HostIPs != nil {
		return fmt.Errorf("%w: docker.hostIPs requires a privileged session", bridgeipc.ErrForbidden)
	}
	return nil
}

func shouldSyncDockerMountOrdering(cfg *userconfig.Settings, payload *apischema.ConfigSetPayload, storageMode userconfig.StorageMode) bool {
	if storageMode == userconfig.StorageModeMemory || payload == nil || payload.Docker == nil {
		return false
	}
	if payload.Docker.RequireMountsForFolders != nil {
		return true
	}
	return payload.Docker.Folders != nil && cfg.Docker.RequireMountsForFolders
}

// shouldSyncDockerHostIPWait reports whether the saved config (cfg, after the
// payload was applied) changes the host IP drop-in: the setting itself was
// sent, or the list was sent while the setting is on.
func shouldSyncDockerHostIPWait(cfg *userconfig.Settings, payload *apischema.ConfigSetPayload, storageMode userconfig.StorageMode) bool {
	if storageMode == userconfig.StorageModeMemory || payload == nil || payload.Docker == nil {
		return false
	}
	if payload.Docker.WaitForHostIPs != nil {
		return true
	}
	return payload.Docker.HostIPs != nil && cfg.Docker.WaitForHostIPs
}
