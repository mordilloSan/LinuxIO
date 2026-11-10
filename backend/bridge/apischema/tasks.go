package apischema

type TaskState string

type TaskError struct {
	Code    *int   `json:"code,omitzero"`
	Message string `json:"message"`
}

type TaskOwner struct {
	Username *string `json:"username,omitzero"`
	UID      *int    `json:"uid,omitzero"`
}

type TaskMetadata struct {
	Action      *string  `json:"action,omitzero"`
	Capability  *string  `json:"capability,omitzero"`
	Device      *string  `json:"device,omitzero"`
	Identity    []string `json:"identity,omitempty"`
	Label       *string  `json:"label,omitzero"`
	PackageIDs  []string `json:"packageIds,omitempty"`
	Path        *string  `json:"path,omitzero"`
	ProjectName *string  `json:"projectName,omitzero"`
	TestType    *string  `json:"testType,omitzero"`
}

type TaskSnapshot struct {
	// CancelRequested is set once the owner cancels a running task, which
	// stays running until its handler returns.
	CancelRequested *bool         `json:"cancel_requested,omitzero"`
	CreatedAt       string        `json:"created_at"`
	Error           *TaskError    `json:"error,omitzero"`
	FinishedAt      *string       `json:"finished_at,omitzero"`
	ID              string        `json:"id"`
	Owner           *TaskOwner    `json:"owner,omitzero"`
	Metadata        *TaskMetadata `json:"metadata,omitzero"`
	Progress        *TaskProgress `json:"progress,omitzero"`
	Result          any           `json:"result,omitzero"`
	StartedAt       *string       `json:"started_at,omitzero"`
	State           TaskState     `json:"state"`
	Type            string        `json:"type"`
	UpdatedAt       string        `json:"updated_at"`
}

type TaskEvent struct {
	Error    *TaskError    `json:"error,omitzero"`
	Task     TaskSnapshot  `json:"task"`
	Progress *TaskProgress `json:"progress,omitzero"`
	Result   any           `json:"result,omitzero"`
	Type     string        `json:"type"`
}

type TaskIDRequest struct {
	TaskID string `json:"taskId"`
}

type TaskDataRequest struct {
	TaskID string  `json:"taskId"`
	Offset *string `json:"offset,omitzero"`
}

type TaskListRequest struct {
	Status *string `json:"status,omitzero"`
}

type TaskHistoryRequest struct {
	// Before is the cursor of a page: the end time of the last record of the
	// page before. A page is never cut between records that ended at the same
	// instant, so it may hold more than Limit.
	Before *string `json:"before,omitzero"`
	Limit  *int    `json:"limit,omitzero"`
	// Unit lists only the runs of this systemd unit.
	Unit *string `json:"unit,omitzero"`
}

type TaskHistoryEntry struct {
	Task TaskSnapshot `json:"task"`
	// Reason is shutdown, reboot or crash for an interrupted job.
	Reason *string `json:"reason,omitzero"`
	// Unit is the systemd unit of a scheduled run.
	Unit *string `json:"unit,omitzero"`
	// Output is set when a saved output of the run exists.
	Output *bool `json:"output,omitzero"`
}

type TaskHistory struct {
	Entries []TaskHistoryEntry `json:"entries"`
	More    bool               `json:"more"`
}

type TaskOutputRequest struct {
	TaskID string `json:"taskId"`
	// Offset is where the chunk starts in the output; without it the chunk is
	// the end of the output.
	Offset *int64 `json:"offset,omitzero"`
	// Limit is the most bytes of the output the chunk holds: 0 means 256 KiB,
	// and more than 1 MiB is capped.
	Limit *int `json:"limit,omitzero"`
}

type TaskOutput struct {
	// Content is valid UTF-8, invalid bytes replaced, and starts on a
	// character boundary.
	Content string `json:"content"`
	// Offset is where Content starts in the output.
	Offset int64 `json:"offset"`
	// Size is the size of the whole output in bytes.
	Size int64 `json:"size"`
}

type TaskHostStatus struct {
	// UpdatePending is set while the job host is of an older build: a LinuxIO
	// update waits for its jobs to end, and new jobs are refused until then.
	UpdatePending bool `json:"update_pending"`
	// Running is the number of jobs the job host still runs.
	Running int `json:"running"`
	// Jobs are those jobs, sent only while an update waits.
	Jobs []TaskSnapshot `json:"jobs,omitzero"`
}
