package apischema

type ServiceLogsFollowRequest struct {
	ServiceName string  `json:"serviceName"`
	Lines       *string `json:"lines,omitempty"`
}

type GeneralLogsFollowRequest struct {
	Lines        *string  `json:"lines,omitempty"`
	TimePeriod   *string  `json:"timePeriod,omitempty"`
	Priority     *string  `json:"priority,omitempty"`
	Identifier   *string  `json:"identifier,omitempty"`
	Unit         *string  `json:"unit,omitempty"`
	InvocationID *string  `json:"invocationId,omitempty"`
	FieldFilters []string `json:"fieldFilters,omitempty"`
	// Follow controls whether the Task keeps streaming new entries after the
	// backlog. nil defaults to true (live mode).
	Follow *bool `json:"follow,omitempty"`
	// AfterCursor resumes a live stream strictly after an entry already held by
	// the client. When set, the initial backlog query is skipped.
	AfterCursor *string `json:"afterCursor,omitempty"`
}

type GeneralLogEntryRequest struct {
	Cursor string `json:"cursor"`
}

type GeneralLogsPageRequest struct {
	// Cursor is the exclusive boundary: the page holds entries strictly older.
	Cursor       string   `json:"cursor"`
	Lines        *string  `json:"lines,omitempty"`
	TimePeriod   *string  `json:"timePeriod,omitempty"`
	Priority     *string  `json:"priority,omitempty"`
	Identifier   *string  `json:"identifier,omitempty"`
	Unit         *string  `json:"unit,omitempty"`
	InvocationID *string  `json:"invocationId,omitempty"`
	FieldFilters []string `json:"fieldFilters,omitempty"`
}

type GeneralLogsPageResponse struct {
	// Entries are trimmed journalctl -o json lines, newest-first.
	Entries []string `json:"entries"`
	HasMore bool     `json:"hasMore"`
}
