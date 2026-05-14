package docker

import (
	"context"
	"log/slog"
	"slices"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/config"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

type autoUpdatePayload struct {
	Container string `json:"container"`
	Enabled   bool   `json:"enabled"`
}

func (h dockerHandlers) handleListAutoUpdateContainers(ctx context.Context, args []string, emit bridgeipc.Events) error {
	cfg, _, err := config.SnapshotForUser(ctx, h.username, h.store)
	if err != nil {
		return err
	}
	names := cfg.Docker.AutoUpdateStacks
	if names == nil {
		names = []string{}
	}
	return bridgeipc.EmitResult(emit, names, nil)
}

func (h dockerHandlers) handleSetAutoUpdate(ctx context.Context, args []string, emit bridgeipc.Events) error {
	payload, err := bridgeipc.DecodeJSONArg[autoUpdatePayload](args, 0)
	if err != nil {
		return err
	}
	if payload.Container == "" {
		return bridgeipc.ErrInvalidArgs
	}
	slog.Info("set_auto_update requested", "component", "docker", "container", payload.Container, "mode", payload.Enabled, "user", h.username)

	if _, _, err := config.UpdateForUser(ctx, h.username, h.store, func(cfg *config.Settings) error {
		if payload.Enabled {
			if !slices.Contains(cfg.Docker.AutoUpdateStacks, payload.Container) {
				cfg.Docker.AutoUpdateStacks = append(cfg.Docker.AutoUpdateStacks, payload.Container)
			}
		} else {
			cfg.Docker.AutoUpdateStacks = slices.DeleteFunc(cfg.Docker.AutoUpdateStacks, func(name string) bool {
				return name == payload.Container
			})
		}
		return nil
	}); err != nil {
		return err
	}

	go SyncWatchtowerStackDetached(h.username, h.store)

	return bridgeipc.EmitResult(emit, map[string]any{"message": "auto-update updated"}, nil)
}

func (h dockerHandlers) handleGetCaddyStatus(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := GetCaddyStatusWithStore(ctx, h.username, h.store)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleEnableCaddy(ctx context.Context, args []string, emit bridgeipc.Events) error {
	slog.Info("enable_caddy requested", "component", "docker", "user", h.username)
	result, err := EnableCaddyWithStore(ctx, h.username, h.store)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleDisableCaddy(ctx context.Context, args []string, emit bridgeipc.Events) error {
	slog.Info("disable_caddy requested", "component", "docker", "user", h.username)
	result, err := DisableCaddyWithStore(ctx, h.username, h.store)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleReloadCaddy(ctx context.Context, args []string, emit bridgeipc.Events) error {
	slog.Info("reload_caddy requested", "component", "docker", "user", h.username)
	result, err := ReloadCaddyWithStore(ctx, h.username, h.store)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleConnectToProxy(ctx context.Context, args []string, emit bridgeipc.Events) error {
	id, err := bridgeipc.Arg(args, 0)
	if err != nil {
		return err
	}
	slog.Info("connect_to_proxy requested", "component", "docker", "container", id)
	result, err := ConnectToProxy(ctx, id)
	return bridgeipc.EmitResult(emit, result, err)
}
