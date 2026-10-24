package main

import (
	"context"
	"fmt"
	"io"

	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
)

// checkBtrfsHealth is swapped by tests.
var checkBtrfsHealth = btrfs.CheckHealth

func runHealth(ctx context.Context, args []string, stderr io.Writer) int {
	if len(args) != 1 || args[0] != "run" {
		fmt.Fprintln(stderr, "Usage: linuxio-btrfs-timer health run")
		return 2
	}
	if err := checkBtrfsHealth(ctx); err != nil {
		fmt.Fprintf(stderr, "btrfs health check failed: %v\n", err)
		return 1
	}
	return 0
}
