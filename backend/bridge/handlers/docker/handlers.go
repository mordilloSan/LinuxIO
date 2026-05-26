package docker

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

// RegisterHandlers registers all docker handlers with the global registry
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	handlers := newDockerHandlers(rt)
	prepareDockerHandlers(router, handlers)

	apischema.RegisterRoutes(router, "docker", []bridgeipc.Command{
		{Name: "list_containers", Mode: bridgeipc.ModeQuery, Handler: handlers.handleListContainers},
		{Name: "start_container", Mode: bridgeipc.ModeJob, Handler: handlers.handleStartContainer},
		{Name: "stop_container", Mode: bridgeipc.ModeJob, Handler: handlers.handleStopContainer},
		{Name: "remove_container", Mode: bridgeipc.ModeJob, Handler: handlers.handleRemoveContainer},
		{Name: "restart_container", Mode: bridgeipc.ModeJob, Handler: handlers.handleRestartContainer},
		{Name: "list_images", Mode: bridgeipc.ModeQuery, Handler: handlers.handleListImages},
		{Name: "delete_image", Mode: bridgeipc.ModeJob, Handler: handlers.handleDeleteImage},
		{Name: "list_networks", Mode: bridgeipc.ModeQuery, Handler: handlers.handleListNetworks},
		{Name: "create_network", Mode: bridgeipc.ModeJob, Handler: handlers.handleCreateNetwork},
		{Name: "delete_network", Mode: bridgeipc.ModeJob, Handler: handlers.handleDeleteNetwork},
		{Name: "list_volumes", Mode: bridgeipc.ModeQuery, Handler: handlers.handleListVolumes},
		{Name: "create_volume", Mode: bridgeipc.ModeJob, Handler: handlers.handleCreateVolume},
		{Name: "delete_volume", Mode: bridgeipc.ModeJob, Handler: handlers.handleDeleteVolume},
		{Name: "list_compose_projects", Mode: bridgeipc.ModeQuery, Handler: handlers.handleListComposeProjects},
		{Name: "get_compose_project", Mode: bridgeipc.ModeQuery, Handler: handlers.handleGetComposeProject},
		{Name: "compose_up", Mode: bridgeipc.ModeJob, Handler: handlers.handleComposeUp},
		{Name: "compose_down", Mode: bridgeipc.ModeJob, Handler: handlers.handleComposeDown},
		{Name: "compose_stop", Mode: bridgeipc.ModeJob, Handler: handlers.handleComposeStop},
		{Name: "compose_restart", Mode: bridgeipc.ModeJob, Handler: handlers.handleComposeRestart},
		{Name: "delete_stack", Mode: bridgeipc.ModeJob, Handler: handlers.handleDeleteStack},
		{Name: "get_docker_folders", Mode: bridgeipc.ModeQuery, Handler: handlers.handleGetDockerFolders},
		{Name: "validate_compose", Mode: bridgeipc.ModeQuery, Handler: handlers.handleValidateCompose},
		{Name: "normalize_compose", Mode: bridgeipc.ModeQuery, Handler: handlers.handleNormalizeCompose},
		{Name: "get_compose_file_path", Mode: bridgeipc.ModeQuery, Handler: handlers.handleGetComposeFilePath},
		{Name: "validate_stack_directory", Mode: bridgeipc.ModeQuery, Handler: handlers.handleValidateStackDirectory},
		{Name: "reindex_docker_folders", Mode: bridgeipc.ModeJob, Handler: handlers.handleReindexDockerFolders},
		{Name: "delete_compose_stack", Mode: bridgeipc.ModeJob, Handler: handlers.handleDeleteComposeStack},
		{Name: "get_docker_info", Mode: bridgeipc.ModeQuery, Handler: handlers.handleGetDockerInfo},
		{Name: "get_icon_uri", Mode: bridgeipc.ModeQuery, Handler: handlers.handleGetIconURI},
		{Name: "get_icon", Mode: bridgeipc.ModeQuery, Handler: handlers.handleGetIcon},
		{Name: "get_icon_info", Mode: bridgeipc.ModeQuery, Handler: handlers.handleGetIconInfo},
		{Name: "clear_icon_cache", Mode: bridgeipc.ModeJob, Handler: handlers.handleClearIconCache},
		{Name: "start_all_stopped", Mode: bridgeipc.ModeJob, Handler: handlers.handleStartAllStopped},
		{Name: "stop_all_running", Mode: bridgeipc.ModeJob, Handler: handlers.handleStopAllRunning},
		{Name: "list_auto_update_containers", Mode: bridgeipc.ModeQuery, Handler: handlers.handleListAutoUpdateContainers},
		{Name: "set_auto_update", Mode: bridgeipc.ModeJob, Handler: handlers.handleSetAutoUpdate},
		{Name: "get_caddy_status", Mode: bridgeipc.ModeQuery, Handler: handlers.handleGetCaddyStatus},
		{Name: "enable_caddy", Mode: bridgeipc.ModeJob, Handler: handlers.handleEnableCaddy},
		{Name: "disable_caddy", Mode: bridgeipc.ModeJob, Handler: handlers.handleDisableCaddy},
		{Name: "reload_caddy", Mode: bridgeipc.ModeJob, Handler: handlers.handleReloadCaddy},
		{Name: "connect_to_proxy", Mode: bridgeipc.ModeJob, Handler: handlers.handleConnectToProxy},
		{Name: "system_prune", Mode: bridgeipc.ModeJob, Handler: handlers.handleSystemPrune},
	})

	apischema.AttachRunner(router, apischema.RunnerBinding{
		Route: "docker.logs.follow",
		Runner: func(ctx context.Context, job *bridgeipc.Job, args []string) (any, error) {
			return runDockerLogsJob(ctx, rt, job, args)
		},
		Policy: bridgeipc.StreamDefault,
	})
}
