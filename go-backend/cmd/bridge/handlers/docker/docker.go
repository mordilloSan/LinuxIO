package docker

import (
	"go-backend/cmd/bridge/handlers/types"
)

func DockerHandlers() map[string]types.HandlerFunc {
	return map[string]types.HandlerFunc{
		"list_containers":   func(args []string) (any, error) { return ListContainers() },
		"start_container":   func(args []string) (any, error) { return StartContainer(args[0]) },
		"stop_container":    func(args []string) (any, error) { return StopContainer(args[0]) },
		"remove_container":  func(args []string) (any, error) { return RemoveContainer(args[0]) },
		"restart_container": func(args []string) (any, error) { return RestartContainer(args[0]) },
		"list_images":       func(args []string) (any, error) { return ListImages() },
	}
}
