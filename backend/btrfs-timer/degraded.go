package main

import (
	"context"
	"fmt"
	"io"

	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
	"github.com/mordilloSan/LinuxIO/backend/common/scheduledrun"
)

// mountDegradedPools and resyncPool are swapped by tests.
var (
	mountDegradedPools = btrfs.MountDegradedPools
	resyncPool         = btrfs.Resync
)

func runDegraded(ctx context.Context, args []string, stderr io.Writer) int {
	if len(args) != 1 || args[0] != "run" {
		fmt.Fprintln(stderr, "Usage: linuxio-btrfs-timer degraded run")
		return 2
	}
	if err := mountDegradedPools(ctx, btrfs.DegradedGrace); err != nil {
		fmt.Fprintf(stderr, "btrfs mount of pools missing disks or held back by a device replace failed: %v\n", err)
		return 1
	}
	return 0
}

func runResync(ctx context.Context, args []string, stderr io.Writer) int {
	if len(args) != 1 || args[0] == "" {
		fmt.Fprintln(stderr, "Usage: linuxio-btrfs-timer resync <uuid>")
		return 2
	}
	err := resyncPool(ctx, args[0], func(percent float64) {
		reportProgress(int(percent), "Scrubbing")
	})
	if err != nil {
		fmt.Fprintf(stderr, "btrfs resync of %s failed: %v\n", args[0], err)
		// The pool's own alerts report a resync that failed (it is retried
		// and gives up with an alert; a disk that dropped out again is a
		// health alert), so the unit's end hook raises no run alert for it.
		// One that left those alerts as they were gets the run alert.
		return scheduledrun.ExitStatus(err)
	}
	return 0
}
