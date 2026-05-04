package docker

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/config"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

type dockerRegistration struct {
	command string
	handler ipc.HandlerFunc
}

// RegisterHandlers registers all docker handlers with the global registry
func RegisterHandlers(sess *session.Session) {
	username := sess.User.Username
	sessionUsername = username
	RegisterJobRunners(username)

	if err := initIconCache(); err != nil {
		slog.Warn("failed to initialize icon cache", "component", "docker", "subsystem", "icons", "error", err)
	}

	registerDockerHandlers([]dockerRegistration{
		{command: "list_containers", handler: dockerNoArgCallWithContext(ListContainers)},
		{command: "start_container", handler: dockerOneArgCallWithContext(logStartContainer, StartContainer)},
		{command: "stop_container", handler: dockerOneArgCallWithContext(logStopContainer, StopContainer)},
		{command: "remove_container", handler: dockerOneArgCallWithContext(logRemoveContainer, RemoveContainer)},
		{command: "restart_container", handler: dockerOneArgCallWithContext(logRestartContainer, RestartContainer)},
		{command: "list_images", handler: dockerNoArgCall(ListImages)},
		{command: "delete_image", handler: dockerOneArgCall(logDeleteImage, DeleteImage)},
		{command: "list_networks", handler: dockerNoArgCall(ListDockerNetworks)},
		{command: "create_network", handler: dockerOneArgCall(logCreateNetwork, CreateDockerNetwork)},
		{command: "delete_network", handler: dockerOneArgCall(logDeleteNetwork, DeleteDockerNetwork)},
		{command: "list_volumes", handler: dockerNoArgCall(ListVolumes)},
		{command: "create_volume", handler: dockerOneArgCall(logCreateVolume, CreateVolume)},
		{command: "delete_volume", handler: dockerOneArgCall(logDeleteVolume, DeleteVolume)},
		{command: "list_compose_projects", handler: dockerUserCall(username, ListComposeProjects)},
		{command: "get_compose_project", handler: dockerUserOneArgCall(username, GetComposeProject)},
		{command: "compose_up", handler: composeUpHandler(username)},
		{command: "compose_down", handler: dockerUserOneArgCall(username, ComposeDown)},
		{command: "compose_stop", handler: dockerUserOneArgCall(username, ComposeStop)},
		{command: "compose_restart", handler: dockerUserOneArgCall(username, ComposeRestart)},
		{command: "delete_stack", handler: deleteStackHandler(username)},
		{command: "get_docker_folder", handler: dockerUserCall(username, GetDockerFolder)},
		{command: "validate_compose", handler: dockerOneArgCall(nil, ValidateComposeFile)},
		{command: "normalize_compose", handler: normalizeComposeHandler()},
		{command: "get_compose_file_path", handler: dockerUserOneArgCall(username, GetComposeFilePath)},
		{command: "validate_stack_directory", handler: dockerOneArgCall(nil, ValidateStackDirectory)},
		{command: "reindex_docker_folder", handler: reindexDockerFolderHandler(username)},
		{command: "delete_compose_stack", handler: deleteComposeStackHandler(username)},
		{command: "get_docker_info", handler: dockerNoArgCall(GetDockerInfo)},
		{command: "get_icon_uri", handler: getIconURIHandler()},
		{command: "get_icon", handler: getIconHandler()},
		{command: "get_icon_info", handler: getIconInfoHandler()},
		{command: "clear_icon_cache", handler: clearIconCacheHandler()},
		{command: "start_all_stopped", handler: loggedDockerNoArgCallWithContext("start_all_stopped requested", StartAllStopped)},
		{command: "stop_all_running", handler: loggedDockerNoArgCallWithContext("stop_all_running requested", StopAllRunning)},
		{command: "list_auto_update_containers", handler: listAutoUpdateContainersHandler(username)},
		{command: "set_auto_update", handler: setAutoUpdateHandler(username)},
		{command: "get_caddy_status", handler: dockerUserCall(username, GetCaddyStatus)},
		{command: "enable_caddy", handler: loggedDockerUserCall("enable_caddy requested", username, EnableCaddy)},
		{command: "disable_caddy", handler: loggedDockerUserCall("disable_caddy requested", username, DisableCaddy)},
		{command: "reload_caddy", handler: loggedDockerUserCall("reload_caddy requested", username, ReloadCaddy)},
		{command: "connect_to_proxy", handler: dockerOneArgCall(logConnectToProxy, ConnectToProxy)},
		{command: "system_prune", handler: systemPruneHandler()},
	})
}

