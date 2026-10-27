package cmd

import (
	"fmt"
	"io"
)

// runJobsCommand runs "linuxio-bridge jobs submit", the one jobs subcommand,
// and returns its exit code.
func runJobsCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "submit" {
		return runJobsSubmit(args[1:], stdout, stderr)
	}
	fmt.Fprintln(stderr, jobsSubmitUsage)
	return 2
}
