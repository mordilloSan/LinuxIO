package docker

import "github.com/mordilloSan/LinuxIO/backend/common/ipc"

func DockerHandlers() map[string]ipc.HandlerFunc {
	return map[string]ipc.HandlerFunc{
		"list_containers":    ipc.WrapSimpleHandler(func([]string) (any, error) { return ListContainers() }),
		"start_container":    ipc.WrapSimpleHandler(func(args []string) (any, error) { return StartContainer(args[0]) }),
		"stop_container":     ipc.WrapSimpleHandler(func(args []string) (any, error) { return StopContainer(args[0]) }),
		"get_container_logs": ipc.WrapSimpleHandler(func(args []string) (any, error) { return LogContainer(args[0]) }),
		"remove_container":   ipc.WrapSimpleHandler(func(args []string) (any, error) { return RemoveContainer(args[0]) }),
		"restart_container":  ipc.WrapSimpleHandler(func(args []string) (any, error) { return RestartContainer(args[0]) }),
		"list_images":        ipc.WrapSimpleHandler(func([]string) (any, error) { return ListImages() }),
		"list_networks":      ipc.WrapSimpleHandler(func([]string) (any, error) { return ListDockerNetworks() }),
		"create_network":     ipc.WrapSimpleHandler(func(args []string) (any, error) { return CreateDockerNetwork(args[0]) }),
		"delete_network":     ipc.WrapSimpleHandler(func(args []string) (any, error) { return DeleteDockerNetwork(args[0]) }),
		"list_volumes":       ipc.WrapSimpleHandler(func([]string) (any, error) { return ListVolumes() }),
		"create_volume":      ipc.WrapSimpleHandler(func(args []string) (any, error) { return CreateVolume(args[0]) }),
		"delete_volume":      ipc.WrapSimpleHandler(func(args []string) (any, error) { return DeleteVolume(args[0]) }),
	}
}
