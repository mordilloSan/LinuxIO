package docker

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var Routes = apischema.CombineRoutes(routeBindings(runtime.Runtime{}, dockerHandlers{}).Routes(), dockerJobRoutes)

func routeBindings(rt runtime.Runtime, handlers dockerHandlers) apischema.BindingSet {
	return apischema.Bindings(
		apischema.Query[apischema.NoRequest, []apischema.ContainerInfo]("docker.list_containers").Handle(handlers.handleListContainers),
		apischema.Job[apischema.ContainerIDRequest, apischema.NoResponse]("docker.start_container").Handle(handlers.handleStartContainer),
		apischema.Job[apischema.ContainerIDRequest, apischema.NoResponse]("docker.stop_container").Handle(handlers.handleStopContainer),
		apischema.Job[apischema.ContainerIDRequest, apischema.NoResponse]("docker.remove_container").Handle(handlers.handleRemoveContainer),
		apischema.Job[apischema.ContainerIDRequest, apischema.NoResponse]("docker.restart_container").Handle(handlers.handleRestartContainer),
		apischema.Query[apischema.NoRequest, []apischema.DockerImage]("docker.list_images").Handle(handlers.handleListImages),
		apischema.Job[apischema.ImageIDRequest, apischema.NoResponse]("docker.delete_image").Handle(handlers.handleDeleteImage),
		apischema.Query[apischema.NoRequest, []apischema.DockerNetwork]("docker.list_networks").Handle(handlers.handleListNetworks),
		apischema.Job[apischema.NameRequest, apischema.NoResponse]("docker.create_network").Handle(handlers.handleCreateNetwork),
		apischema.Job[apischema.IDRequest, apischema.NoResponse]("docker.delete_network").Handle(handlers.handleDeleteNetwork),
		apischema.Query[apischema.NoRequest, []apischema.DockerVolume]("docker.list_volumes").Handle(handlers.handleListVolumes),
		apischema.Job[apischema.NameRequest, apischema.NoResponse]("docker.create_volume").Handle(handlers.handleCreateVolume),
		apischema.Job[apischema.NameRequest, apischema.NoResponse]("docker.delete_volume").Handle(handlers.handleDeleteVolume),
		apischema.Query[apischema.NoRequest, []apischema.ComposeProject]("docker.list_compose_projects").Handle(handlers.handleListComposeProjects),
		apischema.Query[apischema.ProjectNameRequest, apischema.ComposeProject]("docker.get_compose_project").Handle(handlers.handleGetComposeProject),
		apischema.Job[apischema.ProjectNameRequest, any]("docker.compose_up").Handle(handlers.handleComposeUp),
		apischema.Job[apischema.ProjectNameRequest, any]("docker.compose_down").Handle(handlers.handleComposeDown),
		apischema.Job[apischema.ProjectNameRequest, any]("docker.compose_stop").Handle(handlers.handleComposeStop),
		apischema.Job[apischema.ProjectNameRequest, any]("docker.compose_restart").Handle(handlers.handleComposeRestart),
		apischema.Job[apischema.DeleteStackRequest, apischema.DeleteStackResult]("docker.delete_stack").Handle(handlers.handleDeleteStack),
		apischema.Query[apischema.NoRequest, apischema.DockerFoldersResponse]("docker.get_docker_folders").Handle(handlers.handleGetDockerFolders),
		apischema.Query[apischema.ContentRequest, apischema.ValidateComposeResponse]("docker.validate_compose").Handle(handlers.handleValidateCompose),
		apischema.Query[apischema.ContentRequest, apischema.NoResponse]("docker.normalize_compose", apischema.NoEndpoint()).Handle(handlers.handleNormalizeCompose),
		apischema.Query[apischema.StackNameRequest, apischema.ComposeFilePathResponse]("docker.get_compose_file_path").Handle(handlers.handleGetComposeFilePath),
		apischema.Query[apischema.DirPathRequest, apischema.DirectoryValidationResult]("docker.validate_stack_directory").Handle(handlers.handleValidateStackDirectory),
		apischema.Job[apischema.NoRequest, apischema.NoResponse]("docker.reindex_docker_folders", apischema.NoEndpoint()).Handle(handlers.handleReindexDockerFolders),
		apischema.Job[apischema.ProjectNameRequest, apischema.NoResponse]("docker.delete_compose_stack", apischema.NoEndpoint()).Handle(handlers.handleDeleteComposeStack),
		apischema.Query[apischema.NoRequest, apischema.DockerSystemInfo]("docker.get_docker_info").Handle(handlers.handleGetDockerInfo),
		apischema.Query[apischema.IdentifierRequest, apischema.DockerIconURIResponse]("docker.get_icon_uri").Handle(handlers.handleGetIconURI),
		apischema.Query[apischema.IdentifierRequest, apischema.DockerIconDataResponse]("docker.get_icon").Handle(handlers.handleGetIcon),
		apischema.Query[apischema.IdentifierRequest, apischema.DockerIconInfoResponse]("docker.get_icon_info").Handle(handlers.handleGetIconInfo),
		apischema.Job[apischema.NoRequest, apischema.MessageResponse]("docker.clear_icon_cache").Handle(handlers.handleClearIconCache),
		apischema.Job[apischema.NoRequest, apischema.DockerStartedFailedResponse]("docker.start_all_stopped").Handle(handlers.handleStartAllStopped),
		apischema.Job[apischema.NoRequest, apischema.DockerStoppedFailedResponse]("docker.stop_all_running").Handle(handlers.handleStopAllRunning),
		apischema.Job[apischema.NoRequest, apischema.DockerUpdateCheckResult]("docker.check_updates").Handle(handlers.handleCheckUpdates),
		apischema.Job[apischema.ContainerIDRequest, apischema.DockerContainerUpdateResult]("docker.update_container").Handle(handlers.handleUpdateContainer),
		apischema.Query[apischema.NoRequest, apischema.CaddyStatusResponse]("docker.get_caddy_status").Handle(handlers.handleGetCaddyStatus),
		apischema.Job[apischema.NoRequest, apischema.MessageResponse]("docker.enable_caddy").Handle(handlers.handleEnableCaddy),
		apischema.Job[apischema.NoRequest, apischema.MessageResponse]("docker.disable_caddy").Handle(handlers.handleDisableCaddy),
		apischema.Job[apischema.NoRequest, apischema.MessageResponse]("docker.reload_caddy").Handle(handlers.handleReloadCaddy),
		apischema.Job[apischema.ContainerIDRequest, apischema.MessageResponse]("docker.connect_to_proxy").Handle(handlers.handleConnectToProxy),
		apischema.Job[apischema.DockerSystemPruneRequest, apischema.DockerSystemPruneResponse]("docker.system_prune").Handle(handlers.handleSystemPrune),
		apischema.Runner[apischema.DockerLogsFollowRequest, apischema.NoResponse](routeDockerLogsFollow, apischema.NoEndpoint()).Run(
			func(ctx context.Context, job *bridgeipc.Job, req apischema.DockerLogsFollowRequest) (any, error) {
				return runDockerLogsJob(ctx, rt, job, req)
			},
			bridgeipc.StreamDefault,
		),
	)
}

// RegisterHandlers registers all docker handlers with the global registry
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	handlers := newDockerHandlers(rt)
	prepareDockerHandlers(router, handlers)

	routeBindings(rt, handlers).Register(router)
}
