package main

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

var versionExecCommand = exec.CommandContext

const versionProbeTimeout = 5 * time.Second

var versionProbes = []struct{ binary, flag string }{
	{"linuxio-webserver", "version"},
	{"linuxio-bridge", "version"},
	{"linuxio-auth", "version"},
	{"linuxio-indexer", "--version"},
	{"linuxio-monitoring", "--version"},
}

func cliVersionLine() string {
	return fmt.Sprintf("LinuxIO CLI %s", version.Version)
}

func showVersion(ctx context.Context, args []string) {
	if len(args) == 1 && args[0] == "--self" {
		fmt.Println(cliVersionLine())
		return
	}

	fmt.Printf("\033[1mLinuxIO CLI - Manage LinuxIO services\033[0m\n")
	fmt.Println("\nInstalled components:")
	fmt.Printf("  %s\n", cliVersionLine())

	for _, c := range versionProbes {
		fmt.Printf("  %s\n", probeVersion(ctx, c.binary, c.flag))
	}
}

// probeVersion returns the first version line of binary, or a failure line if it is missing, fails or hangs.
func probeVersion(parent context.Context, binary, flag string) string {
	ctx, cancel := context.WithTimeout(parent, versionProbeTimeout)
	defer cancel()
	cmd := versionExecCommand(ctx, binary, flag)
	cmd.WaitDelay = time.Second // do not hang on a pipe inherited by a child
	out, err := cmd.CombinedOutput()
	if err != nil {
		return binary + ": not found or error"
	}
	line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	return line
}
