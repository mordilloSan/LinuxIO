package config

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/rpc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/runtime"
	"github.com/mordilloSan/LinuxIO/backend/bridge/settings"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

type configHandlers struct {
	username string
	store    *settings.UserStore
}

// RegisterHandlers registers config handlers with the new handler system
func RegisterHandlers(rt runtime.Runtime) {
	handlers := configHandlers{
		username: rt.Username(),
		store:    rt.Store,
	}
	rpc.Register("config", rt, []rpc.Command{
		{Name: "get", Handler: handlers.handleGetConfig},
		{Name: "set", Handler: handlers.handleSetConfig},
	})
}

func (h configHandlers) handleGetConfig(ctx context.Context, args []string, emit ipc.Events) error {
	cfg, cfgPath, err := settings.SnapshotForUser(h.username, h.store)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	cfg.Jobs = settings.EffectiveJobSettings(cfg.Jobs)
	slog.Debug("loaded user config", "component", "config", "user", h.username, "path", cfgPath)
	return rpc.EmitResult(emit, cfg, nil)
}

func (h configHandlers) handleSetConfig(ctx context.Context, args []string, emit ipc.Events) error {
	payload, err := rpc.DecodeJSONArg[configSetPayload](args, 0)
	if err != nil {
		return err
	}
	slog.Info("config update requested", "component", "config", "user", h.username)

	_, cfgPath, err := settings.UpdateForUser(h.username, h.store, func(cfg *settings.Settings) error {
		return applyConfigPayload(cfg, &payload)
	})
	if err != nil {
		return fmt.Errorf("update config: %w", err)
	}
	slog.Info("user config updated", "component", "config", "user", h.username, "path", cfgPath)
	return rpc.EmitResult(emit, map[string]any{
		"message": "config updated",
		"path":    cfgPath,
	}, nil)
}
