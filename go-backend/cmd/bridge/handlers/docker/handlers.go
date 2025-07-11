package docker

import (
	"go-backend/cmd/bridge/handlers/types"
)

func DockerHandlers() map[string]types.HandlerFunc {
	return map[string]types.HandlerFunc{
		"list_containers":    func(args []string) (any, error) { return ListContainers() },
		"start_container":    func(args []string) (any, error) { return StartContainer(args[0]) },
		"stop_container":     func(args []string) (any, error) { return StopContainer(args[0]) },
		"get_container_logs": func(args []string) (any, error) { return LogContainer(args[0]) },
		"remove_container":   func(args []string) (any, error) { return RemoveContainer(args[0]) },
		"restart_container":  func(args []string) (any, error) { return RestartContainer(args[0]) },
		"list_images":        func(args []string) (any, error) { return ListImages() },
		"list_networks":      func(args []string) (any, error) { return ListDockerNetworks() },
		"create_network":     func(args []string) (any, error) { return CreateDockerNetwork(args[0]) },
		"delete_network":     func(args []string) (any, error) { return DeleteDockerNetwork(args[0]) },
		"list_volumes":       func(args []string) (any, error) { return ListVolumes() },
		"create_volume":      func(args []string) (any, error) { return CreateVolume(args[0]) },
		"delete_volume":      func(args []string) (any, error) { return DeleteVolume(args[0]) },
	}
}
