package indexer

import (
	"context"
	"encoding/json"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
	indexerapi "github.com/mordilloSan/LinuxIO/backend/indexer/api"
)

var api = apischema.Bindings(
	apischema.Call[apischema.NoRequest, indexerapi.IndexerConfig]("indexer.get_config", apischema.RetrySafe(), apischema.Privileged()).Handle(handleGetConfig),
	apischema.Call[apischema.NoRequest, apischema.IndexerDaemonStatus]("indexer.get_status", apischema.RetrySafe(), apischema.Privileged()).Handle(handleGetStatus),
	apischema.Call[indexerapi.IndexerConfigPatch, apischema.IndexerConfigSetResult]("indexer.set_config", apischema.Privileged()).Handle(handleSetConfig),
	apischema.Call[apischema.IntervalRequest, apischema.IndexerTimerSetResult]("indexer.set_timer_interval", apischema.Privileged()).Handle(handleSetTimerInterval),
)

var Routes = api.Routes()

// RegisterHandlers registers indexer admin handlers with the bridge.
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	api.Register(router)
}

func handleGetConfig(ctx context.Context, _ apischema.NoRequest) (indexerapi.IndexerConfig, error) {
	return FetchConfig(ctx)
}

func handleGetStatus(ctx context.Context, _ apischema.NoRequest) (apischema.IndexerDaemonStatus, error) {
	status, err := FetchStatus(ctx)
	return indexerStatusToAPI(status), err
}

func handleSetConfig(ctx context.Context, req indexerapi.IndexerConfigPatch) (apischema.IndexerConfigSetResult, error) {
	raw, err := json.Marshal(req)
	if err != nil {
		return apischema.IndexerConfigSetResult{}, err
	}
	cfg, restartRequired, err := UpdateConfig(ctx, raw)
	if err != nil {
		return apischema.IndexerConfigSetResult{}, err
	}
	if req.ListenAddr != nil {
		if err := configureTCPListener(ctx, cfg.ListenAddr); err != nil {
			return apischema.IndexerConfigSetResult{}, err
		}
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
