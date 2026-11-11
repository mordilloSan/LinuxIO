package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/alerts"
	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
)

const snapshotUsage = "Usage: linuxio-btrfs-timer snapshot run [--now RFC3339] | pre-update"

// runSnapshots, runReplication and raiseAlert are swapped by tests.
var (
	runSnapshots   = btrfs.Run
	runReplication = btrfs.RunReplication
	raiseAlert     = alerts.Raise
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

// snapshotPass runs the snapshot rules and, after a timeline pass, the
// replication jobs and the notice the settings ask for, printing a summary
// and returning the exit code.
func snapshotPass(ctx context.Context, now time.Time, kind btrfs.SnapshotKind, stdout, stderr io.Writer) int {
	rules, err := btrfs.LoadRules()
	if err == nil {
		var report btrfs.RunReport
		reportProgress(-1, "Taking snapshots")
		report, err = runSnapshots(ctx, rules, now, kind)
		summary := fmt.Sprintf("taken=%d pruned=%d skipped=%d errors=%d", len(report.Taken), len(report.Pruned), len(report.Skipped), len(report.Errors))
		failures := report.Errors
		if err == nil && kind == btrfs.KindTimeline {
			replicated, sent, replErrs := replicateJobs(ctx)
			summary += fmt.Sprintf(" replicated=%d sent=%d replication_errors=%d", replicated, sent, len(replErrs))
			for _, e := range replErrs {
				failures = append(failures, e.Error())
			}
			// A stopped pass reports nothing.
			if ctx.Err() == nil {
				reportRun(ctx, now, len(report.Taken), sent, len(failures) > 0)
			}
		}
		fmt.Fprintln(stdout, summary)
		for _, e := range failures {
			fmt.Fprintln(stderr, e)
		}
		if err == nil && len(failures) > 0 {
			return 1
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

// replicateJobs runs the replication jobs after a timeline pass and returns
// how many enabled jobs succeeded (sent or already up to date), how many sent
// a snapshot, and the errors.
func replicateJobs(ctx context.Context) (replicated, sent int, errs []error) {
	jobs, err := btrfs.LoadJobs()
	if err != nil {
		return 0, 0, []error{fmt.Errorf("load replication jobs: %w", err)}
	}
	sent, errs = runReplication(ctx, jobs, func(done, total int) {
		reportProgress(-1, fmt.Sprintf("Replicating %d of %d", done+1, total))
	})
	enabled := 0
	for _, j := range jobs {
		if j.Enabled {
			enabled++
		}
	}
	return max(enabled-len(errs), 0), sent, errs
}

// reportRun raises the notice of a timeline pass that the notification
// settings ask for: under changes a pass that took or sent a snapshot, under
// every_run every pass, except one that failed and took and sent nothing.
// Each pass is a new occurrence of the one notice. A failed rule or job raises
// its own alert whatever the settings say.
func reportRun(ctx context.Context, now time.Time, taken, sent int, failed bool) {
	changed := taken+sent > 0
	if failed && !changed {
		return
	}
	report := alerts.NotificationsOrDefaults().ReportFor(alerts.KindSnapshots)
	if report != alerts.ReportEveryRun && (report != alerts.ReportChanges || !changed) {
		return
	}
	title := "Snapshots: nothing to do"
	if changed {
		title = fmt.Sprintf("Snapshots: %d taken, %d sent", taken, sent)
	}
	if err := raiseAlert(ctx, alerts.Observation{
		Source: btrfs.AlertSource, Key: "snapshot-run", Severity: alerts.SeverityInfo,
		Title: title, Link: "/storage/subvolumes?view=snapshots",
		OccurrenceID: now.UTC().Format(time.RFC3339Nano),
	}); err != nil {
		slog.Warn("failed to raise btrfs snapshot run notice", "error", err)
	}
}
