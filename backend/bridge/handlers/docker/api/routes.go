package api

import (
	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var ClearIconCache = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "docker.clear_icon_cache", Mode: bridgeipc.ModeJob, Request: apischema.NoRequest(), Result: apischema.TypeOf[apischema.MessageResponse]()}
var Compose = apischema.RouteSpec{Kind: apischema.KindRunner, Route: "docker.compose", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.DockerComposeRequest](), Result: apischema.TypeOf[apischema.JobSnapshot]()}
var ComposeDown = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "docker.compose_down", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.ProjectNameRequest](), Result: apischema.TypeOf[any]()}
var ComposeRestart = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "docker.compose_restart", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.ProjectNameRequest](), Result: apischema.TypeOf[any]()}
var ComposeStop = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "docker.compose_stop", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.ProjectNameRequest](), Result: apischema.TypeOf[any]()}
var ComposeUp = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "docker.compose_up", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.ProjectNameRequest](), Result: apischema.TypeOf[any]()}
var ConnectToProxy = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "docker.connect_to_proxy", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.ContainerIDRequest](), Result: apischema.TypeOf[apischema.MessageResponse]()}
var CreateNetwork = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "docker.create_network", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.NameRequest](), Result: apischema.NoResponse()}
var CreateVolume = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "docker.create_volume", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.NameRequest](), Result: apischema.NoResponse()}
var DeleteComposeStack = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "docker.delete_compose_stack", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.ProjectNameRequest](), Result: apischema.NoResponse(), NoEndpoint: true}
var DeleteImage = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "docker.delete_image", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.ImageIDRequest](), Result: apischema.NoResponse()}
var DeleteNetwork = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "docker.delete_network", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.IDRequest](), Result: apischema.NoResponse()}
var DeleteStack = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "docker.delete_stack", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.DeleteStackRequest](), Result: apischema.TypeOf[apischema.DeleteStackResult]()}
var DeleteVolume = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "docker.delete_volume", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.NameRequest](), Result: apischema.NoResponse()}
var DisableCaddy = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "docker.disable_caddy", Mode: bridgeipc.ModeJob, Request: apischema.NoRequest(), Result: apischema.TypeOf[apischema.MessageResponse]()}
var EnableCaddy = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "docker.enable_caddy", Mode: bridgeipc.ModeJob, Request: apischema.NoRequest(), Result: apischema.TypeOf[apischema.MessageResponse]()}
var GetCaddyStatus = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "docker.get_caddy_status", Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.TypeOf[apischema.CaddyStatusResponse]()}
var GetComposeFilePath = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "docker.get_compose_file_path", Mode: bridgeipc.ModeQuery, Request: apischema.TypeOf[apischema.StackNameRequest](), Result: apischema.TypeOf[apischema.ComposeFilePathResponse]()}
var GetComposeProject = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "docker.get_compose_project", Mode: bridgeipc.ModeQuery, Request: apischema.TypeOf[apischema.ProjectNameRequest](), Result: apischema.TypeOf[apischema.ComposeProject]()}
var GetDockerFolders = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "docker.get_docker_folders", Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.TypeOf[apischema.DockerFoldersResponse]()}
var GetDockerInfo = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "docker.get_docker_info", Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.TypeOf[apischema.DockerSystemInfo]()}
var GetIcon = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "docker.get_icon", Mode: bridgeipc.ModeQuery, Request: apischema.TypeOf[apischema.IdentifierRequest](), Result: apischema.TypeOf[apischema.DockerIconDataResponse]()}
var GetIconInfo = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "docker.get_icon_info", Mode: bridgeipc.ModeQuery, Request: apischema.TypeOf[apischema.IdentifierRequest](), Result: apischema.TypeOf[apischema.DockerIconInfoResponse]()}
var GetIconURI = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "docker.get_icon_uri", Mode: bridgeipc.ModeQuery, Request: apischema.TypeOf[apischema.IdentifierRequest](), Result: apischema.TypeOf[apischema.DockerIconURIResponse]()}
var Indexer = apischema.RouteSpec{Kind: apischema.KindRunner, Route: "docker.indexer", Mode: bridgeipc.ModeJob, Request: apischema.NoRequest(), Result: apischema.TypeOf[apischema.JobSnapshot]()}
var ListAutoUpdateContainers = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "docker.list_auto_update_containers", Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.TypeOf[[]string]()}
var ListComposeProjects = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "docker.list_compose_projects", Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.TypeOf[[]apischema.ComposeProject]()}
var ListContainers = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "docker.list_containers", Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.TypeOf[[]apischema.ContainerInfo]()}
var ListImages = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "docker.list_images", Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.TypeOf[[]apischema.DockerImage]()}
var ListNetworks = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "docker.list_networks", Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.TypeOf[[]apischema.DockerNetwork]()}
var ListVolumes = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "docker.list_volumes", Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.TypeOf[[]apischema.DockerVolume]()}
var LogsFollow = apischema.RouteSpec{Kind: apischema.KindRunner, Route: "docker.logs.follow", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.DockerLogsFollowRequest](), Result: apischema.NoResponse(), NoEndpoint: true}
var NormalizeCompose = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "docker.normalize_compose", Mode: bridgeipc.ModeQuery, Request: apischema.TypeOf[apischema.ContentRequest](), Result: apischema.NoResponse(), NoEndpoint: true}
var ReindexDockerFolders = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "docker.reindex_docker_folders", Mode: bridgeipc.ModeJob, Request: apischema.NoRequest(), Result: apischema.NoResponse(), NoEndpoint: true}
var ReloadCaddy = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "docker.reload_caddy", Mode: bridgeipc.ModeJob, Request: apischema.NoRequest(), Result: apischema.TypeOf[apischema.MessageResponse]()}
var RemoveContainer = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "docker.remove_container", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.ContainerIDRequest](), Result: apischema.NoResponse()}
var RestartContainer = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "docker.restart_container", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.ContainerIDRequest](), Result: apischema.NoResponse()}
var SetAutoUpdate = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "docker.set_auto_update", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.DockerSetAutoUpdateRequest](), Result: apischema.TypeOf[apischema.MessageResponse]()}
var StartAllStopped = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "docker.start_all_stopped", Mode: bridgeipc.ModeJob, Request: apischema.NoRequest(), Result: apischema.TypeOf[apischema.DockerStartedFailedResponse]()}
var StartContainer = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "docker.start_container", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.ContainerIDRequest](), Result: apischema.NoResponse()}
var StopAllRunning = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "docker.stop_all_running", Mode: bridgeipc.ModeJob, Request: apischema.NoRequest(), Result: apischema.TypeOf[apischema.DockerStoppedFailedResponse]()}
var StopContainer = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "docker.stop_container", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.ContainerIDRequest](), Result: apischema.NoResponse()}
var SystemPrune = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "docker.system_prune", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.DockerSystemPruneRequest](), Result: apischema.TypeOf[apischema.DockerSystemPruneResponse]()}
var ValidateCompose = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "docker.validate_compose", Mode: bridgeipc.ModeQuery, Request: apischema.TypeOf[apischema.ContentRequest](), Result: apischema.TypeOf[apischema.ValidateComposeResponse]()}
var ValidateStackDirectory = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "docker.validate_stack_directory", Mode: bridgeipc.ModeQuery, Request: apischema.TypeOf[apischema.DirPathRequest](), Result: apischema.TypeOf[apischema.DirectoryValidationResult]()}

var Routes = []apischema.RouteSpec{
	ClearIconCache,
	Compose,
	ComposeDown,
	ComposeRestart,
	ComposeStop,
	ComposeUp,
	ConnectToProxy,
	CreateNetwork,
	CreateVolume,
	DeleteComposeStack,
	DeleteImage,
	DeleteNetwork,
	DeleteStack,
	DeleteVolume,
	DisableCaddy,
	EnableCaddy,
	GetCaddyStatus,
	GetComposeFilePath,
	GetComposeProject,
	GetDockerFolders,
	GetDockerInfo,
	GetIcon,
	GetIconInfo,
	GetIconURI,
	Indexer,
	ListAutoUpdateContainers,
	ListComposeProjects,
	ListContainers,
	ListImages,
	ListNetworks,
	ListVolumes,
	LogsFollow,
	NormalizeCompose,
	ReindexDockerFolders,
	ReloadCaddy,
	RemoveContainer,
	RestartContainer,
	SetAutoUpdate,
	StartAllStopped,
	StartContainer,
	StopAllRunning,
	StopContainer,
	SystemPrune,
	ValidateCompose,
	ValidateStackDirectory,
}
