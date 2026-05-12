package docker

import (
	"log/slog"
	"net"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/internal/rpc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/runtime"
	"github.com/mordilloSan/LinuxIO/backend/bridge/settings"
)

type dockerHandlers struct {
	username string
	store    *settings.UserStore
}

// RegisterHandlers registers all docker handlers with the global registry
func RegisterHandlers(rt runtime.Runtime) {
	handlers := dockerHandlers{
		username: rt.Username(),
		store:    rt.Store,
	}

	RegisterJobRunners(handlers.username, handlers.store)
	go watchtowerOnce.Do(func() { SyncWatchtowerStackWithStore(handlers.username, handlers.store) })

	if err := initIconCache(); err != nil {
		slog.Warn("failed to initialize icon cache", "component", "docker", "subsystem", "icons", "error", err)
	}

	rpc.Register("docker", rt, []rpc.Command{
		{Name: "list_containers", Handler: handlers.handleListContainers},
		{Name: "start_container", Handler: handlers.handleStartContainer},
		{Name: "stop_container", Handler: handlers.handleStopContainer},
		{Name: "remove_container", Handler: handlers.handleRemoveContainer},
		{Name: "restart_container", Handler: handlers.handleRestartContainer},
		{Name: "list_images", Handler: handlers.handleListImages},
		{Name: "delete_image", Handler: handlers.handleDeleteImage},
		{Name: "list_networks", Handler: handlers.handleListNetworks},
		{Name: "create_network", Handler: handlers.handleCreateNetwork},
		{Name: "delete_network", Handler: handlers.handleDeleteNetwork},
		{Name: "list_volumes", Handler: handlers.handleListVolumes},
		{Name: "create_volume", Handler: handlers.handleCreateVolume},
		{Name: "delete_volume", Handler: handlers.handleDeleteVolume},
		{Name: "list_compose_projects", Handler: handlers.handleListComposeProjects},
		{Name: "get_compose_project", Handler: handlers.handleGetComposeProject},
		{Name: "compose_up", Handler: handlers.handleComposeUp},
		{Name: "compose_down", Handler: handlers.handleComposeDown},
		{Name: "compose_stop", Handler: handlers.handleComposeStop},
		{Name: "compose_restart", Handler: handlers.handleComposeRestart},
		{Name: "delete_stack", Handler: handlers.handleDeleteStack},
		{Name: "get_docker_folders", Handler: handlers.handleGetDockerFolders},
		{Name: "validate_compose", Handler: handlers.handleValidateCompose},
		{Name: "normalize_compose", Handler: handlers.handleNormalizeCompose},
		{Name: "get_compose_file_path", Handler: handlers.handleGetComposeFilePath},
		{Name: "validate_stack_directory", Handler: handlers.handleValidateStackDirectory},
		{Name: "reindex_docker_folders", Handler: handlers.handleReindexDockerFolders},
		{Name: "delete_compose_stack", Handler: handlers.handleDeleteComposeStack},
		{Name: "get_docker_info", Handler: handlers.handleGetDockerInfo},
		{Name: "get_icon_uri", Handler: handlers.handleGetIconURI},
		{Name: "get_icon", Handler: handlers.handleGetIcon},
		{Name: "get_icon_info", Handler: handlers.handleGetIconInfo},
		{Name: "clear_icon_cache", Handler: handlers.handleClearIconCache},
		{Name: "start_all_stopped", Handler: handlers.handleStartAllStopped},
		{Name: "stop_all_running", Handler: handlers.handleStopAllRunning},
		{Name: "list_auto_update_containers", Handler: handlers.handleListAutoUpdateContainers},
		{Name: "set_auto_update", Handler: handlers.handleSetAutoUpdate},
		{Name: "get_caddy_status", Handler: handlers.handleGetCaddyStatus},
		{Name: "enable_caddy", Handler: handlers.handleEnableCaddy},
		{Name: "disable_caddy", Handler: handlers.handleDisableCaddy},
		{Name: "reload_caddy", Handler: handlers.handleReloadCaddy},
		{Name: "connect_to_proxy", Handler: handlers.handleConnectToProxy},
		{Name: "system_prune", Handler: handlers.handleSystemPrune},
	})
}

// RegisterStreamHandlers registers all docker stream handlers.
func RegisterStreamHandlers(handlers map[string]func(runtime.Runtime, net.Conn, []string) error) {
	handlers[StreamTypeDockerLogs] = HandleDockerLogsStream
}
