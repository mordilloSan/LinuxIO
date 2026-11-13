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
// settings ask for, printing a summary and returning the exit code:
// scheduledrun.ExitReported for a timeline pass whose every failure raised
// its rule's alert, 1 for any other failure. The replication runs in its own
// unit, so a send that takes hours never holds up the next snapshots.
func snapshotPass(ctx context.Context, now time.Time, kind btrfs.SnapshotKind, stdout, stderr io.Writer) int {
	rules, err := btrfs.LoadRules()
	if err == nil {
		var report btrfs.RunReport
		reportProgress(-1, "Taking snapshots")
		report, err = runSnapshots(ctx, rules, now, kind)
		summary := fmt.Sprintf("taken=%d pruned=%d skipped=%d errors=%d", len(report.Taken), len(report.Pruned), len(report.Skipped), len(report.Errors))
		// A failed rule raised its own alert: only a failure without one
		// needs the run alert of the unit's end hook.
		failures, reported := report.Errors, kind == btrfs.KindTimeline
		if err == nil && kind == btrfs.KindTimeline {
			if ctx.Err() != nil {
				// A stopped pass neither starts the replication nor reports.
				failures, reported = append(failures, ctx.Err().Error()), false
			} else {
				started, replErr := startReplicationWhenEnabled(ctx)
				summary += fmt.Sprintf(" replication_started=%t", started)
				if replErr != nil {
					failures, reported = append(failures, replErr.Error()), false
				}
				reportRun(ctx, now, len(report.Taken), len(failures) > 0)
			}
		}
		fmt.Fprintln(stdout, summary)
		for _, e := range failures {
			fmt.Fprintln(stderr, e)
		}
		if err == nil && len(failures) > 0 {
			return failedExit(reported)
		}
	}
	if err != nil {
		fmt.Fprintf(stderr, "Failed to take btrfs snapshots: %v\n", err)
		if errors.Is(err, fs.ErrPermission) {
			fmt.Fprintln(stderr, "This command requires root")
		}
		return 1
	}
	return 0
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

// allReported reports whether each of errs is a failure its job's alert
// reports.
func allReported(errs []error) bool {
	for _, e := range errs {
		if !scheduledrun.AllReported(e) {
			return false
		}
	}
	return true
}

// failedExit is the exit status of a pass with failures: ExitReported when
// each of them raised its own alert.
func failedExit(reported bool) int {
	if reported {
		return scheduledrun.ExitReported
	}
	return 1
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
