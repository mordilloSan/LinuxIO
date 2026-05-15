package docker

import (
	"context"

	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

func (h dockerHandlers) handleGetDockerInfo(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := GetDockerInfo(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleSystemPrune(ctx context.Context, args []string, emit bridgeipc.Events) error {
	opts, err := bridgeipc.DecodeJSONArg[PruneOptions](args, 0)
	if err != nil {
		return err
	}
	result, err := SystemPrune(ctx, opts)
	return bridgeipc.EmitResult(emit, result, err)
}
