package cmd

import (
	"fmt"
	"os"

	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

// program is what the bridge answers to --help and --version.
var program = version.Program{
	Binary:    "linuxio-bridge",
	About:     "the LinuxIO session process of one signed-in user, and their job host",
	StartedBy: "linuxio-auth after sign-in, and by systemd as the job host (linuxio-jobs@.service)",
	Usage:     []string{"linuxio-bridge"},
}

// handleBridgeArgs handles informational and invalid CLI invocations. A bridge is exec'd by the auth daemon without arguments, and
// systemd starts the job host without arguments too.
func handleBridgeArgs(args []string) (bool, int) {
	if len(args) <= 1 {
		return false, 0
	}
	if program.Answer(args[1:], os.Stdout) {
		return true, 0
	}
	fmt.Fprintf(os.Stderr, "linuxio-bridge: unknown argument: %q\n", args[1])
	return true, 2
}

// isDirectBridgeInvocation detects a user launching the bridge directly from a
// terminal instead of through the auth daemon bootstrap pipe.
func isDirectBridgeInvocation(stdin *os.File) (bool, error) {
	fileInfo, err := stdin.Stat()
	if err != nil {
		return false, fmt.Errorf("inspect bridge stdin: %w", err)
	}
	return (fileInfo.Mode() & os.ModeCharDevice) != 0, nil
}
