package btrfs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/alerts"
	"github.com/mordilloSan/LinuxIO/backend/common/filelock"
)

// ErrAlreadyRunning reports that another process holds the run lock of the
// job or filesystem: a skipped run, not a failure.
var ErrAlreadyRunning = errors.New("already running")

// Wait bounds for the run lock, variables so tests can shorten them. A
// snapshot waits out the timer's run, since a pre-update snapshot must not be
// lost to an overlap; a replication is skipped quickly, its send can take hours.
var (
	snapshotLockWait    = 5 * time.Minute
	replicationLockWait = 2 * time.Second
)

// runLocked runs fn under an exclusive cross-process lock, so the timer and a
// manual run never work on the same job or filesystem at once.
func runLocked(ctx context.Context, lockPath, what string, wait time.Duration, fn func() error) error {
	err := filelock.RunExclusive(ctx, lockPath, fn, filelock.WithDirPermissions(0o700), filelock.WithTimeout(wait))
	if errors.Is(err, filelock.ErrTimeout) {
		return fmt.Errorf("%s: %w", what, ErrAlreadyRunning)
	}
	return err
}

// RunReport summarises one pass over the snapshot rules.
type RunReport struct {
	Taken   []BtrfsSnapshot `json:"taken"`
	Pruned  []BtrfsSnapshot `json:"pruned"`
	Skipped []string        `json:"skipped"`
	Errors  []string        `json:"errors"`
}

// Run applies the rules: timeline runs enabled rules that are due, pre-update
// runs rules with BeforeUpdates whether or not they are enabled; both then
// prune, a disabled rule only its pre-update snapshots. Rule failures land in
// the report and as alerts; only a done ctx is an error.
func Run(ctx context.Context, rules []BtrfsSnapshotRule, now time.Time, kind SnapshotKind) (RunReport, error) {
	report := RunReport{Taken: []BtrfsSnapshot{}, Pruned: []BtrfsSnapshot{}, Skipped: []string{}, Errors: []string{}}
	mounts, mountsErr := listMounts(ctx)
	for _, rule := range rules {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		if (kind == KindPreUpdate && !rule.BeforeUpdates) || (kind != KindPreUpdate && !rule.Enabled) {
			continue
		}
		id, key := rule.UUID+"/"+rule.Path, snapshotAlertKey(rule.UUID, rule.Path)
		err := mountsErr
		if err == nil && !hasFilesystem(mounts, rule.UUID) {
			report.Skipped = append(report.Skipped, id)
			raise(ctx, alerts.Observation{
				Key: key, Severity: alerts.SeverityWarning,
				Title:        "Snapshot skipped for " + rule.Path,
				Message:      "Filesystem " + rule.UUID + " is not mounted",
				Link:         "/storage/subvolumes?view=snapshots&fs=" + rule.UUID,
				OccurrenceID: "unmounted",
			})
			continue
		}
		if err == nil {
			err = runLocked(ctx, RulesPath+"."+rule.UUID+".lock", "snapshot run for this filesystem", snapshotLockWait, func() error {
				return runRule(ctx, rule, now, kind, &report)
			})
		}
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return report, ctxErr
			}
			report.Errors = append(report.Errors, id+": "+err.Error())
			raise(ctx, alerts.Observation{
				Key: key, Severity: alerts.SeverityError,
				Title:        "Snapshot failed for " + rule.Path,
				Message:      err.Error(),
				Link:         "/storage/subvolumes?view=snapshots&fs=" + rule.UUID,
				OccurrenceID: "failed",
			})
			continue
		}
		if err := alerts.Resolve(ctx, AlertSource, key); err != nil {
			slog.Warn("failed to resolve btrfs snapshot alert", "rule", id, "error", err)
		}
	}
	return report, nil
}

func runRule(ctx context.Context, rule BtrfsSnapshotRule, now time.Time, kind SnapshotKind, report *RunReport) error {
	snaps, err := ListSnapshots(ctx, rule.UUID, &rule.Path)
	if err != nil {
		return err
	}
	take := kind == KindPreUpdate
	if kind == KindTimeline {
		var newest *time.Time
		for _, s := range snaps { // newest first
			if s.Kind == KindTimeline {
				newest = &s.CreatedAt
				break
			}
		}
		take = Due(rule, newest, now)
	}
	if take {
		snap, err := CreateSnapshot(ctx, rule.UUID, rule.Path, kind, now)
		if err != nil {
			return err
		}
		report.Taken = append(report.Taken, snap)
		snaps = append(snaps, snap)
	}
	if !rule.Enabled {
		// Only BeforeUpdates runs a disabled rule: leave its timeline alone.
		snaps = slices.DeleteFunc(snaps, func(s BtrfsSnapshot) bool { return s.Kind != KindPreUpdate })
	}
	// A snapshot that cannot be deleted (e.g. mounted) must not block the rest.
	var pruneErrs []error
	for _, s := range protectReplicationParent(snaps, rule, Prune(snaps, rule.Keep)) {
		if err := DeleteSnapshot(ctx, rule.UUID, s.Path); err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			pruneErrs = append(pruneErrs, fmt.Errorf("prune %s: %w", s.Path, err))
			continue
		}
		report.Pruned = append(report.Pruned, s)
	}
	return errors.Join(pruneErrs...)
}

// protectReplicationParent drops the two newest timeline snapshots from del
// when an enabled timeline replication job covers the rule: after a send the
// target holds the previous newest, the incremental parent of the next send,
// and pruning it locally would force a full send every cycle.
func protectReplicationParent(snaps []BtrfsSnapshot, rule BtrfsSnapshotRule, del []BtrfsSnapshot) []BtrfsSnapshot {
	jobs, err := LoadJobs()
	if err != nil {
		slog.Warn("failed to load btrfs replication jobs; not protecting the replication parent", "rule", rule.UUID+"/"+rule.Path, "error", err)
		return del
	}
	if !slices.ContainsFunc(jobs, func(j BtrfsReplicationJob) bool {
		return j.Enabled && j.After == AfterTimeline && j.UUID == rule.UUID && j.Path == rule.Path
	}) {
		return del
	}
	var timeline []BtrfsSnapshot
	for _, s := range snaps {
		if s.Kind == KindTimeline {
			timeline = append(timeline, s)
		}
	}
	slices.SortStableFunc(timeline, func(a, b BtrfsSnapshot) int { return b.CreatedAt.Compare(a.CreatedAt) })
	keep := timeline[:min(len(timeline), 2)]
	return slices.DeleteFunc(del, func(s BtrfsSnapshot) bool {
		return slices.ContainsFunc(keep, func(k BtrfsSnapshot) bool { return k.Path == s.Path })
	})
}

func hasFilesystem(mounts []Mount, uuid string) bool {
	for _, m := range mounts {
		if m.UUID == uuid {
			return true
		}
	}
	return false
}

// ResolveRuleAlert clears the alert of a snapshot rule that was deleted or
// will no longer run.
func ResolveRuleAlert(ctx context.Context, uuid, path string) {
	resolve(ctx, snapshotAlertKey(uuid, path))
}

// snapshotAlertKey is a stable alert key for a rule within the alert key
// alphabet: the uuid prefix plus the sanitised, lower-cased path.
func snapshotAlertKey(uuid, path string) string {
	safe := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			return r
		}
		return '-'
	}, strings.ToLower(uuid[:min(len(uuid), 8)]+"-"+path))
	return truncate("snapshot-"+safe, 64)
}

func truncate(s string, n int) string { return s[:min(len(s), n)] }
