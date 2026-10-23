package btrfs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"uuid"

	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

// ReplicationPath is the root-owned replication job file. Tests may override it.
var ReplicationPath = version.ConfigDir + "/btrfs-replication.json"

// Replication target kinds.
const (
	TargetLocal = "local"
	TargetSSH   = "ssh"
)

// When a job replicates.
const (
	AfterTimeline = "timeline"
	AfterManual   = "manual"
)

const defaultSSHPort = 22

var (
	jobIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	// sshNamePattern is stricter than ssh needs: no whitespace, '@' or ':',
	// and no leading '-' so a host or user can never be read as an option.
	sshNamePattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]*$`)
)

// BtrfsReplicationTarget is where a job sends snapshots: a path on another
// local btrfs filesystem, or a path on an SSH host.
type BtrfsReplicationTarget struct {
	Kind string `json:"kind"`
	Path string `json:"path"`
	Host string `json:"host,omitempty"`
	User string `json:"user,omitempty"`
	Port int    `json:"port,omitempty"`
}

// BtrfsReplicationJob sends the snapshots of one subvolume, identified by
// (UUID, Path), to a target.
type BtrfsReplicationJob struct {
	ID           string                 `json:"id"`
	UUID         string                 `json:"uuid"`
	Path         string                 `json:"path"`
	Target       BtrfsReplicationTarget `json:"target"`
	KeepOnTarget int                    `json:"keepOnTarget"`
	Enabled      bool                   `json:"enabled"`
	After        string                 `json:"after"`
	LastRun      *time.Time             `json:"lastRun,omitempty"`
	LastError    *string                `json:"lastError,omitempty"`
}

type jobsDocument struct {
	Jobs []BtrfsReplicationJob `json:"jobs"`
}

// LoadJobs reads the job file. A missing file is no jobs.
func LoadJobs() ([]BtrfsReplicationJob, error) {
	data, err := os.ReadFile(ReplicationPath)
	if errors.Is(err, fs.ErrNotExist) {
		return []BtrfsReplicationJob{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", ReplicationPath, err)
	}
	var doc jobsDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", ReplicationPath, err)
	}
	if doc.Jobs == nil {
		doc.Jobs = []BtrfsReplicationJob{}
	}
	return doc.Jobs, nil
}

// UpdateJobs loads the jobs, applies fn and writes the result, all under the
// job file lock, and returns the jobs written.
func UpdateJobs(ctx context.Context, fn func(jobs *[]BtrfsReplicationJob) error) ([]BtrfsReplicationJob, error) {
	return updateListFile(ctx, ReplicationPath, LoadJobs, func(j []BtrfsReplicationJob) any { return jobsDocument{Jobs: j} }, fn)
}

// UpsertJob replaces the job with the same ID or appends it.
func UpsertJob(jobs []BtrfsReplicationJob, job BtrfsReplicationJob) []BtrfsReplicationJob {
	for i := range jobs {
		if jobs[i].ID == job.ID {
			jobs[i] = job
			return jobs
		}
	}
	return append(jobs, job)
}

// DeleteJob removes the job with the given ID, if any.
func DeleteJob(jobs []BtrfsReplicationJob, id string) []BtrfsReplicationJob {
	return slices.DeleteFunc(jobs, func(j BtrfsReplicationJob) bool { return j.ID == id })
}

// ValidateJob checks a job is complete and safe to run, generating its ID
// when empty and defaulting an SSH port of 0 to 22.
func ValidateJob(ctx context.Context, job *BtrfsReplicationJob) error {
	if job.ID == "" {
		job.ID = uuid.NewV4().String()
	} else if !jobIDPattern.MatchString(job.ID) {
		return fmt.Errorf("invalid replication job id %q", job.ID)
	}
	if job.UUID == "" {
		return errors.New("job filesystem uuid is required")
	}
	if err := ValidateSubvolumePath(job.Path); err != nil {
		return err
	}
	if job.After != AfterTimeline && job.After != AfterManual {
		return fmt.Errorf("invalid replication trigger %q", job.After)
	}
	if job.KeepOnTarget < 0 {
		return errors.New("keep on target must not be negative")
	}
	t := &job.Target
	if !filepath.IsAbs(t.Path) || filepath.Clean(t.Path) != t.Path {
		return fmt.Errorf("target path %q must be absolute and clean", t.Path)
	}
	switch t.Kind {
	case TargetLocal:
		return validateLocalTarget(ctx, job.UUID, t.Path)
	case TargetSSH:
		if t.Port == 0 {
			t.Port = defaultSSHPort
		}
		if err := validateSSHHost(t.Host, t.Port); err != nil {
			return err
		}
		if !sshNamePattern.MatchString(t.User) {
			return fmt.Errorf("invalid ssh user %q", t.User)
		}
		return nil
	default:
		return fmt.Errorf("invalid replication target kind %q", t.Kind)
	}
}

// CheckTargetUnique rejects a job whose target another job, enabled or not,
// already uses: jobs sharing a target would see and prune each other's
// copies.
func CheckTargetUnique(jobs []BtrfsReplicationJob, job BtrfsReplicationJob) error {
	t := job.Target
	for _, j := range jobs {
		o := j.Target
		if j.ID != job.ID && o.Kind == t.Kind && o.Host == t.Host && o.Port == t.Port && o.Path == t.Path {
			return errors.New("another job already replicates to this target")
		}
	}
	return nil
}

func validateSSHHost(host string, port int) error {
	if !sshNamePattern.MatchString(host) {
		return fmt.Errorf("invalid ssh host %q", host)
	}
	if port < 1 || port > 65535 {
		return fmt.Errorf("invalid ssh port %d", port)
	}
	return nil
}

// validateLocalTarget requires path to sit on a mounted btrfs filesystem
// other than the source one.
// ponytail: only btrfs mounts are listed, so a non-btrfs mount nested under a
// btrfs one is not detected here; the receive itself fails on it.
func validateLocalTarget(ctx context.Context, sourceUUID, path string) error {
	mounts, err := listMounts(ctx)
	if err != nil {
		return fmt.Errorf("list btrfs mounts: %w", err)
	}
	var best *Mount
	for i, m := range mounts {
		under := m.Mountpoint == "/" || path == m.Mountpoint || strings.HasPrefix(path, m.Mountpoint+"/")
		if under && (best == nil || len(m.Mountpoint) > len(best.Mountpoint)) {
			best = &mounts[i]
		}
	}
	if best == nil {
		return fmt.Errorf("target path %q is not on a mounted btrfs filesystem", path)
	}
	if best.UUID == sourceUUID {
		return errors.New("target must be a different btrfs filesystem")
	}
	return nil
}

// ChooseParent returns the newest local snapshot the target already holds a
// received copy of, the parent for an incremental send, or nil when none.
func ChooseParent(local, remote []BtrfsSnapshot) *BtrfsSnapshot {
	received := make(map[string]bool, len(remote))
	for _, r := range remote {
		if r.ReceivedUUID != "" {
			received[r.ReceivedUUID] = true
		}
	}
	var best *BtrfsSnapshot
	for i := range local {
		if received[local[i].UUID] && (best == nil || local[i].CreatedAt.After(best.CreatedAt)) {
			best = &local[i]
		}
	}
	if best == nil {
		return nil
	}
	parent := *best
	return &parent
}

// PruneTarget returns the target snapshots beyond the newest keep, oldest
// first. keep <= 0 means unlimited and prunes nothing.
func PruneTarget(remote []BtrfsSnapshot, keep int) []BtrfsSnapshot {
	if keep <= 0 || len(remote) <= keep {
		return nil
	}
	sorted := slices.Clone(remote)
	slices.SortStableFunc(sorted, func(a, b BtrfsSnapshot) int { return a.CreatedAt.Compare(b.CreatedAt) })
	return sorted[:len(sorted)-keep]
}
