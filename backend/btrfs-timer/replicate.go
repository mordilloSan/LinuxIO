package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"slices"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/alerts"
	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
	"github.com/mordilloSan/LinuxIO/backend/common/scheduledrun"
)

const replicateUsage = "Usage: linuxio-btrfs-timer replicate run [--now RFC3339]"

// runReplication is swapped by tests.
var runReplication = btrfs.RunReplication

func runReplicate(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "run" {
		fmt.Fprintln(stderr, replicateUsage)
		return 2
	}
	flags := flag.NewFlagSet("linuxio-btrfs-timer replicate run", flag.ContinueOnError)
	flags.SetOutput(stderr)
	nowFlag := flags.String("now", "", "date the run's notice at this RFC3339 time")
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, replicateUsage)
		return 2
	}
	now := time.Now()
	if *nowFlag != "" {
		t, err := time.Parse(time.RFC3339, *nowFlag)
		if err != nil {
			fmt.Fprintf(stderr, "Invalid --now: %v\n", err)
			return 2
		}
		now = t
	}
	return replicatePass(ctx, now, stdout, stderr)
}

// replicatePass runs the enabled replication jobs, which the snapshot pass
// starts in a unit of their own, raises the notice the settings ask for,
// prints a summary and returns the exit code: scheduledrun.ExitReported when
// each failure raised its job's alert, scheduledrun.ExitSkipped when another
// run held every enabled job, 1 for any other failure, such as jobs that
// cannot be read or an outcome that could not be recorded.
func replicatePass(ctx context.Context, now time.Time, stdout, stderr io.Writer) int {
	jobs, err := btrfs.LoadJobs()
	if err != nil {
		fmt.Fprintf(stderr, "Failed to load btrfs replication jobs: %v\n", err)
		if errors.Is(err, fs.ErrPermission) {
			fmt.Fprintln(stderr, "This command requires root")
		}
		return 1
	}
	replicated, sent, errs := runReplication(ctx, jobs, func(done, total int) {
		reportProgress(-1, fmt.Sprintf("Replicating %d of %d", done+1, total))
	})
	fmt.Fprintf(stdout, "replicated=%d sent=%d errors=%d\n", replicated, sent, len(errs))
	for _, e := range errs {
		fmt.Fprintln(stderr, e)
	}
	// Another run, such as one started from the page, held every enabled
	// job: this run did no work, so it changes no alert and raises no notice.
	if len(errs) == 0 && replicated == 0 && slices.ContainsFunc(jobs, func(j btrfs.BtrfsReplicationJob) bool { return j.Enabled }) {
		return scheduledrun.ExitSkipped
	}
	// A stopped run reports nothing.
	if ctx.Err() == nil {
		reportReplication(ctx, now, sent, len(errs) > 0)
	}
	if len(errs) > 0 {
		return failedExit(allReported(errs))
	}
	return 0
}

// reportReplication raises the notice of a replication run that the
// snapshot notification settings ask for: under changes a run that sent a
// snapshot, under every_run every run, except one that failed and sent
// nothing. Each run is a new occurrence of the one notice. A failed job
// raises its own alert whatever the settings say.
func reportReplication(ctx context.Context, now time.Time, sent int, failed bool) {
	title := "Replication: nothing to do"
	if sent > 0 {
		title = fmt.Sprintf("Replication: %d sent", sent)
	}
	raiseRunNotice(ctx, alerts.Observation{
		Key: "replication-run", Title: title, Link: "/storage/subvolumes?view=replication",
		OccurrenceID: now.UTC().Format(time.RFC3339Nano),
	}, sent > 0, failed)
}
