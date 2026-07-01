package cmd

import (
	"fmt"
	"os"

	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

// handleBridgeArgs handles informational CLI invocations such as version
// output. A real bridge process is normally exec'd by the auth daemon.
func handleBridgeArgs(args []string) bool {
	if len(args) > 1 {
		switch args[1] {
		case "version", "--version", "-v":
			printBridgeVersion()
		default:
			printBridgeVersion()
			fmt.Println("(to be spawned by auth daemon, not for direct use)")
		}
		return true
	}
	return false
}

// isDirectBridgeInvocation detects a user launching the bridge directly from a
// terminal instead of through the auth daemon bootstrap pipe.
func isDirectBridgeInvocation() bool {
	fileInfo, err := os.Stdin.Stat()
	return err != nil || (fileInfo.Mode()&os.ModeCharDevice) != 0
}

// printBridgeVersion writes the bridge binary version for diagnostics.
func printBridgeVersion() {
	fmt.Printf("LinuxIO Bridge %s\n", version.Version)
}
