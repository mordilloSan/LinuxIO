package config

import (
	"context"
	"log/slog"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

// RegisterHandlers registers config handlers with the new handler system
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	handlers := configHandlers{rt: rt}
	apischema.RegisterRoutes(router, "config", []bridgeipc.Command{
		{Name: "get", Mode: bridgeipc.ModeQuery, Handler: handlers.handleGetConfig},
		{Name: "set", Mode: bridgeipc.ModeJob, Handler: handlers.handleSetConfig},
	})
}

func (h configHandlers) handleGetConfig(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	slog.Debug("config.get requested", "component", "config", "user", h.rt.Username())
	result, err := GetConfigForUser(ctx, h.rt.Username(), h.rt.Store)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h configHandlers) handleSetConfig(ctx context.Context, req apischema.ConfigSetPayload, emit bridgeipc.Events) error {
	result, err := SetConfigForUser(ctx, req, h.rt.Username(), h.rt.Store)
	return bridgeipc.EmitResult(emit, result, err)
}
