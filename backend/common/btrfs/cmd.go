// Package btrfs wraps the btrfs-progs command line and btrfs mount discovery.
package btrfs

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// execCommand is swapped by tests so no real command runs.
var execCommand = exec.CommandContext

// searchDirs are consulted when a command is not on PATH; btrfs and mount
// usually live in /usr/sbin or /sbin, which the service environment may omit.
var searchDirs = []string{"/usr/sbin", "/sbin", "/usr/bin", "/bin"}

func findCommand(name string) (string, error) {
	if path, err := exec.LookPath(name); err == nil {
		return path, nil
	}
	for _, dir := range searchDirs {
		path := filepath.Join(dir, name)
		if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return path, nil
		}
	}
	return "", exec.ErrNotFound
}

// run executes btrfs with args and returns trimmed stdout.
func run(ctx context.Context, args ...string) (string, error) {
	return runCommand(ctx, "btrfs", args...)
}

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
