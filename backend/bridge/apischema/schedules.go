package apischema

// ScheduleOptions is administrator-managed script content and native execution policy.
type ScheduleOptions struct {
	Name             string   `json:"name"`
	Script           string   `json:"script"`
	User             string   `json:"user"`
	Arguments        []string `json:"arguments"`
	OnCalendar       string   `json:"on_calendar"`
	Timezone         string   `json:"timezone"`
	Persistent       bool     `json:"persistent"`
	TimeoutSeconds   int      `json:"timeout_seconds"`
	WorkingDirectory string   `json:"working_directory"`
}

type ScheduleDefinition struct {
	ID      string          `json:"id"`
	Options ScheduleOptions `json:"options"`
	Enabled bool            `json:"enabled"`
}

// Native properties are the current/latest state, not a durable run ledger.
type ScheduleStatus struct {
	Definition   ScheduleDefinition `json:"definition"`
	UnitName     string             `json:"unit_name"`
	ActiveState  string             `json:"active_state"`
	Result       string             `json:"result"`
	InvocationID string             `json:"invocation_id"`
	ExitCode     *int32             `json:"exit_code"`
	ExitStatus   *int32             `json:"exit_status"`
	NextRunAt    *string            `json:"next_run_at"`
	LastRunAt    *string            `json:"last_run_at"`
	CanStop      bool               `json:"can_stop"`
	Error        *string            `json:"error"`
}

type SchedulesListResult struct {
	Available bool             `json:"available"`
	Error     *string          `json:"error"`
	Schedules []ScheduleStatus `json:"schedules"`
}

type ScheduleIDRequest struct {
	ID string `json:"id"`
}
type ScheduleCreateRequest struct {
	Options ScheduleOptions `json:"options"`
}
type ScheduleUpdateRequest struct {
	ID      string          `json:"id"`
	Options ScheduleOptions `json:"options"`
}
type ScheduleStopRequest struct {
	ID           string `json:"id"`
	InvocationID string `json:"invocation_id"`
}
