package apischema

type Service struct {
	ActiveEnterTimestamp   int64   `json:"active_enter_timestamp"`
	ActiveState            string  `json:"active_state"`
	Description            *string `json:"description,omitempty"`
	InactiveEnterTimestamp int64   `json:"inactive_enter_timestamp"`
	LoadState              string  `json:"load_state"`
	Name                   string  `json:"name"`
	SubState               string  `json:"sub_state"`
	UnitFileState          string  `json:"unit_file_state"`
}

type UnitInfo struct {
	ActiveEnterTimestamp   *int64   `json:"ActiveEnterTimestamp,omitempty"`
	ActiveState            *string  `json:"ActiveState,omitempty"`
	After                  []string `json:"After,omitempty"`
	Before                 []string `json:"Before,omitempty"`
	Conflicts              []string `json:"Conflicts,omitempty"`
	Description            *string  `json:"Description,omitempty"`
	ExecMainStatus         *int     `json:"ExecMainStatus,omitempty"`
	FragmentPath           *string  `json:"FragmentPath,omitempty"`
	ID                     *string  `json:"Id,omitempty"`
	InactiveEnterTimestamp *int64   `json:"InactiveEnterTimestamp,omitempty"`
	LastTriggerUSec        *int64   `json:"LastTriggerUSec,omitempty"`
	Listen                 []string `json:"Listen,omitempty"`
	LoadState              *string  `json:"LoadState,omitempty"`
	MainPID                *int     `json:"MainPID,omitempty"`
	MemoryCurrent          *int64   `json:"MemoryCurrent,omitempty"`
	NAccepted              *int     `json:"NAccepted,omitempty"`
	NConnections           *int     `json:"NConnections,omitempty"`
	NextElapseUSec         *int64   `json:"NextElapseUSec,omitempty"`
	PartOf                 []string `json:"PartOf,omitempty"`
	Requires               []string `json:"Requires,omitempty"`
	SubState               *string  `json:"SubState,omitempty"`
	TriggeredBy            []string `json:"TriggeredBy,omitempty"`
	Unit                   *string  `json:"Unit,omitempty"`
	UnitFileState          *string  `json:"UnitFileState,omitempty"`
	WantedBy               []string `json:"WantedBy,omitempty"`
	Wants                  []string `json:"Wants,omitempty"`
}

type Timer struct {
	ActiveEnterTimestamp   int64   `json:"active_enter_timestamp"`
	ActiveState            string  `json:"active_state"`
	Description            *string `json:"description,omitempty"`
	InactiveEnterTimestamp int64   `json:"inactive_enter_timestamp"`
	LastTriggerUSec        int64   `json:"last_trigger_usec"`
	LoadState              string  `json:"load_state"`
	Name                   string  `json:"name"`
	NextElapseUSec         int64   `json:"next_elapse_usec"`
	SubState               string  `json:"sub_state"`
	Unit                   string  `json:"unit"`
	UnitFileState          string  `json:"unit_file_state"`
}

type Socket struct {
	ActiveEnterTimestamp   int64    `json:"active_enter_timestamp"`
	ActiveState            string   `json:"active_state"`
	Description            *string  `json:"description,omitempty"`
	InactiveEnterTimestamp int64    `json:"inactive_enter_timestamp"`
	Listen                 []string `json:"listen"`
	LoadState              string   `json:"load_state"`
	NAccepted              int      `json:"n_accepted"`
	NConnections           int      `json:"n_connections"`
	Name                   string   `json:"name"`
	SubState               string   `json:"sub_state"`
	UnitFileState          string   `json:"unit_file_state"`
}

type ServiceNameRequest struct {
	ServiceName string `json:"serviceName"`
}

type UnitNameRequest struct {
	UnitName string `json:"unitName"`
}
