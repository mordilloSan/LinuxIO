package watchtower

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const (
	// LockPath serializes Watchtower one-shot runs across processes. The
	// systemd timer unit takes the same lock via flock(1), so a scheduled
	// run and a bridge-triggered run can never operate concurrently.
	LockPath = "/run/linuxio-watchtower.lock"

	lockWait     = 10 * time.Second
	lockPollStep = 250 * time.Millisecond
)

// Target selects which containers a run operates on. Watchtower treats an
// empty argument list as "all containers", so the choice is explicit here:
// either All is set, or Names must be non-empty.
type Target struct {
	All   bool
	Names []string
}

// Options controls a single Watchtower one-shot run.
type Options struct {
	MonitorOnly bool
	Cleanup     bool
}

// State is a per-container session state as printed by --porcelain v1.
type State string

const (
	StateUnknown   State = "Unknown"
	StateSkipped   State = "Skipped"
	StateScanned   State = "Scanned"
	StateUpdated   State = "Updated"
	StateFailed    State = "Failed"
	StateFresh     State = "Fresh"
	StateStale     State = "Stale"
	StateRestarted State = "Restarted"
)

// Result is one container's outcome from a Watchtower run.
type Result struct {
	Name  string
	Image string
	State State
	Err   string
}

// Run executes a Watchtower one-shot and returns the per-container results
// parsed from its porcelain output.
func Run(ctx context.Context, target Target, opts Options) ([]Result, error) {
	args, err := runArgs(target, opts)
	if err != nil {
		return nil, err
	}

	binary := BinaryPath()
	if _, statErr := os.Stat(binary); statErr != nil {
		if os.IsNotExist(statErr) {
			return nil, fmt.Errorf("%s is not installed", BinaryName)
		}
		return nil, fmt.Errorf("stat %s: %w", binary, statErr)
	}

	release, err := acquireLock(ctx, LockPath)
	if err != nil {
		return nil, err
	}
	defer release()

	cmd := exec.CommandContext(ctx, binary, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if runErr := cmd.Run(); runErr != nil {
		return nil, fmt.Errorf("run %s: %w: %s", BinaryName, runErr, lastLine(stderr.String()))
	}
	return ParsePorcelain(stdout.String()), nil
}

func runArgs(target Target, opts Options) ([]string, error) {
	names := make([]string, 0, len(target.Names))
	for _, name := range target.Names {
		name = strings.TrimPrefix(strings.TrimSpace(name), "/")
		if name == "" {
			continue
		}
		names = append(names, QuoteName(name))
	}
	if target.All && len(names) > 0 {
		return nil, errors.New("watchtower target cannot combine All with container names")
	}
	if !target.All && len(names) == 0 {
		return nil, errors.New("no containers selected for watchtower run")
	}

	args := []string{"--run-once", "--porcelain", "v1"}
	if opts.MonitorOnly {
		args = append(args, "--monitor-only")
	}
	if opts.Cleanup {
		args = append(args, "--cleanup")
	}
	return append(args, names...), nil
}

// QuoteName escapes a container name for use as a Watchtower positional
// argument. Watchtower falls back to anchored regex matching when the exact
// name comparison misses, so "app.service" would otherwise also match
// "appXservice". Anything writing LINUXIO_WATCHTOWER_CONTAINERS must apply
// the same quoting, doubling backslashes for systemd EnvironmentFile parsing.
func QuoteName(name string) string {
	return regexp.QuoteMeta(name)
}

// acquireLock takes an exclusive flock on path, polling until lockWait
// elapses. The returned release function unlocks and closes the file.
func acquireLock(ctx context.Context, path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}

	deadline := time.Now().Add(lockWait)
	for {
		err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return func() {
				_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
				_ = f.Close()
			}, nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) {
			_ = f.Close()
			return nil, fmt.Errorf("lock %s: %w", path, err)
		}
		if time.Now().After(deadline) {
			_ = f.Close()
			return nil, errors.New("another Watchtower run is already in progress")
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, ctx.Err()
		case <-time.After(lockPollStep):
		}
	}
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for _, line := range slices.Backward(lines) {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return "no error output"
}
