package indexer

import (
	"context"
	"encoding/json/v2"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
)

var api = apischema.Bindings(
	apischema.Call[apischema.NoRequest, apischema.IndexerConfig]("indexer.get_config", apischema.RetrySafe(), apischema.Privileged()).Handle(handleGetConfig),
	apischema.Call[apischema.NoRequest, apischema.IndexerDaemonStatus]("indexer.get_status", apischema.RetrySafe(), apischema.Privileged()).Handle(handleGetStatus),
	apischema.Mutation("indexer.set_config", handleSetConfig, apischema.Privileged()),
	apischema.Mutation("indexer.set_timer_interval", handleSetTimerInterval, apischema.Privileged()),
)

var Routes = api.Routes()

// RegisterHandlers registers indexer admin handlers with the bridge.
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	api.Register(router)
}

func handleGetConfig(ctx context.Context, _ apischema.NoRequest) (apischema.IndexerConfig, error) {
	cfg, err := FetchConfig(ctx)
	if err != nil {
		return apischema.IndexerConfig{}, err
	}
	interval, err := currentTimerInterval(ctx)
	if err != nil {
		return apischema.IndexerConfig{}, err
	}
	return apischema.IndexerConfig{IndexerConfig: cfg, Interval: interval}, nil
}

func handleGetStatus(ctx context.Context, _ apischema.NoRequest) (apischema.IndexerDaemonStatus, error) {
	status, err := FetchStatus(ctx)
	return indexerStatusToAPI(status), err
}

func handleSetConfig(ctx context.Context, req apischema.IndexerConfigPatch) (apischema.IndexerConfigSetResult, error) {
	interval, err := currentTimerInterval(ctx)
	if err != nil {
		return apischema.IndexerConfigSetResult{}, err
	}
	raw, err := json.Marshal(req.IndexerConfigPatch)
	if err != nil {
		return apischema.IndexerConfigSetResult{}, err
	}
	cfg, err := UpdateConfig(ctx, raw)
	if err != nil {
		return apischema.IndexerConfigSetResult{}, err
	}
	return apischema.IndexerConfigSetResult{
		Config: apischema.IndexerConfig{
			IndexerConfig: cfg,
			Interval:      interval,
		},
	}, nil
}

func handleSetTimerInterval(ctx context.Context, req apischema.IntervalRequest) (apischema.IndexerTimerSetResult, error) {
	if req.Interval == "" {
		return apischema.IndexerTimerSetResult{}, bridgeipc.ErrInvalidArgs
	}
	return SetTimerInterval(ctx, req.Interval)
}
