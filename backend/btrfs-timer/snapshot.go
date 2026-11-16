package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"slices"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/alerts"
	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
	"github.com/mordilloSan/LinuxIO/backend/common/scheduledrun"
)

const snapshotUsage = "Usage: linuxio-btrfs-timer snapshot run [--now RFC3339] | pre-update"

// runSnapshots, startReplication and raiseAlert are swapped by tests.
var (
	runSnapshots     = btrfs.Run
	startReplication = func(ctx context.Context) error { return btrfs.ReplicateUnit().Start(ctx) }
	raiseAlert       = alerts.Raise
)

func runSnapshot(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, snapshotUsage)
		return 2
	}
	var kind btrfs.SnapshotKind
	switch args[0] {
	case "run":
		kind = btrfs.KindTimeline
	case "pre-update":
		kind = btrfs.KindPreUpdate
	default:
		fmt.Fprintf(stderr, "Unknown snapshot action: %s\n%s\n", args[0], snapshotUsage)
		return 2
	}
	flags := flag.NewFlagSet("linuxio-btrfs-timer snapshot "+args[0], flag.ContinueOnError)
	flags.SetOutput(stderr)
	var nowFlag string
	if kind == btrfs.KindTimeline {
		flags.StringVar(&nowFlag, "now", "", "evaluate rules at this RFC3339 time")
	}
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	now := time.Now()
	if nowFlag != "" {
		t, err := time.Parse(time.RFC3339, nowFlag)
		if err != nil {
			fmt.Fprintf(stderr, "Invalid --now: %v\n", err)
			return 2
		}
		now = t
	}
	return snapshotPass(ctx, now, kind, stdout, stderr)
}

// snapshotPass runs the snapshot rules and, after a timeline pass, starts
// the replication unit when a job is enabled and raises the notice the
// settings ask for, printing a summary line and returning the exit code. A
// timeline pass stores its run's summary and exits with 0 also when rules
// failed, since each raised its rule's alert, and with 1 for a failure
// without one. The replication runs in its own unit, so a send that takes
// hours never holds up the next snapshots.
func snapshotPass(ctx context.Context, now time.Time, kind btrfs.SnapshotKind, stdout, stderr io.Writer) int {
	rules, err := btrfs.LoadRules()
	if err != nil {
		return snapshotsFailed(stderr, err)
	}
	reportProgress(-1, "Taking snapshots")
	report, err := runSnapshots(ctx, rules, now, kind)
	line := fmt.Sprintf("taken=%d pruned=%d skipped=%d errors=%d", len(report.Taken), len(report.Pruned), len(report.Skipped), len(report.Errors))
	failures := report.Errors
	// unreported is a failure of a timeline pass that no rule's alert
	// reports: only that needs the run alert of the unit's end hook.
	var unreported error
	if err == nil && kind == btrfs.KindTimeline {
		if ctx.Err() != nil {
			// A stopped pass neither starts the replication nor reports.
			unreported = ctx.Err()
			failures = append(failures, unreported.Error())
		} else {
			started, replErr := startReplicationWhenEnabled(ctx)
			line += fmt.Sprintf(" replication_started=%t", started)
			if replErr != nil {
				unreported = replErr
				failures = append(failures, replErr.Error())
			}
			reportRun(ctx, now, len(report.Taken), len(failures) > 0)
		}
	}
	fmt.Fprintln(stdout, line)
	for _, e := range failures {
		fmt.Fprintln(stderr, e)
	}
	if err != nil {
		return snapshotsFailed(stderr, err)
	}
	if kind != btrfs.KindTimeline {
		// The pre-update pass runs inside the system update's unit, whose
		// record is not this pass's, and its rules raise no alert: it stores
		// no summary, and a failed rule fails it.
		if len(failures) > 0 {
			return 1
		}
		return 0
	}
	return scheduledrun.Conclude(ctx, summarize, snapshotSummary(report), unreported)
}

func snapshotsFailed(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "Failed to take btrfs snapshots: %v\n", err)
	if errors.Is(err, fs.ErrPermission) {
		fmt.Fprintln(stderr, "This command requires root")
	}
	return 1
}

// snapshotSummary is the one line the history shows for a timeline pass. A
// rule that failed raised its own alert and counts as a failure of the
// pass's items.
func snapshotSummary(report btrfs.RunReport) scheduledrun.Summary {
	return runSummary(len(report.Errors), count{len(report.Taken), "taken"}, count{len(report.Pruned), "pruned"})
}

// startReplicationWhenEnabled starts the replication unit, without waiting
// for it, when some job is enabled, and reports whether it did.
func startReplicationWhenEnabled(ctx context.Context) (bool, error) {
	jobs, err := btrfs.LoadJobs()
	if err != nil {
		return false, fmt.Errorf("load replication jobs: %w", err)
	}
	if !slices.ContainsFunc(jobs, func(j btrfs.BtrfsReplicationJob) bool { return j.Enabled }) {
		return false, nil
	}
	if err := startReplication(ctx); err != nil {
		return false, fmt.Errorf("start btrfs replication: %w", err)
	}
	return true, nil
}

// reportRun raises the notice of a timeline pass that the notification
// settings ask for: under changes a pass that took a snapshot, under
// every_run every pass, except one that failed and took nothing. Each pass is
// a new occurrence of the one notice. A failed rule raises its own alert
// whatever the settings say. The replication unit raises its own notice.
func reportRun(ctx context.Context, now time.Time, taken int, failed bool) {
	title := "Snapshots: nothing to do"
	if taken > 0 {
		title = fmt.Sprintf("Snapshots: %d taken", taken)
	}
	raiseRunNotice(ctx, alerts.Observation{
		Key: "snapshot-run", Title: title, Link: "/storage/subvolumes?view=snapshots",
		OccurrenceID: now.UTC().Format(time.RFC3339Nano),
	}, taken > 0, failed)
}

// raiseRunNotice raises obs, a notice of a timer run that changed something
// or not, when the snapshot notification settings ask for it: under changes
// a run that changed something, under every_run every run, except one that
// failed and changed nothing, whose failures have their own alerts.
func raiseRunNotice(ctx context.Context, obs alerts.Observation, changed, failed bool) {
	if failed && !changed {
		return
	}
	report := alerts.NotificationsOrDefaults().ReportFor(alerts.KindSnapshots)
	if report != alerts.ReportEveryRun && (report != alerts.ReportChanges || !changed) {
		return
	}
	obs.Source, obs.Severity = btrfs.AlertSource, alerts.SeverityInfo
	if err := raiseAlert(ctx, obs); err != nil {
		slog.Warn("failed to raise btrfs run notice", "key", obs.Key, "error", err)
	}
}
