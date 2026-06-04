package docker

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var routes = apischema.NewRouteCatalog()

var RouteClearIconCache = routes.Job("docker.clear_icon_cache", apischema.NoRequest(), apischema.TypeOf[apischema.MessageResponse]())
var RouteCompose = routes.Runner("docker.compose", apischema.TypeOf[apischema.DockerComposeRequest](), apischema.TypeOf[apischema.JobSnapshot]())
var RouteComposeDown = routes.Job("docker.compose_down", apischema.TypeOf[apischema.ProjectNameRequest](), apischema.TypeOf[any]())
var RouteComposeRestart = routes.Job("docker.compose_restart", apischema.TypeOf[apischema.ProjectNameRequest](), apischema.TypeOf[any]())
var RouteComposeStop = routes.Job("docker.compose_stop", apischema.TypeOf[apischema.ProjectNameRequest](), apischema.TypeOf[any]())
var RouteComposeUp = routes.Job("docker.compose_up", apischema.TypeOf[apischema.ProjectNameRequest](), apischema.TypeOf[any]())
var RouteConnectToProxy = routes.Job("docker.connect_to_proxy", apischema.TypeOf[apischema.ContainerIDRequest](), apischema.TypeOf[apischema.MessageResponse]())
var RouteCreateNetwork = routes.Job("docker.create_network", apischema.TypeOf[apischema.NameRequest](), apischema.NoResponse())
var RouteCreateVolume = routes.Job("docker.create_volume", apischema.TypeOf[apischema.NameRequest](), apischema.NoResponse())
var RouteDeleteComposeStack = routes.Job("docker.delete_compose_stack", apischema.TypeOf[apischema.ProjectNameRequest](), apischema.NoResponse(), apischema.NoEndpoint())
var RouteDeleteImage = routes.Job("docker.delete_image", apischema.TypeOf[apischema.ImageIDRequest](), apischema.NoResponse())
var RouteDeleteNetwork = routes.Job("docker.delete_network", apischema.TypeOf[apischema.IDRequest](), apischema.NoResponse())
var RouteDeleteStack = routes.Job("docker.delete_stack", apischema.TypeOf[apischema.DeleteStackRequest](), apischema.TypeOf[apischema.DeleteStackResult]())
var RouteDeleteVolume = routes.Job("docker.delete_volume", apischema.TypeOf[apischema.NameRequest](), apischema.NoResponse())
var RouteDisableCaddy = routes.Job("docker.disable_caddy", apischema.NoRequest(), apischema.TypeOf[apischema.MessageResponse]())
var RouteEnableCaddy = routes.Job("docker.enable_caddy", apischema.NoRequest(), apischema.TypeOf[apischema.MessageResponse]())
var RouteGetCaddyStatus = routes.Query("docker.get_caddy_status", apischema.NoRequest(), apischema.TypeOf[apischema.CaddyStatusResponse]())
var RouteGetComposeFilePath = routes.Query("docker.get_compose_file_path", apischema.TypeOf[apischema.StackNameRequest](), apischema.TypeOf[apischema.ComposeFilePathResponse]())
var RouteGetComposeProject = routes.Query("docker.get_compose_project", apischema.TypeOf[apischema.ProjectNameRequest](), apischema.TypeOf[apischema.ComposeProject]())
var RouteGetDockerFolders = routes.Query("docker.get_docker_folders", apischema.NoRequest(), apischema.TypeOf[apischema.DockerFoldersResponse]())
var RouteGetDockerInfo = routes.Query("docker.get_docker_info", apischema.NoRequest(), apischema.TypeOf[apischema.DockerSystemInfo]())
var RouteGetIcon = routes.Query("docker.get_icon", apischema.TypeOf[apischema.IdentifierRequest](), apischema.TypeOf[apischema.DockerIconDataResponse]())
var RouteGetIconInfo = routes.Query("docker.get_icon_info", apischema.TypeOf[apischema.IdentifierRequest](), apischema.TypeOf[apischema.DockerIconInfoResponse]())
var RouteGetIconURI = routes.Query("docker.get_icon_uri", apischema.TypeOf[apischema.IdentifierRequest](), apischema.TypeOf[apischema.DockerIconURIResponse]())
var RouteIndexer = routes.Runner("docker.indexer", apischema.NoRequest(), apischema.TypeOf[apischema.JobSnapshot]())
var RouteListAutoUpdateContainers = routes.Query("docker.list_auto_update_containers", apischema.NoRequest(), apischema.TypeOf[[]string]())
var RouteListComposeProjects = routes.Query("docker.list_compose_projects", apischema.NoRequest(), apischema.TypeOf[[]apischema.ComposeProject]())
var RouteListContainers = routes.Query("docker.list_containers", apischema.NoRequest(), apischema.TypeOf[[]apischema.ContainerInfo]())
var RouteListImages = routes.Query("docker.list_images", apischema.NoRequest(), apischema.TypeOf[[]apischema.DockerImage]())
var RouteListNetworks = routes.Query("docker.list_networks", apischema.NoRequest(), apischema.TypeOf[[]apischema.DockerNetwork]())
var RouteListVolumes = routes.Query("docker.list_volumes", apischema.NoRequest(), apischema.TypeOf[[]apischema.DockerVolume]())
var RouteLogsFollow = routes.Runner("docker.logs.follow", apischema.TypeOf[apischema.DockerLogsFollowRequest](), apischema.NoResponse(), apischema.NoEndpoint())
var RouteNormalizeCompose = routes.Query("docker.normalize_compose", apischema.TypeOf[apischema.ContentRequest](), apischema.NoResponse(), apischema.NoEndpoint())
var RouteReindexDockerFolders = routes.Job("docker.reindex_docker_folders", apischema.NoRequest(), apischema.NoResponse(), apischema.NoEndpoint())
var RouteReloadCaddy = routes.Job("docker.reload_caddy", apischema.NoRequest(), apischema.TypeOf[apischema.MessageResponse]())
var RouteRemoveContainer = routes.Job("docker.remove_container", apischema.TypeOf[apischema.ContainerIDRequest](), apischema.NoResponse())
var RouteRestartContainer = routes.Job("docker.restart_container", apischema.TypeOf[apischema.ContainerIDRequest](), apischema.NoResponse())
var RouteSetAutoUpdate = routes.Job("docker.set_auto_update", apischema.TypeOf[apischema.DockerSetAutoUpdateRequest](), apischema.TypeOf[apischema.MessageResponse]())
var RouteStartAllStopped = routes.Job("docker.start_all_stopped", apischema.NoRequest(), apischema.TypeOf[apischema.DockerStartedFailedResponse]())
var RouteStartContainer = routes.Job("docker.start_container", apischema.TypeOf[apischema.ContainerIDRequest](), apischema.NoResponse())
var RouteStopAllRunning = routes.Job("docker.stop_all_running", apischema.NoRequest(), apischema.TypeOf[apischema.DockerStoppedFailedResponse]())
var RouteStopContainer = routes.Job("docker.stop_container", apischema.TypeOf[apischema.ContainerIDRequest](), apischema.NoResponse())
var RouteSystemPrune = routes.Job("docker.system_prune", apischema.TypeOf[apischema.DockerSystemPruneRequest](), apischema.TypeOf[apischema.DockerSystemPruneResponse]())
var RouteValidateCompose = routes.Query("docker.validate_compose", apischema.TypeOf[apischema.ContentRequest](), apischema.TypeOf[apischema.ValidateComposeResponse]())
var RouteValidateStackDirectory = routes.Query("docker.validate_stack_directory", apischema.TypeOf[apischema.DirPathRequest](), apischema.TypeOf[apischema.DirectoryValidationResult]())

