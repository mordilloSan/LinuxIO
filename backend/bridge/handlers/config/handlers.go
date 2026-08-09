package config

import (
	"context"
	"log/slog"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var Routes = routeBindings(runtime.Runtime{}).Routes()

func routeBindings(rt runtime.Runtime) apischema.BindingSet {
	handlers := configHandlers{rt: rt}
	return apischema.Bindings(
		apischema.Call[apischema.NoRequest, apischema.AppConfig]("config.get").Handle(handlers.handleGetConfig),
		apischema.Call[apischema.ConfigSetPayload, apischema.ConfigSetResult]("config.set").Handle(handlers.handleSetConfig),
	)
}

// RegisterHandlers registers config handlers with the new handler system
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	routeBindings(rt).Register(router)
}

func (h configHandlers) handleGetConfig(ctx context.Context, _ apischema.NoRequest) (apischema.AppConfig, error) {
	slog.Debug("config.get requested", "component", "config", "user", h.rt.Session.User.Username)
	result, err := GetConfigForUser(ctx, h.rt.Session.User.Username, h.rt.Store)
	if err != nil {
		return apischema.AppConfig{}, err
	}
	return appConfigToAPI(*result), nil
}

func (h configHandlers) handleSetConfig(ctx context.Context, req apischema.ConfigSetPayload) (apischema.ConfigSetResult, error) {
	return SetConfigForUser(ctx, req, h.rt.Session.User.Username, h.rt.Store, h.rt.Session.Privileged)
}
