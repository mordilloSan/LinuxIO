package docker

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handler"
)

// RegisterHandlers registers all docker handlers with the global registry
func RegisterHandlers() {
	handler.RegisterFunc("docker", "list_containers", func(ctx context.Context, args []string, emit handler.Events) error {
		containers, err := ListContainers()
		if err != nil {
			return err
		}
		return emit.Result(containers)
	})

	handler.RegisterFunc("docker", "start_container", func(ctx context.Context, args []string, emit handler.Events) error {
		if len(args) < 1 {
			return handler.ErrInvalidArgs
		}
		result, err := StartContainer(args[0])
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	handler.RegisterFunc("docker", "stop_container", func(ctx context.Context, args []string, emit handler.Events) error {
		if len(args) < 1 {
			return handler.ErrInvalidArgs
		}
		result, err := StopContainer(args[0])
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	handler.RegisterFunc("docker", "get_container_logs", func(ctx context.Context, args []string, emit handler.Events) error {
		if len(args) < 1 {
			return handler.ErrInvalidArgs
		}
		logs, err := LogContainer(args[0])
		if err != nil {
			return err
		}
		return emit.Result(logs)
	})

	handler.RegisterFunc("docker", "remove_container", func(ctx context.Context, args []string, emit handler.Events) error {
		if len(args) < 1 {
			return handler.ErrInvalidArgs
		}
		result, err := RemoveContainer(args[0])
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	handler.RegisterFunc("docker", "restart_container", func(ctx context.Context, args []string, emit handler.Events) error {
		if len(args) < 1 {
			return handler.ErrInvalidArgs
		}
		result, err := RestartContainer(args[0])
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	handler.RegisterFunc("docker", "list_images", func(ctx context.Context, args []string, emit handler.Events) error {
		images, err := ListImages()
		if err != nil {
			return err
		}
		return emit.Result(images)
	})

	handler.RegisterFunc("docker", "list_networks", func(ctx context.Context, args []string, emit handler.Events) error {
		networks, err := ListDockerNetworks()
		if err != nil {
			return err
		}
		return emit.Result(networks)
	})

	handler.RegisterFunc("docker", "create_network", func(ctx context.Context, args []string, emit handler.Events) error {
		if len(args) < 1 {
			return handler.ErrInvalidArgs
		}
		result, err := CreateDockerNetwork(args[0])
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	handler.RegisterFunc("docker", "delete_network", func(ctx context.Context, args []string, emit handler.Events) error {
		if len(args) < 1 {
			return handler.ErrInvalidArgs
		}
		result, err := DeleteDockerNetwork(args[0])
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	handler.RegisterFunc("docker", "list_volumes", func(ctx context.Context, args []string, emit handler.Events) error {
		volumes, err := ListVolumes()
		if err != nil {
			return err
		}
		return emit.Result(volumes)
	})

	handler.RegisterFunc("docker", "create_volume", func(ctx context.Context, args []string, emit handler.Events) error {
		if len(args) < 1 {
			return handler.ErrInvalidArgs
		}
		result, err := CreateVolume(args[0])
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	handler.RegisterFunc("docker", "delete_volume", func(ctx context.Context, args []string, emit handler.Events) error {
		if len(args) < 1 {
			return handler.ErrInvalidArgs
		}
		result, err := DeleteVolume(args[0])
		if err != nil {
			return err
		}
		return emit.Result(result)
	})
}
