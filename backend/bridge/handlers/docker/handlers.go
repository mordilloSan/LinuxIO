package docker

import (
	"context"
	"encoding/base64"
	"errors"
	"log/slog"
	"net"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
)

var Routes = apischema.CombineRoutes(routeBindings(runtime.Runtime{}, dockerHandlers{}).Routes(), dockerTaskRoutes)

func routeBindings(rt runtime.Runtime, handlers dockerHandlers) apischema.BindingSet {
	return apischema.Bindings(
		apischema.Call[apischema.NoRequest, []apischema.ContainerInfo]("docker.list_containers", apischema.RetrySafe()).Handle(handlers.handleListContainers),
		apischema.Call[apischema.ContainerIDRequest, apischema.ContainerInspectInfo]("docker.inspect_container", apischema.RetrySafe()).Handle(handlers.handleInspectContainer),
		apischema.MutationVoid("docker.start_container", handlers.handleStartContainer, apischema.Privileged()),
		apischema.MutationVoid("docker.stop_container", handlers.handleStopContainer, apischema.Privileged()),
		apischema.MutationVoid("docker.restart_container", handlers.handleRestartContainer, apischema.Privileged()),
		apischema.MutationVoid("docker.pause_container", handlers.handlePauseContainer, apischema.Privileged()),
		apischema.MutationVoid("docker.unpause_container", handlers.handleUnpauseContainer, apischema.Privileged()),
		apischema.MutationVoid("docker.kill_container", handlers.handleKillContainer, apischema.Privileged()),
		apischema.MutationVoid("docker.remove_container", handlers.handleRemoveContainer, apischema.Privileged()),
		apischema.Mutation("docker.create_container", handlers.handleCreateContainer, apischema.Privileged()),
		apischema.Mutation("docker.edit_container", handlers.handleEditContainer, apischema.Privileged()),
		apischema.Call[apischema.NoRequest, []apischema.DockerImage]("docker.list_images", apischema.RetrySafe()).Handle(handlers.handleListImages),
		apischema.MutationVoid("docker.delete_image", handlers.handleDeleteImage, apischema.Privileged()),
		apischema.Call[apischema.NoRequest, []apischema.DockerNetwork]("docker.list_networks", apischema.RetrySafe()).Handle(handlers.handleListNetworks),
		apischema.MutationVoid("docker.create_network", handlers.handleCreateNetwork, apischema.Privileged()),
		apischema.MutationVoid("docker.delete_network", handlers.handleDeleteNetwork, apischema.Privileged()),
		apischema.MutationVoid("docker.connect_network", handlers.handleConnectNetwork, apischema.Privileged()),
		apischema.MutationVoid("docker.disconnect_network", handlers.handleDisconnectNetwork, apischema.Privileged()),
		apischema.Call[apischema.NoRequest, []apischema.DockerVolume]("docker.list_volumes", apischema.RetrySafe()).Handle(handlers.handleListVolumes),
		apischema.MutationVoid("docker.create_volume", handlers.handleCreateVolume, apischema.Privileged()),
		apischema.MutationVoid("docker.delete_volume", handlers.handleDeleteVolume, apischema.Privileged()),
		apischema.Call[apischema.NoRequest, []*apischema.ComposeProject]("docker.list_compose_projects", apischema.RetrySafe()).Handle(handlers.handleListComposeProjects),
		apischema.Call[apischema.ProjectNameRequest, *apischema.ComposeProject]("docker.get_compose_project", apischema.RetrySafe()).Handle(handlers.handleGetComposeProject),
		apischema.Mutation("docker.compose_up", handlers.handleComposeUp, apischema.Privileged()),
		apischema.Mutation("docker.compose_down", handlers.handleComposeDown, apischema.Privileged()),
		apischema.Mutation("docker.compose_stop", handlers.handleComposeStop, apischema.Privileged()),
		apischema.Mutation("docker.compose_restart", handlers.handleComposeRestart, apischema.Privileged()),
		apischema.Mutation("docker.delete_stack", handlers.handleDeleteStack, apischema.Privileged()),
		apischema.Call[apischema.NoRequest, apischema.DockerFoldersResponse]("docker.get_docker_folders", apischema.RetrySafe()).Handle(handlers.handleGetDockerFolders),
		apischema.Call[apischema.ValidateComposeRequest, apischema.ValidateComposeResponse]("docker.validate_compose", apischema.RetrySafe()).Handle(handlers.handleValidateCompose),
		apischema.Call[apischema.StackNameRequest, apischema.ComposeFilePathResponse]("docker.get_compose_file_path", apischema.RetrySafe()).Handle(handlers.handleGetComposeFilePath),
		apischema.Call[apischema.DirPathRequest, apischema.DirectoryValidationResult]("docker.validate_stack_directory", apischema.RetrySafe()).Handle(handlers.handleValidateStackDirectory),
		apischema.Call[apischema.NoRequest, apischema.DockerDaemonConfig]("docker.get_daemon_config", apischema.RetrySafe()).Handle(handlers.handleGetDaemonConfig),
		apischema.Mutation("docker.set_daemon_config", handlers.handleSetDaemonConfig, apischema.Privileged()),
		apischema.Call[apischema.NoRequest, *apischema.DockerSystemInfo]("docker.get_docker_info", apischema.RetrySafe()).Handle(handlers.handleGetDockerInfo),
		apischema.Call[apischema.IdentifierRequest, apischema.DockerIconURIResponse]("docker.get_icon_uri").Handle(handlers.handleGetIconURI),
		apischema.Call[apischema.IdentifierRequest, apischema.DockerIconDataResponse]("docker.get_icon").Handle(handlers.handleGetIcon),
		apischema.Call[apischema.IdentifierRequest, apischema.DockerIconInfoResponse]("docker.get_icon_info", apischema.RetrySafe()).Handle(handlers.handleGetIconInfo),
		apischema.Mutation("docker.clear_icon_cache", handlers.handleClearIconCache, apischema.Privileged()),
		apischema.Mutation("docker.start_all_stopped", handlers.handleStartAllStopped, apischema.Privileged()),
		apischema.Mutation("docker.stop_all_running", handlers.handleStopAllRunning, apischema.Privileged()),
		apischema.Call[apischema.NoRequest, apischema.DockerUpdateCheckResult]("docker.check_updates").Handle(handlers.handleCheckUpdates),
		apischema.Call[apischema.ContainerIDRequest, apischema.DockerUpdateCheckResult]("docker.check_container_update").Handle(handlers.handleCheckContainerUpdate),
		apischema.Call[apischema.NoRequest, apischema.DockerContainerAutoUpdateState]("docker.get_container_auto_update", apischema.RetrySafe()).Handle(handlers.handleGetContainerAutoUpdate),
		apischema.Mutation("docker.set_container_auto_update", handlers.handleSetContainerAutoUpdate, apischema.Privileged()),
		apischema.Mutation("docker.system_prune", handlers.handleSystemPrune, apischema.Privileged()),
		apischema.DuplexRoute[apischema.DockerLogsFollowRequest, apischema.NoResponse](routeDockerLogsFollow, apischema.NoEndpoint()).Duplex(
			func(ctx context.Context, stream net.Conn, req apischema.DockerLogsFollowRequest) error {
				return streamDockerLogsChannel(ctx, stream, rt, req)
			},
		),
		apischema.DuplexRoute[apischema.DockerLogsFollowAllRequest, apischema.NoResponse](routeDockerLogsFollowAll, apischema.NoEndpoint()).Duplex(
			func(ctx context.Context, stream net.Conn, req apischema.DockerLogsFollowAllRequest) error {
				return streamDockerLogsFollowAllChannel(ctx, stream, rt, req)
			},
		),
	)
}

