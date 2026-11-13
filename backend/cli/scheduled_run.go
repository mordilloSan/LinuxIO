package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/scheduledrun"
)

const (
	scheduledRunUsage = "Usage: linuxio scheduled-run begin|end --unit <unit>"

	// Below TimeoutStopSec=15s of the scheduled units, so a slow store never
	// holds a unit in its stopping state. The alert end raises last is not
	// bound by it, and its delivery may outlast it.
	scheduledRunTimeout = 10 * time.Second
)

// runScheduledRun is the systemd hook of every scheduled unit: begin from
// ExecStartPre, end from ExecStopPost. It reads only --unit and the
// environment systemd gives the hook. The hooks start with "-", so a failure
// here is printed and never stops the run.
func runScheduledRun(ctx context.Context, args []string, getenv func(string) string, hooks scheduledrun.Hooks, stderr io.Writer) int {
	if len(args) == 0 || (args[0] != "begin" && args[0] != "end") {
		fmt.Fprintln(stderr, scheduledRunUsage)
		return 2
	}
	verb := args[0]

	fs := flag.NewFlagSet("linuxio scheduled-run "+verb, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	unit := fs.String("unit", "", "scheduled unit (required)")
	if err := fs.Parse(args[1:]); err != nil || *unit == "" || fs.NArg() != 0 {
		fmt.Fprintln(stderr, scheduledRunUsage)
		return 2
	}
	invocationID := getenv("INVOCATION_ID")
	if invocationID == "" {
		fmt.Fprintln(stderr, "Failed to record scheduled run: INVOCATION_ID is not set")
		return 1
	}

	ctx, cancel := context.WithTimeout(ctx, scheduledRunTimeout)
	defer cancel()
	var err error
	if verb == "begin" {
		err = hooks.Begin(ctx, *unit, invocationID)
	} else {
		err = hooks.End(ctx, *unit, invocationID, scheduledrun.Result{
			Service: getenv("SERVICE_RESULT"),
			Code:    getenv("EXIT_CODE"),
			Status:  getenv("EXIT_STATUS"),
		})
	}
	if err != nil {
		fmt.Fprintf(stderr, "Failed to record scheduled run: %v\n", err)
		if errors.Is(err, os.ErrPermission) {
			fmt.Fprintln(stderr, "This command requires sudo")
		}
		return 1
	}
	return 0
}
