// Command linuxio-docker-update runs the short-lived Docker update jobs.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/dockerupdate"
	"github.com/mordilloSan/LinuxIO/backend/common/jobs"
	"github.com/mordilloSan/LinuxIO/backend/common/logging"
	"github.com/mordilloSan/LinuxIO/backend/common/scheduledrun"
)

// runUpdates keeps argument and dispatch behavior testable without contacting Docker.
var runUpdates = dockerupdate.RunScheduled
var configureLogging = logging.Configure

// reportProgress writes the progress of the scheduled run this process belongs
// to; tests swap it. A negative percentage means unknown.
var reportProgress = func(int, string) {}

// summaryTimeout bounds the write of the run's summary.
const summaryTimeout = 5 * time.Second

// summarize stores what the pass did in the record of the scheduled run this
// process belongs to; tests swap it.
var summarize = func(context.Context, scheduledrun.Summary) error { return nil }

// newReporter finds the scheduled run this process belongs to and returns its
// progress and summary writers; tests swap it.
var newReporter = func() (func(int, string), func(context.Context, scheduledrun.Summary) error) {
	reporter := scheduledrun.ReporterFromEnv(jobs.NewScheduledStore())
	return reporter.Report, reporter.Summarize
}

func main() {
	os.Exit(run(os.Args))
}

func run(args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runWorker(ctx, args, os.Stdout, os.Stderr)
}

func runWorker(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if err := configureLogging("linuxio-docker-update", false); err != nil {
		fmt.Fprintf(stderr, "linuxio-docker-update: initialize logging: %v\n", err)
		return 1
	}
	// After the logging setup, so the reporter logs through the configured logger.
	reportProgress, summarize = newReporter()
	return runCLI(ctx, args, stdout, stderr)
}

func runCLI(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) < 2 {
		writeHelp(stdout)
		return 0
	}

	switch args[1] {
	case "run":
		configPath, ok := parseRunArgs(args[2:], stderr)
		if !ok {
			return 2
		}
		report := func(message string) { reportProgress(-1, message) }
		pass, err := runUpdates(ctx, configPath, report)
		// The report names each container the pass replaced or failed to; the
		// run's saved output shows it, and the history its summary.
		pass.Write(stdout)
		if summary, ok := pass.Summary(); ok {
			// Stored even when a signal ended the pass, but not for longer
			// than a stopping unit can wait for the store's lock.
			summaryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), summaryTimeout)
			summaryErr := summarize(summaryCtx, summary)
			cancel()
			// A run started by hand has no record to store it in.
			if summaryErr != nil && !errors.Is(summaryErr, scheduledrun.ErrNoRun) {
				slog.Warn("failed to store the run summary", "component", "docker", "subsystem", "update", "error", summaryErr)
			}
			// Without its summary the end hook would read a skipped pass as
			// one that ran and clear the unit's run alert; the exit status
			// tells it instead, at the price of a unit shown as failed.
			if summaryErr != nil && summary.Skipped {
				return scheduledrun.ExitSkipped
			}
		}
		// A container that failed raised its own alert and is in the report:
		// the pass did its work, so the unit does not fail for it. Only what
		// has no alert of its own fails the run, and the end hook reports it.
		if unreported := scheduledrun.Unreported(err); unreported != nil {
			fmt.Fprintf(stderr, "linuxio-docker-update: run failed: %v\n", unreported)
			return 1
		}
		return 0
	case "help", "-h", "--help":
		writeHelp(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "linuxio-docker-update: unknown command %q\n", args[1])
		writeUsage(stderr)
		return 2
	}
}

func parseRunArgs(args []string, stderr io.Writer) (string, bool) {
	configPath := dockerupdate.ConfigPath
	for i := 0; i < len(args); i++ {
		if args[i] != "--config" {
			fmt.Fprintf(stderr, "linuxio-docker-update: run: unknown argument %q\n", args[i])
			return "", false
		}
		if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
			fmt.Fprintln(stderr, "linuxio-docker-update: run: --config requires a path")
			return "", false
		}
		configPath = args[i+1]
		i++
	}
	return configPath, true
}

func writeUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: linuxio-docker-update run [options]")
}

func writeHelp(w io.Writer) {
	fmt.Fprintln(w, "LinuxIO Docker update command")
	fmt.Fprintln(w)
	writeUsage(w)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  run  Run one configured Docker update pass")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Options for run:")
	fmt.Fprintln(w, "  --config PATH  Use an alternate update configuration file")
}