func registerDockerHandlers(registrations []dockerRegistration) {
	for _, registration := range registrations {
		ipc.RegisterFunc("docker", registration.command, registration.handler)
	}
}

func dockerNoArgCall[T any](fn func() (T, error)) ipc.HandlerFunc {
	return func(ctx context.Context, args []string, emit ipc.Events) error {
		result, err := fn()
		return emitDockerResult(emit, result, err)
	}
}

func dockerNoArgCallWithContext[T any](fn func(context.Context) (T, error)) ipc.HandlerFunc {
	return func(ctx context.Context, args []string, emit ipc.Events) error {
		result, err := fn(ctx)
		return emitDockerResult(emit, result, err)
	}
}

func loggedDockerNoArgCallWithContext[T any](message string, fn func(context.Context) (T, error)) ipc.HandlerFunc {
	return func(ctx context.Context, args []string, emit ipc.Events) error {
		slog.Info(message, "component", "docker")
		result, err := fn(ctx)
		return emitDockerResult(emit, result, err)
	}
}

func dockerOneArgCall[T any](logFn func(string), fn func(string) (T, error)) ipc.HandlerFunc {
	return func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 1 {
			return ipc.ErrInvalidArgs
		}
		if logFn != nil {
			logFn(args[0])
		}
		result, err := fn(args[0])
		return emitDockerResult(emit, result, err)
	}
}

func dockerOneArgCallWithContext[T any](logFn func(string), fn func(context.Context, string) (T, error)) ipc.HandlerFunc {
	return func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 1 {
			return ipc.ErrInvalidArgs
		}
		if logFn != nil {
			logFn(args[0])
		}
		result, err := fn(ctx, args[0])
		return emitDockerResult(emit, result, err)
	}
}

func dockerUserCall[T any](username string, fn func(string) (T, error)) ipc.HandlerFunc {
	return func(ctx context.Context, args []string, emit ipc.Events) error {
		result, err := fn(username)
		return emitDockerResult(emit, result, err)
	}
}

func loggedDockerUserCall[T any](message, username string, fn func(string) (T, error)) ipc.HandlerFunc {
	return func(ctx context.Context, args []string, emit ipc.Events) error {
		slog.Info(message, "component", "docker", "user", username)
		result, err := fn(username)
		return emitDockerResult(emit, result, err)
	}
}

func dockerUserOneArgCall[T any](username string, fn func(string, string) (T, error)) ipc.HandlerFunc {
	return func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 1 {
			return ipc.ErrInvalidArgs
		}
		result, err := fn(username, args[0])
		return emitDockerResult(emit, result, err)
	}
}

func composeUpHandler(username string) ipc.HandlerFunc {
	return func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 1 {
			return ipc.ErrInvalidArgs
		}
		composePath := ""
		if len(args) >= 2 {
			composePath = args[1]
		}
		result, err := ComposeUp(username, args[0], composePath)
		return emitDockerResult(emit, result, err)
	}
}

func deleteStackHandler(username string) ipc.HandlerFunc {
	return func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 1 {
			return ipc.ErrInvalidArgs
		}
		options := DeleteStackOptions{
			DeleteFile:      len(args) >= 2 && args[1] == "true",
			DeleteDirectory: len(args) >= 3 && args[2] == "true",
		}
		result, err := DeleteStack(username, args[0], options)
		return emitDockerResult(emit, result, err)
	}
}

func normalizeComposeHandler() ipc.HandlerFunc {
	return func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 1 {
			return ipc.ErrInvalidArgs
		}
		normalized, err := NormalizeComposeFile(args[0])
		if err != nil {
			return err
		}
		return emit.Result(map[string]string{"content": normalized})
	}
}

func reindexDockerFolderHandler(username string) ipc.HandlerFunc {
	return func(ctx context.Context, args []string, emit ipc.Events) error {
		slog.Info("reindex_docker_folder requested")
		result, err := IndexDockerFolder(username)
		return emitDockerResult(emit, result, err)
	}
}

func deleteComposeStackHandler(username string) ipc.HandlerFunc {
	return func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 1 {
			return ipc.ErrInvalidArgs
		}
		if err := DeleteComposeStack(username, args[0]); err != nil {
			return err
		}
		return emit.Result(map[string]any{
			"success": true,
			"message": "Compose stack deleted successfully",
		})
	}
}

func getIconURIHandler() ipc.HandlerFunc {
	return func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 1 {
			return ipc.ErrInvalidArgs
		}
		uri, err := GetIconURI(args[0])
		if err != nil {
			return err
		}
		return emit.Result(map[string]string{"uri": uri})
	}
}

