package bridgeipc

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
)

// SuspendFunc stops a resumable Mutation job at shutdown and returns the
// checkpoint its ResumeFunc continues from. record is the job record.
type SuspendFunc func(ctx context.Context, record jsontext.Value) (checkpoint jsontext.Value, err error)

// ResumeFunc continues an interrupted resumable Mutation job from its record
// and returns the job's result.
type ResumeFunc func(ctx context.Context, record jsontext.Value) (result any, err error)

// ResumeAt says at which starts of the root job host an interrupted job of a
// resumable route is resumed.
type ResumeAt uint8

const (
	// ResumeAfterReboot resumes, at the next boot, a job that the shutdown or
	// the power loss of the boot before interrupted: work the kernel keeps
	// across a reboot, such as a btrfs balance. It is the default.
	ResumeAfterReboot ResumeAt = 1 << iota
	// ResumeAfterCrash resumes, at the root host's next start in the same
	// boot, a job whose host crashed (a kill, an out-of-memory kill, a
	// panic): work that runs on outside the host, in PackageKit or in a
	// drive, which Resume follows and never starts again.
	ResumeAfterCrash
	// ResumeAfterStop resumes, at the root host's next start in the same
	// boot, a job whose host was stopped by hand, which an admin may have
	// done to halt the job.
	ResumeAfterStop
)

// ResumeInBoot resumes in the same boot after a crash or a stop.
const ResumeInBoot = ResumeAfterCrash | ResumeAfterStop

// ErrNotResumed is what a Resume hook returns, wrapped with the reason, when
// the source of truth of its work cannot say what became of the job. The job
// then stays interrupted, as its host left it, with that error, and is not
// resumed again.
var ErrNotResumed = errors.New("not picked up again")

// SaveCheckpoint stores checkpoint in the record of the job running in ctx,
// so that a Resume hook after the job's host died finds where the job stood.
// The write is fsynced. Outside a job host it does nothing.
func SaveCheckpoint(ctx context.Context, checkpoint any) error {
	task, ok := TaskFromContext(ctx)
	if !ok {
		return nil
	}
	task.mu.RLock()
	rec := task.recorder
	task.mu.RUnlock()
	if rec == nil {
		return nil
	}
	raw, err := json.Marshal(checkpoint)
	if err != nil {
		return fmt.Errorf("encode checkpoint: %w", err)
	}
	return rec.Checkpoint(task, raw)
}

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
