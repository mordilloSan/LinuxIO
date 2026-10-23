package apischema

type TaskState string

type TaskError struct {
	Code    *int   `json:"code,omitempty"`
	Message string `json:"message"`
}

type TaskOwner struct {
	Username *string `json:"username,omitempty"`
	UID      *int    `json:"uid,omitempty"`
}

type TaskMetadata struct {
	Action      *string  `json:"action,omitempty"`
	Capability  *string  `json:"capability,omitempty"`
	Device      *string  `json:"device,omitempty"`
	Identity    []string `json:"identity,omitempty"`
	Label       *string  `json:"label,omitempty"`
	PackageIDs  []string `json:"packageIds,omitempty"`
	Path        *string  `json:"path,omitempty"`
	ProjectName *string  `json:"projectName,omitempty"`
	TestType    *string  `json:"testType,omitempty"`
}

type TaskSnapshot struct {
	CreatedAt  string        `json:"created_at"`
	Error      *TaskError    `json:"error,omitempty"`
	FinishedAt *string       `json:"finished_at,omitempty"`
	ID         string        `json:"id"`
	Owner      *TaskOwner    `json:"owner,omitempty"`
	Metadata   *TaskMetadata `json:"metadata,omitempty"`
	Progress   *TaskProgress `json:"progress,omitempty"`
	Result     any           `json:"result,omitempty"`
	StartedAt  *string       `json:"started_at,omitempty"`
	State      TaskState     `json:"state"`
	Type       string        `json:"type"`
	UpdatedAt  string        `json:"updated_at"`
}

type TaskEvent struct {
	Error    *TaskError    `json:"error,omitempty"`
	Task     TaskSnapshot  `json:"task"`
	Progress *TaskProgress `json:"progress,omitempty"`
	Result   any           `json:"result,omitempty"`
	Type     string        `json:"type"`
}

type TaskIDRequest struct {
	TaskID string `json:"taskId"`
}

type TaskDataRequest struct {
	TaskID string  `json:"taskId"`
	Offset *string `json:"offset,omitempty"`
}

type TaskListRequest struct {
	Status *string `json:"status,omitempty"`
}
