package app

import "encoding/json"

// parseNvtopData parses a single nvtop JSON snapshot payload.
func (gm *GPUManager) parseNvtopData(output []byte) bool {
	var snapshots []nvtopSnapshot
	if err := json.Unmarshal(output, &snapshots); err != nil || len(snapshots) == 0 {
		return false
	}
	return gm.updateNvtopSnapshots(snapshots)
}
