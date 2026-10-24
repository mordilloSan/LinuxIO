package api

// ProcessListResponse is the body of RouteProcesses.
type ProcessListResponse struct {
	CapturedAt int64        `json:"captured_at"`
	Count      ProcessCount `json:"count"`
	Items      []Process    `json:"items"`
}

// ProgramListResponse is the body of RoutePrograms.
type ProgramListResponse struct {
	CapturedAt int64     `json:"captured_at"`
	Items      []Program `json:"items"`
}
