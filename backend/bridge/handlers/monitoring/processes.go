package monitoring

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	monitoringapi "github.com/mordilloSan/LinuxIO/backend/monitoring/api"
)

const maxProcessPayloadBytes = 8 << 20

func FetchProcesses(ctx context.Context) (apischema.MonitoringProcessesResponse, error) {
	var payload monitoringapi.ProcessListResponse
	if err := getAPI(ctx, monitoringapi.RouteProcesses, maxProcessPayloadBytes, &payload); err != nil {
		return apischema.MonitoringProcessesResponse{}, err
	}
	items := payload.Items
	if items == nil {
		items = []monitoringapi.Process{}
	}
	return apischema.MonitoringProcessesResponse{
		CapturedAtMs: payload.CapturedAt,
		Count:        payload.Count,
		Items:        items,
	}, nil
}

func FetchPrograms(ctx context.Context) (apischema.MonitoringProgramsResponse, error) {
	var payload monitoringapi.ProgramListResponse
	if err := getAPI(ctx, monitoringapi.RoutePrograms, maxProcessPayloadBytes, &payload); err != nil {
		return apischema.MonitoringProgramsResponse{}, err
	}
	items := payload.Items
	if items == nil {
		items = []monitoringapi.Program{}
	}
	return apischema.MonitoringProgramsResponse{CapturedAtMs: payload.CapturedAt, Items: items}, nil
}
