package storage

import (
	"context"
	"fmt"
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
