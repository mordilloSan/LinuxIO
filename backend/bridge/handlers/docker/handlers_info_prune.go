package docker

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func (h dockerHandlers) handleGetDockerInfo(ctx context.Context, _ apischema.NoRequest) (*apischema.DockerSystemInfo, error) {
	return GetDockerInfo(ctx)
}

func (h dockerHandlers) handleSystemPrune(ctx context.Context, req apischema.DockerSystemPruneRequest) (*apischema.DockerSystemPruneResponse, error) {
	return SystemPrune(ctx, req)
}
