package monitoring

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	monitoringapi "github.com/mordilloSan/LinuxIO/backend/monitoring/api"
)

const maxLivePayloadBytes = 4 << 20

// FetchLive reads the daemon's live payload over api.sock. Any session may
// call it; the socket is world-readable like /proc.
func FetchLive(ctx context.Context) (monitoringapi.Live, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix"+monitoringapi.RouteLive, nil)
	if err != nil {
		return monitoringapi.Live{}, fmt.Errorf("create live request: %w", err)
	}
	resp, err := apiClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return monitoringapi.Live{}, ctx.Err()
		}
		return monitoringapi.Live{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return monitoringapi.Live{}, fmt.Errorf("%w: live returned %s", ErrUnavailable, resp.Status)
	}
	var live monitoringapi.Live
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxLivePayloadBytes)).Decode(&live); err != nil {
		return monitoringapi.Live{}, fmt.Errorf("decode live payload: %w", err)
	}
	return live, nil
}
