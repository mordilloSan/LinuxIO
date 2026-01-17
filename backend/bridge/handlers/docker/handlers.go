package docker

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// RegisterHandlers registers all docker handlers with the global registry
func RegisterHandlers(sess *session.Session) {
	username := sess.User.Username
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
}
