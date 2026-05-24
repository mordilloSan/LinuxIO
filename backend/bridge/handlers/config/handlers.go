package config

import (
	"context"
	"log/slog"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

// RegisterHandlers registers config handlers with the new handler system
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	handlers := configHandlers{
		username: rt.Username(),
		store:    rt.Store,
	}
	bridgeipc.RegisterRoutes(router, "config", []bridgeipc.Command{
		{Name: "get", Mode: bridgeipc.ModeQuery, Handler: handlers.handleGetConfig},
		{Name: "set", Mode: bridgeipc.ModeJob, Handler: handlers.handleSetConfig},
	})
}

func (h configHandlers) handleGetConfig(ctx context.Context, args []string, emit bridgeipc.Events) error {
	slog.Debug("config.get requested", "component", "config", "user", h.username)
	result, err := GetConfigForUser(ctx, h.username, h.store)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h configHandlers) handleSetConfig(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := SetConfigForUser(ctx, args, h.username, h.store)
	return bridgeipc.EmitResult(emit, result, err)
}
