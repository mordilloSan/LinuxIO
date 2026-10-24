package gpu

import (
	"encoding/json/v2"
)

// parseNvtopData parses a single nvtop JSON snapshot payload.
func (gm *Manager) parseNvtopData(output []byte) bool {
	var snapshots []nvtopSnapshot
	if err := json.Unmarshal(output, &snapshots); err != nil || len(snapshots) == 0 {
		return false
	}
	return gm.updateNvtopSnapshots(snapshots)
}
