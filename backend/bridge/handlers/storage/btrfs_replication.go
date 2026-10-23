package storage

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
)

// setBtrfsReplicationJob validates the job, checks its source subvolume
// exists and stores it.
func setBtrfsReplicationJob(ctx context.Context, job btrfs.BtrfsReplicationJob) ([]btrfs.BtrfsReplicationJob, error) {
	if err := btrfs.ValidateJob(ctx, &job); err != nil {
		return nil, err
	}
	subvols, err := btrfs.ListSubvolumes(ctx, job.UUID)
	if err != nil {
		return nil, err
	}
	if !slices.ContainsFunc(subvols, func(sv btrfs.BtrfsSubvolume) bool { return sv.Path == job.Path }) {
		return nil, fmt.Errorf("subvolume %q not found", job.Path)
	}
	return updateBtrfsReplicationJobs(ctx, func(jobs *[]btrfs.BtrfsReplicationJob) error {
		if err := btrfs.CheckTargetUnique(*jobs, job); err != nil {
			return err
		}
		*jobs = btrfs.UpsertJob(*jobs, withStoredRunState(*jobs, job))
		return nil
	})
}

// withStoredRunState replaces the client's LastRun/LastError with the stored
// job's, which only the replication run writes; a new job has neither.
func withStoredRunState(jobs []btrfs.BtrfsReplicationJob, job btrfs.BtrfsReplicationJob) btrfs.BtrfsReplicationJob {
	job.LastRun, job.LastError = nil, nil
	if i := slices.IndexFunc(jobs, func(j btrfs.BtrfsReplicationJob) bool { return j.ID == job.ID }); i >= 0 {
		job.LastRun, job.LastError = jobs[i].LastRun, jobs[i].LastError
	}
	return job
}

// CheckBtrfsReplicationHost scans a remote host's keys and, when confirmed, trusts
// the ones the user approved.
func CheckBtrfsReplicationHost(ctx context.Context, req apischema.BtrfsSSHTrustRequest) (btrfs.BtrfsSSHTrust, error) {
	slog.Info("checking btrfs replication host key", "host", req.Host, "port", req.Port, "confirm", req.Confirm)
	trust, err := trustBtrfsReplicationHost(ctx, req)
	if err != nil {
		slog.Error("failed to check btrfs replication host key", "host", req.Host, "port", req.Port, "error", err)
		return btrfs.BtrfsSSHTrust{}, err
	}
	return trust, nil
}

// trustBtrfsReplicationHost scans the host keys and, when confirmed, trusts
// the keys matching the fingerprints the user approved.
func trustBtrfsReplicationHost(ctx context.Context, req apischema.BtrfsSSHTrustRequest) (btrfs.BtrfsSSHTrust, error) {
	if req.Confirm {
		trusted, err := btrfs.TrustApprovedHost(ctx, req.Host, req.Port, req.Fingerprints)
		if err != nil {
			return btrfs.BtrfsSSHTrust{}, err
		}
		return btrfs.BtrfsSSHTrust{Fingerprints: trusted, Trusted: true}, nil
	}
	fingerprints, _, err := btrfs.ScanHost(ctx, req.Host, req.Port)
	if err != nil {
		return btrfs.BtrfsSSHTrust{}, err
	}
	return btrfs.BtrfsSSHTrust{Fingerprints: fingerprints}, nil
}

// SaveBtrfsReplicationJob stores a replication job and returns all jobs.
func SaveBtrfsReplicationJob(ctx context.Context, job btrfs.BtrfsReplicationJob) ([]btrfs.BtrfsReplicationJob, error) {
	slog.Info("setting btrfs replication job", "id", job.ID, "uuid", job.UUID, "path", job.Path, "target", job.Target.Kind)
	jobs, err := setBtrfsReplicationJob(ctx, job)
	if err != nil {
		slog.Error("failed to set btrfs replication job", "id", job.ID, "uuid", job.UUID, "path", job.Path, "error", err)
		return nil, err
	}
	return jobs, nil
}

// DeleteBtrfsReplicationJob removes a replication job and returns the rest.
func DeleteBtrfsReplicationJob(ctx context.Context, id string) ([]btrfs.BtrfsReplicationJob, error) {
	slog.Info("deleting btrfs replication job", "id", id)
	jobs, err := updateBtrfsReplicationJobs(ctx, func(jobs *[]btrfs.BtrfsReplicationJob) error {
		*jobs = btrfs.DeleteJob(*jobs, id)
		return nil
	})
	if err != nil {
		slog.Error("failed to delete btrfs replication job", "id", id, "error", err)
		return nil, err
	}
	return jobs, nil
}

// EnsureBtrfsReplicationSSHKey returns the replication public key, creating
// the key pair on first use.
func EnsureBtrfsReplicationSSHKey(ctx context.Context) (btrfs.BtrfsSSHKey, error) {
	pub, err := btrfs.EnsureSSHKey(ctx)
	if err != nil {
		slog.Error("failed to prepare btrfs replication ssh key", "error", err)
		return btrfs.BtrfsSSHKey{}, err
	}
	return btrfs.BtrfsSSHKey{PublicKey: pub}, nil
}
