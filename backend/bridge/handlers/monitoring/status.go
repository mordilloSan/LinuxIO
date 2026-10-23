package monitoring

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	monitoringapi "github.com/mordilloSan/LinuxIO/backend/monitoring/api"
)

func FetchStatus(ctx context.Context) (apischema.MonitoringStatus, error) {
	resp, err := runCommand(ctx, monitoringapi.CommandStatusGet, nil)
	if err != nil {
		return apischema.MonitoringStatus{}, fmt.Errorf("fetch monitoring status: %w", err)
	}
	var status monitoringapi.Status
	if err := json.Unmarshal(resp.Data, &status); err != nil {
		return apischema.MonitoringStatus{}, fmt.Errorf("decode monitoring status: %w", err)
	}
	return statusFromAPI(status), nil
}

// statusFromAPI drops what the frontend contract does not carry: the
// per-listener plugin allowlist.
func statusFromAPI(status monitoringapi.Status) apischema.MonitoringStatus {
	var listeners []apischema.MonitoringListenerStatus
	for _, listener := range status.Listeners {
		listeners = append(listeners, apischema.MonitoringListenerStatus{
			Active:           listener.Active,
			Address:          listener.Address,
			APIs:             listener.APIs,
			EffectiveAddress: listener.EffectiveAddress,
			Name:             listener.Name,
		})
	}
	return apischema.MonitoringStatus{
		CollectorInterval:    status.CollectorInterval,
		Config:               status.Config,
		DataDir:              status.DataDir,
		DBPath:               status.DBPath,
		DBSizeBytes:          status.DBSizeBytes,
		Listeners:            listeners,
		Retention:            status.Retention,
		SmartRefreshInterval: status.SmartRefreshInterval,
		Version:              status.Version,
	}
}
