package main

import (
	"context"
	"fmt"
	"io"

	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
	"github.com/mordilloSan/LinuxIO/backend/common/systemd"
)

// checkBtrfsHealth, startUnit and narrowSnapshotDirs are swapped by tests.
var (
	checkBtrfsHealth   = btrfs.CheckHealth
	startUnit          = systemd.StartUnit
	narrowSnapshotDirs = btrfs.NarrowSnapshotDirs
)

func runHealth(ctx context.Context, args []string, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprintln(stderr, "Usage: linuxio-worker health")
		return 2
	}
	actions, err := checkBtrfsHealth(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "btrfs health check failed: %v\n", err)
		return 1
	}
	// Snapshot folders an earlier version, or a run under an open umask,
	// left readable by everyone are made root's alone here, so one whose
	// source takes no snapshot any more does not wait for a snapshot that
	// never comes (docs/btrfs.md). A pool that refuses is named and does not
	// fail the pass, whose job is the pools' health.
	if err := narrowSnapshotDirs(ctx); err != nil {
		fmt.Fprintf(stderr, "narrow the snapshot folders: %v\n", err)
	}
	code := 0
	// Units of their own, so a scrub or a replace that takes hours neither
	// blocks the health timer nor stops with it.
	for _, uuid := range actions.Resync {
		if err := startUnit(ctx, btrfs.ResyncUnitName(uuid)); err != nil {
			fmt.Fprintf(stderr, "start btrfs resync of %s: %v\n", uuid, err)
			code = 1
		}
	}
	for _, uuid := range actions.Rebuild {
		if err := startUnit(ctx, btrfs.RebuildUnitName(uuid)); err != nil {
			fmt.Fprintf(stderr, "start btrfs rebuild of %s: %v\n", uuid, err)
			code = 1
		}
	}
	return code
}
