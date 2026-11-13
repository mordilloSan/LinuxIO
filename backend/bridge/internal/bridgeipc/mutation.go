package bridgeipc

import (
	"context"
	"encoding/json/jsontext"
)

// SuspendFunc stops a resumable Mutation job at shutdown and returns the
// checkpoint its ResumeFunc continues from. record is the job record.
type SuspendFunc func(ctx context.Context, record jsontext.Value) (checkpoint jsontext.Value, err error)

// ResumeFunc continues an interrupted resumable Mutation job from its record
// and returns the job's result.
type ResumeFunc func(ctx context.Context, record jsontext.Value) (result any, err error)

type taskContextKey struct{}

// TaskFromContext returns the task whose runner received ctx.
func TaskFromContext(ctx context.Context) (*Task, bool) {
	task, ok := ctx.Value(taskContextKey{}).(*Task)
	return task, ok
}

// ReportProgress reports progress on the task carried by ctx. Outside a task
// it does nothing, so a handler can report whether or not it runs as a job.
func ReportProgress(ctx context.Context, p TaskProgress) {
	if task, ok := TaskFromContext(ctx); ok {
		task.ReportProgress(p)
	}
}
