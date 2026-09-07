package docker

import (
	"context"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

func (h dockerHandlers) handleListContainers(ctx context.Context, _ apischema.NoRequest) ([]apischema.ContainerInfo, error) {
	return ListContainers(ctx)
}

func validateContainerID(containerID string) error {
	if strings.TrimSpace(containerID) == "" {
		return bridgeipc.ErrInvalidArgs
	}
	return nil
}

func (h dockerHandlers) handleInspectContainer(ctx context.Context, req apischema.ContainerIDRequest) (apischema.ContainerInspectInfo, error) {
	if err := validateContainerID(req.ContainerID); err != nil {
		return apischema.ContainerInspectInfo{}, err
	}
	return InspectContainer(ctx, req.ContainerID)
}

func (h dockerHandlers) handleStartContainer(ctx context.Context, req apischema.ContainerIDRequest) error {
	if err := validateContainerID(req.ContainerID); err != nil {
		return err
	}
	return StartContainer(ctx, req.ContainerID)
}

func (h dockerHandlers) handleStopContainer(ctx context.Context, req apischema.ContainerIDRequest) error {
	if err := validateContainerID(req.ContainerID); err != nil {
		return err
	}
	return StopContainer(ctx, req.ContainerID)
}

func (h dockerHandlers) handleRestartContainer(ctx context.Context, req apischema.ContainerIDRequest) error {
	if err := validateContainerID(req.ContainerID); err != nil {
		return err
	}
	return RestartContainer(ctx, req.ContainerID)
}

func (h dockerHandlers) handlePauseContainer(ctx context.Context, req apischema.ContainerIDRequest) error {
	if err := validateContainerID(req.ContainerID); err != nil {
		return err
	}
	return PauseContainer(ctx, req.ContainerID)
}

func (h dockerHandlers) handleUnpauseContainer(ctx context.Context, req apischema.ContainerIDRequest) error {
	if err := validateContainerID(req.ContainerID); err != nil {
		return err
	}
	return UnpauseContainer(ctx, req.ContainerID)
}

func (h dockerHandlers) handleKillContainer(ctx context.Context, req apischema.ContainerIDRequest) error {
	if err := validateContainerID(req.ContainerID); err != nil {
		return err
	}
	return KillContainer(ctx, req.ContainerID)
}

func (h dockerHandlers) handleRemoveContainer(ctx context.Context, req apischema.ContainerRemoveRequest) error {
	if err := validateContainerID(req.ContainerID); err != nil {
		return err
	}
	return RemoveContainer(ctx, req.ContainerID, req.Force)
}

func (h dockerHandlers) handleCreateContainer(ctx context.Context, req apischema.ContainerCreateRequest) (apischema.ContainerConfigurationResult, error) {
	return CreateConfiguredContainer(ctx, req)
}

func (h dockerHandlers) handleEditContainer(ctx context.Context, req apischema.ContainerEditRequest) (apischema.ContainerConfigurationResult, error) {
	if err := validateContainerID(req.ContainerID); err != nil {
		return apischema.ContainerConfigurationResult{}, err
	}
	return EditConfiguredContainer(ctx, req)
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

func (h dockerHandlers) handleGetContainerAutoUpdate(ctx context.Context, _ apischema.NoRequest) (apischema.DockerContainerAutoUpdateState, error) {
	return GetContainerAutoUpdate(ctx)
}

func (h dockerHandlers) handleSetContainerAutoUpdate(ctx context.Context, req apischema.DockerContainerAutoUpdateOptions) (apischema.DockerContainerAutoUpdateState, error) {
	return SetContainerAutoUpdate(ctx, req)
}
