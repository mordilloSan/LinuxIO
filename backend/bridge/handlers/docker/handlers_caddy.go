package docker

import (
	"context"
	"log/slog"
	"slices"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/internal/rpc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/settings"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

type autoUpdatePayload struct {
	Container string `json:"container"`
	Enabled   bool   `json:"enabled"`
}

func (h dockerHandlers) handleListAutoUpdateContainers(ctx context.Context, args []string, emit ipc.Events) error {
	cfg, _, err := settings.SnapshotForUser(h.username, h.store)
	if err != nil {
		return err
	}
	names := cfg.Docker.AutoUpdateStacks
	if names == nil {
		names = []string{}
	}
	return rpc.EmitResult(emit, names, nil)
}

func (h dockerHandlers) handleSetAutoUpdate(ctx context.Context, args []string, emit ipc.Events) error {
	payload, err := rpc.DecodeJSONArg[autoUpdatePayload](args, 0)
	if err != nil {
		return err
	}
	if payload.Container == "" {
		return ipc.ErrInvalidArgs
	}
	slog.Info("set_auto_update requested", "component", "docker", "container", payload.Container, "mode", payload.Enabled, "user", h.username)

	if _, _, err := settings.UpdateForUser(h.username, h.store, func(cfg *settings.Settings) error {
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

	go SyncWatchtowerStackWithStore(h.username, h.store)

	return rpc.EmitResult(emit, map[string]any{"message": "auto-update updated"}, nil)
}

func (h dockerHandlers) handleGetCaddyStatus(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := GetCaddyStatusWithStore(h.username, h.store)
	return rpc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleEnableCaddy(ctx context.Context, args []string, emit ipc.Events) error {
	slog.Info("enable_caddy requested", "component", "docker", "user", h.username)
	result, err := EnableCaddyWithStore(h.username, h.store)
	return rpc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleDisableCaddy(ctx context.Context, args []string, emit ipc.Events) error {
	slog.Info("disable_caddy requested", "component", "docker", "user", h.username)
	result, err := DisableCaddyWithStore(h.username, h.store)
	return rpc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleReloadCaddy(ctx context.Context, args []string, emit ipc.Events) error {
	slog.Info("reload_caddy requested", "component", "docker", "user", h.username)
	result, err := ReloadCaddyWithStore(h.username, h.store)
	return rpc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleConnectToProxy(ctx context.Context, args []string, emit ipc.Events) error {
	id, err := rpc.Arg(args, 0)
	if err != nil {
		return err
	}
	slog.Info("connect_to_proxy requested", "component", "docker", "container", id)
	result, err := ConnectToProxy(id)
	return rpc.EmitResult(emit, result, err)
}
