// Command linuxio-worker is the one program systemd starts with a word: the
// btrfs snapshot, replication, health, boot-time degraded mount, resync and
// rebuild jobs of the timer units, and the hooks that record a scheduled run and
// raise alerts. It is installed for root only and is not an admin command;
// linuxio is.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/mordilloSan/LinuxIO/backend/common/jobs"
	"github.com/mordilloSan/LinuxIO/backend/common/scheduledrun"
	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

// program is what the worker answers to --help and --version. Its usage is
// every word it takes.
var program = version.Program{
	Binary:    "linuxio-worker",
	About:     "the jobs and hooks LinuxIO runs from systemd timers and units",
	StartedBy: "systemd",
	Usage: []string{
		"linuxio-worker snapshot [--now RFC3339]",
		"linuxio-worker snapshot pre-update",
		"linuxio-worker replicate [--now RFC3339]",
		"linuxio-worker health",
		"linuxio-worker degraded",
		"linuxio-worker resync <uuid>",
		"linuxio-worker rebuild <uuid>",
		"linuxio-worker recycle",
		"linuxio-worker scheduled-run begin|end --unit <unit>",
		"linuxio-worker alert raise|resolve|auto-update|pending-updates [flags]",
	},
}

// reportProgress writes the progress of the scheduled run this process belongs
// to; tests swap it. A negative percentage means unknown.
var reportProgress = func(int, string) {}

// summarize stores what the run did in the record of the scheduled run this
// process belongs to; tests swap it.
var summarize = func(context.Context, scheduledrun.Summary) error { return nil }

// count is one number of a run's summary, such as 2 "taken".
type count struct {
	n     int
	label string
}

// runSummary is the one line the history shows for a run: its counts that
// are not zero, then how many of its items failed with alerts of their own,
// or "Nothing to do".
func runSummary(failed int, counts ...count) scheduledrun.Summary {
	var parts []string
	for _, c := range append(counts, count{failed, "failed"}) {
		if c.n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", c.n, c.label))
		}
	}
	if len(parts) == 0 {
		return scheduledrun.Summary{Message: "Nothing to do"}
	}
	return scheduledrun.Summary{Message: strings.Join(parts, ", "), Failures: failed}
}

func main() {
	os.Exit(run(os.Args))
}

func run(args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	reporter := scheduledrun.ReporterFromEnv(jobs.NewScheduledStore())
	reportProgress, summarize = reporter.Report, reporter.Summarize
	return runCLI(ctx, args[1:], os.Stdout, os.Stderr)
}

func runCLI(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		program.WriteHelp(stderr)
		return 2
	}
	if program.Answer(args, stdout) {
		return 0
	}
	switch args[0] {
	case "snapshot":
		return runSnapshot(ctx, args[1:], stdout, stderr)
	case "replicate":
		return runReplicate(ctx, args[1:], stdout, stderr)
	case "health":
		return runHealth(ctx, args[1:], stderr)
	case "degraded":
		return runDegraded(ctx, args[1:], stderr)
	case "resync":
		return runResync(ctx, args[1:], stderr)
	case "rebuild":
		return runRebuild(ctx, args[1:], stderr)
	case "recycle":
		return runRecycle(ctx, args[1:], stdout, stderr)
	case "scheduled-run":
		return runScheduledRun(ctx, args[1:], os.Getenv, scheduledrun.DefaultHooks(), stderr)
	case "alert":
		return runAlert(ctx, args[1:])
	default:
		fmt.Fprintf(stderr, "linuxio-worker: unknown command %q\n\n", args[0])
		program.WriteHelp(stderr)
		return 2
	}
}
