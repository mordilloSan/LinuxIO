package docker

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func (h dockerHandlers) handleGetCaddyStatus(ctx context.Context, _ apischema.NoRequest) (apischema.CaddyStatusResponse, error) {
	return GetCaddyStatus(ctx, h.rt.Username(), h.rt.Store)
}

func (h dockerHandlers) handleEnableCaddy(ctx context.Context, _ apischema.NoRequest) (apischema.MessageResponse, error) {
	return EnableCaddy(ctx, h.rt.Username(), h.rt.Store)
}

func (h dockerHandlers) handleDisableCaddy(ctx context.Context, _ apischema.NoRequest) (apischema.MessageResponse, error) {
	return DisableCaddy(ctx, h.rt.Username(), h.rt.Store)
}

func (h dockerHandlers) handleReloadCaddy(ctx context.Context, _ apischema.NoRequest) (apischema.MessageResponse, error) {
	return ReloadCaddy(ctx, h.rt.Username(), h.rt.Store)
}

func (h dockerHandlers) handleConnectToProxy(ctx context.Context, req apischema.ContainerIDRequest) (apischema.MessageResponse, error) {
	return ConnectToProxy(ctx, req.ContainerID)
}
