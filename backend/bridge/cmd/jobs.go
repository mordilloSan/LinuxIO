package cmd

import (
	"fmt"
	"io"
	"os"
)

const jobsUsage = "usage: linuxio-bridge jobs host|launch|submit|reconcile"

// runJobsCommand runs a "linuxio-bridge jobs" subcommand and returns its exit
// code. stdin and stdout are for the subcommands that talk over them: host
// and reconcile use neither, launch needs stdin to be the accepted socket
// (an *os.File), and submit prints to stdout.
func runJobsCommand(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "host":
			return runJobsHost(stderr)
		case "launch":
			f, ok := stdin.(*os.File)
			if !ok {
				fmt.Fprintln(stderr, "linuxio-bridge jobs launch: stdin must be the accepted socket")
				return 2
			}
			return runJobsLaunch(f, stderr)
		case "submit":
			return runJobsSubmit(args[1:], stdout, stderr)
		case "reconcile":
			return runJobsReconcile(stderr)
		}
	}
	fmt.Fprintln(stderr, jobsUsage)
	return 2
}
