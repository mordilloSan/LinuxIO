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
	apischema.Query[apischema.NoRequest, apischema.IndexerDaemonStatus]("indexer.get_status", apischema.Privileged()).HandleEvents(handleGetStatus),
	apischema.Job[apischema.IndexerConfigPatch, apischema.IndexerConfigSetResult]("indexer.set_config", apischema.Privileged()).Handle(handleSetConfig),
	apischema.Job[apischema.IntervalRequest, apischema.IndexerTimerSetResult]("indexer.set_timer_interval", apischema.Privileged()).Handle(handleSetTimerInterval),
)

var Routes = api.Routes()

// RegisterHandlers registers indexer admin handlers with the bridge.
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	api.Register(router)
}

func handleGetConfig(ctx context.Context, _ apischema.NoRequest) (apischema.IndexerConfig, error) {
	return FetchConfig(ctx)
}

func handleGetStatus(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	status, err := FetchStatus(ctx)
	return bridgeipc.EmitResult(emit, status, err)
}

func handleSetConfig(ctx context.Context, req apischema.IndexerConfigPatch) (apischema.IndexerConfigSetResult, error) {
	raw, err := json.Marshal(req)
	if err != nil {
		return apischema.IndexerConfigSetResult{}, err
	}
	cfg, restartRequired, err := UpdateConfig(ctx, raw)
	if err != nil {
		return apischema.IndexerConfigSetResult{}, err
	}
	return apischema.IndexerConfigSetResult{
		Config:          cfg,
		RestartRequired: restartRequired,
	}, nil
}

func handleSetTimerInterval(ctx context.Context, req apischema.IntervalRequest) (apischema.IndexerTimerSetResult, error) {
	if req.Interval == "" {
		return apischema.IndexerTimerSetResult{}, bridgeipc.ErrInvalidArgs
	}
	return SetTimerInterval(ctx, req.Interval)
}