var Routes = routes.All()

// RegisterHandlers registers all docker handlers with the global registry
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	handlers := newDockerHandlers(rt)
	prepareDockerHandlers(router, handlers)

	apischema.RegisterRoutes(router,
		RouteListContainers.Handle(handlers.handleListContainers),
		RouteStartContainer.Handle(handlers.handleStartContainer),
		RouteStopContainer.Handle(handlers.handleStopContainer),
		RouteRemoveContainer.Handle(handlers.handleRemoveContainer),
		RouteRestartContainer.Handle(handlers.handleRestartContainer),
		RouteListImages.Handle(handlers.handleListImages),
		RouteDeleteImage.Handle(handlers.handleDeleteImage),
		RouteListNetworks.Handle(handlers.handleListNetworks),
		RouteCreateNetwork.Handle(handlers.handleCreateNetwork),
		RouteDeleteNetwork.Handle(handlers.handleDeleteNetwork),
		RouteListVolumes.Handle(handlers.handleListVolumes),
		RouteCreateVolume.Handle(handlers.handleCreateVolume),
		RouteDeleteVolume.Handle(handlers.handleDeleteVolume),
		RouteListComposeProjects.Handle(handlers.handleListComposeProjects),
		RouteGetComposeProject.Handle(handlers.handleGetComposeProject),
		RouteComposeUp.Handle(handlers.handleComposeUp),
		RouteComposeDown.Handle(handlers.handleComposeDown),
		RouteComposeStop.Handle(handlers.handleComposeStop),
		RouteComposeRestart.Handle(handlers.handleComposeRestart),
		RouteDeleteStack.Handle(handlers.handleDeleteStack),
		RouteGetDockerFolders.Handle(handlers.handleGetDockerFolders),
		RouteValidateCompose.Handle(handlers.handleValidateCompose),
		RouteNormalizeCompose.Handle(handlers.handleNormalizeCompose),
		RouteGetComposeFilePath.Handle(handlers.handleGetComposeFilePath),
		RouteValidateStackDirectory.Handle(handlers.handleValidateStackDirectory),
		RouteReindexDockerFolders.Handle(handlers.handleReindexDockerFolders),
		RouteDeleteComposeStack.Handle(handlers.handleDeleteComposeStack),
		RouteGetDockerInfo.Handle(handlers.handleGetDockerInfo),
		RouteGetIconURI.Handle(handlers.handleGetIconURI),
		RouteGetIcon.Handle(handlers.handleGetIcon),
		RouteGetIconInfo.Handle(handlers.handleGetIconInfo),
		RouteClearIconCache.Handle(handlers.handleClearIconCache),
		RouteStartAllStopped.Handle(handlers.handleStartAllStopped),
		RouteStopAllRunning.Handle(handlers.handleStopAllRunning),
		RouteListAutoUpdateContainers.Handle(handlers.handleListAutoUpdateContainers),
		RouteSetAutoUpdate.Handle(handlers.handleSetAutoUpdate),
		RouteGetCaddyStatus.Handle(handlers.handleGetCaddyStatus),
		RouteEnableCaddy.Handle(handlers.handleEnableCaddy),
		RouteDisableCaddy.Handle(handlers.handleDisableCaddy),
		RouteReloadCaddy.Handle(handlers.handleReloadCaddy),
		RouteConnectToProxy.Handle(handlers.handleConnectToProxy),
		RouteSystemPrune.Handle(handlers.handleSystemPrune),
	)

	apischema.AttachRunner(router, RouteLogsFollow.Run(func(ctx context.Context, job *bridgeipc.Job, req apischema.DockerLogsFollowRequest) (any, error) {
		return runDockerLogsJob(ctx, rt, job, req)
	}, bridgeipc.StreamDefault))
}
