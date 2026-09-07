package monitoring

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	monitoringapi "github.com/mordilloSan/LinuxIO/backend/monitoring/api"
)

const (
	processesRoute         = "/api/v1/processes"
	programsRoute          = "/api/v1/programs"
	maxProcessPayloadBytes = 8 << 20
)

type processListResponse struct {
	CapturedAt int64                      `json:"captured_at"`
	Count      monitoringapi.ProcessCount `json:"count"`
	Items      []monitoringapi.Process    `json:"items"`
}

type programListResponse struct {
	CapturedAt int64                   `json:"captured_at"`
	Items      []monitoringapi.Program `json:"items"`
}

func FetchProcesses(ctx context.Context) (apischema.MonitoringProcessesResponse, error) {
	var payload processListResponse
	if err := fetchProcessEndpoint(ctx, processesRoute, &payload); err != nil {
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
	var payload programListResponse
	if err := fetchProcessEndpoint(ctx, programsRoute, &payload); err != nil {
		return apischema.MonitoringProgramsResponse{}, err
	}
	items := payload.Items
	if items == nil {
		items = []monitoringapi.Program{}
	}
	return apischema.MonitoringProgramsResponse{CapturedAtMs: payload.CapturedAt, Items: items}, nil
}

func fetchProcessEndpoint(ctx context.Context, path string, result any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix"+path, nil)
	if err != nil {
		return fmt.Errorf("create monitoring request: %w", err)
	}
	resp, err := apiClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: monitoring returned %s", ErrUnavailable, resp.Status)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxProcessPayloadBytes)).Decode(result); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("decode monitoring response: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}
