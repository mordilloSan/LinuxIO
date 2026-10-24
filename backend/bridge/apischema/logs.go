package apischema

type ServiceLogsFollowRequest struct {
	ServiceName string  `json:"serviceName"`
	Lines       *string `json:"lines,omitzero"`
}

type GeneralLogsFollowRequest struct {
	Lines        *string  `json:"lines,omitzero"`
	TimePeriod   *string  `json:"timePeriod,omitzero"`
	Priority     *string  `json:"priority,omitzero"`
	Identifier   *string  `json:"identifier,omitzero"`
	Unit         *string  `json:"unit,omitzero"`
	InvocationID *string  `json:"invocationId,omitzero"`
	FieldFilters []string `json:"fieldFilters,omitempty"`
	// Follow controls whether the Task keeps streaming new entries after the
	// backlog. nil defaults to true (live mode).
	Follow *bool `json:"follow,omitzero"`
	// AfterCursor resumes a live stream strictly after an entry already held by
	// the client. When set, the initial backlog query is skipped.
	AfterCursor *string `json:"afterCursor,omitzero"`
}

type GeneralLogEntryRequest struct {
	Cursor string `json:"cursor"`
}

type GeneralLogsPageRequest struct {
	// Cursor is the exclusive boundary: the page holds entries strictly older.
	Cursor       string   `json:"cursor"`
	Lines        *string  `json:"lines,omitzero"`
	TimePeriod   *string  `json:"timePeriod,omitzero"`
	Priority     *string  `json:"priority,omitzero"`
	Identifier   *string  `json:"identifier,omitzero"`
	Unit         *string  `json:"unit,omitzero"`
	InvocationID *string  `json:"invocationId,omitzero"`
	FieldFilters []string `json:"fieldFilters,omitempty"`
}

type GeneralLogsPageResponse struct {
	// Entries are trimmed journalctl -o json lines, newest-first.
	Entries []string `json:"entries"`
	HasMore bool     `json:"hasMore"`
}
