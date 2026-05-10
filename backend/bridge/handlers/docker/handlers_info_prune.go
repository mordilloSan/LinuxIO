package docker

import (
	"context"
	"log/slog"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/internal/rpc"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

func (h dockerHandlers) handleGetDockerInfo(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := GetDockerInfo()
	return rpc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleSystemPrune(ctx context.Context, args []string, emit ipc.Events) error {
	opts, err := rpc.DecodeJSONArg[PruneOptions](args, 0)
	if err != nil {
		return err
	}
	slog.Info("system_prune requested", "component", "docker",
		"containers", opts.Containers,
		"images", opts.Images,
		"build_cache", opts.BuildCache,
		"networks", opts.Networks,
		"volumes", opts.Volumes)
	result, err := SystemPrune(opts)
	return rpc.EmitResult(emit, result, err)
}
