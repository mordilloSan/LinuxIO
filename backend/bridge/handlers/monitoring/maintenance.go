package monitoring

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func RefreshSmart(ctx context.Context) (apischema.MonitoringSmartRefreshResult, error) {
	resp, err := runCommand(ctx, "smart.refresh", nil)
	if err != nil {
		return apischema.MonitoringSmartRefreshResult{}, fmt.Errorf("refresh monitoring SMART cache: %w", err)
	}
	return decodeCommandData[apischema.MonitoringSmartRefreshResult]("smart.refresh", resp.Data)
}

func CheckDatabase(ctx context.Context) (apischema.MonitoringDatabaseResult, error) {
	resp, err := runCommand(ctx, "db.check", nil)
	if err != nil {
		return apischema.MonitoringDatabaseResult{}, fmt.Errorf("check monitoring database: %w", err)
	}
	return decodeCommandData[apischema.MonitoringDatabaseResult]("db.check", resp.Data)
}

func MaintainDatabase(ctx context.Context) (apischema.MonitoringDatabaseResult, error) {
	resp, err := runCommand(ctx, "db.maintain", nil)
	if err != nil {
		return apischema.MonitoringDatabaseResult{}, fmt.Errorf("maintain monitoring database: %w", err)
	}
	return decodeCommandData[apischema.MonitoringDatabaseResult]("db.maintain", resp.Data)
}

func decodeCommandData[T any](command string, data json.RawMessage) (T, error) {
	var result T
	if err := json.Unmarshal(data, &result); err != nil {
		return result, fmt.Errorf("decode %s response: %w", command, err)
	}
	return result, nil
}
