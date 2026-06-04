package indexer

import (
	"context"
	"encoding/json"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	indexerapi "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/indexer/api"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

// RegisterHandlers registers indexer admin handlers with the bridge.
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	apischema.RegisterRoutes(router, []apischema.HandlerBinding{
		{Route: indexerapi.GetConfig, Handle: handleGetConfig},
		{Route: indexerapi.GetStatus, Handle: handleGetStatus},
		{Route: indexerapi.SetConfig, Handle: handleSetConfig},
		{Route: indexerapi.SetTimerInterval, Handle: handleSetTimerInterval},
	})
}

func handleGetConfig(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	cfg, err := FetchConfig(ctx)
	return bridgeipc.EmitResult(emit, cfg, err)
}

func handleGetStatus(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	status, err := FetchStatus(ctx)
	return bridgeipc.EmitResult(emit, status, err)
}

func handleSetConfig(ctx context.Context, req apischema.IndexerConfigPatch, emit bridgeipc.Events) error {
	raw, err := json.Marshal(req)
	if err != nil {
		return err
	}
	cfg, restartRequired, err := UpdateConfig(ctx, raw)
	if err != nil {
		return err
	}
	return bridgeipc.EmitResult(emit, apischema.IndexerConfigSetResult{
		Config:          cfg,
		RestartRequired: restartRequired,
	}, nil)
}

func handleSetTimerInterval(ctx context.Context, req apischema.IntervalRequest, emit bridgeipc.Events) error {
	if req.Interval == "" {
		return bridgeipc.ErrInvalidArgs
	}
	result, err := SetTimerInterval(ctx, req.Interval)
	return bridgeipc.EmitResult(emit, result, err)
}
