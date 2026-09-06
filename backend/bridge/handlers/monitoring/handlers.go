package monitoring

import (
	"context"
	"errors"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
	monitoringapi "github.com/mordilloSan/LinuxIO/backend/monitoring/api"
)

var api = apischema.Bindings(
	apischema.Call[apischema.NoRequest, apischema.MonitoringLive]("monitoring.get_live", apischema.RetrySafe()).Handle(handleGetLive),
	apischema.Call[apischema.NoRequest, apischema.MonitoringProcessesResponse]("monitoring.get_processes", apischema.RetrySafe()).Handle(handleGetProcesses),
	apischema.Call[apischema.NoRequest, apischema.MonitoringProgramsResponse]("monitoring.get_programs", apischema.RetrySafe()).Handle(handleGetPrograms),
	apischema.Call[apischema.NoRequest, apischema.MonitoringSmartRefreshResult]("monitoring.refresh_smart", apischema.Privileged()).Handle(handleRefreshSmart),
	apischema.Call[apischema.NoRequest, apischema.MonitoringDatabaseResult]("monitoring.check_database", apischema.Privileged()).Handle(handleCheckDatabase),
	apischema.Call[apischema.NoRequest, apischema.MonitoringDatabaseResult]("monitoring.maintain_database", apischema.Privileged()).Handle(handleMaintainDatabase),
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

func handleGetLive(ctx context.Context, _ apischema.NoRequest) (apischema.MonitoringLive, error) {
	live, err := FetchLive(ctx)
	if err != nil {
		if errors.Is(err, ErrUnavailable) {
			return apischema.MonitoringLive{}, nil
		}
		return apischema.MonitoringLive{}, err
	}
	return apischema.MonitoringLive{Live: live}, nil
}

func handleGetProcesses(ctx context.Context, _ apischema.NoRequest) (apischema.MonitoringProcessesResponse, error) {
	result, err := FetchProcesses(ctx)
	if err != nil {
		if errors.Is(err, ErrUnavailable) {
			return apischema.MonitoringProcessesResponse{Items: []monitoringapi.Process{}}, nil
		}
		return apischema.MonitoringProcessesResponse{}, err
	}
	return result, nil
}

func handleGetPrograms(ctx context.Context, _ apischema.NoRequest) (apischema.MonitoringProgramsResponse, error) {
	result, err := FetchPrograms(ctx)
	if err != nil {
		if errors.Is(err, ErrUnavailable) {
			return apischema.MonitoringProgramsResponse{Items: []monitoringapi.Program{}}, nil
		}
		return apischema.MonitoringProgramsResponse{}, err
	}
	return result, nil
}

func handleRefreshSmart(ctx context.Context, _ apischema.NoRequest) (apischema.MonitoringSmartRefreshResult, error) {
	return RefreshSmart(ctx)
}

func handleCheckDatabase(ctx context.Context, _ apischema.NoRequest) (apischema.MonitoringDatabaseResult, error) {
	return CheckDatabase(ctx)
}

func handleMaintainDatabase(ctx context.Context, _ apischema.NoRequest) (apischema.MonitoringDatabaseResult, error) {
	return MaintainDatabase(ctx)
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
