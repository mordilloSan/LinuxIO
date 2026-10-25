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
	if len(args) != 1 || args[0] != "run" {
		fmt.Fprintln(stderr, "Usage: linuxio-btrfs-timer health run")
		return 2
	}
	resync, err := checkBtrfsHealth(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "btrfs health check failed: %v\n", err)
		return 1
	}
	code := 0
	// A unit of its own, so a scrub that takes hours neither blocks the
	// health timer nor stops with it.
	for _, uuid := range resync {
		if err := startUnit(ctx, btrfs.ResyncUnitName(uuid)); err != nil {
			fmt.Fprintf(stderr, "start btrfs resync of %s: %v\n", uuid, err)
			code = 1
		}
	}
	return code
}