func getIconHandler() ipc.HandlerFunc {
	return func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 1 {
			return ipc.ErrInvalidArgs
		}
		data, err := GetIcon(args[0])
		if err != nil {
			return err
		}
		encoded := base64.StdEncoding.EncodeToString(data)
		return emit.Result(map[string]string{"data": encoded})
	}
}

func getIconInfoHandler() ipc.HandlerFunc {
	return func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 1 {
			return ipc.ErrInvalidArgs
		}
		return emit.Result(GetIconInfo(args[0]))
	}
}

func clearIconCacheHandler() ipc.HandlerFunc {
	return func(ctx context.Context, args []string, emit ipc.Events) error {
		slog.Info("clear_icon_cache requested")
		if err := ClearIconCache(); err != nil {
			return err
		}
		return emit.Result(map[string]string{"message": "Icon cache cleared successfully"})
	}
}

func listAutoUpdateContainersHandler(username string) ipc.HandlerFunc {
	return func(ctx context.Context, args []string, emit ipc.Events) error {
		cfg, _, err := config.Load(username)
		if err != nil {
			return err
		}
		names := cfg.Docker.AutoUpdateStacks
		if names == nil {
			names = []string{}
		}
		return emit.Result(names)
	}
}

func setAutoUpdateHandler(username string) ipc.HandlerFunc {
	return func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 1 {
			return ipc.ErrInvalidArgs
		}
		var payload struct {
			Container string `json:"container"`
			Enabled   bool   `json:"enabled"`
		}
		if err := json.Unmarshal([]byte(args[0]), &payload); err != nil {
			return ipc.ErrInvalidArgs
		}
		if payload.Container == "" {
			return ipc.ErrInvalidArgs
		}
		slog.Info("set_auto_update requested", "component", "docker", "container", payload.Container, "mode", payload.Enabled, "user", username)

		cfg, _, err := config.Load(username)
		if err != nil {
			return err
		}
		if payload.Enabled {
			if !slices.Contains(cfg.Docker.AutoUpdateStacks, payload.Container) {
				cfg.Docker.AutoUpdateStacks = append(cfg.Docker.AutoUpdateStacks, payload.Container)
			}
		} else {
			cfg.Docker.AutoUpdateStacks = slices.DeleteFunc(cfg.Docker.AutoUpdateStacks, func(name string) bool {
				return name == payload.Container
			})
		}
		if _, err := config.Save(username, cfg); err != nil {
			return err
		}

		go SyncWatchtowerStack(username)

		return emit.Result(map[string]any{"message": "auto-update updated"})
	}
}

func systemPruneHandler() ipc.HandlerFunc {
	return func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 1 {
			return ipc.ErrInvalidArgs
		}
		var opts PruneOptions
		if err := json.Unmarshal([]byte(args[0]), &opts); err != nil {
			return ipc.ErrInvalidArgs
		}
		slog.Info("system_prune requested", "component", "docker", "error", fmt.Errorf("containers=%t images=%t build_cache=%t networks=%t volumes=%t", opts.Containers, opts.Images, opts.BuildCache, opts.Networks, opts.Volumes))
		result, err := SystemPrune(opts)
		return emitDockerResult(emit, result, err)
	}
}

func emitDockerResult(emit ipc.Events, result any, err error) error {
	if err != nil {
		return err
	}
	return emit.Result(result)
}

func logStartContainer(id string) {
	slog.Info("start_container requested", "component", "docker", "container", id)
}

func logStopContainer(id string) {
	slog.Info("stop_container requested", "component", "docker", "container", id)
}

func logRemoveContainer(id string) {
	slog.Info("remove_container requested", "component", "docker", "container", id)
}

func logRestartContainer(id string) {
	slog.Info("restart_container requested", "component", "docker", "container", id)
}

func logDeleteImage(id string) {
	slog.Info("delete_image requested", "component", "docker", "image", id)
}

func logCreateNetwork(name string) {
	slog.Info("create_network requested", "component", "docker", "service", name)
}

func logDeleteNetwork(name string) {
	slog.Info("delete_network requested", "component", "docker", "service", name)
}

func logCreateVolume(name string) {
	slog.Info("create_volume requested", "component", "docker", "service", name)
}

func logDeleteVolume(name string) {
	slog.Info("delete_volume requested", "component", "docker", "service", name)
}

func logConnectToProxy(id string) {
	slog.Info("connect_to_proxy requested", "component", "docker", "container", id)
}
