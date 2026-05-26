package indexer

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

// RegisterHandlers registers indexer admin handlers with the bridge.
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	apischema.RegisterRoutes(router, "indexer", []bridgeipc.Command{
		{Name: "get_config", Mode: bridgeipc.ModeQuery, Handler: handleGetConfig, Privileged: true},
		{Name: "get_status", Mode: bridgeipc.ModeQuery, Handler: handleGetStatus, Privileged: true},
		{Name: "set_config", Mode: bridgeipc.ModeJob, Handler: handleSetConfig, Privileged: true},
		{Name: "set_timer_interval", Mode: bridgeipc.ModeJob, Handler: handleSetTimerInterval, Privileged: true},
	})
}

func handleGetConfig(ctx context.Context, args []string, emit bridgeipc.Events) error {
	if len(args) != 0 {
		return bridgeipc.ErrInvalidArgs
	}
	cfg, err := FetchConfig(ctx)
	return bridgeipc.EmitResult(emit, cfg, err)
}

func handleGetStatus(ctx context.Context, args []string, emit bridgeipc.Events) error {
	if len(args) != 0 {
		return bridgeipc.ErrInvalidArgs
	}
	status, err := FetchStatus(ctx)
	return bridgeipc.EmitResult(emit, status, err)
}

func handleSetConfig(ctx context.Context, args []string, emit bridgeipc.Events) error {
	if len(args) != 1 {
		return bridgeipc.ErrInvalidArgs
	}
	cfg, restartRequired, err := UpdateConfig(ctx, []byte(args[0]))
	if err != nil {
		return err
	}
	return bridgeipc.EmitResult(emit, ConfigSetResult{
		Config:          cfg,
		RestartRequired: restartRequired,
	}, nil)
}

func handleSetTimerInterval(ctx context.Context, args []string, emit bridgeipc.Events) error {
	if len(args) != 1 {
		return bridgeipc.ErrInvalidArgs
	}
	result, err := SetTimerInterval(ctx, args[0])
	return bridgeipc.EmitResult(emit, result, err)
}
