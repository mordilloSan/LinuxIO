package docker

import (
	"context"
	"encoding/base64"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// RegisterHandlers registers all docker handlers with the global registry
func RegisterHandlers(sess *session.Session) {
	username := sess.User.Username

	// Initialize icon cache at startup to catch permission issues early
	if err := initIconCache(); err != nil {
		// Just log warning - cache will be created lazily if this fails
		_ = err // Suppress unused error
	}

	ipc.RegisterFunc("docker", "list_containers", func(ctx context.Context, args []string, emit ipc.Events) error {
		containers, err := ListContainers()
		if err != nil {
			return err
		}
		return emit.Result(containers)
	})

	ipc.RegisterFunc("docker", "start_container", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 1 {
			return ipc.ErrInvalidArgs
		}
		result, err := StartContainer(args[0])
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	ipc.RegisterFunc("docker", "stop_container", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 1 {
			return ipc.ErrInvalidArgs
		}
		result, err := StopContainer(args[0])
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	ipc.RegisterFunc("docker", "remove_container", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 1 {
			return ipc.ErrInvalidArgs
		}
		result, err := RemoveContainer(args[0])
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	ipc.RegisterFunc("docker", "restart_container", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 1 {
			return ipc.ErrInvalidArgs
		}
		result, err := RestartContainer(args[0])
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	ipc.RegisterFunc("docker", "list_images", func(ctx context.Context, args []string, emit ipc.Events) error {
		images, err := ListImages()
		if err != nil {
			return err
		}
		return emit.Result(images)
	})

	ipc.RegisterFunc("docker", "delete_image", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 1 {
			return ipc.ErrInvalidArgs
		}
		result, err := DeleteImage(args[0])
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	ipc.RegisterFunc("docker", "list_networks", func(ctx context.Context, args []string, emit ipc.Events) error {
		networks, err := ListDockerNetworks()
		if err != nil {
			return err
		}
		return emit.Result(networks)
	})

	ipc.RegisterFunc("docker", "create_network", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 1 {
			return ipc.ErrInvalidArgs
		}
		result, err := CreateDockerNetwork(args[0])
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	ipc.RegisterFunc("docker", "delete_network", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 1 {
			return ipc.ErrInvalidArgs
		}
		result, err := DeleteDockerNetwork(args[0])
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	ipc.RegisterFunc("docker", "list_volumes", func(ctx context.Context, args []string, emit ipc.Events) error {
		volumes, err := ListVolumes()
		if err != nil {
			return err
		}
		return emit.Result(volumes)
	})

	ipc.RegisterFunc("docker", "create_volume", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 1 {
			return ipc.ErrInvalidArgs
		}
		result, err := CreateVolume(args[0])
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	ipc.RegisterFunc("docker", "delete_volume", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 1 {
			return ipc.ErrInvalidArgs
		}
		result, err := DeleteVolume(args[0])
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	// Compose handlers
	ipc.RegisterFunc("docker", "list_compose_projects", func(ctx context.Context, args []string, emit ipc.Events) error {
		projects, err := ListComposeProjects(username)
		if err != nil {
			return err
		}
		return emit.Result(projects)
	})

	ipc.RegisterFunc("docker", "get_compose_project", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 1 {
			return ipc.ErrInvalidArgs
		}
		project, err := GetComposeProject(username, args[0])
		if err != nil {
			return err
		}
		return emit.Result(project)
	})

	ipc.RegisterFunc("docker", "compose_up", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 1 {
			return ipc.ErrInvalidArgs
		}
		projectName := args[0]
		var composePath string
		if len(args) >= 2 {
			composePath = args[1]
		}
		result, err := ComposeUp(username, projectName, composePath)
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	ipc.RegisterFunc("docker", "compose_down", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 1 {
			return ipc.ErrInvalidArgs
		}
		result, err := ComposeDown(username, args[0])
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	ipc.RegisterFunc("docker", "compose_stop", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 1 {
			return ipc.ErrInvalidArgs
		}
		result, err := ComposeStop(username, args[0])
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	ipc.RegisterFunc("docker", "compose_restart", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 1 {
			return ipc.ErrInvalidArgs
		}
		result, err := ComposeRestart(username, args[0])
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	// delete_stack: args[0] = projectName, args[1] = deleteFile (bool), args[2] = deleteDirectory (bool)
	ipc.RegisterFunc("docker", "delete_stack", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 1 {
			return ipc.ErrInvalidArgs
		}
		projectName := args[0]

		options := DeleteStackOptions{
			DeleteFile:      false,
			DeleteDirectory: false,
		}

		// Parse boolean options from args
		if len(args) >= 2 && args[1] == "true" {
			options.DeleteFile = true
		}
		if len(args) >= 3 && args[2] == "true" {
			options.DeleteDirectory = true
		}

		result, err := DeleteStack(username, projectName, options)
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	// Compose file management handlers
	ipc.RegisterFunc("docker", "get_docker_folder", func(ctx context.Context, args []string, emit ipc.Events) error {
		result, err := GetDockerFolder(username)
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	ipc.RegisterFunc("docker", "validate_compose", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 1 {
			return ipc.ErrInvalidArgs
		}
		result, err := ValidateComposeFile(args[0])
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	ipc.RegisterFunc("docker", "normalize_compose", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 1 {
			return ipc.ErrInvalidArgs
		}
		normalized, err := NormalizeComposeFile(args[0])
		if err != nil {
			return err
		}
		return emit.Result(map[string]string{
			"content": normalized,
		})
	})

	ipc.RegisterFunc("docker", "get_compose_file_path", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 1 {
			return ipc.ErrInvalidArgs
		}
		result, err := GetComposeFilePath(username, args[0])
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	ipc.RegisterFunc("docker", "validate_stack_directory", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 1 {
			return ipc.ErrInvalidArgs
		}
		result, err := ValidateStackDirectory(args[0])
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	ipc.RegisterFunc("docker", "reindex_docker_folder", func(ctx context.Context, args []string, emit ipc.Events) error {
		result, err := ReindexDockerFolder(username)
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	ipc.RegisterFunc("docker", "delete_compose_stack", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 1 {
			return ipc.ErrInvalidArgs
		}
		projectName := args[0]
		err := DeleteComposeStack(username, projectName)
		if err != nil {
			return err
		}
		return emit.Result(map[string]any{
			"success": true,
			"message": "Compose stack deleted successfully",
		})
	})

	// Icon handlers
	ipc.RegisterFunc("docker", "get_icon_uri", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 1 {
			return ipc.ErrInvalidArgs
		}
		uri, err := GetIconURI(args[0])
		if err != nil {
			return err
		}
		return emit.Result(map[string]string{"uri": uri})
	})

	ipc.RegisterFunc("docker", "get_icon", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 1 {
			return ipc.ErrInvalidArgs
		}
		data, err := GetIcon(args[0])
		if err != nil {
			return err
		}
		// Return as base64 string
		encoded := base64.StdEncoding.EncodeToString(data)
		return emit.Result(map[string]string{"data": encoded})
	})

	ipc.RegisterFunc("docker", "get_icon_info", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 1 {
			return ipc.ErrInvalidArgs
		}
		info := GetIconInfo(args[0])
		return emit.Result(info)
	})

	ipc.RegisterFunc("docker", "clear_icon_cache", func(ctx context.Context, args []string, emit ipc.Events) error {
		err := ClearIconCache()
		if err != nil {
			return err
		}
		return emit.Result(map[string]string{"message": "Icon cache cleared successfully"})
	})
}
