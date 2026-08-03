package cmd

import (
	"fmt"
	"os"

	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

// handleBridgeArgs handles informational and invalid CLI invocations. A real
// bridge process is normally exec'd by the auth daemon without arguments.
func handleBridgeArgs(args []string) (bool, int) {
	if len(args) <= 1 {
		return false, 0
	}
	switch args[1] {
	case "version", "--version", "-v":
		printBridgeVersion()
		return true, 0
	default:
		fmt.Fprintf(os.Stderr, "unknown argument: %q\n", args[1])
		return true, 2
	}
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

// printBridgeVersion writes the bridge binary version for diagnostics.
func printBridgeVersion() {
	fmt.Printf("LinuxIO Bridge %s\n", version.Version)
}
