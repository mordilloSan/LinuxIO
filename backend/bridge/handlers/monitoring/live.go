package monitoring

import (
	"context"

	monitoringapi "github.com/mordilloSan/LinuxIO/backend/monitoring/api"
)

const maxLivePayloadBytes = 4 << 20

// FetchLive reads the daemon's live payload over api.sock. Any session may
// call it; the socket is world-readable like /proc.
func FetchLive(ctx context.Context) (monitoringapi.Live, error) {
	var live monitoringapi.Live
	if err := getAPI(ctx, monitoringapi.RouteLive, maxLivePayloadBytes, &live); err != nil {
		return monitoringapi.Live{}, err
	}
	return live, nil
}
