// Command linuxio-docker-update runs the short-lived Docker update jobs.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

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
	return runUpdater(ctx, args, os.Stdout, os.Stderr)
}

func runUpdater(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if err := configureLogging("linuxio-docker-update", false); err != nil {
		fmt.Fprintf(stderr, "linuxio-docker-update: initialize logging: %v\n", err)
		return 1
	}
	// After the logging setup, so the reporter logs through the configured logger.
	reportProgress, summarize = newReporter()
	return runCLI(ctx, args, stdout, stderr)
}

func runCLI(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) > 1 {
		switch args[1] {
		case "help", "-h", "--help":
			writeHelp(stdout)
			return 0
		}
	}
	configPath, ok := parseRunArgs(args[1:], stderr)
	if !ok {
		writeUsage(stderr)
		return 2
	}
	report := func(message string) { reportProgress(-1, message) }
	pass, err := runUpdates(ctx, configPath, report)
	// The report names each container the pass replaced or failed to; the
	// run's saved output shows it, and the history its summary.
	pass.Write(stdout)
	// A container that failed raised its own alert and is in the report:
	// the pass did its work, so the unit does not fail for it. Only what
	// has no alert of its own fails the run, and the end hook reports it.
	unreported := scheduledrun.Unreported(err)
	if unreported != nil {
		fmt.Fprintf(stderr, "linuxio-docker-update: run failed: %v\n", unreported)
	}
	// The zero summary of a pass that checked nothing is not stored.
	summary, _ := pass.Summary()
	return scheduledrun.Conclude(ctx, summarize, summary, unreported)
}

func parseRunArgs(args []string, stderr io.Writer) (string, bool) {
	configPath := dockerupdate.ConfigPath
	for i := 0; i < len(args); i++ {
		if args[i] != "--config" {
			fmt.Fprintf(stderr, "linuxio-docker-update: unknown argument %q\n", args[i])
			return "", false
		}
		if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
			fmt.Fprintln(stderr, "linuxio-docker-update: --config requires a path")
			return "", false
		}
		configPath = args[i+1]
		i++
	}
	return configPath, true
}

func writeUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: linuxio-docker-update [--config PATH]")
}

func writeHelp(w io.Writer) {
	fmt.Fprintln(w, "LinuxIO Docker updater: runs one configured Docker update pass.")
	fmt.Fprintln(w, "It is started by systemd, not by hand.")
	fmt.Fprintln(w)
	writeUsage(w)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Options:")
	fmt.Fprintln(w, "  --config PATH  Use an alternate update configuration file")
}
