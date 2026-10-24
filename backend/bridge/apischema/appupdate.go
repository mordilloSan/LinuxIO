package apischema

type VersionResponse struct {
	CheckedAt       string `json:"checked_at"`
	CurrentVersion  string `json:"current_version"`
	Error           string `json:"error,omitempty"`
	LatestVersion   string `json:"latest_version,omitempty"`
	UpdateAvailable bool   `json:"update_available"`
}

type AppUpdateRequest struct {
	RunID   string  `json:"runId"`
	Version *string `json:"version,omitzero"`
}
