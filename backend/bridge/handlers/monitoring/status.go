package monitoring

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func FetchStatus(ctx context.Context) (apischema.MonitoringStatus, error) {
	resp, err := runCommand(ctx, "status.get", nil)
	if err != nil {
		return apischema.MonitoringStatus{}, fmt.Errorf("fetch monitoring status: %w", err)
	}
	var status apischema.MonitoringStatus
	if err := json.Unmarshal(resp.Data, &status); err != nil {
		return apischema.MonitoringStatus{}, fmt.Errorf("decode monitoring status: %w", err)
	}
	return status, nil
}
