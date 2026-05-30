package docker

import (
	"context"
	"slices"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/config"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

func (h dockerHandlers) handleListAutoUpdateContainers(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
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

func (h dockerHandlers) handleSetAutoUpdate(ctx context.Context, req apischema.DockerSetAutoUpdateRequest, emit bridgeipc.Events) error {
	if req.Container == "" {
		return bridgeipc.ErrInvalidArgs
	}

	if _, _, err := config.UpdateForUser(ctx, h.username, h.store, func(cfg *config.Settings) error {
		if req.Enabled {
			if !slices.Contains(cfg.Docker.AutoUpdateStacks, req.Container) {
				cfg.Docker.AutoUpdateStacks = append(cfg.Docker.AutoUpdateStacks, req.Container)
			}
		} else {
			cfg.Docker.AutoUpdateStacks = slices.DeleteFunc(cfg.Docker.AutoUpdateStacks, func(name string) bool {
				return name == req.Container
			})
		}
		return nil
	}); err != nil {
		return err
	}

	go SyncWatchtowerStackDetached(h.username, h.store)

	return bridgeipc.EmitResult(emit, map[string]any{"message": "auto-update updated"}, nil)
}

func (h dockerHandlers) handleGetCaddyStatus(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	result, err := GetCaddyStatusWithStore(ctx, h.username, h.store)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleEnableCaddy(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	result, err := EnableCaddyWithStore(ctx, h.username, h.store)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleDisableCaddy(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	result, err := DisableCaddyWithStore(ctx, h.username, h.store)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleReloadCaddy(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	result, err := ReloadCaddyWithStore(ctx, h.username, h.store)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleConnectToProxy(ctx context.Context, req apischema.ContainerIDRequest, emit bridgeipc.Events) error {
	result, err := ConnectToProxy(ctx, req.ContainerID)
	return bridgeipc.EmitResult(emit, result, err)
}
