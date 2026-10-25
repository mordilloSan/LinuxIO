// Command linuxio-btrfs-timer runs the btrfs snapshot, health, boot-time
// degraded mount and resync jobs that the systemd units and the auto-update
// drop-in start.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
)

const usage = `Usage: linuxio-btrfs-timer snapshot run [--now RFC3339]
       linuxio-btrfs-timer snapshot pre-update
       linuxio-btrfs-timer health run
       linuxio-btrfs-timer degraded run
       linuxio-btrfs-timer resync <uuid>`

func main() {
	os.Exit(run(os.Args))
}

func run(args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runCLI(ctx, args[1:], os.Stdout, os.Stderr)
}

func runCLI(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	switch args[0] {
	case "snapshot":
		return runSnapshot(ctx, args[1:], stdout, stderr)
	case "health":
		return runHealth(ctx, args[1:], stderr)
	case "degraded":
		return runDegraded(ctx, args[1:], stderr)
	case "resync":
		return runResync(ctx, args[1:], stderr)
	case "help", "-h", "--help":
		fmt.Fprintln(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "linuxio-btrfs-timer: unknown command %q\n%s\n", args[0], usage)
		return 2
	}
}
