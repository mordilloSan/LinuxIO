package indexer

import (
	"context"
	"encoding/json"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var api = apischema.Bindings(
	apischema.Query[apischema.NoRequest, apischema.IndexerConfig]("indexer.get_config", apischema.Privileged()).Handle(handleGetConfig),
	apischema.Query[apischema.NoRequest, apischema.IndexerDaemonStatus]("indexer.get_status", apischema.Privileged()).Handle(handleGetStatus),
	apischema.Job[apischema.IndexerConfigPatch, apischema.IndexerConfigSetResult]("indexer.set_config", apischema.Privileged()).Handle(handleSetConfig),
	apischema.Job[apischema.IntervalRequest, apischema.IndexerTimerSetResult]("indexer.set_timer_interval", apischema.Privileged()).Handle(handleSetTimerInterval),
)

var Routes = api.Routes()

// RegisterHandlers registers indexer admin handlers with the bridge.
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	api.Register(router)
}

func handleGetConfig(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	cfg, err := FetchConfig(ctx)
	return bridgeipc.EmitResult(emit, cfg, err)
}

func handleGetStatus(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
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
