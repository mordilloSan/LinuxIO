package main

import (
	"context"
	"fmt"
	"io"

	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
	"github.com/mordilloSan/LinuxIO/backend/common/systemd"
)

// checkBtrfsHealth and startUnit are swapped by tests.
var (
	checkBtrfsHealth = btrfs.CheckHealth
	startUnit        = systemd.StartUnit
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
