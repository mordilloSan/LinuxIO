package docker

import (
	"context"
	"net"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var Routes = apischema.CombineRoutes(routeBindings(runtime.Runtime{}, dockerHandlers{}).Routes(), dockerTaskRoutes)

// Only 9 of these 42 routes can use the typed Handle: the rest call domain
// functions that return bare `any` (containers, images, networks, volumes,
// caddy, icons, folders, validation), so no declared result type here is
// checked against what the handler actually produces. Tightening those
func routeBindings(rt runtime.Runtime, handlers dockerHandlers) apischema.BindingSet {
	return apischema.Bindings(
		apischema.Call[apischema.NoRequest, []apischema.ContainerInfo]("docker.list_containers", apischema.RetrySafe()).Handle(handlers.handleListContainers),
		apischema.Call[apischema.ContainerIDRequest, apischema.NoResponse]("docker.start_container").HandleVoid(handlers.handleStartContainer),
		apischema.Call[apischema.ContainerIDRequest, apischema.NoResponse]("docker.stop_container").HandleVoid(handlers.handleStopContainer),
		apischema.Call[apischema.ContainerIDRequest, apischema.NoResponse]("docker.remove_container").HandleVoid(handlers.handleRemoveContainer),
		apischema.Call[apischema.ContainerIDRequest, apischema.NoResponse]("docker.restart_container").HandleVoid(handlers.handleRestartContainer),
		apischema.Call[apischema.NoRequest, []apischema.DockerImage]("docker.list_images", apischema.RetrySafe()).Handle(handlers.handleListImages),
		apischema.Call[apischema.ImageIDRequest, apischema.NoResponse]("docker.delete_image").HandleVoid(handlers.handleDeleteImage),
		apischema.Call[apischema.NoRequest, []apischema.DockerNetwork]("docker.list_networks", apischema.RetrySafe()).Handle(handlers.handleListNetworks),
		apischema.Call[apischema.NameRequest, apischema.NoResponse]("docker.create_network").HandleVoid(handlers.handleCreateNetwork),
		apischema.Call[apischema.IDRequest, apischema.NoResponse]("docker.delete_network").HandleVoid(handlers.handleDeleteNetwork),
		apischema.Call[apischema.NoRequest, []apischema.DockerVolume]("docker.list_volumes", apischema.RetrySafe()).Handle(handlers.handleListVolumes),
		apischema.Call[apischema.NameRequest, apischema.NoResponse]("docker.create_volume").HandleVoid(handlers.handleCreateVolume),
		apischema.Call[apischema.NameRequest, apischema.NoResponse]("docker.delete_volume").HandleVoid(handlers.handleDeleteVolume),
		apischema.Call[apischema.NoRequest, []*apischema.ComposeProject]("docker.list_compose_projects", apischema.RetrySafe()).Handle(handlers.handleListComposeProjects),
		apischema.Call[apischema.ProjectNameRequest, *apischema.ComposeProject]("docker.get_compose_project", apischema.RetrySafe()).Handle(handlers.handleGetComposeProject),
		apischema.Call[apischema.ProjectNameRequest, apischema.ComposeActionResult]("docker.compose_up").Handle(handlers.handleComposeUp),
		apischema.Call[apischema.ProjectNameRequest, apischema.ComposeActionResult]("docker.compose_down").Handle(handlers.handleComposeDown),
		apischema.Call[apischema.ProjectNameRequest, apischema.ComposeActionResult]("docker.compose_stop").Handle(handlers.handleComposeStop),
		apischema.Call[apischema.ProjectNameRequest, apischema.ComposeActionResult]("docker.compose_restart").Handle(handlers.handleComposeRestart),
		apischema.Call[apischema.DeleteStackRequest, apischema.DeleteStackResult]("docker.delete_stack").Handle(handlers.handleDeleteStack),
		apischema.Call[apischema.NoRequest, apischema.DockerFoldersResponse]("docker.get_docker_folders", apischema.RetrySafe()).Handle(handlers.handleGetDockerFolders),
		apischema.Call[apischema.ContentRequest, apischema.ValidateComposeResponse]("docker.validate_compose", apischema.RetrySafe()).Handle(handlers.handleValidateCompose),
		apischema.Call[apischema.ContentRequest, apischema.NoResponse]("docker.normalize_compose", apischema.NoEndpoint()).HandleVoid(handlers.handleNormalizeCompose),
		apischema.Call[apischema.StackNameRequest, apischema.ComposeFilePathResponse]("docker.get_compose_file_path", apischema.RetrySafe()).Handle(handlers.handleGetComposeFilePath),
		apischema.Call[apischema.DirPathRequest, apischema.DirectoryValidationResult]("docker.validate_stack_directory", apischema.RetrySafe()).Handle(handlers.handleValidateStackDirectory),
		apischema.Call[apischema.ProjectNameRequest, apischema.NoResponse]("docker.delete_compose_stack", apischema.NoEndpoint()).HandleVoid(handlers.handleDeleteComposeStack),
		apischema.Call[apischema.NoRequest, *apischema.DockerSystemInfo]("docker.get_docker_info", apischema.RetrySafe()).Handle(handlers.handleGetDockerInfo),
		apischema.Call[apischema.IdentifierRequest, apischema.DockerIconURIResponse]("docker.get_icon_uri").Handle(handlers.handleGetIconURI),
		apischema.Call[apischema.IdentifierRequest, apischema.DockerIconDataResponse]("docker.get_icon").Handle(handlers.handleGetIcon),
		apischema.Call[apischema.IdentifierRequest, apischema.DockerIconInfoResponse]("docker.get_icon_info", apischema.RetrySafe()).Handle(handlers.handleGetIconInfo),
		apischema.Call[apischema.NoRequest, apischema.MessageResponse]("docker.clear_icon_cache").Handle(handlers.handleClearIconCache),
		apischema.Call[apischema.NoRequest, apischema.DockerStartedFailedResponse]("docker.start_all_stopped").Handle(handlers.handleStartAllStopped),
		apischema.Call[apischema.NoRequest, apischema.DockerStoppedFailedResponse]("docker.stop_all_running").Handle(handlers.handleStopAllRunning),
		apischema.Call[apischema.NoRequest, apischema.DockerUpdateCheckResult]("docker.check_updates").Handle(handlers.handleCheckUpdates),
		apischema.Call[apischema.ContainerIDRequest, apischema.DockerUpdateCheckResult]("docker.check_container_update").Handle(handlers.handleCheckContainerUpdate),
		apischema.Call[apischema.ContainerIDRequest, apischema.DockerContainerUpdateResult]("docker.update_container").Handle(handlers.handleUpdateContainer),
		apischema.Call[apischema.NoRequest, apischema.DockerContainerAutoUpdateState]("docker.get_container_auto_update", apischema.RetrySafe()).Handle(handlers.handleGetContainerAutoUpdate),
		apischema.Call[apischema.DockerContainerAutoUpdateOptions, apischema.DockerContainerAutoUpdateState]("docker.set_container_auto_update").Handle(handlers.handleSetContainerAutoUpdate),
		apischema.Call[apischema.NoRequest, apischema.CaddyStatusResponse]("docker.get_caddy_status", apischema.RetrySafe()).Handle(handlers.handleGetCaddyStatus),
		apischema.Call[apischema.NoRequest, apischema.MessageResponse]("docker.enable_caddy").Handle(handlers.handleEnableCaddy),
		apischema.Call[apischema.NoRequest, apischema.MessageResponse]("docker.disable_caddy").Handle(handlers.handleDisableCaddy),
		apischema.Call[apischema.NoRequest, apischema.MessageResponse]("docker.reload_caddy").Handle(handlers.handleReloadCaddy),
		apischema.Call[apischema.ContainerIDRequest, apischema.MessageResponse]("docker.connect_to_proxy").Handle(handlers.handleConnectToProxy),
		apischema.Call[apischema.DockerSystemPruneRequest, *apischema.DockerSystemPruneResponse]("docker.system_prune").Handle(handlers.handleSystemPrune),
		apischema.DuplexRoute[apischema.DockerLogsFollowRequest, apischema.NoResponse](routeDockerLogsFollow, apischema.NoEndpoint()).Duplex(
			func(ctx context.Context, stream net.Conn, req apischema.DockerLogsFollowRequest) error {
				return streamDockerLogsChannel(ctx, stream, rt, req)
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
