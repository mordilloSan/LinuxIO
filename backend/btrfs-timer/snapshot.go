package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
)

const snapshotUsage = "Usage: linuxio-btrfs-timer snapshot run [--now RFC3339] | pre-update"

// runSnapshots and runReplication are swapped by tests.
var (
	runSnapshots   = btrfs.Run
	runReplication = btrfs.RunReplication
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
// replication jobs, printing a summary and returning the exit code.
func snapshotPass(ctx context.Context, now time.Time, kind btrfs.SnapshotKind, stdout, stderr io.Writer) int {
	rules, err := btrfs.LoadRules()
	if err == nil {
		var report btrfs.RunReport
		report, err = runSnapshots(ctx, rules, now, kind)
		summary := fmt.Sprintf("taken=%d pruned=%d skipped=%d errors=%d", len(report.Taken), len(report.Pruned), len(report.Skipped), len(report.Errors))
		failures := report.Errors
		if err == nil && kind == btrfs.KindTimeline {
			replicated, replErrs := replicateJobs(ctx)
			summary += fmt.Sprintf(" replicated=%d replication_errors=%d", replicated, len(replErrs))
			for _, e := range replErrs {
				failures = append(failures, e.Error())
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
// how many enabled jobs succeeded (sent or already up to date) and the errors.
func replicateJobs(ctx context.Context) (int, []error) {
	jobs, err := btrfs.LoadJobs()
	if err != nil {
		return 0, []error{fmt.Errorf("load replication jobs: %w", err)}
	}
	errs := runReplication(ctx, jobs)
	enabled := 0
	for _, j := range jobs {
		if j.Enabled {
			enabled++
		}
	}
	return max(enabled-len(errs), 0), errs
}
