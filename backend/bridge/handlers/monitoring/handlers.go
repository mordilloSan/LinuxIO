package monitoring

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var api = apischema.Bindings(
	apischema.Call[apischema.NoRequest, apischema.MonitoringConfig]("monitoring.get_config", apischema.RetrySafe(), apischema.Privileged()).Handle(handleGetConfig),
	apischema.Call[apischema.NoRequest, apischema.MonitoringStatus]("monitoring.get_status", apischema.RetrySafe(), apischema.Privileged()).Handle(handleGetStatus),
	apischema.Call[apischema.MonitoringHistoryRequest, []apischema.MonitoringCPUHistoryPoint]("monitoring.get_cpu_history", apischema.RetrySafe(), apischema.Privileged()).Handle(handleGetCPUHistory),
	apischema.Call[apischema.MonitoringHistoryRequest, []apischema.MonitoringMemoryHistoryPoint]("monitoring.get_memory_history", apischema.RetrySafe(), apischema.Privileged()).Handle(handleGetMemoryHistory),
	apischema.Call[apischema.MonitoringHistoryRequest, []apischema.MonitoringDiskIOHistoryPoint]("monitoring.get_diskio_history", apischema.RetrySafe(), apischema.Privileged()).Handle(handleGetDiskIOHistory),
	apischema.Call[apischema.MonitoringHistoryRequest, []apischema.MonitoringNetworkHistoryPoint]("monitoring.get_network_history", apischema.RetrySafe(), apischema.Privileged()).Handle(handleGetNetworkHistory),
	apischema.Call[apischema.MonitoringHistoryRequest, []apischema.MonitoringContainerHistoryPoint]("monitoring.get_container_history", apischema.RetrySafe(), apischema.Privileged()).Handle(handleGetContainerHistory),
	apischema.Call[apischema.MonitoringConfigPatch, apischema.MonitoringConfigSetResult]("monitoring.set_config", apischema.Privileged()).Handle(handleSetConfig),
	apischema.Call[apischema.NoRequest, apischema.NoResponse]("monitoring.restart", apischema.Privileged()).HandleVoid(handleRestart),
)

var Routes = api.Routes()

// RegisterHandlers registers monitoring admin handlers with the bridge.
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	api.Register(router)
}

func handleGetConfig(ctx context.Context, _ apischema.NoRequest) (apischema.MonitoringConfig, error) {
	return FetchConfig(ctx)
}

func handleGetStatus(ctx context.Context, _ apischema.NoRequest) (apischema.MonitoringStatus, error) {
	return FetchStatus(ctx)
}

func handleGetCPUHistory(ctx context.Context, req apischema.MonitoringHistoryRequest) ([]apischema.MonitoringCPUHistoryPoint, error) {
	return FetchCPUHistory(ctx, req)
}

func handleGetMemoryHistory(ctx context.Context, req apischema.MonitoringHistoryRequest) ([]apischema.MonitoringMemoryHistoryPoint, error) {
	return FetchMemoryHistory(ctx, req)
}

func handleGetDiskIOHistory(ctx context.Context, req apischema.MonitoringHistoryRequest) ([]apischema.MonitoringDiskIOHistoryPoint, error) {
	return FetchDiskIOHistory(ctx, req)
}

func handleGetNetworkHistory(ctx context.Context, req apischema.MonitoringHistoryRequest) ([]apischema.MonitoringNetworkHistoryPoint, error) {
	return FetchNetworkHistory(ctx, req)
}

func handleGetContainerHistory(ctx context.Context, req apischema.MonitoringHistoryRequest) ([]apischema.MonitoringContainerHistoryPoint, error) {
	return FetchContainerHistory(ctx, req)
}

func handleSetConfig(ctx context.Context, req apischema.MonitoringConfigPatch) (apischema.MonitoringConfigSetResult, error) {
	cfg, restartRequired, err := UpdateConfig(ctx, req)
	if err != nil {
		return apischema.MonitoringConfigSetResult{}, err
	}
	return apischema.MonitoringConfigSetResult{
		Config:          cfg,
		RestartRequired: restartRequired,
	}, nil
}

func handleRestart(ctx context.Context, _ apischema.NoRequest) error {
	return RestartAgent(ctx)
}
