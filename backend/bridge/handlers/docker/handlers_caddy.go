package docker

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

func (h dockerHandlers) handleGetCaddyStatus(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	result, err := GetCaddyStatus(ctx, h.rt.Username(), h.rt.Store)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleEnableCaddy(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	result, err := EnableCaddy(ctx, h.rt.Username(), h.rt.Store)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleDisableCaddy(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	result, err := DisableCaddy(ctx, h.rt.Username(), h.rt.Store)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleReloadCaddy(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	result, err := ReloadCaddy(ctx, h.rt.Username(), h.rt.Store)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleConnectToProxy(ctx context.Context, req apischema.ContainerIDRequest, emit bridgeipc.Events) error {
	result, err := ConnectToProxy(ctx, req.ContainerID)
	return bridgeipc.EmitResult(emit, result, err)
}
