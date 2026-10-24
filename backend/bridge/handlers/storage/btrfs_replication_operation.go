package storage

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
)

var btrfsReplicationRoutes = btrfsReplicationBindings().Routes()

func btrfsReplicationBindings() apischema.BindingSet {
	return apischema.Bindings(
		apischema.TaskRunner[apischema.BtrfsJobIDRequest, btrfs.BtrfsReplicationResult]("storage.btrfs_replication_run", apischema.Privileged(), apischema.SessionTask(), apischema.WithTaskProgress[BtrfsOpProgress](), apischema.WithTaskMetadata(func(req apischema.BtrfsJobIDRequest) bridgeipc.TaskMetadata {
			// The uuid is best effort: a missing job fails when the task runs.
			job, _ := findBtrfsReplicationJob(req.ID)
			return bridgeipc.TaskMetadata{Identity: []string{job.UUID, "replication", req.ID}, Label: "Replicating btrfs snapshots"}
		})).Run(runBtrfsReplicationTask, bridgeipc.TaskSingletonSystem),
	)
}

func findBtrfsReplicationJob(id string) (btrfs.BtrfsReplicationJob, error) {
	jobs, err := btrfs.LoadJobs()
	if err != nil {
		return btrfs.BtrfsReplicationJob{}, err
	}
	i := slices.IndexFunc(jobs, func(j btrfs.BtrfsReplicationJob) bool { return j.ID == id })
	if i < 0 {
		return btrfs.BtrfsReplicationJob{}, fmt.Errorf("replication job %q not found", id)
	}
	return jobs[i], nil
}

func runBtrfsReplicationTask(ctx context.Context, task *bridgeipc.Task, req apischema.BtrfsJobIDRequest) (btrfs.BtrfsReplicationResult, error) {
	job, err := findBtrfsReplicationJob(req.ID)
	if err != nil {
		return btrfs.BtrfsReplicationResult{}, err
	}
	slog.Info("replicating btrfs snapshots", "id", job.ID, "uuid", job.UUID, "path", job.Path, "target", job.Target.Kind)
	task.ReportProgress(BtrfsOpProgress{Op: "send", Message: "Starting replication"})
	result, runErr := btrfs.Replicate(ctx, job, func(sent int64) {
		task.ReportProgress(BtrfsOpProgress{Op: "send", Message: fmt.Sprintf("%.1f MiB sent", float64(sent)/(1<<20))})
	})
	if err := btrfs.RecordReplicationOutcome(ctx, job, runErr); err != nil {
		slog.Warn("failed to record btrfs replication outcome", "id", job.ID, "error", err)
	}
	if runErr != nil {
		return btrfs.BtrfsReplicationResult{}, fmt.Errorf("replicate btrfs job %s (%s %s): %w", job.ID, job.UUID, job.Path, runErr)
	}
	slog.Info("replicated btrfs snapshots", "id", job.ID, "upToDate", result.UpToDate, "bytes", result.Bytes, "pruned", result.Pruned)
	return result, nil
}
