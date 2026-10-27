// Package jobs persists one JSON record per job run by a per-user job host.
package jobs

import (
	"encoding/json/jsontext"
	"fmt"
	"time"
	"uuid"
)

const (
	Protocol                 = 1
	TerminalRetention        = 30 * 24 * time.Hour
	MaxTerminalRecordsPerUID = 200
	TransientMaxDuration     = time.Second
)

type State string

const (
	StateRunning     State = "running"
	StateCompleted   State = "completed"
	StateFailed      State = "failed"
	StateCanceled    State = "canceled"
	StateInterrupted State = "interrupted"
)

type Reason string

const (
	ReasonNone     Reason = ""
	ReasonShutdown Reason = "shutdown"
	ReasonCrash    Reason = "crash"
	ReasonReboot   Reason = "reboot"
)

type Owner struct {
	UID      uint32 `json:"uid"`
	Username string `json:"username"`
}

type Executor struct {
	Kind         string `json:"kind"`
	PID          int32  `json:"pid,omitzero"`
	PIDStart     uint64 `json:"pid_start,omitzero"`
	Unit         string `json:"unit,omitzero"`
	InvocationID string `json:"invocation_id,omitzero"`
}

type Error struct {
	Code    int    `json:"code,omitzero"`
	Message string `json:"message"`
}

type Record struct {
	ID         string         `json:"id"`
	Route      string         `json:"route"`
	Owner      Owner          `json:"owner"`
	Target     []string       `json:"target,omitzero"`
	State      State          `json:"state"`
	Reason     Reason         `json:"reason,omitzero"`
	CreatedAt  time.Time      `json:"created_at"`
	StartedAt  time.Time      `json:"started_at,omitzero"`
	FinishedAt time.Time      `json:"finished_at,omitzero"`
	Request    jsontext.Value `json:"request"`
	Progress   jsontext.Value `json:"progress,omitzero"`
	Result     jsontext.Value `json:"result,omitzero"`
	Error      *Error         `json:"error,omitzero"`
	Executor   Executor       `json:"executor"`
	Resumable  bool           `json:"resumable,omitzero"`
	Checkpoint jsontext.Value `json:"checkpoint,omitzero"`

	CancelRequested bool `json:"cancel_requested,omitzero"`
	Protocol        int  `json:"protocol"`
}

// ValidateID requires a canonical, non-zero UUID.
func ValidateID(value string) error {
	id, err := uuid.Parse(value)
	if err != nil || id == uuid.Nil() || id.String() != value {
		return fmt.Errorf("job ID must be a canonical non-zero UUID")
	}
	return nil
}

func (s State) terminal() bool {
	switch s {
	case StateCompleted, StateFailed, StateCanceled, StateInterrupted:
		return true
	default:
		return false
	}
}

func validateRecord(r Record) error {
	if err := ValidateID(r.ID); err != nil {
		return err
	}
	if r.Route == "" || len(r.Route) > 128 {
		return fmt.Errorf("job route is invalid")
	}
	if r.State != StateRunning && !r.State.terminal() {
		return fmt.Errorf("invalid job state %q", r.State)
	}
	switch r.Reason {
	case ReasonNone, ReasonShutdown, ReasonCrash, ReasonReboot:
	default:
		return fmt.Errorf("invalid job reason %q", r.Reason)
	}
	if r.CreatedAt.IsZero() {
		return fmt.Errorf("job created_at is required")
	}
	return nil
}
