package docker

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

func (h dockerHandlers) handleListContainers(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	result, err := ListContainers(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleStartContainer(ctx context.Context, req apischema.ContainerIDRequest, emit bridgeipc.Events) error {
	result, err := StartContainer(ctx, req.ContainerID)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleStopContainer(ctx context.Context, req apischema.ContainerIDRequest, emit bridgeipc.Events) error {
	result, err := StopContainer(ctx, req.ContainerID)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleRemoveContainer(ctx context.Context, req apischema.ContainerIDRequest, emit bridgeipc.Events) error {
	result, err := RemoveContainer(ctx, req.ContainerID)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleRestartContainer(ctx context.Context, req apischema.ContainerIDRequest, emit bridgeipc.Events) error {
	result, err := RestartContainer(ctx, req.ContainerID)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleStartAllStopped(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	result, err := StartAllStopped(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleStopAllRunning(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	result, err := StopAllRunning(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleCheckUpdates(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	result, err := RefreshDockerImageUpdates(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleUpdateContainer(ctx context.Context, req apischema.ContainerIDRequest, emit bridgeipc.Events) error {
	if req.ContainerID == "" {
		return bridgeipc.ErrInvalidArgs
	}
	result, err := UpdateContainer(ctx, req.ContainerID)
	return bridgeipc.EmitResult(emit, result, err)
}
