package system

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/docker"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

type capabilitiesResponse struct {
	DockerAvailable  bool   `json:"docker_available"`
	IndexerAvailable bool   `json:"indexer_available"`
	DockerError      string `json:"docker_error,omitempty"`
	IndexerError     string `json:"indexer_error,omitempty"`
}

func registerCapabilitiesHandlers() {
	ipc.RegisterFunc("system", "get_capabilities", func(ctx context.Context, args []string, emit ipc.Events) error {
		var out capabilitiesResponse

		if _, err := docker.CheckDockerAvailability(); err != nil {
			out.DockerAvailable = false
			out.DockerError = err.Error()
		} else {
			out.DockerAvailable = true
		}

		if ok, err := filebrowser.CheckIndexerAvailability(); err != nil {
			out.IndexerAvailable = false
			out.IndexerError = err.Error()
		} else {
			out.IndexerAvailable = ok
		}

		return emit.Result(out)
	})
}
