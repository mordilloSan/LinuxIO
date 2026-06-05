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
		apischema.Query("docker.list_containers", apischema.NoRequest(), apischema.TypeOf[[]apischema.ContainerInfo]()).Handle(handlers.handleListContainers),
		apischema.Job("docker.start_container", apischema.TypeOf[apischema.ContainerIDRequest](), apischema.NoResponse()).Handle(handlers.handleStartContainer),
		apischema.Job("docker.stop_container", apischema.TypeOf[apischema.ContainerIDRequest](), apischema.NoResponse()).Handle(handlers.handleStopContainer),
		apischema.Job("docker.remove_container", apischema.TypeOf[apischema.ContainerIDRequest](), apischema.NoResponse()).Handle(handlers.handleRemoveContainer),
		apischema.Job("docker.restart_container", apischema.TypeOf[apischema.ContainerIDRequest](), apischema.NoResponse()).Handle(handlers.handleRestartContainer),
		apischema.Query("docker.list_images", apischema.NoRequest(), apischema.TypeOf[[]apischema.DockerImage]()).Handle(handlers.handleListImages),
		apischema.Job("docker.delete_image", apischema.TypeOf[apischema.ImageIDRequest](), apischema.NoResponse()).Handle(handlers.handleDeleteImage),
		apischema.Query("docker.list_networks", apischema.NoRequest(), apischema.TypeOf[[]apischema.DockerNetwork]()).Handle(handlers.handleListNetworks),
		apischema.Job("docker.create_network", apischema.TypeOf[apischema.NameRequest](), apischema.NoResponse()).Handle(handlers.handleCreateNetwork),
		apischema.Job("docker.delete_network", apischema.TypeOf[apischema.IDRequest](), apischema.NoResponse()).Handle(handlers.handleDeleteNetwork),
		apischema.Query("docker.list_volumes", apischema.NoRequest(), apischema.TypeOf[[]apischema.DockerVolume]()).Handle(handlers.handleListVolumes),
		apischema.Job("docker.create_volume", apischema.TypeOf[apischema.NameRequest](), apischema.NoResponse()).Handle(handlers.handleCreateVolume),
		apischema.Job("docker.delete_volume", apischema.TypeOf[apischema.NameRequest](), apischema.NoResponse()).Handle(handlers.handleDeleteVolume),
		apischema.Query("docker.list_compose_projects", apischema.NoRequest(), apischema.TypeOf[[]apischema.ComposeProject]()).Handle(handlers.handleListComposeProjects),
		apischema.Query("docker.get_compose_project", apischema.TypeOf[apischema.ProjectNameRequest](), apischema.TypeOf[apischema.ComposeProject]()).Handle(handlers.handleGetComposeProject),
		apischema.Job("docker.compose_up", apischema.TypeOf[apischema.ProjectNameRequest](), apischema.TypeOf[any]()).Handle(handlers.handleComposeUp),
		apischema.Job("docker.compose_down", apischema.TypeOf[apischema.ProjectNameRequest](), apischema.TypeOf[any]()).Handle(handlers.handleComposeDown),
		apischema.Job("docker.compose_stop", apischema.TypeOf[apischema.ProjectNameRequest](), apischema.TypeOf[any]()).Handle(handlers.handleComposeStop),
		apischema.Job("docker.compose_restart", apischema.TypeOf[apischema.ProjectNameRequest](), apischema.TypeOf[any]()).Handle(handlers.handleComposeRestart),
		apischema.Job("docker.delete_stack", apischema.TypeOf[apischema.DeleteStackRequest](), apischema.TypeOf[apischema.DeleteStackResult]()).Handle(handlers.handleDeleteStack),
		apischema.Query("docker.get_docker_folders", apischema.NoRequest(), apischema.TypeOf[apischema.DockerFoldersResponse]()).Handle(handlers.handleGetDockerFolders),
		apischema.Query("docker.validate_compose", apischema.TypeOf[apischema.ContentRequest](), apischema.TypeOf[apischema.ValidateComposeResponse]()).Handle(handlers.handleValidateCompose),
		apischema.Query("docker.normalize_compose", apischema.TypeOf[apischema.ContentRequest](), apischema.NoResponse(), apischema.NoEndpoint()).Handle(handlers.handleNormalizeCompose),
		apischema.Query("docker.get_compose_file_path", apischema.TypeOf[apischema.StackNameRequest](), apischema.TypeOf[apischema.ComposeFilePathResponse]()).Handle(handlers.handleGetComposeFilePath),
		apischema.Query("docker.validate_stack_directory", apischema.TypeOf[apischema.DirPathRequest](), apischema.TypeOf[apischema.DirectoryValidationResult]()).Handle(handlers.handleValidateStackDirectory),
		apischema.Job("docker.reindex_docker_folders", apischema.NoRequest(), apischema.NoResponse(), apischema.NoEndpoint()).Handle(handlers.handleReindexDockerFolders),
		apischema.Job("docker.delete_compose_stack", apischema.TypeOf[apischema.ProjectNameRequest](), apischema.NoResponse(), apischema.NoEndpoint()).Handle(handlers.handleDeleteComposeStack),
		apischema.Query("docker.get_docker_info", apischema.NoRequest(), apischema.TypeOf[apischema.DockerSystemInfo]()).Handle(handlers.handleGetDockerInfo),
		apischema.Query("docker.get_icon_uri", apischema.TypeOf[apischema.IdentifierRequest](), apischema.TypeOf[apischema.DockerIconURIResponse]()).Handle(handlers.handleGetIconURI),
		apischema.Query("docker.get_icon", apischema.TypeOf[apischema.IdentifierRequest](), apischema.TypeOf[apischema.DockerIconDataResponse]()).Handle(handlers.handleGetIcon),
		apischema.Query("docker.get_icon_info", apischema.TypeOf[apischema.IdentifierRequest](), apischema.TypeOf[apischema.DockerIconInfoResponse]()).Handle(handlers.handleGetIconInfo),
		apischema.Job("docker.clear_icon_cache", apischema.NoRequest(), apischema.TypeOf[apischema.MessageResponse]()).Handle(handlers.handleClearIconCache),
		apischema.Job("docker.start_all_stopped", apischema.NoRequest(), apischema.TypeOf[apischema.DockerStartedFailedResponse]()).Handle(handlers.handleStartAllStopped),
		apischema.Job("docker.stop_all_running", apischema.NoRequest(), apischema.TypeOf[apischema.DockerStoppedFailedResponse]()).Handle(handlers.handleStopAllRunning),
		apischema.Query("docker.list_auto_update_containers", apischema.NoRequest(), apischema.TypeOf[[]string]()).Handle(handlers.handleListAutoUpdateContainers),
		apischema.Job("docker.set_auto_update", apischema.TypeOf[apischema.DockerSetAutoUpdateRequest](), apischema.TypeOf[apischema.MessageResponse]()).Handle(handlers.handleSetAutoUpdate),
		apischema.Query("docker.get_caddy_status", apischema.NoRequest(), apischema.TypeOf[apischema.CaddyStatusResponse]()).Handle(handlers.handleGetCaddyStatus),
		apischema.Job("docker.enable_caddy", apischema.NoRequest(), apischema.TypeOf[apischema.MessageResponse]()).Handle(handlers.handleEnableCaddy),
		apischema.Job("docker.disable_caddy", apischema.NoRequest(), apischema.TypeOf[apischema.MessageResponse]()).Handle(handlers.handleDisableCaddy),
		apischema.Job("docker.reload_caddy", apischema.NoRequest(), apischema.TypeOf[apischema.MessageResponse]()).Handle(handlers.handleReloadCaddy),
		apischema.Job("docker.connect_to_proxy", apischema.TypeOf[apischema.ContainerIDRequest](), apischema.TypeOf[apischema.MessageResponse]()).Handle(handlers.handleConnectToProxy),
		apischema.Job("docker.system_prune", apischema.TypeOf[apischema.DockerSystemPruneRequest](), apischema.TypeOf[apischema.DockerSystemPruneResponse]()).Handle(handlers.handleSystemPrune),
		apischema.Runner(routeDockerLogsFollow, apischema.TypeOf[apischema.DockerLogsFollowRequest](), apischema.NoResponse(), apischema.NoEndpoint()).Run(
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
