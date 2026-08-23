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
		apischema.Call[apischema.NoRequest, apischema.AppConfig]("config.get", apischema.RetrySafe()).Handle(handlers.handleGetConfig),
		apischema.Call[apischema.NoRequest, apischema.UIConfig]("config.get_ui", apischema.RetrySafe()).Handle(handlers.handleGetUIConfig),
		apischema.Call[apischema.ConfigSetPayload, apischema.ConfigSetResult]("config.set").Handle(handlers.handleSetConfig),
		apischema.Call[apischema.ConfigUISetPayload, apischema.ConfigSetResult]("config.set_ui").Handle(handlers.handleSetUIConfig),
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

func (h configHandlers) handleGetUIConfig(ctx context.Context, _ apischema.NoRequest) (apischema.UIConfig, error) {
	slog.Debug("config.get_ui requested", "component", "config", "user", h.rt.Session.User.Username)
	result, err := GetUIConfigForUser(ctx, h.rt.Session.User.Username, h.rt.Store)
	if err != nil {
		return apischema.UIConfig{}, err
	}
	return uiConfigToAPI(*result), nil
}

func (h configHandlers) handleSetUIConfig(ctx context.Context, req apischema.ConfigUISetPayload) (apischema.ConfigSetResult, error) {
	return SetUIConfigForUser(ctx, req, h.rt.Session.User.Username, h.rt.Store)
}
