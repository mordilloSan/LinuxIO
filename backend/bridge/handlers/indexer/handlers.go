package indexer

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/internal/rpc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/runtime"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

// RegisterHandlers registers indexer admin handlers with the bridge.
func RegisterHandlers(rt runtime.Runtime) {
	rpc.Register("indexer", rt, []rpc.Command{
		{Name: "get_config", Handler: handleGetConfig, Privileged: true},
		{Name: "get_status", Handler: handleGetStatus, Privileged: true},
		{Name: "set_config", Handler: handleSetConfig, Privileged: true},
	})
}

func handleGetConfig(ctx context.Context, args []string, emit ipc.Events) error {
	if len(args) != 0 {
		return ipc.ErrInvalidArgs
	}
	cfg, err := FetchConfig(ctx)
	return rpc.EmitResult(emit, cfg, err)
}

func handleGetStatus(ctx context.Context, args []string, emit ipc.Events) error {
	if len(args) != 0 {
		return ipc.ErrInvalidArgs
	}
	status, err := FetchStatus(ctx)
	return rpc.EmitResult(emit, status, err)
}

func handleSetConfig(ctx context.Context, args []string, emit ipc.Events) error {
	if len(args) != 1 {
		return ipc.ErrInvalidArgs
	}
	cfg, restartRequired, err := UpdateConfig(ctx, []byte(args[0]))
	if err != nil {
		return err
	}
	return emit.Result(ConfigSetResult{
		Config:          cfg,
		RestartRequired: restartRequired,
	})
}
