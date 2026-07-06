package monitoring

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var api = apischema.Bindings(
	apischema.Query[apischema.NoRequest, apischema.MonitoringConfig]("monitoring.get_config", apischema.Privileged()).Handle(handleGetConfig),
	apischema.Query[apischema.NoRequest, apischema.MonitoringStatus]("monitoring.get_status", apischema.Privileged()).Handle(handleGetStatus),
	apischema.Query[apischema.MonitoringHistoryRequest, []apischema.MonitoringCPUHistoryPoint]("monitoring.get_cpu_history", apischema.Privileged()).Handle(handleGetCPUHistory),
	apischema.Query[apischema.MonitoringHistoryRequest, []apischema.MonitoringMemoryHistoryPoint]("monitoring.get_memory_history", apischema.Privileged()).Handle(handleGetMemoryHistory),
	apischema.Query[apischema.MonitoringHistoryRequest, []apischema.MonitoringDiskIOHistoryPoint]("monitoring.get_diskio_history", apischema.Privileged()).Handle(handleGetDiskIOHistory),
	apischema.Query[apischema.MonitoringHistoryRequest, []apischema.MonitoringNetworkHistoryPoint]("monitoring.get_network_history", apischema.Privileged()).Handle(handleGetNetworkHistory),
	apischema.Job[apischema.MonitoringConfigPatch, apischema.MonitoringConfigSetResult]("monitoring.set_config", apischema.Privileged()).Handle(handleSetConfig),
	apischema.Job[apischema.NoRequest, apischema.NoResponse]("monitoring.restart", apischema.Privileged()).Handle(handleRestart),
)

var Routes = api.Routes()

// RegisterHandlers registers monitoring admin handlers with the bridge.
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

func handleGetCPUHistory(ctx context.Context, req apischema.MonitoringHistoryRequest, emit bridgeipc.Events) error {
	points, err := FetchCPUHistory(ctx, req)
	return bridgeipc.EmitResult(emit, points, err)
}

func handleGetMemoryHistory(ctx context.Context, req apischema.MonitoringHistoryRequest, emit bridgeipc.Events) error {
	points, err := FetchMemoryHistory(ctx, req)
	return bridgeipc.EmitResult(emit, points, err)
}

func handleGetDiskIOHistory(ctx context.Context, req apischema.MonitoringHistoryRequest, emit bridgeipc.Events) error {
	points, err := FetchDiskIOHistory(ctx, req)
	return bridgeipc.EmitResult(emit, points, err)
}

func handleGetNetworkHistory(ctx context.Context, req apischema.MonitoringHistoryRequest, emit bridgeipc.Events) error {
	points, err := FetchNetworkHistory(ctx, req)
	return bridgeipc.EmitResult(emit, points, err)
}

func handleSetConfig(ctx context.Context, req apischema.MonitoringConfigPatch, emit bridgeipc.Events) error {
	cfg, restartRequired, err := UpdateConfig(ctx, req)
	if err != nil {
		return err
	}
	return bridgeipc.EmitResult(emit, apischema.MonitoringConfigSetResult{
		Config:          cfg,
		RestartRequired: restartRequired,
	}, nil)
}

func handleRestart(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	return bridgeipc.EmitResult(emit, nil, RestartAgent(ctx))
}
