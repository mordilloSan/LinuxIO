package cmd

import (
	"fmt"
	"io"
)

const jobsUsage = "usage: linuxio-bridge jobs host|submit|reconcile"

// runJobsCommand runs a "linuxio-bridge jobs" subcommand and returns its exit
// code. host and reconcile are the entry points of their systemd units and
// use neither stdin nor stdout; submit prints to stdout.
func runJobsCommand(args []string, _ io.Reader, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "host":
			return runJobsHost(stderr)
		case "submit":
			return runJobsSubmit(args[1:], stdout, stderr)
		case "reconcile":
			return runJobsReconcile(stderr)
		}
	}
	fmt.Fprintln(stderr, jobsUsage)
	return 2
}
