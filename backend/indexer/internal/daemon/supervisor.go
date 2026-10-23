package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// Worker supervision: full scans run in a private subprocess of this binary
// (--index-mode), so a crash or memory spike in the scan cannot take the
// daemon down. The worker's side of the protocol is in wire.go and fullscan.go.

// runIndexSubprocess spawns the current binary with --index-mode flag.
// Wire events decoded from the subprocess stdout stream to onEvent (may be
// nil); the returned stats come from the subprocess's summary event, nil when
// it never emitted one.
func (d *daemon) runIndexSubprocess(ctx context.Context, onEvent func(indexWireEvent)) (*IndexRunStats, error) {
	slog.Info("starting index process")
	stats, err := runIndexProcess(exec.CommandContext(ctx, os.Args[0], "--index-mode", "--config-file", d.settings.path), onEvent)
	if err != nil {
		return nil, err
	}

	slog.Info("index process completed successfully")
	return stats, nil
}

// runIndexProcess runs a prepared index-mode command, decoding wire events
// from its stdout as they arrive. Each event is handed to onEvent (which may
// be nil); the final summary, when the subprocess emitted one, is also
// returned as run statistics.
func runIndexProcess(cmd *exec.Cmd, onEvent func(indexWireEvent)) (*IndexRunStats, error) {
	cmd.Stderr = os.Stderr
	cancel := func() error {
		if cmd.Process == nil {
			return nil
		}
		return cmd.Process.Signal(syscall.SIGTERM)
	}
	if cmd.Cancel != nil {
		cmd.Cancel = cancel
	}
	if cmd.WaitDelay == 0 {
		cmd.WaitDelay = workerKillDelay
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("index subprocess stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("index subprocess start: %w", err)
	}

	summary, decodeErr := decodeIndexProcessOutput(stdout, onEvent)
	var killTimer *time.Timer
	if decodeErr != nil {
		_ = cancel()
		// A manual Cancel does not start exec.Cmd's WaitDelay clock. Force a
		// kill after the same bound so malformed output cannot leave Wait
		// blocked on a child that ignores SIGTERM or remains stuck writing.
		killTimer = time.AfterFunc(cmd.WaitDelay, func() {
			_ = cmd.Process.Kill()
		})
	}
	waitErr := cmd.Wait()
	if killTimer != nil {
		killTimer.Stop()
	}
	if decodeErr != nil {
		if waitErr != nil {
			return nil, fmt.Errorf("%w (wait: %v)", decodeErr, waitErr)
		}
		return nil, decodeErr
	}
	if waitErr != nil {
		return nil, fmt.Errorf("index subprocess failed: %w", waitErr)
	}
	return summary, nil
}
