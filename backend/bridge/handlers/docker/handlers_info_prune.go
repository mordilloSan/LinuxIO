package docker

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

func (h dockerHandlers) handleGetDockerInfo(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	result, err := GetDockerInfo(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleSystemPrune(ctx context.Context, req apischema.DockerSystemPruneRequest, emit bridgeipc.Events) error {
	opts := PruneOptions(req)
	result, err := SystemPrune(ctx, opts)
	return bridgeipc.EmitResult(emit, result, err)
}
