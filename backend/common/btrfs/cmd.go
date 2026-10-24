// Package btrfs wraps the btrfs-progs command line and btrfs mount discovery.
package btrfs

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/common/hostcmd"
)

// execCommand is swapped by tests so no real command runs.
var execCommand = exec.CommandContext

// searchDirs is the fallback list findCommand uses after PATH; a var so tests
// can add fake executables.
var searchDirs = hostcmd.SearchDirs

func findCommand(name string) (string, error) {
	return hostcmd.LookPathIn(name, searchDirs)
}

// run executes btrfs with args and returns trimmed stdout.
func run(ctx context.Context, args ...string) (string, error) {
	return runCommand(ctx, "btrfs", args...)
}

// runBtrfs is a variable so tests can stub the btrfs command.
var runBtrfs = run

func runCommand(ctx context.Context, name string, args ...string) (string, error) {
	return runCommandInput(ctx, "", name, args...)
}

// runCommandInput is runCommand with input fed to stdin when non-empty.
func runCommandInput(ctx context.Context, input string, name string, args ...string) (string, error) {
	path, err := findCommand(name)
	if err != nil {
		return "", fmt.Errorf("%s: %w", name, err)
	}
	var stdout, stderr bytes.Buffer
	cmd := execCommand(ctx, path, args...)
	// A fixed locale keeps error text such as "target is busy" matchable.
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	if input != "" {
		cmd.Stdin = strings.NewReader(input)
	}
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		label := strings.Join(append([]string{name}, args[:min(len(args), 2)]...), " ")
		// stdout is returned alongside the error so callers can tell a
		// silent non-zero exit (e.g. findmnt with no match) from a failure.
		out := strings.TrimSpace(stdout.String())
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return out, fmt.Errorf("%s: %s", label, msg)
		}
		return out, fmt.Errorf("%s: %w", label, err)
	}
	return strings.TrimSpace(stdout.String()), nil
}
