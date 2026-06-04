package config

import (
	"context"
	"log/slog"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var routes = apischema.NewRouteCatalog()

var RouteGet = routes.Query("config.get", apischema.NoRequest(), apischema.TypeOf[apischema.AppConfig]())
var RouteSet = routes.Job("config.set", apischema.TypeOf[apischema.ConfigSetPayload](), apischema.TypeOf[apischema.ConfigSetResult]())

var Routes = routes.All()

// RegisterHandlers registers config handlers with the new handler system
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	handlers := configHandlers{rt: rt}
	apischema.RegisterRoutes(router, []apischema.HandlerBinding{
		{Route: RouteGet, Handle: handlers.handleGetConfig},
		{Route: RouteSet, Handle: handlers.handleSetConfig},
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
