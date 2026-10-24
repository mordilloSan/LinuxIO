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
	CreatedAt  string        `json:"created_at"`
	Error      *TaskError    `json:"error,omitzero"`
	FinishedAt *string       `json:"finished_at,omitzero"`
	ID         string        `json:"id"`
	Owner      *TaskOwner    `json:"owner,omitzero"`
	Metadata   *TaskMetadata `json:"metadata,omitzero"`
	Progress   *TaskProgress `json:"progress,omitzero"`
	Result     any           `json:"result,omitzero"`
	StartedAt  *string       `json:"started_at,omitzero"`
	State      TaskState     `json:"state"`
	Type       string        `json:"type"`
	UpdatedAt  string        `json:"updated_at"`
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