// RegisterHandlers registers all docker handlers with the global registry
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	handlers := newDockerHandlers(rt)
	prepareDockerHandlers(router, handlers)

	routeBindings(rt, handlers).Register(router)
}

func (h dockerHandlers) handleListContainers(ctx context.Context, _ apischema.NoRequest) ([]apischema.ContainerInfo, error) {
	return ListContainers(ctx)
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

func (h dockerHandlers) handleListImages(ctx context.Context, _ apischema.NoRequest) ([]apischema.DockerImage, error) {
	return ListImages(ctx)
}

// The delete/create handlers below are void: their routes declare NoResponse, so
// the docker object or status string their domain functions return was never
// reachable — and on a Task route it lingered in the snapshot instead.
func (h dockerHandlers) handleDeleteImage(ctx context.Context, req apischema.ImageIDRequest) error {
	_, err := DeleteImage(ctx, req.ImageID)
	return err
}

func (h dockerHandlers) handleListNetworks(ctx context.Context, _ apischema.NoRequest) ([]apischema.DockerNetwork, error) {
	return ListDockerNetworks(ctx)
}

func (h dockerHandlers) handleCreateNetwork(ctx context.Context, req apischema.DockerNetworkCreateRequest) error {
	return CreateDockerNetwork(ctx, req)
}

func (h dockerHandlers) handleDeleteNetwork(ctx context.Context, req apischema.IDRequest) error {
	return DeleteDockerNetwork(ctx, req.ID)
}

func (h dockerHandlers) handleConnectNetwork(ctx context.Context, req apischema.DockerNetworkConnectRequest) error {
	return ConnectDockerNetwork(ctx, req)
}

func (h dockerHandlers) handleDisconnectNetwork(ctx context.Context, req apischema.DockerNetworkDisconnectRequest) error {
	return DisconnectDockerNetwork(ctx, req)
}

func (h dockerHandlers) handleListVolumes(ctx context.Context, _ apischema.NoRequest) ([]apischema.DockerVolume, error) {
	return ListVolumes(ctx)
}

func (h dockerHandlers) handleCreateVolume(ctx context.Context, req apischema.DockerVolumeCreateRequest) error {
	return CreateVolume(ctx, req)
}

func (h dockerHandlers) handleDeleteVolume(ctx context.Context, req apischema.NameRequest) error {
	return DeleteVolume(ctx, req.Name)
}

func (h dockerHandlers) handleListComposeProjects(ctx context.Context, _ apischema.NoRequest) ([]*apischema.ComposeProject, error) {
	return ListComposeProjects(ctx, h.rt.Username(), h.rt.Store)
}

func (h dockerHandlers) handleGetComposeProject(ctx context.Context, req apischema.ProjectNameRequest) (*apischema.ComposeProject, error) {
	return GetComposeProject(ctx, h.rt.Username(), h.rt.Store, req.ProjectName)
}

func (h dockerHandlers) handleComposeUp(ctx context.Context, req apischema.ProjectNameRequest) (apischema.ComposeActionResult, error) {
	return ComposeUp(ctx, h.rt.Username(), h.rt.Store, req.ProjectName, "")
}

func (h dockerHandlers) handleComposeDown(ctx context.Context, req apischema.ProjectNameRequest) (apischema.ComposeActionResult, error) {
	return ComposeDown(ctx, h.rt.Username(), h.rt.Store, req.ProjectName)
}

func (h dockerHandlers) handleComposeStop(ctx context.Context, req apischema.ProjectNameRequest) (apischema.ComposeActionResult, error) {
	return ComposeStop(ctx, h.rt.Username(), h.rt.Store, req.ProjectName)
}

func (h dockerHandlers) handleComposeRestart(ctx context.Context, req apischema.ProjectNameRequest) (apischema.ComposeActionResult, error) {
	return ComposeRestart(ctx, h.rt.Username(), h.rt.Store, req.ProjectName)
}

func (h dockerHandlers) handleDeleteStack(ctx context.Context, req apischema.DeleteStackRequest) (apischema.DeleteStackResult, error) {
	options := DeleteStackOptions{
		DeleteFile:      req.DeleteFile,
		DeleteDirectory: req.DeleteDirectory,
	}
	result, err := DeleteStack(ctx, h.rt.Username(), h.rt.Store, req.ProjectName, options)
	return result, err
}

func (h dockerHandlers) handleGetDockerFolders(ctx context.Context, _ apischema.NoRequest) (apischema.DockerFoldersResponse, error) {
	return GetDockerFolders(ctx, h.rt.Username(), h.rt.Store)
}

func (h dockerHandlers) handleValidateCompose(ctx context.Context, req apischema.ValidateComposeRequest) (apischema.ValidateComposeResponse, error) {
	return ValidateComposeFile(ctx, req.Content, req.EnvContent, req.WorkingDir)
}

func (h dockerHandlers) handleGetComposeFilePath(ctx context.Context, req apischema.StackNameRequest) (apischema.ComposeFilePathResponse, error) {
	return GetComposeFilePath(ctx, h.rt.Username(), h.rt.Store, req.StackName)
}

func (h dockerHandlers) handleValidateStackDirectory(ctx context.Context, req apischema.DirPathRequest) (apischema.DirectoryValidationResult, error) {
	return ValidateStackDirectory(ctx, req.DirPath)
}

func (h dockerHandlers) handleGetIconURI(ctx context.Context, req apischema.IdentifierRequest) (apischema.DockerIconURIResponse, error) {
	uri, err := GetIconURI(ctx, req.Identifier)
	if errors.Is(err, errIconNotFound) {
		// No icon is a normal answer, not a failed route: the frontend shows
		// its bundled fallback and must not warn or retry.
		slog.Debug("icon not found", "identifier", req.Identifier, "error", err)
		return apischema.DockerIconURIResponse{}, nil
	}
	return apischema.DockerIconURIResponse{URI: uri}, err
}

func (h dockerHandlers) handleGetIcon(ctx context.Context, req apischema.IdentifierRequest) (apischema.DockerIconDataResponse, error) {
	data, err := GetIcon(ctx, req.Identifier)
	if err != nil {
		return apischema.DockerIconDataResponse{}, err
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	return apischema.DockerIconDataResponse{Data: encoded}, nil
}

func (h dockerHandlers) handleGetIconInfo(ctx context.Context, req apischema.IdentifierRequest) (apischema.DockerIconInfoResponse, error) {
	info := GetIconInfo(ctx, req.Identifier)
	return apischema.DockerIconInfoResponse{Type: string(info.Type), Identifier: info.Identifier, Cached: info.Cached}, nil
}

func (h dockerHandlers) handleClearIconCache(ctx context.Context, _ apischema.NoRequest) (apischema.MessageResponse, error) {
	if err := ClearIconCache(ctx); err != nil {
		return apischema.MessageResponse{}, err
	}
	return apischema.MessageResponse{Message: "Icon cache cleared successfully"}, nil
}

func (h dockerHandlers) handleGetDockerInfo(ctx context.Context, _ apischema.NoRequest) (*apischema.DockerSystemInfo, error) {
	return GetDockerInfo(ctx)
}

func (h dockerHandlers) handleSystemPrune(ctx context.Context, req apischema.DockerSystemPruneRequest) (*apischema.DockerSystemPruneResponse, error) {
	return SystemPrune(ctx, req)
}
