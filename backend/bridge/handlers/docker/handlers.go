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

	apischema.RegisterRoutes(router, []apischema.HandlerBinding{
		{Route: RouteListContainers, Handle: handlers.handleListContainers},
		{Route: RouteStartContainer, Handle: handlers.handleStartContainer},
		{Route: RouteStopContainer, Handle: handlers.handleStopContainer},
		{Route: RouteRemoveContainer, Handle: handlers.handleRemoveContainer},
		{Route: RouteRestartContainer, Handle: handlers.handleRestartContainer},
		{Route: RouteListImages, Handle: handlers.handleListImages},
		{Route: RouteDeleteImage, Handle: handlers.handleDeleteImage},
		{Route: RouteListNetworks, Handle: handlers.handleListNetworks},
		{Route: RouteCreateNetwork, Handle: handlers.handleCreateNetwork},
		{Route: RouteDeleteNetwork, Handle: handlers.handleDeleteNetwork},
		{Route: RouteListVolumes, Handle: handlers.handleListVolumes},
		{Route: RouteCreateVolume, Handle: handlers.handleCreateVolume},
		{Route: RouteDeleteVolume, Handle: handlers.handleDeleteVolume},
		{Route: RouteListComposeProjects, Handle: handlers.handleListComposeProjects},
		{Route: RouteGetComposeProject, Handle: handlers.handleGetComposeProject},
		{Route: RouteComposeUp, Handle: handlers.handleComposeUp},
		{Route: RouteComposeDown, Handle: handlers.handleComposeDown},
		{Route: RouteComposeStop, Handle: handlers.handleComposeStop},
		{Route: RouteComposeRestart, Handle: handlers.handleComposeRestart},
		{Route: RouteDeleteStack, Handle: handlers.handleDeleteStack},
		{Route: RouteGetDockerFolders, Handle: handlers.handleGetDockerFolders},
		{Route: RouteValidateCompose, Handle: handlers.handleValidateCompose},
		{Route: RouteNormalizeCompose, Handle: handlers.handleNormalizeCompose},
		{Route: RouteGetComposeFilePath, Handle: handlers.handleGetComposeFilePath},
		{Route: RouteValidateStackDirectory, Handle: handlers.handleValidateStackDirectory},
		{Route: RouteReindexDockerFolders, Handle: handlers.handleReindexDockerFolders},
		{Route: RouteDeleteComposeStack, Handle: handlers.handleDeleteComposeStack},
		{Route: RouteGetDockerInfo, Handle: handlers.handleGetDockerInfo},
		{Route: RouteGetIconURI, Handle: handlers.handleGetIconURI},
		{Route: RouteGetIcon, Handle: handlers.handleGetIcon},
		{Route: RouteGetIconInfo, Handle: handlers.handleGetIconInfo},
		{Route: RouteClearIconCache, Handle: handlers.handleClearIconCache},
		{Route: RouteStartAllStopped, Handle: handlers.handleStartAllStopped},
		{Route: RouteStopAllRunning, Handle: handlers.handleStopAllRunning},
		{Route: RouteListAutoUpdateContainers, Handle: handlers.handleListAutoUpdateContainers},
		{Route: RouteSetAutoUpdate, Handle: handlers.handleSetAutoUpdate},
		{Route: RouteGetCaddyStatus, Handle: handlers.handleGetCaddyStatus},
		{Route: RouteEnableCaddy, Handle: handlers.handleEnableCaddy},
		{Route: RouteDisableCaddy, Handle: handlers.handleDisableCaddy},
		{Route: RouteReloadCaddy, Handle: handlers.handleReloadCaddy},
		{Route: RouteConnectToProxy, Handle: handlers.handleConnectToProxy},
		{Route: RouteSystemPrune, Handle: handlers.handleSystemPrune},
	})

	apischema.AttachRunner(router, apischema.RunnerBinding{
		Route: RouteLogsFollow,
		Runner: func(ctx context.Context, job *bridgeipc.Job, req apischema.DockerLogsFollowRequest) (any, error) {
			return runDockerLogsJob(ctx, rt, job, req)
		},
		Policy: bridgeipc.StreamDefault,
	})
}
