package docker

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

func (h dockerHandlers) handleListContainers(ctx context.Context, _ apischema.NoRequest) ([]apischema.ContainerInfo, error) {
	return ListContainers(ctx)
}

func (h dockerHandlers) handleStartContainer(ctx context.Context, req apischema.ContainerIDRequest) error {
	_, err := StartContainer(ctx, req.ContainerID)
	return err
}

func (h dockerHandlers) handleStopContainer(ctx context.Context, req apischema.ContainerIDRequest) error {
	_, err := StopContainer(ctx, req.ContainerID)
	return err
}

func (h dockerHandlers) handleRemoveContainer(ctx context.Context, req apischema.ContainerIDRequest) error {
	_, err := RemoveContainer(ctx, req.ContainerID)
	return err
}

func (h dockerHandlers) handleRestartContainer(ctx context.Context, req apischema.ContainerIDRequest) error {
	_, err := RestartContainer(ctx, req.ContainerID)
	return err
}

func (h dockerHandlers) handleStartAllStopped(ctx context.Context, _ apischema.NoRequest) (apischema.DockerStartedFailedResponse, error) {
	return StartAllStopped(ctx)
}

func (h dockerHandlers) handleStopAllRunning(ctx context.Context, _ apischema.NoRequest) (apischema.DockerStoppedFailedResponse, error) {
	return StopAllRunning(ctx)
}

func (h dockerHandlers) handleCheckUpdates(ctx context.Context, _ apischema.NoRequest) (apischema.DockerUpdateCheckResult, error) {
	return RefreshDockerImageUpdates(ctx)
}

func (h dockerHandlers) handleCheckContainerUpdate(ctx context.Context, req apischema.ContainerIDRequest) (apischema.DockerUpdateCheckResult, error) {
	if req.ContainerID == "" {
		return apischema.DockerUpdateCheckResult{}, bridgeipc.ErrInvalidArgs
	}
	return RefreshContainerImageUpdate(ctx, req.ContainerID)
}

func (h dockerHandlers) handleUpdateContainer(ctx context.Context, req apischema.ContainerIDRequest) (apischema.DockerContainerUpdateResult, error) {
	if req.ContainerID == "" {
		return apischema.DockerContainerUpdateResult{}, bridgeipc.ErrInvalidArgs
	}
	return UpdateContainer(ctx, req.ContainerID)
}

func (h dockerHandlers) handleGetContainerAutoUpdate(ctx context.Context, _ apischema.NoRequest) (apischema.DockerContainerAutoUpdateState, error) {
	return GetContainerAutoUpdate(ctx)
}

func (h dockerHandlers) handleSetContainerAutoUpdate(ctx context.Context, req apischema.DockerContainerAutoUpdateOptions) (apischema.DockerContainerAutoUpdateState, error) {
	return SetContainerAutoUpdate(ctx, req)
}
