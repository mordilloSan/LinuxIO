package config

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	"github.com/mordilloSan/LinuxIO/backend/common/logging"
	"github.com/mordilloSan/LinuxIO/backend/common/verbosemode"
)

var Routes = routeBindings(runtime.Runtime{}).Routes()

func routeBindings(rt runtime.Runtime) apischema.BindingSet {
	handlers := configHandlers{rt: rt}
	return apischema.Bindings(
		apischema.Call[apischema.NoRequest, apischema.AppConfig]("config.get", apischema.RetrySafe()).Handle(handlers.handleGetConfig),
		apischema.Call[apischema.NoRequest, apischema.UIConfig]("config.get_ui", apischema.RetrySafe()).Handle(handlers.handleGetUIConfig),
		apischema.Mutation("config.set", handlers.handleSetConfig),
		apischema.Mutation("config.set_ui", handlers.handleSetUIConfig),
		apischema.Call[apischema.NoRequest, apischema.LoggingDebugState]("logging.get_debug", apischema.RetrySafe(), apischema.Privileged()).Handle(handleGetDebug),
		apischema.Call[apischema.LoggingDebugRequest, apischema.LoggingDebugState]("logging.set_debug", apischema.Privileged()).Handle(handleSetDebug),
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
	return appConfigToAPI(*result, h.rt.Store.StorageMode()), nil
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

func handleGetDebug(context.Context, apischema.NoRequest) (apischema.LoggingDebugState, error) {
	return apischema.LoggingDebugState{Enabled: verbosemode.Enabled()}, nil
}

// handleSetDebug persists the switch in the drop-ins, applies it to this
// bridge and signals the daemons to re-read it.
func handleSetDebug(ctx context.Context, req apischema.LoggingDebugRequest) (apischema.LoggingDebugState, error) {
	apply := verbosemode.Disable
	if req.Enabled {
		apply = verbosemode.Enable
	}
	if err := apply(ctx); err != nil {
		return apischema.LoggingDebugState{}, fmt.Errorf("persist debug logging: %w", err)
	}
	logging.SetDebug(req.Enabled)
	if err := verbosemode.ReloadDaemons(ctx); err != nil {
		return apischema.LoggingDebugState{}, fmt.Errorf("reload daemons: %w", err)
	}
	slog.Info("debug logging changed", "enabled", req.Enabled)
	return apischema.LoggingDebugState(req), nil
}
