package main

import (
	"context"
	"fmt"
	"os"

	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
)

const btrfsHealthUsage = "Usage: linuxio btrfs-health run"

// checkBtrfsHealth is swapped by tests.
var checkBtrfsHealth = btrfs.CheckHealth

func runBtrfsHealth(ctx context.Context, args []string) int {
	if len(args) != 1 || args[0] != "run" {
		fmt.Fprintln(os.Stderr, btrfsHealthUsage)
		return 2
	}
	if err := checkBtrfsHealth(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "btrfs health check failed: %v\n", err)
		return 1
	}
	return 0
}
