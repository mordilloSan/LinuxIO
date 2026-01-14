package docker

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

// RegisterHandlers registers all docker handlers with the global registry
func RegisterHandlers() {
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
}
