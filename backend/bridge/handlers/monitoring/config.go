package monitoring

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

func FetchConfig(ctx context.Context) (apischema.MonitoringConfig, error) {
	resp, err := runCommand(ctx, "config.get", nil)
	if err != nil {
		return apischema.MonitoringConfig{}, fmt.Errorf("fetch monitoring config: %w", err)
	}
	return decodeConfig(resp.Data)
}

func UpdateConfig(ctx context.Context, patch apischema.MonitoringConfigPatch) (apischema.MonitoringConfig, bool, error) {
	if patchIsEmpty(patch) {
		return apischema.MonitoringConfig{}, false, bridgeipc.ErrInvalidArgs
	}
	resp, err := runCommand(ctx, "config.set", patch)
	if err != nil {
		return apischema.MonitoringConfig{}, false, fmt.Errorf("update monitoring config: %w", err)
	}
	cfg, err := decodeConfig(resp.Data)
	if err != nil {
		return apischema.MonitoringConfig{}, false, err
	}
	return cfg, resp.RestartRequired, nil
}

func decodeConfig(data json.RawMessage) (apischema.MonitoringConfig, error) {
	var cfg apischema.MonitoringConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return apischema.MonitoringConfig{}, fmt.Errorf("decode monitoring config: %w", err)
	}
	return cfg, nil
}

func patchIsEmpty(patch apischema.MonitoringConfigPatch) bool {
	return patch.CollectorInterval == nil &&
		patch.SmartRefreshInterval == nil &&
		patch.History == nil &&
		len(patch.CacheTTL) == 0 &&
		patch.AllowRemoteCommands == nil &&
		len(patch.Listeners) == 0
}
