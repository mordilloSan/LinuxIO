package main

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
	"github.com/mordilloSan/LinuxIO/backend/common/scheduledrun"
)

// mountDegradedPools, resyncPool and rebuildPool are swapped by tests.
var (
	mountDegradedPools = btrfs.MountDegradedPools
	resyncPool         = btrfs.Resync
	rebuildPool        = btrfs.Rebuild
)

// healthUnit is the unit of the btrfs health check, which a rebuild that
// ended well starts so the pool's resync starts at once.
const healthUnit = "linuxio-btrfs-health.service"

func runDegraded(ctx context.Context, args []string, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprintln(stderr, "Usage: linuxio-worker degraded")
		return 2
	}
	if err := mountDegradedPools(ctx, btrfs.DegradedGrace); err != nil {
		fmt.Fprintf(stderr, "btrfs boot check of pools held back at boot failed: %v\n", err)
		return 1
	}
	return 0
}

func runResync(ctx context.Context, args []string, stderr io.Writer) int {
	if len(args) != 1 || args[0] == "" {
		fmt.Fprintln(stderr, "Usage: linuxio-worker resync <uuid>")
		return 2
	}
	err := resyncPool(ctx, args[0], func(percent float64) {
		reportProgress(int(percent), "Scrubbing")
	})
	if err == nil {
		return scheduledrun.Conclude(ctx, summarize, scheduledrun.Summary{Message: "Resynced"}, nil)
	}
	fmt.Fprintf(stderr, "btrfs resync of %s failed: %v\n", args[0], err)
	// The pool's own alerts report a resync that failed (it is retried and
	// gives up with an alert; a disk that dropped out again is a health
	// alert), so the run completes and its summary says it failed: neither
	// the unit nor the run is marked failed, and the end hook raises no run
	// alert. One that left those alerts as they were fails the run and gets
	// the run alert.
	var summary scheduledrun.Summary
	if scheduledrun.AllReported(err) {
		summary = scheduledrun.Summary{Message: "Resync failed", Failures: 1}
	}
	return scheduledrun.Conclude(ctx, summarize, summary, scheduledrun.Unreported(err))
}

func runRebuild(ctx context.Context, args []string, stderr io.Writer) int {
	if len(args) != 1 || args[0] == "" {
		fmt.Fprintln(stderr, "Usage: linuxio-worker rebuild <uuid>")
		return 2
	}
	err := rebuildPool(ctx, args[0], func(percent float64) {
		reportProgress(int(percent), "Rebuilding")
	})
	switch {
	case err == nil:
		// The resync scrubs and converts what was written while the disk was
		// missing; the health check decides and starts it.
		if startErr := startUnit(ctx, healthUnit); startErr != nil {
			fmt.Fprintf(stderr, "start the btrfs health check after the rebuild of %s: %v\n", args[0], startErr)
		}
		return scheduledrun.Conclude(ctx, summarize, scheduledrun.Summary{Message: "Rebuilt"}, nil)
	case errors.Is(err, btrfs.ErrNothingToRebuild) && ctx.Err() == nil:
		fmt.Fprintf(stderr, "btrfs rebuild of %s: %v\n", args[0], err)
		return scheduledrun.Conclude(ctx, summarize, scheduledrun.Summary{Message: "Nothing to do", Skipped: true}, nil)
	}
	fmt.Fprintf(stderr, "btrfs rebuild of %s failed: %v\n", args[0], err)
	// The pool's rebuild alert reports a refused or failed rebuild, which
	// waits for a person, so the run completes and its summary says it
	// failed; one that left the alert as it was fails the run and gets the
	// run alert.
	var summary scheduledrun.Summary
	if scheduledrun.AllReported(err) {
		summary = scheduledrun.Summary{Message: "Rebuild failed", Failures: 1}
	}
	return scheduledrun.Conclude(ctx, summarize, summary, scheduledrun.Unreported(err))
}
