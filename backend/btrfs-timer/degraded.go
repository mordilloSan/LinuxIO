package main

import (
	"context"
	"fmt"
	"io"

	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
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
		fmt.Fprintf(stderr, "btrfs degraded mount failed: %v\n", err)
		return 1
	}
	return 0
}

func runResync(ctx context.Context, args []string, stderr io.Writer) int {
	if len(args) != 1 || args[0] == "" {
		fmt.Fprintln(stderr, "Usage: linuxio-btrfs-timer resync <uuid>")
		return 2
	}
	if err := resyncPool(ctx, args[0]); err != nil {
		fmt.Fprintf(stderr, "btrfs resync of %s failed: %v\n", args[0], err)
		return 1
	}
	return 0
}
